package solitudes

import "time"

// Comment 评论表
type Comment struct {
	ID        string `gorm:"type:uuid;primary_key;default:uuid_generate_v4()"`
	CreatedAt time.Time
	DeletedAt time.Time

	ReplyTo   uint   `form:"reply_to" json:"reply_to,omitempty"`
	Nickname  string `form:"nickname" binding:"required" json:"name,omitempty"`
	Content   string `form:"content" binding:"required" gorm:"text" json:"content,omitempty"`
	Website   string `form:"website" json:"website,omitempty"`
	Version   uint   `form:"-"`
	Email     string `form:"email" json:"email,omitempty"`
	IP        string `gorm:"inet" json:"ip,omitempty"`
	UserAgent string `json:"user_agent,omitempty"`
	IsAdmin   bool   `json:"is_admin,omitempty"`

	ArticleID     uint       `form:"article_id" binding:"required" gorm:"index" json:"article_id,omitempty"`
	Article       *Article   `json:"article,omitempty"`
	ChildComments []*Comment `gorm:"foreignkey:ReplyTo" form:"-" binding:"-"`
}
