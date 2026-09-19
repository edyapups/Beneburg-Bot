package backup

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

type fakeCommandRunner struct {
	run func(context.Context, string, ...string) error
}

func (r fakeCommandRunner) Run(ctx context.Context, name string, arguments ...string) error {
	return r.run(ctx, name, arguments...)
}

func TestPostgresCreatorCreatesCustomArchive(t *testing.T) {
	var temporaryFilePath string
	creator := newPostgresCreator("postgres://user:password@postgres/database", 1024, func() time.Time {
		return time.Date(2026, time.September, 19, 12, 0, 0, 0, time.UTC)
	}, fakeCommandRunner{run: func(_ context.Context, name string, arguments ...string) error {
		if name != "pg_dump" {
			t.Fatalf("command = %q, want pg_dump", name)
		}
		joinedArguments := strings.Join(arguments, " ")
		for _, expectedArgument := range []string{"--format=custom", "--no-owner", "--no-privileges"} {
			if !strings.Contains(joinedArguments, expectedArgument) {
				t.Fatalf("arguments %q do not contain %q", joinedArguments, expectedArgument)
			}
		}
		for _, argument := range arguments {
			if strings.HasPrefix(argument, "--file=") {
				temporaryFilePath = strings.TrimPrefix(argument, "--file=")
				return os.WriteFile(temporaryFilePath, []byte("PGDMP"), 0600)
			}
		}
		t.Fatal("pg_dump output file argument is missing")
		return nil
	}})

	archive, err := creator.Create(context.Background())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if archive.Name != "beneburg-20260919T120000Z.dump" {
		t.Errorf("archive name = %q", archive.Name)
	}
	if string(archive.Bytes) != "PGDMP" {
		t.Errorf("archive contents = %q", archive.Bytes)
	}
	if _, err := os.Stat(temporaryFilePath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temporary backup file still exists, stat error = %v", err)
	}
}

func TestPostgresCreatorRejectsOversizedArchive(t *testing.T) {
	creator := newPostgresCreator("postgres://user:password@postgres/database", 3, time.Now, fakeCommandRunner{run: func(_ context.Context, _ string, arguments ...string) error {
		for _, argument := range arguments {
			if strings.HasPrefix(argument, "--file=") {
				return os.WriteFile(strings.TrimPrefix(argument, "--file="), []byte("PGDMP"), 0600)
			}
		}
		return errors.New("output file argument is missing")
	}})

	_, err := creator.Create(context.Background())
	if err == nil || !strings.Contains(err.Error(), "exceeding Telegram limit") {
		t.Fatalf("Create() error = %v, want size-limit error", err)
	}
}
