package telegram

import (
	"beneburg/pkg/backup"
	"context"
	"errors"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"
)

type fakeBackupCreator struct {
	create func(context.Context) (*backup.Archive, error)
}

func (c fakeBackupCreator) Create(ctx context.Context) (*backup.Archive, error) {
	return c.create(ctx)
}

func TestGetBackupCommandSendsArchiveToAdministrator(t *testing.T) {
	manager := newBackupCommandTestManager(fakeBackupCreator{create: func(context.Context) (*backup.Archive, error) {
		return &backup.Archive{Name: "beneburg-test.dump", Bytes: []byte("PGDMP")}, nil
	}})
	message := privateCommandMessage(100, "/get_backup")

	manager.processPrivateCommand(message)

	statusMessage, ok := receiveOutgoingMessage(t, manager.messagesChan).(tgbotapi.MessageConfig)
	if !ok {
		t.Fatal("backup status is not a message")
	}
	if statusMessage.ChatID != 100 {
		t.Errorf("status chat ID = %d, want 100", statusMessage.ChatID)
	}
	document, ok := receiveOutgoingMessage(t, manager.messagesChan).(tgbotapi.DocumentConfig)
	if !ok {
		t.Fatal("backup response is not a document")
	}
	file, ok := document.File.(tgbotapi.FileBytes)
	if !ok {
		t.Fatal("backup document does not contain file bytes")
	}
	if file.Name != "beneburg-test.dump" || string(file.Bytes) != "PGDMP" {
		t.Errorf("document file = %#v", file)
	}
}

func TestGetBackupCommandIgnoresNonAdministrator(t *testing.T) {
	manager := newBackupCommandTestManager(fakeBackupCreator{create: func(context.Context) (*backup.Archive, error) {
		t.Fatal("backup creator must not be called")
		return nil, nil
	}})

	manager.processPrivateCommand(privateCommandMessage(200, "/get_backup"))

	select {
	case message := <-manager.messagesChan:
		t.Fatalf("unexpected outgoing message: %#v", message)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestGetBackupCommandReportsCreationFailure(t *testing.T) {
	manager := newBackupCommandTestManager(fakeBackupCreator{create: func(context.Context) (*backup.Archive, error) {
		return nil, errors.New("pg_dump failed")
	}})

	manager.processPrivateCommand(privateCommandMessage(100, "/get_backup"))
	_ = receiveOutgoingMessage(t, manager.messagesChan)
	failureMessage, ok := receiveOutgoingMessage(t, manager.messagesChan).(tgbotapi.MessageConfig)
	if !ok || failureMessage.Text != "Не удалось создать резервную копию базы данных." {
		t.Fatalf("failure response = %#v", failureMessage)
	}
}

func newBackupCommandTestManager(creator backup.Creator) *botManager {
	return &botManager{
		adminID:      100,
		backup:       creator,
		ctx:          context.Background(),
		logger:       zap.NewNop(),
		messagesChan: make(chan tgbotapi.Chattable, 4),
	}
}

func privateCommandMessage(userID int64, command string) *tgbotapi.Message {
	return &tgbotapi.Message{
		Text:     command,
		Entities: []tgbotapi.MessageEntity{{Type: "bot_command", Offset: 0, Length: len(command)}},
		From:     &tgbotapi.User{ID: userID},
		Chat:     &tgbotapi.Chat{ID: userID, Type: "private"},
	}
}

func receiveOutgoingMessage(t *testing.T, messages <-chan tgbotapi.Chattable) tgbotapi.Chattable {
	t.Helper()
	select {
	case message := <-messages:
		return message
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for outgoing Telegram message")
		return nil
	}
}
