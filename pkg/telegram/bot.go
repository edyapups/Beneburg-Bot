package telegram

import (
	"beneburg/pkg/backup"
	"beneburg/pkg/database"
	"beneburg/pkg/database/model"
	"context"
	"database/sql"
	"errors"
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
	"strings"
	"sync/atomic"
	"time"
)

//go:generate mockgen -source=bot.go -destination=./mocks/mock_bot.go -package=mock_telegram
type Bot interface {
	Start()
	GetSendFunc() TelegramBotSendFunc
	SetLogger(logger *zap.Logger)
}

type TelegramBotSendFunc func(message tgbotapi.Chattable)

type botManager struct {
	bot        TgBotAPI
	db         database.Database
	templator  Templator
	adminID    int64
	groupID    int64
	inviteLink string
	backup     backup.Creator
	backupBusy atomic.Bool

	updatesChan  chan tgbotapi.Update
	messagesChan chan tgbotapi.Chattable

	ctx    context.Context
	logger *zap.Logger
}

func NewBot(ctx context.Context, bot TgBotAPI, db database.Database, adminID int64, groupID int64, inviteLink string, domain string) Bot {
	return newBot(ctx, bot, db, adminID, groupID, inviteLink, domain, nil)
}

// NewBotWithBackupCreator is intended for tests and custom runtime wiring.
func NewBotWithBackupCreator(ctx context.Context, bot TgBotAPI, db database.Database, adminID int64, groupID int64, inviteLink string, domain string, creator backup.Creator) Bot {
	return newBot(ctx, bot, db, adminID, groupID, inviteLink, domain, creator)
}

func newBot(ctx context.Context, bot TgBotAPI, db database.Database, adminID int64, groupID int64, inviteLink string, domain string, creator backup.Creator) Bot {
	return &botManager{
		bot:          bot,
		templator:    NewTemplator(domain),
		db:           db,
		ctx:          ctx,
		adminID:      adminID,
		groupID:      groupID,
		inviteLink:   inviteLink,
		backup:       creator,
		updatesChan:  make(chan tgbotapi.Update, 60),
		messagesChan: make(chan tgbotapi.Chattable, 60),
	}
}

func (b *botManager) SetLogger(logger *zap.Logger) {
	b.logger = logger
}

// TODO: will be parallelized
func (b *botManager) GetSendFunc() TelegramBotSendFunc {
	return b.send
}

func (b *botManager) Start() {
	go b.startGettingUpdates()
	go b.startProcessingUpdates()
	go b.startProcessingMessages()
}

// TODO: add With() with context to all loggings
func (b *botManager) startGettingUpdates() {
	var offset = 0
	for {
		select {
		case <-b.ctx.Done():
			return
		default:
		}
		updates, err := b.bot.GetUpdates(tgbotapi.UpdateConfig{
			Offset:  offset,
			Timeout: 60,
		})
		b.logger.Named("startGettingUpdates").Debug("Got updates", zap.Int("updates_count", len(updates)))
		if err != nil {
			b.logger.Error("Error while getting bot updates", zap.Error(err))
			b.logger.Info("Sleeping for 3 seconds...")
			time.Sleep(time.Second * 3)
			continue
		}
		for _, update := range updates {
			if update.UpdateID >= offset {
				offset = update.UpdateID + 1
				b.updatesChan <- update
			}
		}
	}
}

func (b *botManager) startProcessingUpdates() {
	for {
		select {
		case <-b.ctx.Done():
			return
		case update := <-b.updatesChan:
			b.processUpdate(update)
		}
	}
}

func (b *botManager) startProcessingMessages() {
	limiter := rate.NewLimiter(30, 30)
	for {
		select {
		case <-b.ctx.Done():
			return
		case message := <-b.messagesChan:
			_ = limiter.Wait(b.ctx)
			_, err := b.bot.Request(message)
			switch typedErr := err.(type) {
			case *tgbotapi.Error:
				b.logger.Named("startProcessingMessages").Error("Error while sending message", zap.Error(typedErr), zap.Any("message", message))
				if typedErr.Code == 429 {
					b.logger.Named("startProcessingMessages").Info("Too many requests, sleeping for 1 second")
					time.Sleep(time.Second)
					b.messagesChan <- message
				}
			case nil:
				continue
			default:
				b.logger.Named("startProcessingMessages").Error("Error while sending message", zap.Error(err), zap.Any("message", message))
			}
		}
	}
}

