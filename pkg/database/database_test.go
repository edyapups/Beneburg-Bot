package database

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInvalidPostgresURLDoesNotExposePassword(t *testing.T) {
	_, err := NewDatabase("postgres://user:private-password@%invalid", nil)
	require.Error(t, err)
	require.False(t, strings.Contains(err.Error(), "private-password"))
}

func TestTokenExpirationDateIsOneCalendarMonthLater(t *testing.T) {
	now := time.Date(2026, time.January, 31, 12, 0, 0, 0, time.FixedZone("UTC+3", 3*60*60))

	expireAt := tokenExpirationDate(now)

	expectedExpireAt := time.Date(2026, time.March, 3, 9, 0, 0, 0, time.UTC)
	if !expireAt.Equal(expectedExpireAt) {
		t.Fatalf("tokenExpirationDate() = %s, want %s", expireAt, expectedExpireAt)
	}
}
