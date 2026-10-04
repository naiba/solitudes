package router

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/adtac/go-akismet/akismet"
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/notify"
)

type commentForm struct {
	ReplyTo  *string `json:"reply_to" validate:"omitempty,uuid4"`
	Nickname string  `json:"nickname"`
	Content  string  `json:"content" validate:"required" gorm:"text"`
	Slug     string  `json:"slug" validate:"required" gorm:"index"`
	Website  string  `json:"website" validate:"omitempty,url"`
	Version  uint    `json:"version" validate:"required"`
	Email    string  `json:"email" validate:"omitempty,email"`
}

func commentHandler(c *fiber.Ctx) error {
	account := currentAccount(c)
	isAdmin := account != nil && account.Role.IsAdmin()
	var cf commentForm
	if err := c.BodyParser(&cf); err != nil {
		return fmt.Errorf("failed to parse comment form: %w", err)
	}
	if account != nil {
		// Ignore all client-supplied identity fields for a signed-in commenter.
		cf.Nickname, cf.Email, cf.Website = account.Nickname, account.Email, ""
	}
	if err := validator.StructCtx(c.Context(), &cf); err != nil {
		return fmt.Errorf("comment form validation failed: %w", err)
	}
	if account == nil && (strings.TrimSpace(cf.Nickname) == "" || len([]rune(cf.Nickname)) > 64) {
		return fiber.NewError(fiber.StatusBadRequest, "guest nickname must be 1–64 characters")
	}

	article, err := verifyArticle(&cf)
	if err != nil {
		return fmt.Errorf("article verification failed: %w", err)
	}
	if !canReadArticle(account, article) {
		return fiber.ErrNotFound
	}

	commentType, replyTo, err := getCommentType(&cf, article.ID)
	if err != nil {
		return fmt.Errorf("failed to determine comment type: %w", err)
	}

	isSpam := false
	// akismet anti spam
	if solitudes.System.Config.Akismet != "" && !isAdmin {
		isSpam, err = akismet.Check(&akismet.Comment{
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
	}

	var cm model.Comment
	if err := fillCommentEntry(c, &cm, &cf, article); err != nil {
		return err
	}
	cm.IsSpam = isSpam

	err = solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Omit("Account").Save(&cm).Error; err != nil {
			return fmt.Errorf("failed to save comment: %w", err)
		}

		if cm.CountsTowardArticle() {
			if err := tx.Model(&model.Article{}).
				Where("id = ?", cm.ArticleID).
				UpdateColumn("comment_num", gorm.Expr("comment_num + ?", 1)).Error; err != nil {
				return fmt.Errorf("failed to update article comment count: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		return err
	}
	if cm.IsSpam {
		return c.JSON(fiber.Map{"pending": true})
	}

	// Email notify and update email read status
	go func() {
		var emailErr error
		// Only send email if replying to someone else's comment
		if replyTo != nil && replyTo.PublicRole() != string(model.RoleAdmin) && replyTo.Email != "" && replyTo.Email != cm.Email {
			emailErr = notify.Email(&cm, replyTo, article, *cm.EmailTrackingToken)

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
		}

		notify.TGNotify(&cm, article, emailErr)
	}()
	// Return only the new identifier, so paginated clients can navigate to the
	// posted comment without echoing email, IP or notification tracking data.
	return c.JSON(fiber.Map{"id": cm.ID})
}

func generateTrackingToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func verifyArticle(cf *commentForm) (*model.Article, error) {
	var article model.Article
	if err := solitudes.System.DB.Select("id,version,title,slug,visibility,author_id").Order("created_at DESC").Take(&article, "slug = ?", cf.Slug).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch article: %w", err)
	}
	if cf.Version > article.Version || cf.Version == 0 {
		return nil, errors.New("invalid article version")
	}
	return &article, nil
}

func getCommentType(cf *commentForm, articleID string) (string, *model.Comment, error) {
	if cf.ReplyTo != nil {
		var innerReplyTo model.Comment
		if err := visibleComments(solitudes.System.DB).Preload("Account", publicCommentAuthor).Take(&innerReplyTo, "id = ? AND article_id = ?", cf.ReplyTo, articleID).Error; err != nil {
			return "", nil, fmt.Errorf("failed to find parent comment: %w", err)
		}
		return "reply", &innerReplyTo, nil
	}
	return "comment", nil, nil
}

func fillCommentEntry(c *fiber.Ctx, cm *model.Comment, cf *commentForm, article *model.Article) error {
	cm.ReplyTo = cf.ReplyTo
	cm.Content = cf.Content
	cm.ArticleID = &article.ID
	token, err := generateTrackingToken()
	if err != nil {
		return fmt.Errorf("failed to generate tracking token: %w", err)
	}
	cm.EmailTrackingToken = &token
	if account := currentAccount(c); account != nil {
		cm.AccountID = &account.ID
		cm.Account = account
		cm.Nickname = account.Nickname
		cm.Email = account.Email
	} else {
		cm.Nickname = strings.TrimSpace(cf.Nickname)
		cm.Email = cf.Email
		cm.Website = cf.Website
		cm.IP = c.IP()
		cm.UserAgent = string(c.Request().Header.UserAgent())
	}
	cm.Version = cf.Version
	return nil
}

func visibleComments(db *gorm.DB) *gorm.DB {
	return db.Where("is_spam = ?", false)
}

func publicCommentAuthor(db *gorm.DB) *gorm.DB {
	return db.Select("id, nickname, role")
}

func loadCommentAccounts(comments []*model.Comment) error {
	ids := make([]string, 0, len(comments))
	for _, cm := range comments {
		if cm.AccountID != nil {
			ids = append(ids, *cm.AccountID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	var accounts []model.Account
	if err := solitudes.System.DB.Select("id, nickname, role").Where("id IN ?", ids).Find(&accounts).Error; err != nil {
		return err
	}
	byID := make(map[string]*model.Account, len(accounts))
	for i := range accounts {
		byID[accounts[i].ID] = &accounts[i]
	}
	for _, cm := range comments {
		if cm.AccountID != nil {
			cm.Account = byID[*cm.AccountID]
		}
	}
	return nil
}
