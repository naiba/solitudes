package model

import "time"

type FeedVisit struct {
	ID        string    `gorm:"type:uuid;primary_key;default:uuid_generate_v4()"`
	IP        string    `gorm:"inet;index"`
	CreatedAt time.Time `gorm:"index"`
}
