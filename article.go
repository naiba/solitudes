package solitudes

import (
	"strings"

	"github.com/jinzhu/gorm"
	"github.com/lib/pq"
)

// Article 文章表
type Article struct {
	gorm.Model
	Slug    string `form:"slug" binding:"required" gorm:"unique_index" json:"slug,omitempty"`
	Title   string `form:"title" binding:"required" json:"title,omitempty"`
	Content string `form:"content" binding:"required" gorm:"text" json:"content,omitempty"`

	TemplateID   byte           `form:"template" binding:"required" json:"template_id,omitempty"`
	CollectionID uint           `form:"collection_id" gorm:"index" json:"collection_id,omitempty"`
	IsCollection bool           `form:"is_collection" json:"is_collection,omitempty"`
	RawTags      string         `form:"tags" gorm:"-" json:"-"`
	Tags         pq.StringArray `gorm:"index;type:varchar(255)[]" json:"tags,omitempty"`

	ReadingNumber uint      `json:"reading_number,omitempty"`
	Comments      []Comment `json:"comments,omitempty"`
}

// BeforeSave hook
func (t *Article) BeforeSave() {
	t.Tags = strings.Split(t.RawTags, ",")
}

// AfterFind hook
func (t *Article) AfterFind() {
	t.RawTags = strings.Join(t.Tags, ",")
}