func (b *botManager) send(message tgbotapi.Chattable) {
	b.messagesChan <- message
}

func (b *botManager) processUpdate(update tgbotapi.Update) {
	b.logger.Named("processUpdate").Info("Processing update", zap.Int("update_id", update.UpdateID))
	if update.Message != nil {
		b.processMessage(update.Message)
		return
	}
	b.logger.Named("processUpdate").Info("Processing update, not message", zap.Any("update", update))
	if update.CallbackQuery != nil {
		b.processCallbackQuery(update.CallbackQuery)
		return
	}
	if update.ChatJoinRequest != nil {
		b.processChatJoinRequest(update.ChatJoinRequest)
		return
	}
}

func (b *botManager) processMessage(message *tgbotapi.Message) {
	b.logger.Named("processMessage").Debug(
		"Processing message",
		zap.String("chat_title", message.Chat.Title),
		zap.Int64("chat_id", message.Chat.ID),
	)
	if from := message.From; from != nil && !from.IsBot {
		b.logger.Named("processMessage").Debug("Processing message from user", zap.String("username", from.UserName), zap.String("first_name", from.FirstName), zap.String("last_name", from.LastName))
		user := model.User{
			FirstName: from.FirstName,
			LastName: func() *string {
				if from.LastName != "" {
					return &from.LastName
				} else {
					return nil
				}
			}(),
			TelegramID: from.ID,
			Username: func() *string {
				if from.UserName != "" {
					return &from.UserName
				} else {
					return nil
				}
			}(),
			Status: func() string {
				if message.Chat != nil && message.Chat.ID == b.groupID {
					return model.UserStatusActive
				}
				return model.UserStatusNew
			}(),
		}
		_, err := b.db.UpdateOrCreateUser(b.ctx, &user)
		if err != nil {
			b.logger.Named("processMessage").Error("Error while updating user", zap.Error(err))
			return
		}
	}

	// TODO: use switch
	if message.Chat != nil && message.Chat.Type == "private" {
		b.processPrivateMessage(message)
	}

	if message.Chat != nil && (message.Chat.Type == "group" || message.Chat.Type == "supergroup") {
		b.processGroupMessage(message)
	}
}
func (b *botManager) processPrivateMessage(message *tgbotapi.Message) {
	b.logger.Named("processPrivateMessage").Debug("Processing private message")
	if message.IsCommand() {
		b.processPrivateCommand(message)
		return
	}

	if message.Text == "ping" {
		b.processPing(message)
		return
	}
}

func (b *botManager) processGroupMessage(message *tgbotapi.Message) {
	b.logger.Named("processGroupMessage").Debug("Processing group message")
	if message.IsCommand() {
		b.processGroupCommand(message)
		return
	}
	if message.NewChatMembers != nil {
		b.processNewChatMembers(message)
		return
	}

	if message.LeftChatMember != nil {
		b.processLeftChatMember(message)
		return
	}

	if message.Text == "ping" {
		b.processPing(message)
		return
	}
}

func (b *botManager) processGroupCommand(message *tgbotapi.Message) {
	b.logger.Named("processGroupCommand").Debug("Processing group command", zap.String("command", message.Command()))
	if message.Command() == "info" {
		b.processInfoCommand(message)
		return
	}
}

func (b *botManager) processPing(message *tgbotapi.Message) {
	b.logger.Named("processPing").Debug("Processing ping")
	b.send(tgbotapi.NewMessage(message.Chat.ID, "pong"))
}

func (b *botManager) processPrivateCommand(message *tgbotapi.Message) {
	b.logger.Named("processPrivateCommand").Debug("Processing private command", zap.String("command", message.Command()))
	if message.Command() == "start" {
		b.processStartCommand(message)
		return
	}
	if message.Command() == "login" {
		b.processLoginCommand(message)
		return
	}
	if message.Command() == "get_backup" {
		b.processGetBackupCommand(message)
		return
	}
}

