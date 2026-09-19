//go:build integration

package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"beneburg/pkg/database/model"
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
	t.Cleanup(func() { admin.Close() })
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

func createSQLiteFixture(t *testing.T) string {
	t.Helper()
	sourcePath := filepath.Join(t.TempDir(), "beneburg.db")
	connection, err := sql.Open("sqlite3", sourcePath)
	require.NoError(t, err)
	require.NoError(t, NewDatabaseWithDB(connection, nil).Migrate(context.Background()))
	timestamp := time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)
	_, err = connection.Exec(`INSERT INTO users(id, created_at, updated_at, deleted_at, telegram_id, username, first_name, last_name, status) VALUES (42,?,?,?,?,?,?,?,?)`, timestamp, timestamp, timestamp, int64(1234567890123), "original", "First", "Last", "banned")
	require.NoError(t, err)
	_, err = connection.Exec(`INSERT INTO tokens(uuid, user_telegram_id, expire_at) VALUES (?,?,?)`, "00000000-0000-0000-0000-000000000042", int64(1234567890123), timestamp.Add(24*time.Hour))
	require.NoError(t, err)
	_, err = connection.Exec(`INSERT INTO forms(id, created_at, updated_at, deleted_at, user_telegram_id, name, age, gender, about, status) VALUES (77,?,?,?,?,?,?,?,?,?)`, timestamp, timestamp, timestamp, int64(1234567890123), "Form", 27, "female", "About", "accepted")
	require.NoError(t, err)
	_, err = connection.Exec(`INSERT INTO users(id, created_at, updated_at, telegram_id, first_name) VALUES (99,?,?,?,?)`, timestamp, timestamp, 999, "Deleted ID")
	require.NoError(t, err)
	_, err = connection.Exec(`DELETE FROM users WHERE id=99`)
	require.NoError(t, err)
	_, err = connection.Exec(`INSERT INTO forms(id, created_at, updated_at, user_telegram_id, name) VALUES (200,?,?,?,?)`, timestamp, timestamp, int64(1234567890123), "Deleted ID")
	require.NoError(t, err)
	_, err = connection.Exec(`DELETE FROM forms WHERE id=200`)
	require.NoError(t, err)
	require.NoError(t, connection.Close())
	return sourcePath
}

func TestPostgresRepositoryAndImport(t *testing.T) {
	ctx := context.Background()
	postgresURL := isolatedPostgresURL(t)
	sourcePath := createSQLiteFixture(t)
	archiveDir := t.TempDir()
	first, err := ImportSQLite(ctx, postgresURL, sourcePath, archiveDir)
	require.NoError(t, err)
	require.False(t, first.AlreadyDone)
	require.Equal(t, ImportCounts{Users: 1, Tokens: 1, Forms: 1}, first.Counts)

	store, err := NewDatabase(postgresURL, nil)
	require.NoError(t, err)
	defer store.Close()
	completed, err := store.ImportCompleted(ctx)
	require.NoError(t, err)
	require.True(t, completed)
	require.NoError(t, store.Migrate(ctx))

	connection, err := sql.Open("pgx", postgresURL)
	require.NoError(t, err)
	defer connection.Close()
	var userID, telegramID int64
	var deletedAt time.Time
	var status string
	require.NoError(t, connection.QueryRow(`SELECT id, telegram_id, deleted_at, status FROM users`).Scan(&userID, &telegramID, &deletedAt, &status))
	require.EqualValues(t, 42, userID)
	require.EqualValues(t, 1234567890123, telegramID)
	require.Equal(t, "banned", status)
	require.True(t, deletedAt.Equal(time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)))
	_, err = store.GetUserByTelegramID(ctx, telegramID)
	require.True(t, errors.Is(err, sql.ErrNoRows))
	var token string
	require.NoError(t, connection.QueryRow(`SELECT uuid::text FROM tokens`).Scan(&token))
	require.Equal(t, "00000000-0000-0000-0000-000000000042", token)
	var formID int64
	require.NoError(t, connection.QueryRow(`SELECT id FROM forms`).Scan(&formID))
	require.EqualValues(t, 77, formID)
	var age int
	require.NoError(t, connection.QueryRow(`SELECT age FROM forms WHERE id=77`).Scan(&age))
	require.Equal(t, 27, age)
	_, err = store.GetFormByID(ctx, uint(formID))
	require.True(t, errors.Is(err, sql.ErrNoRows))

	newUser, err := store.CreateUser(ctx, &model.User{TelegramID: 9876543210123, FirstName: "New"})
	require.NoError(t, err)
	require.EqualValues(t, 100, newUser.ID)
	newToken, err := store.CreateOrProlongToken(ctx, newUser.TelegramID)
	require.NoError(t, err)
	_, err = store.GetUserByToken(ctx, newToken.UUID)
	require.NoError(t, err)
	newForm, err := store.CreateForm(ctx, &model.Form{UserTelegramId: newUser.TelegramID, Name: "New form"})
	require.NoError(t, err)
	require.EqualValues(t, 201, newForm.ID)
	_, err = store.GetFormByID(ctx, newForm.ID)
	require.NoError(t, err)
	_, err = store.AcceptForm(ctx, newForm.ID)
	require.NoError(t, err)

	second, err := ImportSQLite(ctx, postgresURL, sourcePath, archiveDir)
	require.NoError(t, err)
	require.True(t, second.AlreadyDone)
	var count int
	require.NoError(t, connection.QueryRow(`SELECT count(*) FROM users`).Scan(&count))
	require.Equal(t, 2, count)

	source, err := sql.Open("sqlite3", sourcePath)
	require.NoError(t, err)
	_, err = source.Exec(`UPDATE users SET first_name='changed' WHERE id=42`)
	require.NoError(t, err)
	require.NoError(t, source.Close())
	_, err = ImportSQLite(ctx, postgresURL, sourcePath, archiveDir)
	require.ErrorContains(t, err, "differs from existing archive")
	require.NoError(t, connection.QueryRow(`SELECT count(*) FROM users`).Scan(&count))
	require.Equal(t, 2, count)
}

