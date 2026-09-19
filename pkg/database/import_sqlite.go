package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const archiveName = "beneburg-sqlite-archive.db"

type ImportCounts struct {
	Users  int64
	Tokens int64
	Forms  int64
}

type ImportResult struct {
	SourceSHA256 string
	Counts       ImportCounts
	AlreadyDone  bool
}

// ImportSQLite reads the original database from a read-only mount. A writable
// staging copy lets SQLite recover a WAL or hot rollback journal without ever
// touching the original. PostgreSQL commits all rows and its marker together.
func ImportSQLite(ctx context.Context, postgresURL, sourcePath, archiveDir string) (*ImportResult, error) {
	if postgresURL == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	if err := os.MkdirAll(archiveDir, 0700); err != nil {
		return nil, fmt.Errorf("create SQLite archive directory: %w", err)
	}
	archivePath := filepath.Join(archiveDir, archiveName)
	if err := createSnapshot(ctx, sourcePath, archivePath); err != nil {
		return nil, err
	}
	archiveHash, err := hashFile(archivePath)
	if err != nil {
		return nil, err
	}

	source, err := openReadOnlySQLite(archivePath)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	counts, sequences, err := inspectSQLite(ctx, source)
	if err != nil {
		return nil, err
	}

	postgres, err := sql.Open("pgx", postgresURL)
	if err != nil {
		return nil, errors.New("cannot open PostgreSQL connection from DATABASE_URL")
	}
	defer postgres.Close()
	if err := postgres.PingContext(ctx); err != nil {
		return nil, errors.New("cannot connect to PostgreSQL; check DATABASE_URL and server availability")
	}
	transaction, err := postgres.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("start PostgreSQL import transaction: %w", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, migrationLockID); err != nil {
		return nil, fmt.Errorf("lock PostgreSQL import: %w", err)
	}
	result := &ImportResult{SourceSHA256: archiveHash, Counts: counts}
	completed, err := checkExistingImport(ctx, transaction, result)
	if err != nil {
		return nil, err
	}
	if completed {
		result.AlreadyDone = true
		return result, transaction.Commit()
	}

	if _, err := transaction.ExecContext(ctx, postgresSchema); err != nil {
		return nil, fmt.Errorf("create PostgreSQL schema: %w", err)
	}
	if err := copyUsers(ctx, source, transaction); err != nil {
		return nil, err
	}
	if err := copyTokens(ctx, source, transaction); err != nil {
		return nil, err
	}
	if err := copyForms(ctx, source, transaction); err != nil {
		return nil, err
	}
	if err := validatePostgresImport(ctx, transaction, counts, sequences); err != nil {
		return nil, err
	}
	_, err = transaction.ExecContext(ctx, `
		INSERT INTO sqlite_imports(source_sha256, users_count, tokens_count, forms_count)
		VALUES ($1, $2, $3, $4)
	`, archiveHash, counts.Users, counts.Tokens, counts.Forms)
	if err != nil {
		return nil, fmt.Errorf("record completed SQLite import: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return nil, fmt.Errorf("commit SQLite import: %w", err)
	}
	return result, nil
}

func openReadOnlySQLite(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("SQLite file %s: %w", path, err)
	}
	dsn := "file:" + filepath.ToSlash(path) + "?mode=ro&_foreign_keys=on&_busy_timeout=5000"
	connection, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open SQLite read-only: %w", err)
	}
	connection.SetMaxOpenConns(1)
	if err := connection.Ping(); err != nil {
		connection.Close()
		return nil, fmt.Errorf("read SQLite source: %w", err)
	}
	return connection, nil
}

