package solitudes

import (
	"github.com/jinzhu/gorm"
)

// Article 文章表
type Article struct {
	gorm.Model
	TemplateID byte
	Slug       string `gorm:"unique_index"`

	CollectionID  uint
	IsCollection  bool
	ReadingNumber uint
	Title         string
	Content       string `gorm:"text"`

	UserID uint
	User   User

	Comments []Comment
}
