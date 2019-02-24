package solitudes

import (
	"github.com/jinzhu/gorm"
)

// Comment 评论表
type Comment struct {
	gorm.Model

	ReplyTo   uint   `form:"reply_to" json:"reply_to,omitempty"`
	Nickname  string `form:"nickname" binding:"required" json:"name,omitempty"`
	Content   string `form:"content" binding:"required" gorm:"text" json:"content,omitempty"`
	Website   string `form:"website" json:"website,omitempty"`
	Email     string `form:"email" json:"email,omitempty"`
	IP        string `gorm:"inet" json:"ip,omitempty"`
	UserAgent string `json:"user_agent,omitempty"`
	IsAdmin   bool   `json:"is_admin,omitempty"`

	ArticleID uint    `form:"article_id" binding:"required" gorm:"index" json:"article_id,omitempty"`
	Article   Article `json:"article,omitempty"`
}
