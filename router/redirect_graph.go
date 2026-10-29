package router

import (
	"net/url"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/siteurl"
)

func redirectRevision(path string) (string, uint64, bool) {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) != 2 || !strings.HasPrefix(parts[1], "v") {
		return "", 0, false
	}
	version, err := strconv.ParseUint(strings.TrimPrefix(parts[1], "v"), 10, strconv.IntSize)
	if err != nil || version == 0 {
		return "", 0, false
	}
	// Keep virtual revision edges aligned with article()'s single path decode,
	// including literal percent signs and Unicode slugs.
	slug, err := siteurl.DecodeArticleSlug(parts[0])
	return slug, version, err == nil
}

func currentRevisionRedirect(db *gorm.DB, path string, viewer *model.Account, checkAccess bool) (string, error) {
	slug, version, ok := redirectRevision(path)
	if !ok {
		return "", nil
	}
	var article model.Article
	result := db.Select("id", "slug", "version", "visibility", "author_id").Where("slug = ?", slug).Limit(1).Find(&article)
	if result.Error != nil {
		return "", result.Error
	}
	if result.RowsAffected == 0 || uint64(article.Version) != version {
		return "", nil
	}
	if checkAccess && !article.CanRead(viewer) {
		return "", gorm.ErrRecordNotFound
	}
	return normalizedRedirectURL("/"+url.PathEscape(article.Slug), false)
}

// Validate every source after a mutation, not just the new rule's downstream.
// Extending a valid chain must not silently push existing upstream URLs beyond
// the runtime hop limit. Bulk reads keep this admin-only check linear in graph
// size instead of issuing one SQL query per hop per source.
func validateRedirectGraph(tx *gorm.DB) error {
	var rules []model.RedirectRule
	if err := tx.Find(&rules).Error; err != nil {
		return err
	}
	if len(rules) == 0 {
		return nil
	}
	var ids, slugs []string
	for _, rule := range rules {
		if rule.ArticleID != nil {
			ids = append(ids, *rule.ArticleID)
			continue
		}
		target, err := url.Parse(rule.TargetPath)
		if err != nil {
			return err
		}
		if slug, _, ok := redirectRevision(canonicalPath(target.EscapedPath())); ok {
			slugs = append(slugs, slug)
		}
	}
	var articles []model.Article
	if len(ids) > 0 || len(slugs) > 0 {
		if err := tx.Select("id", "slug", "version").Where("id IN ? OR slug IN ?", ids, slugs).Find(&articles).Error; err != nil {
			return err
		}
	}
	byID, bySlug := make(map[string]model.Article), make(map[string]model.Article)
	for _, article := range articles {
		byID[article.ID] = article
		bySlug[article.Slug] = article
	}
	edges := make(map[string]string, len(rules))
	for _, rule := range rules {
		if redirectSystemPath(rule.SourcePath) {
			continue
		}
		target := rule.TargetPath
		if rule.ArticleID != nil {
			article, ok := byID[*rule.ArticleID]
			if !ok {
				return gorm.ErrRecordNotFound
			}
			target = "/" + url.PathEscape(article.Slug)
		}
		normalized, err := normalizedRedirectURL(target, false)
		if err != nil {
			return err
		}
		parsed, err := url.Parse(normalized)
		if err != nil {
			return err
		}
		edges[rule.SourcePath] = parsed.EscapedPath()
	}
	// A current-version article URL redirects to its base URL even without a
	// managed rule. Include that edge or base -> base/vN creates a browser loop.
	var implicit = make(map[string]string)
	for _, target := range edges {
		if _, overridden := edges[target]; overridden || redirectSystemPath(target) {
			continue
		}
		if slug, version, ok := redirectRevision(target); ok {
			if article, exists := bySlug[slug]; exists && uint64(article.Version) == version {
				implicit[target] = "/" + url.PathEscape(article.Slug)
			}
		}
	}
	for source, target := range implicit {
		edges[source] = target
	}
	depths := make(map[string]int, len(edges))
	visiting := make(map[string]bool, len(edges))
	var visit func(string, int) (int, error)
	visit = func(source string, stackDepth int) (int, error) {
		if redirectSystemPath(source) {
			return 0, nil
		}
		target, exists := edges[source]
		if !exists {
			return 0, nil
		}
		if visiting[source] {
			return 0, newHTTPError(409, "redirect cycle detected")
		}
		if depth, ok := depths[source]; ok {
			return depth, nil
		}
		if stackDepth >= maxRedirectHops {
			return 0, newHTTPError(409, "redirect chain exceeds limit")
		}
		visiting[source] = true
		depth, err := visit(target, stackDepth+1)
		if err != nil {
			return 0, err
		}
		visiting[source] = false
		depth++
		if depth > maxRedirectHops {
			return 0, newHTTPError(409, "redirect chain exceeds limit")
		}
		depths[source] = depth
		return depth, nil
	}
	for source := range edges {
		if _, err := visit(source, 0); err != nil {
			return err
		}
	}
	return nil
}
