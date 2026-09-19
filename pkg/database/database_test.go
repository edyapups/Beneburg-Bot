package database

import (
	"beneburg/pkg/database/model"
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDatabaseSQLite(t *testing.T) {
	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	db := NewDatabaseWithDB(sqlDB, nil).(*database)
	t.Cleanup(func() { sqlDB.Close() })
	require.NoError(t, db.Migrate(ctx))
	testID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	db.uuidGen = func() uuid.UUID { return testID }

	u, err := db.CreateUser(ctx, &model.User{TelegramID: 10, FirstName: "Test", Status: model.UserStatusActive})
	require.NoError(t, err)
	require.NotZero(t, u.ID)
	token, err := db.CreateOrProlongToken(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, testID.String(), token.UUID)
	byToken, err := db.GetUserByToken(ctx, token.UUID)
	require.NoError(t, err)
	require.Equal(t, int64(10), byToken.TelegramID)

	form, err := db.CreateForm(ctx, &model.Form{UserTelegramId: 10, Name: "Test", Status: model.FormStatusAccepted})
	require.NoError(t, err)
	require.NotZero(t, form.ID)
	actual, err := db.GetActualForm(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, form.ID, actual.ID)
	require.Equal(t, int64(10), actual.User.TelegramID)
	require.WithinDuration(t, time.Now(), token.ExpireAt, 25*time.Hour)
}