func (b *botManager) processGetBackupCommand(message *tgbotapi.Message) {
	if message.From == nil || message.From.ID != b.adminID {
		b.logger.Named("processGetBackupCommand").Warn("Backup command from non-administrator")
		return
	}
	if b.backup == nil {
		b.logger.Named("processGetBackupCommand").Error("Backup creator is not configured")
		b.send(tgbotapi.NewMessage(message.Chat.ID, "Создание резервной копии сейчас недоступно."))
		return
	}
	if !b.backupBusy.CompareAndSwap(false, true) {
		b.send(tgbotapi.NewMessage(message.Chat.ID, "Резервная копия уже создаётся. Подождите завершения."))
		return
	}

	b.send(tgbotapi.NewMessage(message.Chat.ID, "Создаю резервную копию базы данных…"))
	go func(chatID int64) {
		defer b.backupBusy.Store(false)
		backupContext, cancel := context.WithTimeout(b.ctx, 5*time.Minute)
		defer cancel()

		archive, err := b.backup.Create(backupContext)
		if err != nil {
			b.logger.Named("processGetBackupCommand").Error("Create database backup", zap.Error(err))
			b.send(tgbotapi.NewMessage(chatID, "Не удалось создать резервную копию базы данных."))
			return
		}

		document := tgbotapi.NewDocument(chatID, tgbotapi.FileBytes{Name: archive.Name, Bytes: archive.Bytes})
		document.Caption = "Резервная копия базы данных"
		b.send(document)
	}(message.Chat.ID)
}

func (b *botManager) processInfoCommand(message *tgbotapi.Message) {
	b.logger.Named("processInfoCommand").Debug("Processing info command")
	if message.ReplyToMessage == nil || message.ReplyToMessage.From == nil {
		b.logger.Named("processInfoCommand").Debug("No reply to message")
		b.send(tgbotapi.NewMessage(message.Chat.ID, b.templator.InfoCommandNoReply()))
		return
	}
	user, err := b.db.GetUserByTelegramID(b.ctx, message.ReplyToMessage.From.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			b.logger.Named("processInfoCommand").Info("No user found in db", zap.Error(err))
			b.send(tgbotapi.NewMessage(message.Chat.ID, b.templator.InfoCommandNoUser()))
			return
		}
		b.logger.Named("processInfoCommand").Error("Error while getting user from db", zap.Error(err))
		return
	}
	b.logger.Named("processInfoCommand").Debug("User found", zap.Stringp("username", user.Username), zap.Int64("telegram_id", user.TelegramID))
	form, err := b.db.GetActualForm(b.ctx, user.TelegramID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			b.logger.Named("processInfoCommand").Info("No form found in db", zap.Error(err))
			b.send(tgbotapi.NewMessage(message.Chat.ID, b.templator.InfoCommandNoUser()))
			return
		}
		b.logger.Named("processInfoCommand").Error("Error while getting user's form from db", zap.Error(err))
		return
	}
	b.logger.Named("processInfoCommand").Debug("Form found", zap.String("name", form.Name))
	msg := tgbotapi.NewMessage(message.Chat.ID, b.templator.InfoCommandReply(user, form))
	msg.ParseMode = tgbotapi.ModeHTML
	b.send(msg)
}

func (b *botManager) processLoginCommand(message *tgbotapi.Message) {
	b.logger.Named("processLoginCommand").Debug("Processing login command")
	if message.From == nil {
		b.logger.Named("processLoginCommand").Error("Message's From is nil")
		return
	}
	token, err := b.db.CreateOrProlongToken(b.ctx, message.From.ID)
	if err != nil {
		b.logger.Named("processLoginCommand").Error("Error while creating token", zap.Error(err))
		return
	}
	b.logger.Named("processLoginCommand").Debug("Token created")
	msg := tgbotapi.NewMessage(message.Chat.ID, b.templator.LoginCommandReply(token))
	msg.ParseMode = tgbotapi.ModeHTML
	b.send(msg)
}

