package router

import (
	"bytes"
	"html/template"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/naiba/solitudes/internal/model"
)

var folioTocNumberPattern = regexp.MustCompile(`<span class="toc-number">([^<]+)</span>`)

func TestFolioTocUsesSingleNumberingSource(t *testing.T) {
	templateContent := readFolioThemeFile(t, "templates", "article_title_item.html")
	if !strings.Contains(templateContent, "class=\"toc-number\"") {
		t.Errorf("Folio article_title_item.html missing explicit `.toc-number` markup")
	}

	cssContent := readFolioThemeFile(t, "static", "css", "style.css")
	tocListRule := extractCSSRuleBlock(t, cssContent, ".folio-toc ol")

	comment := "Folio TOC template explicitly adds .toc-number labels. Do not use decimal list-style here or browser will duplicate them."
	if !strings.Contains(tocListRule, comment) {
		t.Errorf("Folio style.css missing explanatory comment for TOC numbering: %q", comment)
	}
	if !strings.Contains(tocListRule, "list-style: none;") {
		t.Errorf("Folio TOC rule missing `list-style: none;` to disable browser markers")
	}
	if strings.Contains(tocListRule, "list-style: decimal;") {
		t.Errorf("Folio TOC rule still contains `list-style: decimal;` causing duplicate TOC numbers")
	}
}

func TestFolioTocRendersHierarchicalNumbering(t *testing.T) {
	folioTocTemplate, err := template.New("folio-toc").Funcs(template.FuncMap{
		"add": func(a, b int) int {
			return a + b
		},
		"tocTemplateData": newTOCTemplateData,
		"tocNumberLabel":  tocNumberLabel,
	}).Parse(readFolioThemeFile(t, "templates", "article_title_item.html"))
	if err != nil {
		t.Fatalf("parse Folio TOC template: %v", err)
	}

	tocItems := []*model.ArticleTOC{
		{
			Title: "Overview",
			Slug:  "overview",
			Level: 2,
			SubTitles: []*model.ArticleTOC{
				{
					Title: "Background",
					Slug:  "background",
					Level: 3,
					SubTitles: []*model.ArticleTOC{
						{Title: "Details", Slug: "details", Level: 4},
					},
				},
				{Title: "Scope", Slug: "scope", Level: 3},
			},
		},
		{Title: "Appendix", Slug: "appendix", Level: 2},
	}

	var rendered bytes.Buffer
	if err := folioTocTemplate.ExecuteTemplate(&rendered, "site/article_title_item", newTOCTemplateData(tocItems, "")); err != nil {
		t.Fatalf("render Folio TOC template: %v", err)
	}

	expectedNumbers := []string{"1.", "1.1.", "1.1.1.", "1.2.", "2."}
	actualNumbers := extractFolioTocNumbers(rendered.String())
	if !reflect.DeepEqual(actualNumbers, expectedNumbers) {
		t.Fatalf("Folio TOC numbers = %v, want %v\nrendered HTML:\n%s", actualNumbers, expectedNumbers, rendered.String())
	}
}

func extractCSSRuleBlock(t *testing.T, cssContent, selector string) string {
	t.Helper()

	ruleStart := strings.Index(cssContent, selector+" {")
	if ruleStart == -1 {
		t.Fatalf("style.css missing %s rule", selector)
	}
	ruleEnd := strings.Index(cssContent[ruleStart:], "\n}")
	if ruleEnd == -1 {
		t.Fatalf("style.css has unterminated %s rule", selector)
	}
	return cssContent[ruleStart : ruleStart+ruleEnd+len("\n}")]
}

func readFolioThemeFile(t *testing.T, parts ...string) string {
	t.Helper()

	pathParts := append([]string{"..", "resource", "themes", "site", "folio"}, parts...)
	content, err := os.ReadFile(filepath.Join(pathParts...))
	if err != nil {
		t.Fatalf("read Folio theme file %v: %v", parts, err)
	}
	return string(content)
}

func extractFolioTocNumbers(renderedHTML string) []string {
	matches := folioTocNumberPattern.FindAllStringSubmatch(renderedHTML, -1)
	numbers := make([]string, 0, len(matches))
	for _, match := range matches {
		numbers = append(numbers, strings.TrimSpace(match[1]))
	}
	return numbers
}
