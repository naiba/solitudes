package router

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

const redirectPageSize = 25

type redirectRuleView struct {
	ID                     uint64
	SourcePath, TargetPath string
	ArticleID              *string
	ArticleTitle           string
	CreatedAt              time.Time
}

// Handler-level authorization must not call requireAdmin: that middleware calls
// Next and would fall through to article routing before this handler completes.
func redirectManagement(c *fiber.Ctx) error {
	if account := currentAccount(c); account == nil || !account.Role.IsAdmin() {
		return fiber.ErrForbidden
	}
	page, err := listPage(c.Query("page"))
	if err != nil {
		return err
	}
	q := strings.TrimSpace(c.Query("q"))
	if len(q) > 200 {
		return fiber.ErrBadRequest
	}
	base := solitudes.System.DB.Table("redirect_rules AS redirects").Joins("LEFT JOIN articles ON articles.id = redirects.article_id")
	if q != "" {
		like := "%" + strings.ToLower(q) + "%"
		base = base.Where("LOWER(redirects.source_path) LIKE ? OR LOWER(redirects.target_path) LIKE ? OR LOWER(articles.title) LIKE ? OR LOWER(articles.slug) LIKE ?", like, like, like, like)
	}
	var total int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return err
	}
	if page > 1 && int64((page-1)*redirectPageSize) >= total {
		return page404(c)
	}
	var rows []struct {
		model.RedirectRule
		ArticleTitle string
		ArticleSlug  string
	}
	if err := base.Select("redirects.*, articles.title AS article_title, articles.slug AS article_slug").Order("redirects.id DESC").Offset((page - 1) * redirectPageSize).Limit(redirectPageSize).Scan(&rows).Error; err != nil {
		return err
	}
	rules := make([]redirectRuleView, 0, len(rows))
	for _, row := range rows {
		target := row.TargetPath
		if row.ArticleID != nil {
			target, err = normalizedRedirectURL("/"+url.PathEscape(row.ArticleSlug), false)
			if err != nil {
				return err
			}
		}
		rules = append(rules, redirectRuleView{ID: row.ID, SourcePath: row.SourcePath, TargetPath: target, ArticleID: row.ArticleID, ArticleTitle: row.ArticleTitle, CreatedAt: row.CreatedAt})
	}
	tr := c.Locals(solitudes.CtxTranslator).(*translator.Translator)
	c.Set(fiber.HeaderCacheControl, "private, no-store")
	return c.Render("admin/redirects", injectSiteData(c, fiber.Map{
		"title": tr.T("manage_redirects"), "noindex": true, "rules": rules, "q": q, "total": total,
		"navigation": pageNavigationFor(c, "page", page, int64(page*redirectPageSize) < total, ""),
	}))
}

func saveRedirect(c *fiber.Ctx) error {
	if account := currentAccount(c); account == nil || !account.Role.IsAdmin() {
		return fiber.ErrForbidden
	}
	var input struct {
		ID         uint64 `json:"id"`
		SourcePath string `json:"source_path"`
		TargetPath string `json:"target_path"`
	}
	if err := c.BodyParser(&input); err != nil {
		return fiber.ErrBadRequest
	}
	source, err := normalizedRedirectURL(input.SourcePath, true)
	if err != nil {
		return err
	}
	target, err := normalizedRedirectURL(input.TargetPath, false)
	if err != nil {
		return err
	}
	targetPath := strings.SplitN(strings.SplitN(target, "#", 2)[0], "?", 2)[0]
	if source == targetPath {
		return fiber.NewError(400, "a redirect cannot target itself")
	}
	rule := model.RedirectRule{ID: input.ID, SourcePath: source, TargetPath: target}
	err = solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockRedirectRules(tx); err != nil {
			return err
		}
		if input.ID != 0 {
			var previous model.RedirectRule
			if err := tx.Take(&previous, input.ID).Error; err != nil {
				return err
			}
			if previous.ArticleID != nil {
				return fiber.NewError(400, "automatic redirects follow the article; delete the rule to replace it manually")
			}
			rule.CreatedAt = previous.CreatedAt
		}
		var duplicate int64
		if err := tx.Model(&model.RedirectRule{}).Where("source_path = ? AND id <> ?", source, input.ID).Count(&duplicate).Error; err != nil {
			return err
		}
		if duplicate != 0 {
			return fiber.NewError(409, "redirect source already exists")
		}
		if input.ID == 0 {
			if err := tx.Create(&rule).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Model(&rule).Updates(map[string]interface{}{"source_path": source, "target_path": target}).Error; err != nil {
				return err
			}
		}
		if err := validateRedirectGraph(tx); err != nil {
			return err
		}
		return auditMutation(c, tx, "redirect.save", strconv.FormatUint(rule.ID, 10), "", "")
	})
	if err != nil {
		return err
	}
	c.Locals("audit_recorded", true)
	status := http.StatusCreated
	if input.ID != 0 {
		status = http.StatusOK
	}
	return c.Status(status).JSON(rule)
}

func deleteRedirects(c *fiber.Ctx) error {
	if account := currentAccount(c); account == nil || !account.Role.IsAdmin() {
		return fiber.ErrForbidden
	}
	var input struct {
		IDs []uint64 `json:"ids"`
	}
	if err := c.BodyParser(&input); err != nil {
		return fiber.ErrBadRequest
	}
	if len(input.IDs) == 0 || len(input.IDs) > 100 {
		return fiber.NewError(400, "select between 1 and 100 redirect rules")
	}
	seen := make(map[uint64]bool, len(input.IDs))
	for _, id := range input.IDs {
		if id == 0 || seen[id] {
			return fiber.ErrBadRequest
		}
		seen[id] = true
	}
	err := solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockRedirectRules(tx); err != nil {
			return err
		}
		var rows []model.RedirectRule
		if err := tx.Where("id IN ?", input.IDs).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) != len(input.IDs) {
			return gorm.ErrRecordNotFound
		}
		if err := tx.Where("id IN ?", input.IDs).Delete(&model.RedirectRule{}).Error; err != nil {
			return err
		}
		// Removing an override can expose an article's built-in revision redirect;
		// deletion therefore needs the same cycle check as adding or editing rules.
		if err := validateRedirectGraph(tx); err != nil {
			return err
		}
		for _, row := range rows {
			if err := auditMutation(c, tx, "redirect.delete", strconv.FormatUint(row.ID, 10), "", ""); err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fiber.ErrNotFound
	}
	if err != nil {
		return err
	}
	c.Locals("audit_recorded", true)
	return c.SendStatus(http.StatusNoContent)
}
