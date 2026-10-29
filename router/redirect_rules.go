package router

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/siteurl"
)

const maxRedirectHops = 32

// System endpoints remain authoritative even for stale rules imported directly
// into the database. The redirect middleware also runs after registered routes.
func redirectSystemPath(path string) bool {
	return siteurl.IsSystemPath(path)
}

func normalizedRedirectURL(raw string, source bool) (string, error) {
	if raw == "" || len(raw) > siteurl.MaxPathLength || strings.TrimSpace(raw) != raw || strings.ContainsAny(raw, "\\\r\n\t") {
		return "", newHTTPError(400, "invalid redirect path")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.User != nil || !strings.HasPrefix(parsed.Path, "/") || strings.HasPrefix(parsed.Path, "//") {
		return "", newHTTPError(400, "redirects require a site-relative path")
	}
	if source && (parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.ContainsAny(raw, "?#")) {
		return "", newHTTPError(400, "redirect source must not contain a query or fragment")
	}
	// Reject traversal, encoded separators and controls rather than depending on
	// the differing normalization rules of browsers, proxies and the HTTP router.
	for _, segment := range strings.Split(parsed.EscapedPath(), "/") {
		decoded, err := url.PathUnescape(segment)
		if err != nil || !utf8.ValidString(decoded) || decoded == "." || decoded == ".." || strings.ContainsAny(decoded, "/\\") || strings.ContainsFunc(decoded, unicode.IsControl) {
			return "", newHTTPError(400, "invalid redirect path segment")
		}
	}
	if strings.Contains(parsed.Path, "//") {
		return "", newHTTPError(400, "invalid redirect path")
	}
	parts := strings.Split(parsed.Path, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	path := canonicalPath(strings.Join(parts, "/"))
	if source && redirectSystemPath(path) {
		return "", newHTTPError(400, "system paths cannot be redirected")
	}
	if strings.ContainsFunc(parsed.Fragment, unicode.IsControl) {
		return "", newHTTPError(400, "invalid redirect fragment")
	}
	if _, err := url.ParseQuery(parsed.RawQuery); err != nil {
		return "", newHTTPError(400, "invalid redirect query")
	}
	target := path
	if parsed.RawQuery != "" {
		target += "?" + parsed.RawQuery
	}
	if parsed.Fragment != "" {
		target += "#" + url.PathEscape(parsed.Fragment)
	}
	// Percent-encoding expands Unicode; enforce the stored URL bound after
	// normalization too, before PostgreSQL's source_path limit can reject it.
	if len(target) > siteurl.MaxPathLength {
		return "", newHTTPError(400, "encoded redirect URL exceeds length limit")
	}
	return target, nil
}

// Every graph mutation, including article renames, takes the same transaction
// lock. Unique sources alone do not prevent concurrent A->B / B->A cycles.
func lockRedirectRules(tx *gorm.DB) error {
	if tx.Dialector.Name() == "postgres" {
		return tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(731492168)).Error
	}
	return nil
}

func redirectRuleTarget(db *gorm.DB, rule model.RedirectRule, viewer *model.Account, checkAccess bool) (string, error) {
	if rule.ArticleID == nil {
		return normalizedRedirectURL(rule.TargetPath, false)
	}
	var article model.Article
	if err := db.Select("id", "slug", "title", "visibility", "author_id").Take(&article, "id = ?", *rule.ArticleID).Error; err != nil {
		return "", err
	}
	if checkAccess && !article.CanRead(viewer) {
		return "", gorm.ErrRecordNotFound
	}
	return normalizedRedirectURL("/"+url.PathEscape(article.Slug), false)
}

func mergeRedirectDestination(destination, incoming string) (string, error) {
	dest, err := url.Parse(destination)
	if err != nil {
		return "", err
	}
	prior, err := url.Parse(incoming)
	if err != nil {
		return "", err
	}
	values, err := url.ParseQuery(prior.RawQuery)
	if err != nil {
		return "", newHTTPError(http.StatusBadRequest)
	}
	for key, value := range dest.Query() {
		values[key] = value
	}
	dest.RawQuery = values.Encode()
	if dest.Fragment == "" {
		dest.Fragment = prior.Fragment
	}
	return dest.String(), nil
}

// Resolve the entire graph on the server so clients make one hop, but validate
// access at every automatic edge; an intermediate private article must not leak.
func resolveRedirect(db *gorm.DB, start string, viewer *model.Account, checkAccess bool) (string, bool, error) {
	current := start
	seen := make(map[string]bool)
	matched := false
	for hops := 0; ; hops++ {
		parsed, err := url.Parse(current)
		if err != nil {
			return "", matched, err
		}
		path, err := normalizedRedirectURL(parsed.EscapedPath(), false)
		if err != nil {
			return "", matched, err
		}
		if redirectSystemPath(path) {
			return current, matched, nil
		}
		if seen[path] {
			return "", matched, newHTTPError(409, "redirect cycle detected")
		}
		seen[path] = true
		var rule model.RedirectRule
		result := db.Where("source_path = ?", path).Limit(1).Find(&rule)
		if result.Error != nil {
			return "", matched, result.Error
		}
		var target string
		if result.RowsAffected == 0 {
			if !matched {
				return current, false, nil
			}
			target, err = currentRevisionRedirect(db, path, viewer, checkAccess)
			if err != nil {
				return "", matched, err
			}
			if target == "" {
				return current, matched, nil
			}
		} else {
			target, err = redirectRuleTarget(db, rule, viewer, checkAccess)
			if err != nil {
				return "", matched, err
			}
		}
		// A chain of exactly maxRedirectHops edges still needs one final lookup
		// to prove the destination is terminal; do not reject that lookup.
		if hops >= maxRedirectHops {
			return "", matched, newHTTPError(409, "redirect chain exceeds limit")
		}
		matched = true
		current, err = mergeRedirectDestination(target, current)
		if err != nil {
			return "", matched, err
		}
	}
}

func registeredRedirect(writer http.ResponseWriter, httpRequest *http.Request) error {
	if httpRequest.Method != http.MethodGet && httpRequest.Method != http.MethodHead {
		return nil
	}
	if redirectSystemPath(requestPath(httpRequest)) {
		return nil
	}
	source, err := normalizedRedirectURL(requestPath(httpRequest), true)
	if err != nil {
		return nil
	}
	// The system's URL normalization has already run; queries never form part of
	// a source key and are merged only after resolving the path graph.
	target, matched, err := resolveRedirect(solitudes.System.DB, source, currentAccount(writer, httpRequest), true)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return page404(writer, httpRequest)
	}
	if err != nil {
		return err
	}
	if !matched {
		return nil
	}
	target, err = mergeRedirectDestination(target, source+"?"+string(httpRequest.URL.RawQuery))
	if err != nil {
		return err
	}
	writer.Header().Set("Cache-Control", "no-store")
	return redirectResponse(writer, httpRequest, target, http.StatusMovedPermanently)
}

