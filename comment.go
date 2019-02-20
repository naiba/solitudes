package solitudes

import (
	"github.com/jinzhu/gorm"
)

// Comment 评论表
type Comment struct {
	gorm.Model
	ReplayTo uint

	Name      string
	Website   string
	Email     string
	Content   string `gorm:"text"`
	IP        string `gorm:"inet"`
	UserAgent string

	ArticleID uint
	Article   Article

	AuthorID uint `gorm:"index"`
	Author   User
}
