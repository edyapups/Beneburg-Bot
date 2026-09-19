package model

import (
	"fmt"
	"time"
)

const TableNameForm = "forms"

const (
	FormStatusNew      = "new"
	FormStatusAccepted = "accepted"
	FormStatusRejected = "rejected"
)

type Form struct {
	ID             uint       `json:"id"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	DeletedAt      *time.Time `json:"deleted_at,omitempty"`
	UserTelegramId int64      `json:"user_telegram_id"`
	User           User       `json:"user"`

	Name        string     `json:"name"`
	BirthDate   *time.Time `json:"birth_date"`
	Gender      string     `json:"gender"`
	About       *string    `json:"about"`
	Hobbies     *string    `json:"hobbies"`
	Work        *string    `json:"work"`
	Education   *string    `json:"education"`
	CoverLetter *string    `json:"cover_letter"`
	Contacts    *string    `json:"contacts"`

	Status string `json:"status"`
}

// Age returns the user's age in full years as of today.
func (u *Form) Age() int {
	return u.AgeAt(time.Now())
}

// AgeText returns the user's age with the correct Russian declension.
func (u *Form) AgeText() string {
	age := u.Age()
	return fmt.Sprintf("%d %s", age, ageSuffix(age))
}

// AgeAt returns the user's age in full years on the supplied date.
// It compares calendar dates, so the age increases on the birthday itself.
func (u *Form) AgeAt(date time.Time) int {
	if u == nil || u.BirthDate == nil {
		return 0
	}

	age := date.Year() - u.BirthDate.Year()
	if date.Month() < u.BirthDate.Month() || (date.Month() == u.BirthDate.Month() && date.Day() < u.BirthDate.Day()) {
		age--
	}
	return age
}

func ageSuffix(age int) string {
	lastTwoDigits := age % 100
	if lastTwoDigits >= 11 && lastTwoDigits <= 14 {
		return "лет"
	}

	switch age % 10 {
	case 1:
		return "год"
	case 2, 3, 4:
		return "года"
	default:
		return "лет"
	}
}

func (u *Form) RuGender() string {
	if u == nil {
		return ""
	}
	switch u.Gender {
	case "male":
		return "мужской"
	case "female":
		return "женский"
	case "nonbinary":
		return "небинарный"
	default:
		return "не указан"
	}
}

const (
	UserNameDescription        = "Имя"
	UserBirthDateDescription   = "Дата рождения"
	UserGenderDescription      = "Пол"
	UserAboutDescription       = "О себе"
	UserHobbiesDescription     = "Хобби"
	UserWorkDescription        = "Работа"
	UserEducationDescription   = "Образование"
	UserCoverLetterDescription = "Почему хочет к нам?"
	UserContactsDescription    = "Контакты"
)
