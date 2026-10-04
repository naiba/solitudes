package model

import "time"

// Comment 评论表
type Comment struct {
	ID        string    `gorm:"type:uuid;primary_key;default:uuid_generate_v4()"`
	CreatedAt time.Time `gorm:"index"`

	ReplyTo   *string  `gorm:"type:uuid;index;default:NULL" form:"reply_to"`
	Nickname  string   `form:"nickname" validate:"required" gorm:"index:idx_nickname_email"`
	Content   string   `form:"content" validate:"required" gorm:"text"`
	Website   string   `form:"website"`
	Version   uint     `form:"-"`
	Email     string   `form:"email" gorm:"index:idx_email;index:idx_nickname_email"`
	AccountID *string  `gorm:"type:uuid;index;default:NULL" form:"-"`
	Account   *Account `gorm:"foreignKey:AccountID" form:"-"`
	IP        string   `gorm:"inet"`
	UserAgent string
	IsSpam    bool `gorm:"not null;default:false;index"`
	// EmailReadStatus tracks email notification read status: nil (not sent/not applicable), "unread", "read"
	EmailReadStatus *string `gorm:"type:varchar(20);default:NULL"`
	// EmailTrackingToken is used to verify email tracking requests (prevents spoofing)
	EmailTrackingToken *string `gorm:"type:varchar(255);default:NULL;uniqueIndex:idx_email_tracking_token"`

	ArticleID     *string `gorm:"type:uuid;index;default:NULL" form:"article_id" validate:"required,uuid"`
	Article       *Article
	ReplyCount    int64      `gorm:"-" form:"-"`
	ChildComments []*Comment `gorm:"foreignkey:ReplyTo" form:"-" validate:"-"`
}

// TableName specifies the table name for Comment model
func (Comment) TableName() string {
	return "comments"
}

// CountsTowardArticle reports whether the comment contributes to the public
// root-comment count stored on its article.
func (c Comment) CountsTowardArticle() bool {
	return c.ReplyTo == nil && !c.IsSpam
}

// PublicRole is derived from an authenticated account, never from a visitor's
// nickname or email.
func (c Comment) PublicRole() string {
	if c.AccountID != nil && c.Account != nil {
		switch c.Account.Role {
		case RoleAdmin, RoleEditor, RoleUser:
			return string(c.Account.Role)
		}
	}
	return "guest"
}

func (c Comment) PublicName() string {
	if c.AccountID != nil && c.Account != nil && c.Account.Nickname != "" {
		return c.Account.Nickname
	}
	return c.Nickname
}
