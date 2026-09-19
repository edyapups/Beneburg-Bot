package scheduler

import (
	"beneburg/pkg/database"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type fakeStore struct {
	ensuredJobs   []database.ScheduledJob
	runs          []database.JobRun
	jobs          []database.ScheduledJob
	finished      []finishedRun
	deletedBefore time.Time
}

type finishedRun struct {
	id        int64
	status    string
	errorText string
}

func (store *fakeStore) EnsureScheduledJob(_ context.Context, job database.ScheduledJob) error {
	store.ensuredJobs = append(store.ensuredJobs, job)
	return nil
}

func (store *fakeStore) ClaimDueScheduledJobs(_ context.Context, _ time.Time, _ int, nextRun func(database.ScheduledJob) (time.Time, error)) ([]database.JobRun, []database.ScheduledJob, error) {
	for _, job := range store.jobs {
		if _, err := nextRun(job); err != nil {
			return nil, nil, err
		}
	}
	return store.runs, store.jobs, nil
}

func (store *fakeStore) FinishJobRun(_ context.Context, runID int64, status, errorText string) error {
	store.finished = append(store.finished, finishedRun{runID, status, errorText})
	return nil
}

func (store *fakeStore) DeleteFinishedJobRunsBefore(_ context.Context, cutoff time.Time) (int64, error) {
	store.deletedBefore = cutoff
	return 3, nil
}

func TestNextOccurrenceUsesConfiguredTimezone(t *testing.T) {
	after := time.Date(2026, time.January, 10, 5, 0, 0, 0, time.UTC)
	next, err := nextOccurrence("0 9 * * *", "Europe/Moscow", after)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, time.January, 10, 6, 0, 0, 0, time.UTC), next)
}

func TestProcessDueJobsCompletesSuccessfulAndFailedRuns(t *testing.T) {
	store := &fakeStore{
		runs: []database.JobRun{{ID: 10}, {ID: 11}},
		jobs: []database.ScheduledJob{
			{ID: 1, Name: "successful", JobType: "success", CronExpression: "0 * * * *", Timezone: "UTC", NextRunAt: time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)},
			{ID: 2, Name: "failed", JobType: "failure", CronExpression: "0 * * * *", Timezone: "UTC", NextRunAt: time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)},
		},
	}
	scheduled, err := New(store, Config{}, zap.NewNop())
	require.NoError(t, err)
	scheduled.clock = func() time.Time { return time.Date(2026, 1, 1, 1, 30, 0, 0, time.UTC) }
	require.NoError(t, scheduled.Register("success", "success", "0 * * * *", "UTC", json.RawMessage(`{}`), func(context.Context, database.ScheduledJob, database.JobRun) error {
		return nil
	}))
	require.NoError(t, scheduled.Register("failure", "failure", "0 * * * *", "UTC", json.RawMessage(`{}`), func(context.Context, database.ScheduledJob, database.JobRun) error {
		return context.DeadlineExceeded
	}))

	scheduled.processDueJobs(context.Background())

	require.Equal(t, []finishedRun{
		{id: 10, status: "succeeded"},
		{id: 11, status: "failed", errorText: context.DeadlineExceeded.Error()},
	}, store.finished)
}

func TestCleanupJobRunsUsesCalendarMonths(t *testing.T) {
	store := &fakeStore{}
	scheduled, err := New(store, Config{RetentionMonths: 2}, zap.NewNop())
	require.NoError(t, err)
	scheduled.clock = func() time.Time { return time.Date(2026, time.March, 31, 10, 0, 0, 0, time.UTC) }

	require.NoError(t, scheduled.cleanupJobRuns(context.Background(), database.ScheduledJob{}, database.JobRun{}))
	require.Equal(t, time.Date(2026, time.January, 31, 10, 0, 0, 0, time.UTC), store.deletedBefore)
}

func TestConfigFromEnvironment(t *testing.T) {
	t.Setenv("SCHEDULER_POLL_INTERVAL", "15s")
	t.Setenv("JOB_RUNS_RETENTION_MONTHS", "4")
	config, err := ConfigFromEnvironment()
	require.NoError(t, err)
	require.Equal(t, 15*time.Second, config.PollInterval)
	require.Equal(t, 4, config.RetentionMonths)
}

func TestConfigFromEnvironmentRejectsInvalidRetention(t *testing.T) {
	t.Setenv("JOB_RUNS_RETENTION_MONTHS", "0")
	_, err := ConfigFromEnvironment()
	require.EqualError(t, err, "JOB_RUNS_RETENTION_MONTHS must be a positive integer")
}
