package router

import (
	"crypto/rand"
	"encoding/hex"
	"errors"

	"github.com/adtac/go-akismet/akismet"
	"github.com/gofiber/fiber/v2"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/notify"
	"gorm.io/gorm"
)

type commentForm struct {
	ReplyTo  *string `json:"reply_to" validate:"omitempty,uuid4"`
	Nickname string  `json:"nickname" validate:"required"`
	Content  string  `json:"content" validate:"required" gorm:"text"`
	Slug     string  `json:"slug" validate:"required" gorm:"index"`
	Website  string  `json:"website" validate:"omitempty,url"`
	Version  uint    `json:"version" validate:"required"`
	Email    string  `json:"email" validate:"omitempty,email"`
}

func commentHandler(c *fiber.Ctx) error {
	isAdmin := c.Locals(solitudes.CtxAuthorized).(bool)
	var cf commentForm
	if err := c.BodyParser(&cf); err != nil {
		return err
	}
	if err := validator.StructCtx(c.Context(), &cf); err != nil {
		return err
	}

	article, err := verifyArticle(&cf)
	if err != nil {
		return err
	}

	commentType, replyTo, err := getCommentType(&cf)
	if err != nil {
		return err
	}

	// akismet anti spam
	if solitudes.System.Config.Akismet != "" && !isAdmin {
		isSpam, err := akismet.Check(&akismet.Comment{
			Blog:               "https://" + solitudes.System.Config.Site.Domain, // required
			UserIP:             c.IP(),                                           // required
			UserAgent:          string(c.Request().Header.UserAgent()),           // required
			CommentType:        commentType,
			Referrer:           string(c.Request().Header.Referer()),
			Permalink:          "https://" + solitudes.System.Config.Site.Domain + "/" + cf.Slug,
			CommentAuthor:      cf.Nickname,
			CommentAuthorEmail: cf.Email,
			CommentAuthorURL:   cf.Website,
			CommentContent:     cf.Content,
		}, solitudes.System.Config.Akismet)
		if err != nil || isSpam {
			return errors.New("rejected by Akismet Anti-Spam System")
		}
	}

	var cm model.Comment
	fillCommentEntry(c, isAdmin, &cm, &cf, article)

	err = solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&cm).Error; err != nil {
			return err
		}

		if err := tx.Model(&model.Article{}).
			Where("id = ?", cm.ArticleID).
			UpdateColumn("comment_num", gorm.Expr("comment_num + ?", 1)).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return err
	}

	// Email notify and update email read status
	go func() {
		// Only send email if replying to someone else's comment
		if replyTo != nil && !replyTo.IsAdmin && replyTo.Email != "" && replyTo.Email != cm.Email {
			emailErr := notify.Email(&cm, replyTo, article, *cm.EmailTrackingToken)
			
			// Update EmailReadStatus based on email sending result
			var emailStatus *string
			if emailErr == nil {
				// Email sent successfully, set to "unread"
				status := "unread"
				emailStatus = &status
				solitudes.System.DB.Model(&model.Comment{}).
					Where("id = ?", cm.ID).
					Update("email_read_status", emailStatus)
			}
			// If emailErr != nil, leave it as nil (not sent)
			
			// Send Telegram notification regardless of email result
			notify.TGNotify(&cm, article, emailErr)
		}
	}()
	return nil
}

// generateTrackingToken generates a secure random token for email tracking
func generateTrackingToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func verifyArticle(cf *commentForm) (*model.Article, error) {
	var article model.Article
	if err := solitudes.System.DB.Select("id,version,title,slug").Take(&article, "slug = ?", cf.Slug).Error; err != nil {
		return nil, err
	}
	if cf.Version > article.Version || cf.Version == 0 {
		return nil, errors.New("error invalid version")
	}
	return &article, nil
}

func getCommentType(cf *commentForm) (commentType string, replyTo *model.Comment, err error) {
	if cf.ReplyTo != nil {
		commentType = "reply"
		var innerReplyTo model.Comment
		if solitudes.System.DB.Take(&innerReplyTo, "id = ?", cf.ReplyTo).Error != nil {
			err = errors.New("reply to invaild comment")
			return
		}
		replyTo = &innerReplyTo
		return
	}
	commentType = "comment"
	return
}

func fillCommentEntry(c *fiber.Ctx, isAdmin bool, cm *model.Comment, cf *commentForm, article *model.Article) {
	cm.ReplyTo = cf.ReplyTo
	cm.Content = cf.Content
	cm.ArticleID = &article.ID
	// Generate tracking token for all comments on insert
	token := generateTrackingToken()
	cm.EmailTrackingToken = &token
	if isAdmin {
		cm.Nickname = solitudes.System.Config.User.Nickname
		cm.Email = solitudes.System.Config.User.Email
	} else {
		cm.Nickname = cf.Nickname
		cm.Email = cf.Email
		cm.Website = cf.Website
		cm.IP = c.IP()
		cm.UserAgent = string(c.Request().Header.UserAgent())
	}
	cm.IsAdmin = isAdmin
	cm.Version = cf.Version
}
