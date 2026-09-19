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
