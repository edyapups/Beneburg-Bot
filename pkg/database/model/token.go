package model

import "time"

const TableNameToken = "tokens"

type Token struct {
	UUID           string    `json:"uuid"`
	UserTelegramId int64     `json:"user_telegram_id"`
	User           User      `json:"user"`
	ExpireAt       time.Time `json:"expire_at"`
}