func createSnapshot(ctx context.Context, sourcePath, archivePath string) error {
	stagingDir, err := os.MkdirTemp(filepath.Dir(archivePath), ".sqlite-stage-*")
	if err != nil {
		return fmt.Errorf("prepare SQLite staging directory: %w", err)
	}
	defer os.RemoveAll(stagingDir)
	stagedPath := filepath.Join(stagingDir, "beneburg.db")
	if err := copySQLiteFile(sourcePath, stagedPath, true); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-journal"} {
		if err := copySQLiteFile(sourcePath+suffix, stagedPath+suffix, false); err != nil {
			return err
		}
	}
	source, err := sql.Open("sqlite3", stagedPath+"?_foreign_keys=on&_busy_timeout=5000")
	if err != nil {
		return fmt.Errorf("open staged SQLite source: %w", err)
	}
	defer source.Close()
	source.SetMaxOpenConns(1)
	if _, _, err := inspectSQLite(ctx, source); err != nil {
		return fmt.Errorf("original SQLite database: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(archivePath), ".sqlite-snapshot-*.db")
	if err != nil {
		return fmt.Errorf("prepare SQLite snapshot: %w", err)
	}
	temporaryPath := temporary.Name()
	temporary.Close()
	if err := os.Remove(temporaryPath); err != nil {
		return err
	}
	defer os.Remove(temporaryPath)
	if _, err := source.ExecContext(ctx, `VACUUM main INTO ?`, temporaryPath); err != nil {
		return fmt.Errorf("snapshot SQLite source (including WAL): %w", err)
	}
	newHash, err := hashFile(temporaryPath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(archivePath); err == nil {
		oldHash, err := hashFile(archivePath)
		if err != nil {
			return err
		}
		if oldHash != newHash {
			return errors.New("SQLite source differs from existing archive; refusing to replace it")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Chmod(temporaryPath, 0600); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, archivePath); err != nil {
		return fmt.Errorf("save SQLite archive: %w", err)
	}
	return nil
}

func copySQLiteFile(sourcePath, destinationPath string, required bool) error {
	source, err := os.Open(sourcePath)
	if os.IsNotExist(err) && !required {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open SQLite source %s: %w", sourcePath, err)
	}
	defer source.Close()
	destination, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("create SQLite staging file: %w", err)
	}
	defer destination.Close()
	if _, err := io.Copy(destination, source); err != nil {
		return fmt.Errorf("copy SQLite source %s: %w", sourcePath, err)
	}
	return destination.Sync()
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func inspectSQLite(ctx context.Context, source *sql.DB) (ImportCounts, map[string]int64, error) {
	var counts ImportCounts
	var integrity string
	if err := source.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		return counts, nil, fmt.Errorf("SQLite integrity check failed: %s: %v", integrity, err)
	}
	foreignKeys, err := source.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return counts, nil, err
	}
	if foreignKeys.Next() {
		foreignKeys.Close()
		return counts, nil, errors.New("SQLite foreign key check failed")
	}
	if err := foreignKeys.Err(); err != nil {
		foreignKeys.Close()
		return counts, nil, err
	}
	foreignKeys.Close()
	rows, err := source.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name <> 'schema_migrations' ORDER BY name`)
	if err != nil {
		return counts, nil, err
	}
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			rows.Close()
			return counts, nil, err
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return counts, nil, err
	}
	rows.Close()
	if strings.Join(tables, ",") != "forms,tokens,users" {
		return counts, nil, fmt.Errorf("unexpected SQLite application tables: %v", tables)
	}
	for table, expectedColumns := range map[string][]string{
		"users":  {"id", "created_at", "updated_at", "deleted_at", "telegram_id", "username", "first_name", "last_name", "status"},
		"tokens": {"uuid", "user_telegram_id", "expire_at"},
		"forms":  {"id", "created_at", "updated_at", "deleted_at", "user_telegram_id", "name", "age", "gender", "about", "hobbies", "work", "education", "cover_letter", "contacts", "status"},
	} {
		if err := validateSQLiteColumns(ctx, source, table, expectedColumns); err != nil {
			return counts, nil, err
		}
	}
	var hasMigrations bool
	if err := source.QueryRowContext(ctx, `SELECT count(*) > 0 FROM sqlite_master WHERE type='table' AND name='schema_migrations'`).Scan(&hasMigrations); err != nil {
		return counts, nil, err
	}
	if hasMigrations {
		var maxVersion int
		if err := source.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&maxVersion); err != nil {
			return counts, nil, err
		}
		if maxVersion > 4 {
			return counts, nil, fmt.Errorf("unsupported SQLite schema migration version %d", maxVersion)
		}
	}
	for _, table := range []struct {
		name        string
		destination *int64
	}{
		{name: "users", destination: &counts.Users},
		{name: "tokens", destination: &counts.Tokens},
		{name: "forms", destination: &counts.Forms},
	} {
		if err := source.QueryRowContext(ctx, "SELECT count(*) FROM "+table.name).Scan(table.destination); err != nil {
			return counts, nil, fmt.Errorf("count SQLite %s: %w", table.name, err)
		}
	}
	sequences := map[string]int64{}
	var hasSequences bool
	if err := source.QueryRowContext(ctx, `SELECT count(*) > 0 FROM sqlite_master WHERE type='table' AND name='sqlite_sequence'`).Scan(&hasSequences); err != nil {
		return counts, nil, err
	}
	if hasSequences {
		sequenceRows, err := source.QueryContext(ctx, `SELECT name, seq FROM sqlite_sequence WHERE name IN ('users', 'forms')`)
		if err != nil {
			return counts, nil, err
		}
		for sequenceRows.Next() {
			var name string
			var value int64
			if err := sequenceRows.Scan(&name, &value); err != nil {
				sequenceRows.Close()
				return counts, nil, err
			}
			sequences[name] = value
		}
		if err := sequenceRows.Err(); err != nil {
			sequenceRows.Close()
			return counts, nil, err
		}
		sequenceRows.Close()
	}
	return counts, sequences, nil
}

func validateSQLiteColumns(ctx context.Context, source *sql.DB, table string, expected []string) error {
	rows, err := source.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return fmt.Errorf("inspect SQLite %s columns: %w", table, err)
	}
	defer rows.Close()
	var actual []string
	for rows.Next() {
		var position, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&position, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		actual = append(actual, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	sort.Strings(actual)
	sort.Strings(expected)
	if strings.Join(actual, ",") != strings.Join(expected, ",") {
		return fmt.Errorf("SQLite %s columns differ from the supported schema: %v", table, actual)
	}
	return nil
}

func checkExistingImport(ctx context.Context, transaction *sql.Tx, expected *ImportResult) (bool, error) {
	var tableCount int
	err := transaction.QueryRowContext(ctx, `
		SELECT count(*) FROM pg_class AS relation
		JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
		WHERE namespace.nspname = current_schema() AND relation.relkind IN ('r', 'p', 'v', 'm', 'f', 'S')
	`).Scan(&tableCount)
	if err != nil {
		return false, err
	}
	if tableCount == 0 {
		return false, nil
	}
	var markerExists bool
	if err := transaction.QueryRowContext(ctx, `SELECT to_regclass('sqlite_imports') IS NOT NULL`).Scan(&markerExists); err != nil {
		return false, err
	}
	if !markerExists {
		return false, errors.New("PostgreSQL is already initialized without a completed SQLite import; refusing to overwrite it")
	}
	var markerCount int
	if err := transaction.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_imports`).Scan(&markerCount); err != nil || markerCount != 1 {
		return false, fmt.Errorf("expected one completed import marker, found %d: %v", markerCount, err)
	}
	var actual ImportResult
	err = transaction.QueryRowContext(ctx, `SELECT source_sha256, users_count, tokens_count, forms_count FROM sqlite_imports`).Scan(
		&actual.SourceSHA256, &actual.Counts.Users, &actual.Counts.Tokens, &actual.Counts.Forms,
	)
	if err != nil {
		return false, fmt.Errorf("read completed import marker: %w", err)
	}
	if actual.SourceSHA256 != expected.SourceSHA256 || actual.Counts != expected.Counts {
		return false, errors.New("completed PostgreSQL import does not match the SQLite archive")
	}
	for _, table := range []struct {
		name         string
		minimumCount int64
	}{
		{name: "users", minimumCount: actual.Counts.Users},
		{name: "tokens", minimumCount: actual.Counts.Tokens},
		{name: "forms", minimumCount: actual.Counts.Forms},
	} {
		var currentCount int64
		if err := transaction.QueryRowContext(ctx, "SELECT count(*) FROM "+table.name).Scan(&currentCount); err != nil {
			return false, fmt.Errorf("verify imported PostgreSQL table %s: %w", table.name, err)
		}
		if currentCount < table.minimumCount {
			return false, fmt.Errorf("PostgreSQL table %s has fewer rows than the completed import", table.name)
		}
	}
	var version int
	if err := transaction.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version); err != nil || version != 1 {
		return false, fmt.Errorf("completed import has incompatible schema version %d: %v", version, err)
	}
	return true, nil
}

