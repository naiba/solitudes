package router

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/content"
	"github.com/naiba/solitudes/pkg/translator"
)

func comparisonVersion(value string, maximum uint) (uint, bool) {
	if len(value) < 2 || len(value) > 21 || value[0] != 'v' {
		return 0, false
	}
	version, err := strconv.ParseUint(value[1:], 10, 32)
	if err != nil || version == 0 || version > uint64(maximum) || value != "v"+strconv.FormatUint(version, 10) {
		return 0, false
	}
	return uint(version), true
}

func loadArticleRevision(c *fiber.Ctx, latest model.Article, version uint) (model.Article, error) {
	if version == latest.Version {
		return latest, nil
	}
	var history model.ArticleHistory
	if err := solitudes.System.DB.Where("article_id = ? AND version = ?", latest.ID, version).Take(&history).Error; err != nil {
		return model.Article{}, err
	}
	latest.NewVersion = latest.Version
	latest.Version, latest.Title, latest.Content = history.Version, history.Title, history.Content
	latest.CreatedAt, latest.UpdatedAt = history.CreatedAt, history.UpdatedAt
	if latest.UpdatedAt.IsZero() {
		latest.UpdatedAt = latest.CreatedAt
	}
	latest.UpdatedByID, latest.UpdatedBy = history.UpdatedByID, model.Account{}
	if latest.Title == "" {
		latest.Title = c.Locals(solitudes.CtxTranslator).(*translator.Translator).T("revision_title_unavailable")
	}
	if latest.UpdatedByID != nil {
		if err := solitudes.System.DB.Take(&latest.UpdatedBy, "id = ?", *latest.UpdatedByID).Error; err != nil {
			return model.Article{}, err
		}
	}
	return latest, nil
}

func articleCompare(c *fiber.Ctx) error {
	// Personalized fragments must never be shared through caches or indexed.
	c.Set(fiber.HeaderCacheControl, "private, no-store")
	c.Set(fiber.HeaderXRobotsTag, "noindex, follow")
	var latest model.Article
	if err := solitudes.System.DB.Preload("Author").Preload("UpdatedBy").Where("slug = ?", c.Params("slug")).Take(&latest).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return page404(c)
		}
		return err
	}
	if !canReadArticle(currentAccount(c), &latest) {
		return page404(c)
	}
	if !latest.Public() {
		c.Set(fiber.HeaderXRobotsTag, "noindex, nofollow")
	}
	if c.Params("versions") == "" {
		from, to := c.Query("from"), c.Query("to")
		if from == "" && to == "" {
			if latest.Version < 2 {
				return page404(c)
			}
			from, to = strconv.FormatUint(uint64(latest.Version-1), 10), strconv.FormatUint(uint64(latest.Version), 10)
		}
		if _, ok := comparisonVersion("v"+from, latest.Version); !ok {
			return page404(c)
		}
		if _, ok := comparisonVersion("v"+to, latest.Version); !ok {
			return page404(c)
		}
		return c.Redirect("/"+latest.Slug+"/compare/v"+from+"...v"+to, http.StatusSeeOther)
	}
	pair := strings.Split(c.Params("versions"), "...")
	if len(pair) != 2 {
		return page404(c)
	}
	from, ok := comparisonVersion(pair[0], latest.Version)
	if !ok {
		return page404(c)
	}
	to, ok := comparisonVersion(pair[1], latest.Version)
	if !ok {
		return page404(c)
	}
	before, err := loadArticleRevision(c, latest, from)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return page404(c)
	}
	if err != nil {
		return err
	}
	after, err := loadArticleRevision(c, latest, to)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return page404(c)
	}
	if err != nil {
		return err
	}
	// Apply each revision's own fragment policy BEFORE rendering or comparison.
	if len(before.Content) > content.MaxDiffBytes || len(after.Content) > content.MaxDiffBytes {
		return fiber.ErrRequestEntityTooLarge
	}
	before.Content = before.ContentFor(currentAccount(c), accessNoticeFor(c))
	after.Content = after.ContentFor(currentAccount(c), accessNoticeFor(c))
	rows, err := content.CompareHTML(mdRender(before.GetIndexID(), before.Content), mdRender(after.GetIndexID(), after.Content))
	if errors.Is(err, content.ErrDiffTooLarge) {
		return fiber.ErrRequestEntityTooLarge
	}
	if err != nil {
		return err
	}
	changed := before.Title != after.Title
	for _, row := range rows {
		changed = changed || row.Kind != "same"
	}
	// Templates get revision metadata and sanitized changes, never another
	// path to raw source or the latest article's unfiltered fragments.
	latest.Content, before.Content, after.Content = "", "", ""
	data := fiber.Map{
		"title": fmt.Sprintf("%s · v%d ↔ v%d", latest.Title, from, to), "noindex": true,
		"article": latest, "before": before, "after": after, "changes": rows, "changed": changed,
		"comparison": true,
	}
	if latest.Public() {
		data["canonical_path"] = "/" + latest.Slug
	}
	return c.Render("site/article_compare", injectSiteData(c, data))
}
