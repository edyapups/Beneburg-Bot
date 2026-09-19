//go:build integration

package database

import (
	"beneburg/pkg/database/model"
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func isolatedPostgresURL(t *testing.T) string {
	t.Helper()
	connectionURL := os.Getenv("TEST_POSTGRES_DSN")
	if connectionURL == "" {
		t.Skip("set TEST_POSTGRES_DSN to run PostgreSQL integration tests")
	}
	admin, err := sql.Open("pgx", connectionURL)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, admin.Close()) })
	schema := "test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.Exec(`CREATE SCHEMA ` + schema)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
		require.NoError(t, err)
	})
	parsed, err := url.Parse(connectionURL)
	require.NoError(t, err)
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func TestPostgresMigrateAndRepository(t *testing.T) {
	ctx := context.Background()
	store, err := NewDatabase(isolatedPostgresURL(t), nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	require.NoError(t, store.Migrate(ctx))
	require.NoError(t, store.Migrate(ctx))

	now := time.Now().UTC()
	require.NoError(t, store.EnsureScheduledJob(ctx, ScheduledJob{
		Name:           "test.cleanup",
		JobType:        "cleanup",
		CronExpression: "* * * * *",
		Timezone:       "UTC",
		Payload:        []byte(`{}`),
		Enabled:        true,
		NextRunAt:      now.Add(-time.Minute),
	}))
	runs, jobs, err := store.ClaimDueScheduledJobs(ctx, now, 10, func(job ScheduledJob) (time.Time, error) {
		return job.NextRunAt.Add(time.Minute), nil
	})
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Len(t, jobs, 1)
	require.NoError(t, store.FinishJobRun(ctx, runs[0].ID, "succeeded", ""))
	deleted, err := store.DeleteFinishedJobRunsBefore(ctx, now.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)

	user, err := store.CreateUser(ctx, &model.User{
		TelegramID: 10,
		FirstName:  "Test",
		Status:     model.UserStatusActive,
	})
	require.NoError(t, err)
	require.NotZero(t, user.ID)

	token, err := store.CreateOrProlongToken(ctx, user.TelegramID)
	require.NoError(t, err)
	byToken, err := store.GetUserByToken(ctx, token.UUID)
	require.NoError(t, err)
	require.Equal(t, user.TelegramID, byToken.TelegramID)

	form, err := store.CreateForm(ctx, &model.Form{
		UserTelegramId: user.TelegramID,
		Name:           "Test",
		BirthDate:      ptr(time.Date(2000, time.January, 2, 0, 0, 0, 0, time.UTC)),
		Status:         model.FormStatusAccepted,
	})
	require.NoError(t, err)
	require.NotZero(t, form.ID)
	actual, err := store.GetActualForm(ctx, user.TelegramID)
	require.NoError(t, err)
	require.Equal(t, form.ID, actual.ID)
	require.Equal(t, user.TelegramID, actual.User.TelegramID)
	require.Equal(t, form.BirthDate, actual.BirthDate)

	_, err = store.AcceptForm(ctx, form.ID)
	require.NoError(t, err)
}

func TestPostgresMigrationReplacesAgeWithBirthDate(t *testing.T) {
	ctx := context.Background()
	connection, err := sql.Open("pgx", isolatedPostgresURL(t))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })

	_, err = connection.ExecContext(ctx, `
CREATE TABLE schema_migrations (version BIGINT PRIMARY KEY);
CREATE TABLE forms (id BIGINT PRIMARY KEY, age INTEGER);
INSERT INTO schema_migrations(version) VALUES (2);
`)
	require.NoError(t, err)
	require.NoError(t, migratePostgres(ctx, connection))

	var ageColumnExists bool
	err = connection.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM information_schema.columns
    WHERE table_schema = current_schema() AND table_name = 'forms' AND column_name = 'age'
)
`).Scan(&ageColumnExists)
	require.NoError(t, err)
	require.False(t, ageColumnExists)

	var birthDateColumnType string
	err = connection.QueryRowContext(ctx, `
SELECT data_type
FROM information_schema.columns
WHERE table_schema = current_schema() AND table_name = 'forms' AND column_name = 'birth_date'
`).Scan(&birthDateColumnType)
	require.NoError(t, err)
	require.Equal(t, "date", birthDateColumnType)
}

func ptr[T any](value T) *T {
	return &value
}