func (b *botManager) processStartCommand(message *tgbotapi.Message) {
	b.logger.Named("processStartCommand").Debug("Processing start command")
	if message.From == nil {
		b.logger.Named("processStartCommand").Error("Message's From is nil")
		return
	}
	msg := tgbotapi.NewMessage(message.Chat.ID, b.templator.StartCommandReply())
	msg.ParseMode = tgbotapi.ModeHTML
	msg.DisableWebPagePreview = true
	b.send(msg)
}

func (b *botManager) processCallbackQuery(query *tgbotapi.CallbackQuery) {
	b.logger.Named("processCallbackQuery").Debug("Processing callback query", zap.String("data", query.Data))
	if query.Message == nil {
		b.logger.Named("processCallbackQuery").Error("Callback query's message is nil")
		return
	}
	if query.Message.Chat == nil {
		b.logger.Named("processCallbackQuery").Error("Callback query's message's chat is nil")
		return
	}
	if query.From == nil {
		b.logger.Named("processFormCallbackQuery").Error("Callback query's message's from is nil")
		return
	}
	switch {
	case strings.HasPrefix(query.Data, "admin:"):
		if query.From.ID != b.adminID {
			b.logger.Named("processCallbackQuery").Info("Callback query message is not from admin")
			return
		}
		var data string
		_, err := fmt.Sscanf(query.Data, "admin:%s", &data)
		if err != nil {
			b.logger.Named("processCallbackQuery").Error("Error while parsing admin callback query", zap.Error(err))
			return
		}
		switch {
		case strings.HasPrefix(data, "form:"):
			b.processFormCallbackQuery(query.Message.Chat.ID, query.Message.MessageID, data)
		case strings.HasPrefix(data, "user:"):
			b.processUserCallbackQuery(query.Message.Chat.ID, query.Message.MessageID, data)
		}
	default:
		return
	}

	// TODO: make a request via bot.send
	_, err := b.bot.MakeRequest("answerCallbackQuery", map[string]string{
		"callback_query_id": query.ID,
	})

	if err != nil {
		b.logger.Named("processFormCallbackQuery").Error("Error while answering callback query", zap.Error(err))
		return
	}

}

func (b *botManager) processFormCallbackQuery(chatID int64, messageID int, queryData string) {
	b.logger.Named("processFormCallbackQuery").Debug("Processing form callback query", zap.Int64("chatID", chatID), zap.Int("messageID", messageID), zap.String("queryData", queryData))
	var err error
	var data string
	_, err = fmt.Sscanf(queryData, "form:%s", &data)
	if err != nil {
		b.logger.Named("processFormCallbackQuery").Error("Error while parsing form callback query", zap.Error(err))
		return
	}

	if !strings.HasPrefix(data, "accept:") && !strings.HasPrefix(data, "reject:") {
		b.logger.Named("processFormCallbackQuery").Error("Form callback query's data is invalid")
		return
	}

	var formID uint
	var command string
	switch {
	case strings.HasPrefix(data, "accept:"):
		command = "accept"
		_, err = fmt.Sscanf(data, "accept:%d", &formID)
	case strings.HasPrefix(data, "reject:"):
		command = "reject"
		_, err = fmt.Sscanf(data, "reject:%d", &formID)
	}
	if err != nil {
		b.logger.Named("processFormCallbackQuery").Error("Error while parsing form callback query", zap.Error(err))
		return
	}

	form, err := b.db.GetFormByID(b.ctx, formID)
	if err != nil {
		b.logger.Named("sendNewFormToGroup").Error("Error while getting form", zap.Error(err))
		return
	}
	user, err := b.db.GetUserByTelegramID(b.ctx, form.UserTelegramId)
	if err != nil {
		b.logger.Named("sendNewFormToGroup").Error("Error while getting user", zap.Error(err))
		return
	}

	switch command {
	case "accept":
		_, err = b.db.AcceptForm(b.ctx, formID)
	case "reject":
		_, err = b.db.RejectForm(b.ctx, formID)
	}
	if err != nil {
		b.logger.Named("processFormCallbackQuery").Error("Error while changing form's status", zap.Error(err))
		return
	}

	switch command {
	case "accept":
		b.sendNewFormToGroup(user, form)
		acceptMsg := tgbotapi.NewMessage(user.TelegramID, b.templator.AcceptFormReply(user.Status))
		b.send(acceptMsg)
	case "reject":
		rejectMsg := tgbotapi.NewMessage(user.TelegramID, b.templator.RejectFormReply(user.Status))
		b.send(rejectMsg)
	}
	editKeyboard := tgbotapi.NewEditMessageReplyMarkup(chatID, messageID, tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}})
	b.send(editKeyboard)
}

