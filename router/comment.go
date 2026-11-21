package router

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/adtac/go-akismet/akismet"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/notify"
	"github.com/naiba/solitudes/pkg/siteurl"
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

func commentHandler(writer http.ResponseWriter, httpRequest *http.Request) error {
	account := currentAccount(writer, httpRequest)
	isAdmin := account != nil && account.Role.IsAdmin()
	var cf commentForm
	if err := decodeRequestBody(httpRequest, &cf); err != nil {
		return fmt.Errorf("failed to parse comment form: %w", err)
	}
	if account != nil {
		// Ignore all client-supplied identity fields for a signed-in commenter.
		cf.Nickname, cf.Email, cf.Website = account.Nickname, account.Email, ""
	}
	if err := validator.StructCtx(httpRequest.Context(), &cf); err != nil {
		return fmt.Errorf("comment form validation failed: %w", err)
	}
	if account == nil && (strings.TrimSpace(cf.Nickname) == "" || len([]rune(cf.Nickname)) > 64) {
		return newHTTPError(http.StatusBadRequest, "guest nickname must be 1–64 characters")
	}

	article, err := verifyArticle(account, &cf)
	if err != nil {
		return fmt.Errorf("article verification failed: %w", err)
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
			UserIP:             requestIP(httpRequest),                           // required
			UserAgent:          string(httpRequest.UserAgent()),                  // required
			CommentType:        commentType,
			Referrer:           string(httpRequest.Referer()),
			Permalink:          "https://" + solitudes.System.Config.Site.Domain + siteurl.ArticlePath(cf.Slug),
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
	if err := fillCommentEntry(writer, httpRequest, &cm, &cf, article); err != nil {
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
		return writeJSON(writer, http.StatusOK, viewData{"pending": true})
	}

	// Email notify and update email read status
	go func() {
		var emailErr error
		// Only send email if replying to someone else's comment
		if recipient := replyNotificationRecipient(replyTo); recipient != nil && recipient.PublicRole() != string(model.RoleAdmin) && !commentAuthorIsRecipient(&cm, recipient) {
			emailErr = notify.Email(&cm, recipient, article, *cm.EmailTrackingToken)

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
	return writeJSON(writer, http.StatusOK, viewData{"id": cm.ID})
}

func generateTrackingToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// verifyArticle returns the article a comment may be posted to.
func verifyArticle(account *model.Account, cf *commentForm) (*model.Article, error) {
	var article model.Article
	if err := solitudes.System.DB.Select("id,version,title,slug,visibility,author_id,disable_comment").Order("created_at DESC").Take(&article, "slug = ?", cf.Slug).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch article: %w", err)
	}
	if cf.Version > article.Version || cf.Version == 0 {
		return nil, errors.New("invalid article version")
	}
	if !canReadArticle(account, &article) {
		return nil, newHTTPError(http.StatusNotFound)
	}
	// The templates hide the comment form when comments are disabled, but the
	// endpoint is reachable without the form. Enforce the setting here, after
	// the visibility check so a hidden article is not revealed by a 403.
	if article.DisableComment {
		return nil, newHTTPError(http.StatusForbidden, "comments are disabled for this article")
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

func fillCommentEntry(writer http.ResponseWriter, httpRequest *http.Request, cm *model.Comment, cf *commentForm, article *model.Article) error {
	cm.ReplyTo = cf.ReplyTo
	cm.Content = cf.Content
	cm.ArticleID = &article.ID
	token, err := generateTrackingToken()
	if err != nil {
		return fmt.Errorf("failed to generate tracking token: %w", err)
	}
	cm.EmailTrackingToken = &token
	if account := currentAccount(writer, httpRequest); account != nil {
		cm.AccountID = &account.ID
		cm.Account = account
		cm.Nickname = account.Nickname
		cm.Email = account.Email
	} else {
		cm.Nickname = strings.TrimSpace(cf.Nickname)
		cm.Email = cf.Email
		cm.Website = cf.Website
		cm.IP = requestIP(httpRequest)
		cm.UserAgent = string(httpRequest.UserAgent())
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

// commentAuthorIsRecipient 判断新评论的作者是不是被回复的那个账号（自己回复自己不发通知）。
// 必须比较账号而不是邮箱：游客的邮箱是表单里随便填的，填上被回复者的邮箱
// 就能让对方收不到这条回复的通知。
func commentAuthorIsRecipient(comment, recipient *model.Comment) bool {
	return comment.AccountID != nil && recipient.AccountID != nil && *comment.AccountID == *recipient.AccountID
}

// replyNotificationRecipient returns the parent comment addressed to the
// verified email of the account that wrote it, or nil when nobody may be
// notified. A guest's address is whatever was typed into the form: mailing it
// let anyone use the site to send their own text to arbitrary people. The
// address stored with the comment is not used either, it can be outdated.
func replyNotificationRecipient(replyTo *model.Comment) *model.Comment {
	if replyTo == nil || replyTo.AccountID == nil {
		return nil
	}
	var author model.Account
	if err := solitudes.System.DB.Select("email").
		Where("id = ? AND email_verified_at IS NOT NULL AND disabled_at IS NULL", *replyTo.AccountID).
		Take(&author).Error; err != nil || author.Email == "" {
		return nil
	}
	recipient := *replyTo
	recipient.Email = author.Email
	return &recipient
}
