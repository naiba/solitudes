package solitudes

import "time"

// Comment 评论表
type Comment struct {
	ID        string `gorm:"type:uuid;primary_key;default:uuid_generate_v4()"`
	CreatedAt time.Time
	UpdatedAt time.Time

	ReplyTo   *string `gorm:"type:uuid;index;default:NULL" form:"reply_to"`
	Nickname  string  `form:"nickname" binding:"required"`
	Content   string  `form:"content" binding:"required" gorm:"text"`
	Website   string  `form:"website"`
	Version   uint    `form:"-"`
	Email     string  `form:"email"`
	IP        string  `gorm:"inet"`
	UserAgent string
	IsAdmin   bool

	ArticleID     *string `gorm:"type:uuid;index;default:NULL" form:"article_id" binding:"required,uuid"`
	Article       *Article
	ChildComments []*Comment `gorm:"foreignkey:ReplyTo" form:"-" binding:"-"`
}