func (b *botManager) sendNewFormToGroup(user *model.User, form *model.Form) {
	b.logger.Named("sendNewFormToGroup").Debug("Sending new form to group", zap.Int64("userTelegramID", user.TelegramID), zap.Uint("formID", form.ID))

	msg := tgbotapi.NewMessage(b.groupID, b.templator.NewFormMessage(user, form))
	msg.ParseMode = tgbotapi.ModeHTML

	// TODO: make a request in goroutine with returning value
	sentMessage, err := b.bot.Send(msg)
	if err != nil {
		b.logger.Named("sendNewFormToGroup").Error("Error while sending new form to group", zap.Error(err))
		return
	}
	if user.Status == model.UserStatusActive {
		b.logger.Named("sendNewFormToGroup").Debug("User is not new, skipping poll")
		return
	}
	poll := tgbotapi.NewPoll(b.groupID, b.templator.NewFormPoll(), "Принимаем", "Отклоняем")
	poll.ReplyToMessageID = sentMessage.MessageID
	acceptUser := tgbotapi.NewInlineKeyboardButtonData("Принять (для Эди)", fmt.Sprintf("admin:user:accept:%d", user.TelegramID))
	rejectUser := tgbotapi.NewInlineKeyboardButtonData("Отклонить (для Эди)", fmt.Sprintf("admin:user:reject:%d", user.TelegramID))
	poll.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(tgbotapi.NewInlineKeyboardRow(acceptUser, rejectUser))
	b.send(poll)
}

func (b *botManager) processUserCallbackQuery(chatID int64, messageID int, queryData string) {
	b.logger.Named("processUserCallbackQuery").Debug("Processing user callback query", zap.Int64("chatID", chatID), zap.Int("messageID", messageID), zap.String("queryData", queryData))
	var err error
	var data string
	_, err = fmt.Sscanf(queryData, "user:%s", &data)
	if err != nil {
		b.logger.Named("processUserCallbackQuery").Error("Error while parsing user callback query", zap.Error(err))
		return
	}

	if !strings.HasPrefix(data, "accept:") && !strings.HasPrefix(data, "reject:") {
		b.logger.Named("processUserCallbackQuery").Error("User callback query's data is invalid")
		return
	}

	var userID int64
	var command string
	switch {
	case strings.HasPrefix(data, "accept:"):
		command = "accept"
		_, err = fmt.Sscanf(data, "accept:%d", &userID)
	case strings.HasPrefix(data, "reject:"):
		command = "reject"
		_, err = fmt.Sscanf(data, "reject:%d", &userID)
	}
	if err != nil {
		b.logger.Named("processUserCallbackQuery").Error("Error while parsing user callback query", zap.Error(err))
		return
	}

	user, err := b.db.GetUserByTelegramID(b.ctx, userID)
	if err != nil {
		b.logger.Named("sendNewFormToGroup").Error("Error while getting user", zap.Error(err))
		return
	}

	switch command {
	case "accept":
		b.logger.Named("processUserCallbackQuery").Debug("Accepting user", zap.Int64("userTelegramID", user.TelegramID))
		_, err := b.db.AcceptUser(b.ctx, user.ID)
		if err != nil {
			b.logger.Named("processUserCallbackQuery").Error("Error while accepting user", zap.Error(err))
			return
		}
		acceptMsg := tgbotapi.NewMessage(user.TelegramID, b.templator.AcceptUserReply(b.inviteLink))
		acceptMsg.ParseMode = tgbotapi.ModeHTML
		b.send(acceptMsg)
		stopPoll := tgbotapi.NewStopPoll(chatID, messageID)
		b.send(stopPoll)
		acceptGroupMsg := tgbotapi.NewMessage(b.groupID, b.templator.AcceptUserGroupReply())
		acceptGroupMsg.ReplyToMessageID = messageID
		b.send(acceptGroupMsg)

	case "reject":
		b.logger.Named("processUserCallbackQuery").Debug("Rejecting user", zap.Int64("userTelegramID", user.TelegramID))
		_, err := b.db.RejectUser(b.ctx, user.ID)
		if err != nil {
			b.logger.Named("processUserCallbackQuery").Error("Error while rejecting user", zap.Error(err))
			return
		}
		rejectMsg := tgbotapi.NewMessage(user.TelegramID, b.templator.RejectUserReply())
		b.send(rejectMsg)
		stopPoll := tgbotapi.NewStopPoll(chatID, messageID)
		b.send(stopPoll)
		rejectGroupMsg := tgbotapi.NewMessage(b.groupID, b.templator.RejectUserGroupReply())
		rejectGroupMsg.ReplyToMessageID = messageID
		b.send(rejectGroupMsg)
	}
}

