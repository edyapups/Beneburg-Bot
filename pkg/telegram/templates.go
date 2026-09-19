package telegram

import (
	"beneburg/pkg/database/model"
	"fmt"
	"html"
	"strings"
)

type Templator interface {
	InfoCommandNoReply() string
	InfoCommandNoUser() string
	FormInfo(form *model.Form) string
	UserIdWithHref(user *model.User) string
	InfoCommandReply(user *model.User, form *model.Form) string
	LoginCommandReply(token *model.Token) string
	StartCommandReply() string
	NewFormMessage(user *model.User, form *model.Form) string
	NewFormPoll() string
	AcceptFormReply(userStatus string) string
	FormReceived() string
	RejectFormReply(userStatus string) string
	RejectUserReply() string
	AcceptUserReply(link string) string
	AcceptUserGroupReply() string
	RejectUserGroupReply() string
	NewChatMember() string
}

var _ Templator = templator{}

type templator struct {
	domain string
}

func (t templator) NewChatMember() string {
	return "Привет! Добро пожаловать! 🎉"
}

func (t templator) RejectUserGroupReply() string {
	return "Анкета отклонена."
}

func (t templator) AcceptUserGroupReply() string {
	return "Анкета одобрена, отправил приглашение участнику."
}

func (t templator) AcceptUserReply(link string) string {
	stringBuilder := strings.Builder{}
	stringBuilder.WriteString("Ура, твоя анкета была успешно одобрена и мы рады пргласить тебя к нам! 🎉\n")
	stringBuilder.WriteString("Теперь нажми ")
	stringBuilder.WriteString(fmt.Sprintf("<a href=\"%s\">сюда</a>", link))
	stringBuilder.WriteString(" и подай заявку на вступление.")
	return stringBuilder.String()
}

func (t templator) RejectUserReply() string {
	return "Извини, но сейчас мы не готовы принять тебя в чатик. Надеемся, что тебя это не очень расстроило 😥"
}

func (t templator) RejectFormReply(userStatus string) string {
	if userStatus == model.UserStatusActive {
		return "Анкетку отклонили."
	}
	return "Извини, но сейчас мы не готовы принять тебя в чатик. Надеемся, что тебя это не очень расстроило 😥"
}

func (t templator) FormReceived() string {
	return "Мы получили твою анкету, она была отправлена на проверку администратором."
}

func (t templator) AcceptFormReply(userStatus string) string {
	if userStatus == model.UserStatusActive {
		return "Окей, одобрили."
	}
	return "Привет, твоя анкета одобрена администратором и была отправлена в чат на голосование. Результат ожидай в ближайшие сутки 🙃"
}

func (t templator) NewFormPoll() string {
	return "Принимаем участника?"
}

func (t templator) NewFormMessage(user *model.User, form *model.Form) string {
	stringBuilder := strings.Builder{}
	if user.Status == model.UserStatusActive {
		stringBuilder.WriteString("<b>")
		stringBuilder.WriteString(fmt.Sprintf("<a href=\"tg://user?id=%d\">Участник</a> ", user.TelegramID))
		stringBuilder.WriteString("изменил анкету:")
		stringBuilder.WriteString("</b>")
	} else {
		stringBuilder.WriteString("<b>Новая анкета!</b>")
	}

	AddDelimiter(&stringBuilder)
	stringBuilder.WriteString(t.FormInfo(form))
	AddDelimiter(&stringBuilder)
	stringBuilder.WriteString(t.UserIdWithHref(user))

	return stringBuilder.String()
}

func (t templator) StartCommandReply() string {
	stringBuilder := strings.Builder{}
	stringBuilder.WriteString("Привет! Я бот, который поможет тебе отправить анкетку в чат.\n")
	stringBuilder.WriteString("Напиши мне /login, чтобы получить ссылку для входа на сайт.")
	AddDelimiter(&stringBuilder)
	stringBuilder.WriteString(fmt.Sprintf("<i>%s</i>", "(бот находится в ранней стадии разработки, возможны ошибки, если столкнёшься с ними, напиши "))
	stringBuilder.WriteString(fmt.Sprintf("<a href=\"%s\">сюда</a>", "https://t.me/edyapups"))
	stringBuilder.WriteString(fmt.Sprintf("<i>%s</i>", ")"))
	return stringBuilder.String()
}

