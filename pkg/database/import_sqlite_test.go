package database

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSQLiteSnapshotIsReadOnlyAndRepeatable(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.db")
	archivePath := filepath.Join(directory, "archive", archiveName)
	require.NoError(t, os.Mkdir(filepath.Dir(archivePath), 0700))

	source, err := sql.Open("sqlite3", sourcePath)
	require.NoError(t, err)
	require.NoError(t, NewDatabaseWithDB(source, nil).Migrate(ctx))
	now := time.Now().UTC().Truncate(time.Second)
	_, err = source.ExecContext(ctx, `INSERT INTO users(id, created_at, updated_at, deleted_at, telegram_id, first_name, status) VALUES (?, ?, ?, ?, ?, ?, ?)`, 42, now, now, now, 1234567890123, "Test", "banned")
	require.NoError(t, err)
	require.NoError(t, source.Close())
	require.NoError(t, os.Chmod(sourcePath, 0400))

	require.NoError(t, createSnapshot(ctx, sourcePath, archivePath))
	firstHash, err := hashFile(archivePath)
	require.NoError(t, err)
	require.NoError(t, createSnapshot(ctx, sourcePath, archivePath))
	secondHash, err := hashFile(archivePath)
	require.NoError(t, err)
	require.Equal(t, firstHash, secondHash)

	archived, err := openReadOnlySQLite(archivePath)
	require.NoError(t, err)
	defer archived.Close()
	counts, _, err := inspectSQLite(ctx, archived)
	require.NoError(t, err)
	require.Equal(t, ImportCounts{Users: 1}, counts)
	var deletedAt time.Time
	require.NoError(t, archived.QueryRowContext(ctx, `SELECT deleted_at FROM users WHERE id=42`).Scan(&deletedAt))
	require.WithinDuration(t, now, deletedAt, time.Second)
}

func TestSQLiteSnapshotIncludesWAL(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.db")
	archivePath := filepath.Join(directory, "archive", archiveName)
	require.NoError(t, os.Mkdir(filepath.Dir(archivePath), 0700))

	source, err := sql.Open("sqlite3", sourcePath)
	require.NoError(t, err)
	defer source.Close()
	_, err = source.ExecContext(ctx, `PRAGMA journal_mode=WAL`)
	require.NoError(t, err)
	require.NoError(t, NewDatabaseWithDB(source, nil).Migrate(ctx))
	now := time.Now().UTC()
	_, err = source.ExecContext(ctx, `INSERT INTO users(created_at, updated_at, telegram_id, first_name) VALUES (?, ?, ?, ?)`, now, now, 73, "WAL user")
	require.NoError(t, err)
	require.FileExists(t, sourcePath+"-wal")
	require.NoError(t, createSnapshot(ctx, sourcePath, archivePath))

	archived, err := openReadOnlySQLite(archivePath)
	require.NoError(t, err)
	defer archived.Close()
	counts, _, err := inspectSQLite(ctx, archived)
	require.NoError(t, err)
	require.Equal(t, ImportCounts{Users: 1}, counts)
}
