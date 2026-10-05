package model

import (
	"fmt"
	"time"
)

// ArticleHistory 文章修订历史
type ArticleHistory struct {
	ArticleID string  `gorm:"type:uuid;not null;uniqueIndex:idx_article_history_version,priority:1"`
	EditorID  *string `gorm:"type:uuid;index"`
	// EditorID is the actor archiving this revision; UpdatedByID identifies
	// the last editor of the archived content, which may be a different person.
	UpdatedByID *string `gorm:"type:uuid"`
	Article     Article
	Version     uint   `gorm:"not null;uniqueIndex:idx_article_history_version,priority:2"`
	Title       string `gorm:"type:text"`
	Desc        string `gorm:"text"`
	Content     string `gorm:"text"`
	CreatedAt   time.Time
	UpdatedAt   time.Time `gorm:"autoUpdateTime:false"`
}

// GetIndexID get index data id
func (t *ArticleHistory) GetIndexID() string {
	return fmt.Sprintf("%s.%d", t.ArticleID, t.Version)
}