func (t templator) LoginCommandReply(token *model.Token) string {
	stringBuilder := strings.Builder{}
	stringBuilder.WriteString("Вот твоя ссылка для входа:\n")
	stringBuilder.WriteString(html.EscapeString(t.URLFromToken(token.UUID)))
	return stringBuilder.String()
}

func (t templator) InfoCommandNoUser() string {
	return "У меня нет информации об этом пользователе"
}

func (t templator) InfoCommandReply(user *model.User, form *model.Form) string {
	stringBuilder := strings.Builder{}
	stringBuilder.WriteString(fmt.Sprintf("<b>Профиль участника:</b>\n%s", html.EscapeString(t.UserPageLink(user))))
	AddDelimiter(&stringBuilder)
	stringBuilder.WriteString(t.UserIdWithHref(user))

	return stringBuilder.String()
}

func (t templator) UserIdWithHref(user *model.User) string {
	return fmt.Sprintf(
		"<i>ID <a href=\"tg://user?id=%d\">пользователя</a>: </i><code>%d</code>",
		user.TelegramID,
		user.TelegramID,
	)
}

func (t templator) UserPageLink(user *model.User) string {
	return fmt.Sprintf("%s/user/%d", t.domain, user.TelegramID)
}

func (t templator) FormInfo(form *model.Form) string {
	stringBuilder := strings.Builder{}
	stringBuilder.WriteString(fmt.Sprintf("<b>%s</b>:\n%s", model.UserNameDescription, html.EscapeString(form.Name)))
	if form.BirthDate != nil {
		AddDelimiter(&stringBuilder)
		stringBuilder.WriteString(fmt.Sprintf("<b>%s</b>:\n%s", model.UserBirthDateDescription, form.BirthDate.Format("02.01.2006")))
	}
	AddDelimiter(&stringBuilder)
	stringBuilder.WriteString(fmt.Sprintf("<b>%s</b>:\n%s", model.UserGenderDescription, html.EscapeString(form.Gender)))
	if form.About != nil && *form.About != "" {
		AddDelimiter(&stringBuilder)
		stringBuilder.WriteString(fmt.Sprintf("<b>%s</b>:\n%s", model.UserAboutDescription, html.EscapeString(*form.About)))
	}
	if form.Hobbies != nil && *form.Hobbies != "" {
		AddDelimiter(&stringBuilder)
		stringBuilder.WriteString(fmt.Sprintf("<b>%s</b>:\n%s", model.UserHobbiesDescription, html.EscapeString(*form.Hobbies)))
	}
	if form.Work != nil && *form.Work != "" {
		AddDelimiter(&stringBuilder)
		stringBuilder.WriteString(fmt.Sprintf("<b>%s</b>:\n%s", model.UserWorkDescription, html.EscapeString(*form.Work)))
	}
	if form.Education != nil && *form.Education != "" {
		AddDelimiter(&stringBuilder)
		stringBuilder.WriteString(fmt.Sprintf("<b>%s</b>:\n%s", model.UserEducationDescription, html.EscapeString(*form.Education)))
	}
	if form.CoverLetter != nil && *form.CoverLetter != "" {
		AddDelimiter(&stringBuilder)
		stringBuilder.WriteString(fmt.Sprintf("<b>%s</b>:\n%s", model.UserCoverLetterDescription, html.EscapeString(*form.CoverLetter)))
	}
	if form.Contacts != nil && *form.Contacts != "" {
		AddDelimiter(&stringBuilder)
		stringBuilder.WriteString(fmt.Sprintf("<b>%s</b>:\n%s", model.UserContactsDescription, html.EscapeString(*form.Contacts)))
	}
	return stringBuilder.String()
}

func AddDelimiter(stringBuilder *strings.Builder) {
	stringBuilder.WriteString("\n\n")
}

func (t templator) InfoCommandNoReply() string {
	return "Для получения информации об участнике необходимо ответить на его сообщение командой /info"
}

func NewTemplator(domain string) Templator {
	return &templator{
		domain: domain,
	}
}

func (t templator) URLFromToken(token string) string {
	return fmt.Sprintf("%s/login/%s", t.domain, token)
}
