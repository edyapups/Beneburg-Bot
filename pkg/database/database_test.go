package database

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInvalidPostgresURLDoesNotExposePassword(t *testing.T) {
	_, err := NewDatabase("postgres://user:private-password@%invalid", nil)
	require.Error(t, err)
	require.False(t, strings.Contains(err.Error(), "private-password"))
}
