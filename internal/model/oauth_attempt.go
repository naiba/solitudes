package model

import "time"

// OAuthAttempt binds the state parameter to a browser cookie and a short-lived
// PKCE verifier. The state is stored hashed to prevent replay after DB leaks.
type OAuthAttempt struct {
	ID        string  `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	StateHash string  `gorm:"uniqueIndex;size:64;not null"`
	Provider  string  `gorm:"not null"`
	Verifier  string  `gorm:"not null"`
	Nonce     string  `gorm:"not null"`
	AccountID *string `gorm:"type:uuid"`
	ReturnTo  string
	ExpiresAt time.Time `gorm:"not null"`
	CreatedAt time.Time
}
