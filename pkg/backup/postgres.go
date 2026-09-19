// Package backup creates portable PostgreSQL database archives.
package backup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const DefaultMaxArchiveBytes int64 = 50 * 1024 * 1024

// Archive is an in-memory PostgreSQL custom-format archive ready for upload.
type Archive struct {
	Name  string
	Bytes []byte
}

// Creator makes one database archive.
type Creator interface {
	Create(context.Context) (*Archive, error)
}

type commandRunner interface {
	Run(context.Context, string, ...string) error
}

type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, name string, arguments ...string) error {
	return exec.CommandContext(ctx, name, arguments...).Run()
}

type postgresCreator struct {
	databaseURL     string
	maximumFileSize int64
	clock           func() time.Time
	runner          commandRunner
}

// NewPostgresCreator creates archives using pg_dump available in the runtime image.
func NewPostgresCreator(databaseURL string) Creator {
	return newPostgresCreator(databaseURL, DefaultMaxArchiveBytes, time.Now, execCommandRunner{})
}

func newPostgresCreator(databaseURL string, maximumFileSize int64, clock func() time.Time, runner commandRunner) *postgresCreator {
	return &postgresCreator{
		databaseURL:     databaseURL,
		maximumFileSize: maximumFileSize,
		clock:           clock,
		runner:          runner,
	}
}

func (c *postgresCreator) Create(ctx context.Context) (*Archive, error) {
	if c.databaseURL == "" {
		return nil, fmt.Errorf("database URL is required")
	}

	temporaryFile, err := os.CreateTemp("", "beneburg-backup-*.dump")
	if err != nil {
		return nil, fmt.Errorf("create temporary backup file: %w", err)
	}
	temporaryFilePath := temporaryFile.Name()
	if err := temporaryFile.Close(); err != nil {
		_ = os.Remove(temporaryFilePath)
		return nil, fmt.Errorf("close temporary backup file: %w", err)
	}
	defer os.Remove(temporaryFilePath)

	err = c.runner.Run(ctx, "pg_dump",
		"--dbname="+c.databaseURL,
		"--format=custom",
		"--no-owner",
		"--no-privileges",
		"--file="+temporaryFilePath,
	)
	if err != nil {
		return nil, fmt.Errorf("run pg_dump: %w", err)
	}

	fileInfo, err := os.Stat(temporaryFilePath)
	if err != nil {
		return nil, fmt.Errorf("inspect database backup: %w", err)
	}
	if fileInfo.Size() > c.maximumFileSize {
		return nil, fmt.Errorf("database backup is %d bytes, exceeding Telegram limit of %d bytes", fileInfo.Size(), c.maximumFileSize)
	}

	contents, err := os.ReadFile(temporaryFilePath)
	if err != nil {
		return nil, fmt.Errorf("read database backup: %w", err)
	}
	archiveName := fmt.Sprintf("beneburg-%s.dump", c.clock().UTC().Format("20060102T150405Z"))
	return &Archive{Name: filepath.Base(archiveName), Bytes: contents}, nil
}
