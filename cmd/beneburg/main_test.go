package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReleaseDatabaseURLMustMatchComposePostgres(t *testing.T) {
	t.Setenv("POSTGRES_USER", "beneburg")
	t.Setenv("POSTGRES_PASSWORD", "local-test-password")
	t.Setenv("POSTGRES_DB", "beneburg")
	require.NoError(t, validateReleaseDatabaseURL("postgres://beneburg:local-test-password@postgres:5432/beneburg?sslmode=disable"))

	for _, databaseURL := range []string{
		"postgres://beneburg:local-test-password@example.com:5432/beneburg",
		"postgres://beneburg:wrong@postgres:5432/beneburg",
		"postgres://beneburg:local-test-password@postgres:5432/other",
		"postgres://postgres:local-test-password@postgres:5432/beneburg",
		"postgres://beneburg:local-test-password@postgres:5432/beneburg?host=example.com",
		"postgres://postgres:local-test-password@postgres:5432/beneburg?sslmode=disable&password=local-test-password",
	} {
		err := validateReleaseDatabaseURL(databaseURL)
		require.Error(t, err)
		require.False(t, strings.Contains(err.Error(), "local-test-password"))
	}
}
