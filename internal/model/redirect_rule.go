package model

import "time"

// RedirectRule either stores a manual destination or follows an article's current
// slug. ArticleID is deliberately separate from the target URL so repeated
// renames never leave chains of stale article destinations.
type RedirectRule struct {
	ID         uint64  `gorm:"primaryKey"`
	SourcePath string  `gorm:"size:2048;not null;uniqueIndex"`
	TargetPath string  `gorm:"type:text;not null;default:''"`
	ArticleID  *string `gorm:"type:uuid;index"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}
