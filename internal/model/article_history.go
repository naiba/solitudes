package model

import (
	"fmt"
	"time"
)

// ArticleHistory 文章修订历史
type ArticleHistory struct {
	ArticleID string  `gorm:"type:uuid;not null;uniqueIndex:idx_article_history_version,priority:1"`
	EditorID  *string `gorm:"type:uuid;index"`
	Article   Article
	Version   uint   `gorm:"not null;uniqueIndex:idx_article_history_version,priority:2"`
	Desc      string `gorm:"text"`
	Content   string `gorm:"text"`
	CreatedAt time.Time
}

// GetIndexID get index data id
func (t *ArticleHistory) GetIndexID() string {
	return fmt.Sprintf("%s.%d", t.ArticleID, t.Version)
}
