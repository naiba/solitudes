package router

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

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
func redirectManagement(writer http.ResponseWriter, httpRequest *http.Request) error {
	if account := currentAccount(writer, httpRequest); account == nil || !account.Role.IsAdmin() {
		return newHTTPError(http.StatusForbidden)
	}
	page, err := listPage(queryValue(httpRequest, "page"))
	if err != nil {
		return err
	}
	q := strings.TrimSpace(queryValue(httpRequest, "q"))
	if len(q) > 200 {
		return newHTTPError(http.StatusBadRequest)
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
		return page404(writer, httpRequest)
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
	tr := requestValue(httpRequest, solitudes.CtxTranslator).(*translator.Translator)
	writer.Header().Set("Cache-Control", "private, no-store")
	return renderPage(writer, httpRequest, http.StatusOK, "admin/redirects", injectSiteData(writer, httpRequest, viewData{
		"title": tr.T("manage_redirects"), "noindex": true, "rules": rules, "q": q, "total": total,
		"navigation": pageNavigationFor(writer, httpRequest, "page", page, int64(page*redirectPageSize) < total, ""),
	}))
}

func saveRedirect(writer http.ResponseWriter, httpRequest *http.Request) error {
	if account := currentAccount(writer, httpRequest); account == nil || !account.Role.IsAdmin() {
		return newHTTPError(http.StatusForbidden)
	}
	var input struct {
		ID         uint64 `json:"id"`
		SourcePath string `json:"source_path"`
		TargetPath string `json:"target_path"`
	}
	if err := decodeRequestBody(httpRequest, &input); err != nil {
		return newHTTPError(http.StatusBadRequest)
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
		return newHTTPError(400, "a redirect cannot target itself")
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
				return newHTTPError(400, "automatic redirects follow the article; delete the rule to replace it manually")
			}
			rule.CreatedAt = previous.CreatedAt
		}
		var duplicate int64
		if err := tx.Model(&model.RedirectRule{}).Where("source_path = ? AND id <> ?", source, input.ID).Count(&duplicate).Error; err != nil {
			return err
		}
		if duplicate != 0 {
			return newHTTPError(409, "redirect source already exists")
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
		return auditMutation(writer, httpRequest, tx, "redirect.save", strconv.FormatUint(rule.ID, 10), "", "")
	})
	if err != nil {
		return err
	}
	requestValue(httpRequest, "audit_recorded", true)
	status := http.StatusCreated
	if input.ID != 0 {
		status = http.StatusOK
	}
	return writeJSON(writer, status, rule)
}

func deleteRedirects(writer http.ResponseWriter, httpRequest *http.Request) error {
	if account := currentAccount(writer, httpRequest); account == nil || !account.Role.IsAdmin() {
		return newHTTPError(http.StatusForbidden)
	}
	var input struct {
		IDs []uint64 `json:"ids"`
	}
	if err := decodeRequestBody(httpRequest, &input); err != nil {
		return newHTTPError(http.StatusBadRequest)
	}
	if len(input.IDs) == 0 || len(input.IDs) > 100 {
		return newHTTPError(400, "select between 1 and 100 redirect rules")
	}
	seen := make(map[uint64]bool, len(input.IDs))
	for _, id := range input.IDs {
		if id == 0 || seen[id] {
			return newHTTPError(http.StatusBadRequest)
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
			if err := auditMutation(writer, httpRequest, tx, "redirect.delete", strconv.FormatUint(row.ID, 10), "", ""); err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return newHTTPError(http.StatusNotFound)
	}
	if err != nil {
		return err
	}
	requestValue(httpRequest, "audit_recorded", true)
	return writeStatus(writer, http.StatusNoContent)
}