func TestImportRefusesInitializedPostgres(t *testing.T) {
	ctx := context.Background()
	postgresURL := isolatedPostgresURL(t)
	connection, err := sql.Open("pgx", postgresURL)
	require.NoError(t, err)
	defer connection.Close()
	_, err = connection.Exec(`CREATE TABLE existing_data(id BIGINT)`)
	require.NoError(t, err)
	_, err = ImportSQLite(ctx, postgresURL, createSQLiteFixture(t), t.TempDir())
	require.ErrorContains(t, err, "already initialized")
}

func TestImportFailureRollsBack(t *testing.T) {
	ctx := context.Background()
	postgresURL := isolatedPostgresURL(t)
	sourcePath := createSQLiteFixture(t)
	connection, err := sql.Open("sqlite3", sourcePath)
	require.NoError(t, err)
	_, err = connection.Exec(`UPDATE tokens SET uuid=?`, "invalid-uuid")
	require.NoError(t, err)
	require.NoError(t, connection.Close())
	_, err = ImportSQLite(ctx, postgresURL, sourcePath, t.TempDir())
	require.ErrorContains(t, err, "invalid UUID")

	postgres, err := sql.Open("pgx", postgresURL)
	require.NoError(t, err)
	defer postgres.Close()
	var exists bool
	require.NoError(t, postgres.QueryRow(`SELECT to_regclass('users') IS NOT NULL`).Scan(&exists))
	require.False(t, exists, fmt.Sprintf("PostgreSQL schema should roll back after %v", err))
}

func TestCompletedImportRefusesMissingRows(t *testing.T) {
	ctx := context.Background()
	postgresURL := isolatedPostgresURL(t)
	sourcePath := createSQLiteFixture(t)
	archiveDir := t.TempDir()
	_, err := ImportSQLite(ctx, postgresURL, sourcePath, archiveDir)
	require.NoError(t, err)

	connection, err := sql.Open("pgx", postgresURL)
	require.NoError(t, err)
	defer connection.Close()
	_, err = connection.ExecContext(ctx, `DELETE FROM tokens`)
	require.NoError(t, err)
	_, err = ImportSQLite(ctx, postgresURL, sourcePath, archiveDir)
	require.ErrorContains(t, err, "fewer rows than the completed import")
}
