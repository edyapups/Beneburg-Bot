package model

import "time"

const TableNameUser = "users"

const (
	UserStatusNew       = "new"
	UserStatusActive    = "active"
	UserStatusNotActive = "not_active"
	UserStatusAccepted  = "accepted"
	UserStatusRejected  = "rejected"
	UserStatusBot       = "bot"
	UserStatusBanned    = "banned"
)

type User struct {
	ID         uint       `json:"id"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	DeletedAt  *time.Time `json:"deleted_at,omitempty"`
	TelegramID int64      `json:"telegram_id"`
	Username   *string    `json:"username"`
	FirstName  string     `json:"first_name"`
	LastName   *string    `json:"last_name"`
	Status     string     `json:"status"`
}

const (
	UserTelegramIDDescription = "Telegram ID"
	UserUsernameDescription   = "Username"
)
