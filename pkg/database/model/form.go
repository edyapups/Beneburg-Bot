package model

import "time"

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

	Name        string  `json:"name"`
	Age         *int32  `json:"age"`
	Gender      string  `json:"gender"`
	About       *string `json:"about"`
	Hobbies     *string `json:"hobbies"`
	Work        *string `json:"work"`
	Education   *string `json:"education"`
	CoverLetter *string `json:"cover_letter"`
	Contacts    *string `json:"contacts"`

	Status string `json:"status"`
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
	UserAgeDescription         = "Возраст"
	UserGenderDescription      = "Пол"
	UserAboutDescription       = "О себе"
	UserHobbiesDescription     = "Хобби"
	UserWorkDescription        = "Работа"
	UserEducationDescription   = "Образование"
	UserCoverLetterDescription = "Почему хочет к нам?"
	UserContactsDescription    = "Контакты"
)