func copyUsers(ctx context.Context, source *sql.DB, transaction *sql.Tx) error {
	rows, err := source.QueryContext(ctx, `
		SELECT id, created_at, updated_at, deleted_at, telegram_id,
		       username, first_name, last_name, status
		FROM users ORDER BY id
	`)
	if err != nil {
		return fmt.Errorf("read SQLite users: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, telegramID int64
		var createdAt, updatedAt time.Time
		var deletedAt sql.NullTime
		var username, lastName sql.NullString
		var firstName, status string
		if err := rows.Scan(&id, &createdAt, &updatedAt, &deletedAt, &telegramID, &username, &firstName, &lastName, &status); err != nil {
			return fmt.Errorf("scan SQLite user: %w", err)
		}
		if createdAt.IsZero() || updatedAt.IsZero() {
			return fmt.Errorf("SQLite user %d has invalid timestamps", id)
		}
		_, err = transaction.ExecContext(ctx, `
			INSERT INTO users(id, created_at, updated_at, deleted_at, telegram_id,
			                  username, first_name, last_name, status)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		`, id, createdAt, updatedAt, deletedAt, telegramID, username, firstName, lastName, status)
		if err != nil {
			return fmt.Errorf("copy SQLite user %d: %w", id, err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read SQLite users: %w", err)
	}
	return nil
}

func copyTokens(ctx context.Context, source *sql.DB, transaction *sql.Tx) error {
	rows, err := source.QueryContext(ctx, `
		SELECT uuid, user_telegram_id, expire_at
		FROM tokens ORDER BY user_telegram_id
	`)
	if err != nil {
		return fmt.Errorf("read SQLite tokens: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var token string
		var telegramID int64
		var expiresAt time.Time
		if err := rows.Scan(&token, &telegramID, &expiresAt); err != nil {
			return fmt.Errorf("scan SQLite token: %w", err)
		}
		if _, err := uuid.Parse(token); err != nil || expiresAt.IsZero() {
			return fmt.Errorf("SQLite token for Telegram ID %d has invalid UUID or expiry", telegramID)
		}
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO tokens(uuid, user_telegram_id, expire_at)
			VALUES ($1,$2,$3)
		`, token, telegramID, expiresAt); err != nil {
			return fmt.Errorf("copy SQLite token for Telegram ID %d: %w", telegramID, err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read SQLite tokens: %w", err)
	}
	return nil
}

func copyForms(ctx context.Context, source *sql.DB, transaction *sql.Tx) error {
	rows, err := source.QueryContext(ctx, `
		SELECT id, created_at, updated_at, deleted_at, user_telegram_id,
		       name, age, gender, about, hobbies, work, education,
		       cover_letter, contacts, status
		FROM forms ORDER BY id
	`)
	if err != nil {
		return fmt.Errorf("read SQLite forms: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, telegramID int64
		var createdAt, updatedAt time.Time
		var deletedAt sql.NullTime
		var age sql.NullInt64
		var name, gender, status string
		var about, hobbies, work, education, coverLetter, contacts sql.NullString
		err := rows.Scan(&id, &createdAt, &updatedAt, &deletedAt, &telegramID, &name, &age, &gender, &about, &hobbies, &work, &education, &coverLetter, &contacts, &status)
		if err != nil {
			return fmt.Errorf("scan SQLite form: %w", err)
		}
		if createdAt.IsZero() || updatedAt.IsZero() {
			return fmt.Errorf("SQLite form %d has invalid timestamps", id)
		}
		_, err = transaction.ExecContext(ctx, `
			INSERT INTO forms(id, created_at, updated_at, deleted_at, user_telegram_id,
			                  name, age, gender, about, hobbies, work, education,
			                  cover_letter, contacts, status)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		`, id, createdAt, updatedAt, deletedAt, telegramID, name, age, gender, about, hobbies, work, education, coverLetter, contacts, status)
		if err != nil {
			return fmt.Errorf("copy SQLite form %d: %w", id, err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read SQLite forms: %w", err)
	}
	return nil
}

func validatePostgresImport(ctx context.Context, transaction *sql.Tx, expected ImportCounts, sequences map[string]int64) error {
	for _, table := range []struct {
		name  string
		count int64
	}{
		{name: "users", count: expected.Users},
		{name: "tokens", count: expected.Tokens},
		{name: "forms", count: expected.Forms},
	} {
		var actual int64
		if err := transaction.QueryRowContext(ctx, "SELECT count(*) FROM "+table.name).Scan(&actual); err != nil {
			return err
		}
		if actual != table.count {
			return fmt.Errorf("%s count mismatch: SQLite=%d PostgreSQL=%d", table.name, table.count, actual)
		}
	}
	for _, table := range []string{"users", "forms"} {
		var maxID int64
		if err := transaction.QueryRowContext(ctx, "SELECT COALESCE(MAX(id), 0) FROM "+table).Scan(&maxID); err != nil {
			return err
		}
		if sequences[table] > maxID {
			maxID = sequences[table]
		}
		if maxID == 0 {
			if _, err := transaction.ExecContext(ctx, `SELECT setval(pg_get_serial_sequence($1, 'id'), 1, false)`, table); err != nil {
				return fmt.Errorf("reset %s identity sequence: %w", table, err)
			}
		} else if _, err := transaction.ExecContext(ctx, `SELECT setval(pg_get_serial_sequence($1, 'id'), $2, true)`, table, maxID); err != nil {
			return fmt.Errorf("advance %s identity sequence: %w", table, err)
		}
	}
	var invalid int64
	err := transaction.QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM tokens t LEFT JOIN users u ON u.telegram_id=t.user_telegram_id WHERE u.id IS NULL)
		     + (SELECT count(*) FROM forms f LEFT JOIN users u ON u.telegram_id=f.user_telegram_id WHERE u.id IS NULL)
	`).Scan(&invalid)
	if err != nil || invalid != 0 {
		return fmt.Errorf("PostgreSQL foreign key validation failed: %d invalid references: %v", invalid, err)
	}
	return nil
}
