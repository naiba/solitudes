package solitudes

import "time"

// ArticleHistory 文章修订历史
type ArticleHistory struct {
	ArticleID uint
	Article   Article
	Version   uint   `gorm:"index"`
	Desc      string `gorm:"text"`
	Content   string `gorm:"text"`
	CreatedAt time.Time
}