func (b *botManager) processChatJoinRequest(request *tgbotapi.ChatJoinRequest) {
	b.logger.Named("processChatJoinRequest").Debug("Processing chat join request")
	user, err := b.db.GetUserByTelegramID(b.ctx, request.From.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			b.logger.Named("processChatJoinRequest").Info("User is not in database")
			rejectRequest := tgbotapi.DeclineChatJoinRequest{
				ChatConfig: tgbotapi.ChatConfig{
					ChatID: request.Chat.ID,
				},
				UserID: request.From.ID,
			}
			b.send(rejectRequest)
			return
		}
		b.logger.Named("processChatJoinRequest").Error("Error while getting user", zap.Error(err))
		return
	}
	switch user.Status {
	case model.UserStatusActive, model.UserStatusAccepted, model.UserStatusNotActive:
		b.logger.Named("processChatJoinRequest").Debug("User accepted")
		acceptRequest := tgbotapi.ApproveChatJoinRequestConfig{
			ChatConfig: tgbotapi.ChatConfig{
				ChatID: request.Chat.ID,
			},
			UserID: request.From.ID,
		}
		b.send(acceptRequest)
		adminMsg := tgbotapi.NewMessage(b.adminID, fmt.Sprintf("Принял пользователя %v", request.From))
		b.send(adminMsg)
	default:
		b.logger.Named("processChatJoinRequest").Debug("User rejected")
		rejectRequest := tgbotapi.DeclineChatJoinRequest{
			ChatConfig: tgbotapi.ChatConfig{
				ChatID: request.Chat.ID,
			},
			UserID: request.From.ID,
		}
		b.send(rejectRequest)
		adminMsg := tgbotapi.NewMessage(b.adminID, fmt.Sprintf("Отклонил пользователя %v", request.From))
		b.send(adminMsg)
	}
}

func (b *botManager) processNewChatMembers(message *tgbotapi.Message) {
	b.logger.Named("processNewChatMembers").Debug("Processing new chat members")
	for _, user := range message.NewChatMembers {
		_, err := b.db.UpdateOrCreateUser(b.ctx, &model.User{
			TelegramID: user.ID,
			Status:     model.UserStatusActive,
		})
		if err != nil {
			b.logger.Named("processNewChatMembers").Error("Error while updating user", zap.Error(err))
			return
		}
	}
	if len(message.NewChatMembers) > 1 {
		b.logger.Named("processNewChatMembers").Debug("More than one user joined")
		return
	}
	msg := tgbotapi.NewMessage(b.groupID, b.templator.NewChatMember())
	msg.ReplyToMessageID = message.MessageID
	b.send(msg)
}

func (b *botManager) processLeftChatMember(message *tgbotapi.Message) {
	b.logger.Named("processLeftChatMember").Debug("Processing left chat member")
	_, err := b.db.UpdateOrCreateUser(b.ctx, &model.User{
		TelegramID: message.LeftChatMember.ID,
		Status:     model.UserStatusNotActive,
	})
	if err != nil {
		b.logger.Named("processLeftChatMember").Error("Error while updating user", zap.Error(err))
		return
	}
}
