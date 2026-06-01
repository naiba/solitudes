package router

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
