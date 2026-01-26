package router

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/adtac/go-akismet/akismet"
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/notify"
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
		return fmt.Errorf("failed to parse comment form: %w", err)
	}
	if err := validator.StructCtx(c.Context(), &cf); err != nil {
		return fmt.Errorf("comment form validation failed: %w", err)
	}

	article, err := verifyArticle(&cf)
	if err != nil {
		return fmt.Errorf("article verification failed: %w", err)
	}

	commentType, replyTo, err := getCommentType(&cf)
	if err != nil {
		return fmt.Errorf("failed to determine comment type: %w", err)
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
		if err != nil {
			return fmt.Errorf("akismet check failed: %w", err)
		}
		if isSpam {
			return errors.New("comment rejected by Akismet Anti-Spam System")
		}
	}

	var cm model.Comment
	fillCommentEntry(c, isAdmin, &cm, &cf, article)

	err = solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&cm).Error; err != nil {
			return fmt.Errorf("failed to save comment: %w", err)
		}

		if err := tx.Model(&model.Article{}).
			Where("id = ?", cm.ArticleID).
			UpdateColumn("comment_num", gorm.Expr("comment_num + ?", 1)).Error; err != nil {
			return fmt.Errorf("failed to update article comment count: %w", err)
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
			if emailErr == nil {
				// Email sent successfully, set to "unread"
				status := "unread"
				if err := solitudes.System.DB.Model(&model.Comment{}).
					Where("id = ?", cm.ID).
					Update("email_read_status", &status).Error; err != nil {
					fmt.Printf("Failed to update email status: %v\n", err)
				}
			}

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
		return nil, fmt.Errorf("failed to fetch article: %w", err)
	}
	if cf.Version > article.Version || cf.Version == 0 {
		return nil, errors.New("invalid article version")
	}
	return &article, nil
}

func getCommentType(cf *commentForm) (string, *model.Comment, error) {
	if cf.ReplyTo != nil {
		var innerReplyTo model.Comment
		if err := solitudes.System.DB.Take(&innerReplyTo, "id = ?", cf.ReplyTo).Error; err != nil {
			return "", nil, fmt.Errorf("failed to find parent comment: %w", err)
		}
		return "reply", &innerReplyTo, nil
	}
	return "comment", nil, nil
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
