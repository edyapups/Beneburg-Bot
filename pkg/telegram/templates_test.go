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

func TestNewFormMessageForActiveUserContainsCollapsedFormAndProfileLink(t *testing.T) {
	templator := NewTemplator("https://beneburg.example")
	user := &model.User{
		TelegramID: 42,
		Status:     model.UserStatusActive,
	}
	form := &model.Form{Name: "Анна", Gender: "женский"}

	message := templator.NewFormMessage(user, form)

	const expectedMessage = "<b>Участник изменил анкету:</b>\n<blockquote expandable><b>Имя</b>:\nАнна\n\n<b>Пол</b>:\nженский\n\n<b>Ссылка на профиль:</b> https://beneburg.example/user/42</blockquote>"
	if message != expectedMessage {
		t.Fatalf("NewFormMessage() = %q, want %q", message, expectedMessage)
	}
}
