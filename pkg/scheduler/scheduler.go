// Package scheduler runs persisted recurring jobs.
package scheduler

import (
	"beneburg/pkg/database"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

const (
	defaultPollInterval    = time.Minute
	defaultRetentionMonths = 2
	claimBatchSize         = 32

	cleanupJobName        = "system.cleanup_job_runs"
	cleanupJobType        = "cleanup_job_runs"
	cleanupCronExpression = "17 3 * * *"
	defaultTimezone       = "Europe/Moscow"
)

// Config configures scheduler polling and retention.
type Config struct {
	PollInterval    time.Duration
	RetentionMonths int
}

// Handler performs one claimed job run.
type Handler func(context.Context, database.ScheduledJob, database.JobRun) error

type definition struct {
	name           string
	jobType        string
	cronExpression string
	timezone       string
	payload        json.RawMessage
	handler        Handler
}

// Scheduler owns persisted job registration and execution.
type Scheduler struct {
	store       database.ScheduledJobStore
	config      Config
	clock       func() time.Time
	logger      *zap.Logger
	definitions []definition
	handlers    map[string]Handler
	waitGroup   sync.WaitGroup
}

// New creates a scheduler. Start must be called before it processes work.
func New(store database.ScheduledJobStore, config Config, logger *zap.Logger) (*Scheduler, error) {
	if store == nil {
		return nil, fmt.Errorf("scheduled job store is required")
	}
	if config.PollInterval == 0 {
		config.PollInterval = defaultPollInterval
	}
	if config.PollInterval < 0 {
		return nil, fmt.Errorf("scheduler poll interval must be positive")
	}
	if config.RetentionMonths == 0 {
		config.RetentionMonths = defaultRetentionMonths
	}
	if config.RetentionMonths < 0 {
		return nil, fmt.Errorf("job run retention months must be positive")
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	scheduler := &Scheduler{
		store:    store,
		config:   config,
		clock:    func() time.Time { return time.Now().UTC() },
		logger:   logger,
		handlers: make(map[string]Handler),
	}
	if err := scheduler.Register(cleanupJobName, cleanupJobType, cleanupCronExpression, defaultTimezone, json.RawMessage(`{}`), scheduler.cleanupJobRuns); err != nil {
		return nil, err
	}
	return scheduler, nil
}

// Register adds a job type and creates its schedule when absent. Calling it
// after Start is unsupported because registrations must be persisted first.
func (s *Scheduler) Register(name, jobType, cronExpression, timezone string, payload json.RawMessage, handler Handler) error {
	if name == "" || jobType == "" {
		return fmt.Errorf("scheduled job name and type are required")
	}
	if handler == nil {
		return fmt.Errorf("scheduled job handler is required")
	}
	s.definitions = append(s.definitions, definition{name, jobType, cronExpression, timezone, payload, handler})
	s.handlers[jobType] = handler
	return nil
}

// Start persists built-in schedules, then processes due work until ctx is done.
func (s *Scheduler) Start(ctx context.Context) error {
	for _, definition := range s.definitions {
		nextRunAt, err := nextOccurrence(definition.cronExpression, definition.timezone, s.clock())
		if err != nil {
			return fmt.Errorf("validate scheduled job %q: %w", definition.name, err)
		}
		if err := s.store.EnsureScheduledJob(ctx, database.ScheduledJob{
			Name: definition.name, JobType: definition.jobType, CronExpression: definition.cronExpression,
			Timezone: definition.timezone, Payload: definition.payload, Enabled: true, NextRunAt: nextRunAt,
		}); err != nil {
			return err
		}
	}
	s.waitGroup.Add(1)
	go func() {
		defer s.waitGroup.Done()
		s.run(ctx)
	}()
	return nil
}

// Stop waits for the polling goroutine after its context was cancelled.
func (s *Scheduler) Stop() {
	s.waitGroup.Wait()
}

func (s *Scheduler) run(ctx context.Context) {
	s.processDueJobs(ctx)
	ticker := time.NewTicker(s.config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.processDueJobs(ctx)
		}
	}
}

func (s *Scheduler) processDueJobs(ctx context.Context) {
	now := s.clock()
	runs, jobs, err := s.store.ClaimDueScheduledJobs(ctx, now, claimBatchSize, func(job database.ScheduledJob) (time.Time, error) {
		return nextOccurrence(job.CronExpression, job.Timezone, job.NextRunAt)
	})
	if err != nil {
		s.logger.Error("claim scheduled jobs", zap.Error(err))
		return
	}
	for index, run := range runs {
		job := jobs[index]
		handler, exists := s.handlers[job.JobType]
		var runError error
		if !exists {
			runError = fmt.Errorf("no handler registered for scheduled job type %q", job.JobType)
		} else {
			runError = handler(ctx, job, run)
		}
		status := "succeeded"
		errorText := ""
		if runError != nil {
			status = "failed"
			errorText = runError.Error()
			s.logger.Error("scheduled job failed", zap.String("job", job.Name), zap.Int64("run_id", run.ID), zap.Error(runError))
		}
		if err := s.store.FinishJobRun(ctx, run.ID, status, errorText); err != nil {
			s.logger.Error("finish scheduled job run", zap.Int64("run_id", run.ID), zap.Error(err))
		}
	}
}

func (s *Scheduler) cleanupJobRuns(ctx context.Context, _ database.ScheduledJob, _ database.JobRun) error {
	cutoff := s.clock().AddDate(0, -s.config.RetentionMonths, 0)
	deleted, err := s.store.DeleteFinishedJobRunsBefore(ctx, cutoff)
	if err == nil {
		s.logger.Info("deleted expired scheduled job runs", zap.Int64("count", deleted), zap.Time("cutoff", cutoff))
	}
	return err
}

func nextOccurrence(expression, timezone string, after time.Time) (time.Time, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("load timezone %q: %w", timezone, err)
	}
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse(expression)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse cron expression %q: %w", expression, err)
	}
	return schedule.Next(after.In(location)).UTC(), nil
}
