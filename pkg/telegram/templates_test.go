package telegram

import (
	"beneburg/pkg/database/model"
	"testing"
)

func TestLoginCommandReplyUsesDomainWithPort(t *testing.T) {
	templator := NewTemplator("localhost:8081")

	message := templator.LoginCommandReply(&model.Token{UUID: "login-token"})

	const expectedURL = "localhost:8081/login/login-token"
	if message != "Вот твоя ссылка для входа:\n"+expectedURL {
		t.Fatalf("LoginCommandReply() = %q, want URL %q", message, expectedURL)
	}
}

func TestNewFormMessageForActiveUserContainsOnlyProfileLink(t *testing.T) {
	templator := NewTemplator("https://beneburg.example")
	user := &model.User{
		TelegramID: 42,
		Status:     model.UserStatusActive,
	}
	form := &model.Form{Name: "Анна", Gender: "женский"}

	message := templator.NewFormMessage(user, form)

	const expectedMessage = "<b>Участник изменил анкету.</b>\nhttps://beneburg.example/user/42"
	if message != expectedMessage {
		t.Fatalf("NewFormMessage() = %q, want %q", message, expectedMessage)
	}
}
