package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// ScheduledJob is a persisted recurring job definition.
type ScheduledJob struct {
	ID             int64
	Name           string
	JobType        string
	CronExpression string
	Timezone       string
	Payload        json.RawMessage
	Enabled        bool
	NextRunAt      time.Time
}

// JobRun records one scheduled execution.
type JobRun struct {
	ID             int64
	ScheduledJobID int64
	ScheduledFor   time.Time
	Status         string
	Attempts       int
	StartedAt      time.Time
}

// ScheduledJobStore is the storage contract required by the scheduler.
// It is intentionally separate from Database so existing application callers
// and generated mocks do not need scheduler-specific methods.
type ScheduledJobStore interface {
	EnsureScheduledJob(context.Context, ScheduledJob) error
	ClaimDueScheduledJobs(context.Context, time.Time, int, func(ScheduledJob) (time.Time, error)) ([]JobRun, []ScheduledJob, error)
	FinishJobRun(context.Context, int64, string, string) error
	DeleteFinishedJobRunsBefore(context.Context, time.Time) (int64, error)
}

var _ ScheduledJobStore = (*database)(nil)

func (d *database) EnsureScheduledJob(ctx context.Context, job ScheduledJob) error {
	query := `
INSERT INTO scheduled_jobs (name, job_type, cron_expression, timezone, payload, enabled, next_run_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (name) DO NOTHING`
	_, err := d.db.ExecContext(ctx, query, job.Name, job.JobType, job.CronExpression, job.Timezone, []byte(job.Payload), job.Enabled, job.NextRunAt)
	if err != nil {
		return fmt.Errorf("ensure scheduled job %q: %w", job.Name, err)
	}
	return nil
}

func (d *database) ClaimDueScheduledJobs(ctx context.Context, now time.Time, limit int, nextRun func(ScheduledJob) (time.Time, error)) ([]JobRun, []ScheduledJob, error) {
	transaction, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("start scheduled job claim: %w", err)
	}
	defer transaction.Rollback()

	rows, err := transaction.QueryContext(ctx, `
SELECT id, name, job_type, cron_expression, timezone, payload, enabled, next_run_at
FROM scheduled_jobs
WHERE enabled AND next_run_at <= $1
ORDER BY next_run_at, id
FOR UPDATE SKIP LOCKED
LIMIT $2`, now, limit)
	if err != nil {
		return nil, nil, fmt.Errorf("select due scheduled jobs: %w", err)
	}
	defer rows.Close()

	var jobs []ScheduledJob
	for rows.Next() {
		var job ScheduledJob
		if err := rows.Scan(&job.ID, &job.Name, &job.JobType, &job.CronExpression, &job.Timezone, &job.Payload, &job.Enabled, &job.NextRunAt); err != nil {
			return nil, nil, fmt.Errorf("scan due scheduled job: %w", err)
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate due scheduled jobs: %w", err)
	}

	runs := make([]JobRun, 0, len(jobs))
	for _, job := range jobs {
		followingRunAt, err := nextRun(job)
		if err != nil {
			return nil, nil, fmt.Errorf("calculate next run for %q: %w", job.Name, err)
		}
		if !followingRunAt.After(job.NextRunAt) {
			return nil, nil, fmt.Errorf("next run for %q must be after current run", job.Name)
		}
		var run JobRun
		err = transaction.QueryRowContext(ctx, `
INSERT INTO job_runs (scheduled_job_id, scheduled_for, status)
VALUES ($1, $2, 'running')
RETURNING id, scheduled_job_id, scheduled_for, status, attempts, started_at`, job.ID, job.NextRunAt).Scan(
			&run.ID, &run.ScheduledJobID, &run.ScheduledFor, &run.Status, &run.Attempts, &run.StartedAt,
		)
		if err != nil {
			return nil, nil, fmt.Errorf("create run for %q: %w", job.Name, err)
		}
		if _, err := transaction.ExecContext(ctx, `UPDATE scheduled_jobs SET next_run_at = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`, followingRunAt, job.ID); err != nil {
			return nil, nil, fmt.Errorf("advance scheduled job %q: %w", job.Name, err)
		}
		runs = append(runs, run)
	}
	if err := transaction.Commit(); err != nil {
		return nil, nil, fmt.Errorf("commit scheduled job claim: %w", err)
	}
	return runs, jobs, nil
}

func (d *database) FinishJobRun(ctx context.Context, runID int64, status, errorText string) error {
	if status != "succeeded" && status != "failed" {
		return fmt.Errorf("unsupported job run status %q", status)
	}
	result, err := d.db.ExecContext(ctx, `
UPDATE job_runs
SET status = $1, error_text = $2, finished_at = CURRENT_TIMESTAMP
WHERE id = $3 AND status = 'running'`, status, nullableString(errorText), runID)
	if err != nil {
		return fmt.Errorf("finish job run %d: %w", runID, err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read finished job run result: %w", err)
	}
	if updated != 1 {
		return fmt.Errorf("job run %d is not running", runID)
	}
	return nil
}

func (d *database) DeleteFinishedJobRunsBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	result, err := d.db.ExecContext(ctx, `
DELETE FROM job_runs
WHERE status IN ('succeeded', 'failed') AND finished_at < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete expired job runs: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read deleted job runs result: %w", err)
	}
	return deleted, nil
}

func nullableString(value string) sql.NullString {
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}
