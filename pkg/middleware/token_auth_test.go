package middleware

import (
	mock_database "beneburg/pkg/database/mocks"
	"beneburg/pkg/database/model"
	"github.com/gin-gonic/gin"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"net/http"
	"net/http/httptest"
	"testing"
)

func Test_TokenAuth(t *testing.T) {
	logger := zap.L()
	t.Run("Auth success", func(t *testing.T) {
		w := httptest.NewRecorder()
		controller := gomock.NewController(t)
		defer controller.Finish()
		dbMock := mock_database.NewMockDatabase(controller)
		r := gin.New()
		authMiddleware := NewTokenAuth(dbMock, logger)
		r.Use(authMiddleware.Auth)
		r.GET("/test", func(ctx *gin.Context) {
			user := ctx.MustGet("currentUser").(*model.User)
			ctx.String(200, "%v", user.ID)
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.AddCookie(&http.Cookie{
			Name:     "token",
			Value:    "test",
			HttpOnly: true,
		})

		dbMock.EXPECT().GetUserByToken(gomock.Any(), "test").Return(&model.User{ID: 10}, nil).Times(1)

		r.ServeHTTP(w, req)
		assert.Equal(t, "10", w.Body.String())
	})
	t.Run("Auth fail", func(t *testing.T) {
		w := httptest.NewRecorder()
		controller := gomock.NewController(t)
		defer controller.Finish()
		dbMock := mock_database.NewMockDatabase(controller)

		r := gin.New()
		authMiddleware := NewTokenAuth(dbMock, logger)
		r.Use(authMiddleware.Auth)
		r.GET("/test", func(ctx *gin.Context) {
			ctx.String(200, "ok")
		})
		req := httptest.NewRequest("GET", "/test", nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, 302, w.Code)
		kek := w.Result().Cookies()
		assert.Equal(t, "token", kek[0].Name)
		assert.Equal(t, "", kek[0].Value)
	})
}
