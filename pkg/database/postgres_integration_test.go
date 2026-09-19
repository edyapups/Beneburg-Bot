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
		Status:         model.FormStatusAccepted,
	})
	require.NoError(t, err)
	require.NotZero(t, form.ID)
	actual, err := store.GetActualForm(ctx, user.TelegramID)
	require.NoError(t, err)
	require.Equal(t, form.ID, actual.ID)
	require.Equal(t, user.TelegramID, actual.User.TelegramID)

	_, err = store.AcceptForm(ctx, form.ID)
	require.NoError(t, err)
}