func syncArticleRedirects(tx *gorm.DB, previous, current *model.Article) error {
	if previous.ID == "" || previous.Slug == current.Slug {
		// A version bump can activate a current-revision redirect even when the
		// slug is unchanged, so publication also validates those implicit edges.
		return validateRedirectGraph(tx)
	}
	destination, err := normalizedRedirectURL(siteurl.ArticlePath(current.Slug), true)
	if err != nil {
		return err
	}
	// Renaming back to an old slug removes only this article's own automatic
	// alias. Manual rules and other articles' aliases must never be overwritten.
	var occupied model.RedirectRule
	err = tx.Where("source_path = ?", destination).Take(&occupied).Error
	if err == nil {
		if occupied.ArticleID == nil || *occupied.ArticleID != current.ID {
			return newHTTPError(409, "new article URL is already a redirect source")
		}
		if err := tx.Delete(&occupied).Error; err != nil {
			return err
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	source, err := normalizedRedirectURL(siteurl.ArticlePath(previous.Slug), true)
	if err != nil {
		// Legacy slugs may already be unreachable (reserved routes or embedded
		// slashes). Allow repairing them without creating an unusable alias or
		// taking priority over a system page; other existing aliases still follow.
		return validateRedirectGraph(tx)
	}
	var existing model.RedirectRule
	err = tx.Where("source_path = ?", source).Take(&existing).Error
	if err == nil {
		if existing.ArticleID == nil || *existing.ArticleID != current.ID {
			return newHTTPError(409, "old article URL already has a redirect rule")
		}
	} else if errors.Is(err, gorm.ErrRecordNotFound) {
		rule := model.RedirectRule{SourcePath: source, ArticleID: &current.ID}
		if err := tx.Create(&rule).Error; err != nil {
			return err
		}
	} else {
		return err
	}
	return validateRedirectGraph(tx)
}
