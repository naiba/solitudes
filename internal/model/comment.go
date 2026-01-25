package model

import "time"

// Comment 评论表
type Comment struct {
	ID        string    `gorm:"type:uuid;primary_key;default:uuid_generate_v4()"`
	CreatedAt time.Time `gorm:"index"`

	ReplyTo   *string `gorm:"type:uuid;index;default:NULL" form:"reply_to"`
	Nickname  string  `form:"nickname" validate:"required" gorm:"index:idx_nickname;index:idx_nickname_email"`
	Content   string  `form:"content" validate:"required" gorm:"text"`
	Website   string  `form:"website"`
	Version   uint    `form:"-"`
	Email     string  `form:"email" gorm:"index:idx_email;index:idx_nickname_email"`
	IP        string  `gorm:"inet"`
	UserAgent string
	IsAdmin   bool
	// EmailReadStatus tracks email notification read status: nil (not sent/not applicable), "unread", "read"
	EmailReadStatus *string `gorm:"type:varchar(20);default:NULL"`
	// EmailTrackingToken is used to verify email tracking requests (prevents spoofing)
	EmailTrackingToken *string `gorm:"type:varchar(255);default:NULL;uniqueIndex:idx_email_tracking_token"`

	ArticleID     *string `gorm:"type:uuid;index;default:NULL" form:"article_id" validate:"required,uuid"`
	Article       *Article
	ChildComments []*Comment `gorm:"foreignkey:ReplyTo" form:"-" validate:"-"`
}

// TableName specifies the table name for Comment model
func (Comment) TableName() string {
	return "comments"
}
