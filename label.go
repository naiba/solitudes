package solitudes

import (
	"github.com/jinzhu/gorm"
)

// Label 标签
type Label struct {
	gorm.Model
	Name string
	Slug string `gorm:"unique_index"`

	ArticleLabels []ArticleLabel
}
