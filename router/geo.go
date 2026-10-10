package router

import (
	"errors"
	"fmt"
	"html"
	"net/url"
	"strings"

	"github.com/88250/lute"
	"github.com/PuerkitoBio/goquery"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/microcosm-cc/bluemonday"
	nethtml "golang.org/x/net/html"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/content"
)

const machinePageSize = 50

func markdownText(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	escaped := strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]", "*", "\\*", "_", "\\_", "<", "&lt;", ">", "&gt;", "`", "\\`", "#", "\\#").Replace(text)
	// Metadata may itself contain URLs. Keep them literal rather than creating
	// an unmediated autolink in Markdown consumers with GFM autolinking enabled.
	return profileURLPattern.ReplaceAllStringFunc(escaped, func(value string) string { return "`" + value + "`" })
}

// Parse only the anonymous body, never the account-filtered HTML response.
// Sanitizing raw HTML before conversion prevents script/embed leakage; rewriting
// every anchor (including autolinks, reference links and raw HTML) prevents a
// second, machine-readable route from bypassing the site's outbound interstitial.
func machineMarkdown(raw, canonical string) (string, error) {
	public, err := content.Filter(raw, nil, nil)
	if err != nil {
		return "", err
	}
	engine := lute.New()
	engine.SetGFMAutoLink(true)
	engine.SetCodeSyntaxHighlight(false)
	policy := bluemonday.UGCPolicy()
	policy.AllowAttrs("class").OnElements("code")
	safe := policy.Sanitize(engine.MarkdownStr("", public))
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(safe))
	if err != nil {
		return "", err
	}
	// Image titles are visible captions in the article renderer, not decorative tooltips.
	doc.Find("img[title]").Each(func(_ int, s *goquery.Selection) {
		title := s.AttrOr("title", "")
		if title != "" {
			s.AfterHtml("<span> — " + html.EscapeString(title) + "</span>")
		}
	})
	// Bare URLs may not be autolinked for every TLD by the Markdown parser.
	// Convert only prose text nodes, never code examples or existing anchors.
	var linkify func(*nethtml.Node)
	linkify = func(n *nethtml.Node) {
		if n.Type == nethtml.ElementNode && (n.Data == "a" || n.Data == "code" || n.Data == "pre") {
			return
		}
		for child := n.FirstChild; child != nil; {
			next := child.NextSibling
			linkify(child)
			child = next
		}
		if n.Type != nethtml.TextNode || n.Parent == nil || !profileURLPattern.MatchString(n.Data) {
			return
		}
		fragment, e := goquery.NewDocumentFromReader(strings.NewReader(string(profileBio(n.Data))))
		if e != nil {
			return
		}
		for _, replacement := range fragment.Find("body").Contents().Nodes {
			replacement.Parent.RemoveChild(replacement)
			n.Parent.InsertBefore(replacement, n)
		}
		n.Parent.RemoveChild(n)
	}
	linkify(doc.Find("body").Get(0))
	base, err := url.Parse(canonical)
	if err != nil {
		return "", err
	}
	doc.Find("a[href],img[src]").Each(func(_ int, s *goquery.Selection) {
		attr := "href"
		image := goquery.NodeName(s) == "img"
		if image {
			attr = "src"
		}
		dest := s.AttrOr(attr, "")
		parsed, e := url.Parse(dest)
		if e != nil {
			s.RemoveAttr(attr)
			return
		}
		absolute := base.ResolveReference(parsed)
		if absolute.Scheme != "http" && absolute.Scheme != "https" || absolute.User != nil {
			s.RemoveAttr(attr)
			return
		}
		if !image && !strings.EqualFold(absolute.Host, base.Host) {
			s.SetAttr(attr, base.Scheme+"://"+base.Host+externalLink(absolute.String()))
		} else {
			s.SetAttr(attr, absolute.String())
		}
	})

	body, err := doc.Find("body").Html()
	if err != nil {
		return "", err
	}
	return engine.HTML2Markdown(body)
}

func machineHeaders(c *fiber.Ctx) {
	// Recheck visibility on every request. Never let an old ETag return a 304 for
	// an article that was just made private, and never share authenticated bodies.
	c.Set("Cache-Control", "no-store")
	c.Set("X-Content-Type-Options", "nosniff")
	c.Set("Content-Type", "text/markdown; charset=utf-8")
}

