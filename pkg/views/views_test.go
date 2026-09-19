package views

import (
	mock_database "beneburg/pkg/database/mocks"
	"beneburg/pkg/database/model"
	"beneburg/pkg/telegram"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/golang/mock/gomock"
	"go.uber.org/zap"
)

func TestParseBirthDate(t *testing.T) {
	birthDate, err := parseBirthDate("19.09.2000")
	if err != nil {
		t.Fatalf("parseBirthDate() returned an error: %v", err)
	}

	want := time.Date(2000, time.September, 19, 0, 0, 0, 0, time.UTC)
	if !birthDate.Equal(want) {
		t.Errorf("parseBirthDate() = %v, want %v", birthDate, want)
	}
}

func TestParseBirthDateRejectsMonthDayYearFormat(t *testing.T) {
	if _, err := parseBirthDate("09.19.2000"); err == nil {
		t.Error("parseBirthDate() accepted month.day.year format")
	}
}

func TestProfileFormAcceptsActiveUserWithoutModeration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	controller := gomock.NewController(t)
	databaseMock := mock_database.NewMockDatabase(controller)
	activeUser := &model.User{
		TelegramID: 42,
		Status:     model.UserStatusActive,
	}

	databaseMock.EXPECT().CreateForm(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, form *model.Form) (*model.Form, error) {
			if form.Status != model.FormStatusAccepted {
				t.Errorf("form status = %q, want %q", form.Status, model.FormStatusAccepted)
			}
			return form, nil
		},
	)

	botWasCalled := false
	view := views{
		db:        databaseMock,
		logger:    zap.NewNop(),
		templator: telegram.NewTemplator("example.com"),
		sendToBot: func(tgbotapi.Chattable) {
			botWasCalled = true
		},
	}
	formValues := url.Values{"name": {"Анна"}, "gender": {"женский"}}
	request := httptest.NewRequest(http.MethodPost, "/profile/form", strings.NewReader(formValues.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	router := gin.New()
	router.Use(func(context *gin.Context) {
		context.Set("currentUser", activeUser)
	})
	router.POST("/profile/form", view.profileForm)

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusFound {
		t.Errorf("status code = %d, want %d", recorder.Code, http.StatusFound)
	}
	if location := recorder.Header().Get("Location"); location != "/profile" {
		t.Errorf("redirect location = %q, want %q", location, "/profile")
	}
	if botWasCalled {
		t.Error("active user's form was sent for moderation")
	}
}
