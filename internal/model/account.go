package model

import "time"

type Role string

const (
	RoleAdmin  Role = "admin"
	RoleEditor Role = "editor"
	RoleUser   Role = "user"
)

func (r Role) CanPublish() bool { return r == RoleAdmin || r == RoleEditor }
func (r Role) IsAdmin() bool    { return r == RoleAdmin }

// Account is the sole persisted login identity and credential source.
type Account struct {
	ID       string `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()" json:"id"`
	Email    string `gorm:"uniqueIndex;not null" json:"email"`
	Nickname string `gorm:"not null" json:"nickname"`
	Bio      string `gorm:"type:text" json:"bio,omitempty"`
	// Accounts participate in the reader circle unless they opt out.
	DirectoryHidden bool       `gorm:"not null;default:false" json:"-"`
	PasswordHash    string     `json:"-"`
	Role            Role       `gorm:"not null;default:user" json:"role"`
	EmailVerifiedAt *time.Time `json:"email_verified_at,omitempty"`
	DisabledAt      *time.Time `json:"-"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"-"`
}

type LoginSession struct {
	ID        string    `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	AccountID string    `gorm:"type:uuid;not null;index"`
	TokenHash string    `gorm:"size:64;uniqueIndex;not null"`
	ExpiresAt time.Time `gorm:"not null;index"`
	CreatedAt time.Time
}

type EmailAction struct {
	ID        string    `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	AccountID string    `gorm:"type:uuid;not null;index"`
	TokenHash string    `gorm:"size:64;uniqueIndex;not null"`
	Purpose   string    `gorm:"not null"`
	ExpiresAt time.Time `gorm:"not null;index"`
	UsedAt    *time.Time
	CreatedAt time.Time
}

type ExternalIdentity struct {
	ID        string `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	AccountID string `gorm:"type:uuid;not null;index"`
	Provider  string `gorm:"uniqueIndex:identity_provider_subject;not null"`
	Subject   string `gorm:"uniqueIndex:identity_provider_subject;not null"`
}

type Passkey struct {
	ID              string `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	AccountID       string `gorm:"type:uuid;not null;index"`
	CredentialID    []byte `gorm:"type:bytea;uniqueIndex;not null"`
	PublicKey       []byte `gorm:"type:bytea;not null"`
	AAGUID          []byte `gorm:"type:bytea"`
	SignCount       uint32
	UserPresent     bool
	UserVerified    bool
	BackupEligible  bool
	BackupState     bool
	AttestationType string
	Name            string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type PasskeyCeremony struct {
	ID          string    `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	AccountID   string    `gorm:"type:uuid;not null"`
	CookieHash  string    `gorm:"size:64;uniqueIndex;not null"`
	Purpose     string    `gorm:"not null"`
	SessionJSON []byte    `gorm:"type:bytea;not null"`
	ExpiresAt   time.Time `gorm:"not null;index"`
}
