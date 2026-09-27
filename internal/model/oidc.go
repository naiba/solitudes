package model

import "time"

// OIDCClient is explicitly registered by an administrator. No dynamic client
// registration or wildcard redirect URIs are accepted.
type OIDCClient struct {
	ID               string  `gorm:"primaryKey"`
	OwnerID          *string `gorm:"type:uuid;index"`
	Name             string  `gorm:"not null"`
	SecretHash       string  `json:"-"`
	Public           bool
	RedirectURIsJSON string `gorm:"type:text;not null" json:"-"`
	DisabledAt       *time.Time
	CreatedAt        time.Time
}

type OIDCAuthRequest struct {
	ID          string  `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	ClientID    string  `gorm:"not null;index"`
	AccountID   *string `gorm:"type:uuid;index"`
	RequestJSON []byte  `gorm:"type:bytea;not null"`
	CodeHash    *string `gorm:"uniqueIndex"`
	CodeUsedAt  *time.Time
	Approved    bool
	AuthTime    *time.Time
	ExpiresAt   time.Time `gorm:"not null;index"`
	CreatedAt   time.Time
}

type OIDCAccessToken struct {
	ID        string    `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	ClientID  string    `gorm:"not null;index"`
	AccountID string    `gorm:"type:uuid;index"`
	Scopes    string    `gorm:"type:text"`
	ExpiresAt time.Time `gorm:"not null;index"`
	CreatedAt time.Time
}

type OIDCRefreshToken struct {
	ID        string `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	TokenHash string `gorm:"uniqueIndex;size:64;not null"`
	ClientID  string `gorm:"not null;index"`
	AccountID string `gorm:"type:uuid;index"`
	AccessID  string `gorm:"type:uuid"`
	Scopes    string `gorm:"type:text"`
	AuthTime  time.Time
	ExpiresAt time.Time `gorm:"not null;index"`
}

type OIDCSigningKey struct {
	ID        string `gorm:"primaryKey"`
	KeyDER    []byte `gorm:"type:bytea;not null"`
	Active    bool   `gorm:"index"`
	CreatedAt time.Time
}

type OIDCCryptoKey struct {
	ID  string `gorm:"primaryKey"`
	Key []byte `gorm:"type:bytea;not null"`
}
