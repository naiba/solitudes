package model

import "time"

// AuditEvent is append-only through the application. It deliberately has no
// request/response payload, credential, cookie, email or arbitrary error field.
// Login totals combine these events with compacted LoginSummary rows, never tokens.
type AuditEvent struct {
	ID            string    `gorm:"primaryKey;size:36"`
	CreatedAt     time.Time `gorm:"not null"`
	Action        string    `gorm:"size:64;index;not null"`
	Outcome       string    `gorm:"size:16;not null"`
	ActorID       string    `gorm:"size:64;index"`
	ClientID      string    `gorm:"size:64;index"`
	TargetID      string    `gorm:"size:64;index"`
	RequestID     string    `gorm:"size:36;index"`
	AuthRequestID *string   `gorm:"size:64;uniqueIndex"`
	Method        string    `gorm:"size:16"`
	Route         string    `gorm:"size:128"`
	Status        int
	IP            string `gorm:"size:64"`
	Reason        string `gorm:"size:64"`
	Details       string `gorm:"size:256"`
}

// LoginSummary preserves lifetime counts when detailed audit events expire.
// One row per action/application/account, not per login. Deliberately no foreign
// keys: deleting an identity must not erase historical accounting.
type LoginSummary struct {
	Action      string    `gorm:"size:64;primaryKey"`
	ClientID    string    `gorm:"size:64;primaryKey"`
	ActorID     string    `gorm:"size:64;primaryKey"`
	LoginCount  int64     `gorm:"not null;check:login_summary_count,login_count > 0"`
	LastLoginAt time.Time `gorm:"not null"`
}