func articleMarkdownURL(a *model.Article) string {
	return publicBaseURL() + "/read/" + url.PathEscape(a.ID) + "/article.md"
}

func articleMachineContent(c *fiber.Ctx) error {
	machineHeaders(c)
	c.Set("X-Robots-Tag", "noindex, follow")
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(404).SendString("Not found")
	}
	var a model.Article
	// A stable ID avoids collisions with user-controlled slugs and .md articles.
	if err := solitudes.System.DB.Preload("Author").Where("id = ? AND visibility = ?", id.String(), model.VisibilityPublic).Take(&a).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(404).SendString("Not found")
		}
		return err
	}
	canonical := publicBaseURL() + "/" + url.PathEscape(a.Slug)
	body, err := machineMarkdown(a.Content, canonical)
	if err != nil {
		return c.Status(500).SendString("Content unavailable")
	}
	c.Set("Link", "<"+canonical+">; rel=\"canonical\", <"+publicBaseURL()+"/llms.txt>; rel=\"describedby\"")
	var out strings.Builder
	fmt.Fprintf(&out, "# %s\n\nOriginal: <%s>\n\n", markdownText(a.Title), canonical)
	if a.Author.ID != "" {
		fmt.Fprintf(&out, "Author: [%s](%s/users/%s)\n\n", markdownText(a.Author.Nickname), publicBaseURL(), url.PathEscape(a.Author.ID))
	}
	if !a.CreatedAt.IsZero() {
		fmt.Fprintf(&out, "Published: %s\n\n", a.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"))
	}
	if !a.UpdatedAt.IsZero() {
		fmt.Fprintf(&out, "Updated: %s\n\n", a.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"))
	}
	fmt.Fprintf(&out, "Version: %d\n\n%s", a.Version, body)
	return c.SendString(out.String())
}

func llmsDirectory(c *fiber.Ctx) error {
	machineHeaders(c)
	c.Set("X-Robots-Tag", "noindex, follow")
	page, err := listPage(c.Query("page"))
	if err != nil {
		return c.Status(400).SendString("Invalid page")
	}
	var articles []model.Article
	if err := solitudes.System.DB.Select("id,title,slug,content,is_book").Where("visibility = ?", model.VisibilityPublic).Order("created_at DESC,id DESC").Offset((page - 1) * machinePageSize).Limit(machinePageSize + 1).Find(&articles).Error; err != nil {
		return err
	}
	if page > 1 && len(articles) == 0 {
		return c.Status(404).SendString("Not found")
	}
	more := len(articles) > machinePageSize
	if more {
		articles = articles[:machinePageSize]
	}
	base := publicBaseURL()
	var out strings.Builder
	fmt.Fprintf(&out, "# %s\n\n> %s\n\n", markdownText(solitudes.System.Config.Site.SpaceName), markdownText(solitudes.System.Config.Site.SpaceDesc))
	out.WriteString("This directory contains current public content only. Markdown omits restricted sections and comments. Cite the original article URL. Outbound links use this site's safety interstitial. This file does not grant additional usage or training rights.\n\n")
	fmt.Fprintf(&out, "## Navigation\n\n- [Articles](%s/posts/)\n- [Series](%s/books/)\n- [Topics](%s/tags/)\n- [Sitemap](%s/sitemap.xml)\n\n## Public content — page %d\n\n", base, base, base, base, page)
	for _, a := range articles {
		kind := ""
		if a.IsBook {
			kind = "Series. "
		}
		fmt.Fprintf(&out, "- [%s](<%s>): %s%s — [Original](<%s/%s>)\n", markdownText(a.Title), articleMarkdownURL(&a), kind, markdownText(content.Excerpt(a.Content, 140)), base, url.PathEscape(a.Slug))
	}
	if more || page > 1 {
		out.WriteString("\n## Continue browsing\n\n")
	}
	if more {
		fmt.Fprintf(&out, "- [Next page](%s/llms.txt?page=%d)\n", base, page+1)
	}
	if page > 1 {
		fmt.Fprintf(&out, "- [Previous page](%s/llms.txt?page=%d)\n", base, page-1)
	}
	return c.SendString(out.String())
}
