package router

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArticleSharingDoesNotHaveAStandaloneActionRow(t *testing.T) {
	for _, theme := range []string{"cactus", "folio"} {
		for _, name := range []string{"article.html", "page.html"} {
			file := filepath.Join("..", "resource", "themes", "site", theme, "templates", name)
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			body := string(raw)
			for _, marker := range []string{`data-testid="article-byline"`, `data-testid="article-meta"`, `{{template "site/share_button" .}}`} {
				if strings.Count(body, marker) != 1 {
					t.Errorf("%s must contain one %s", file, marker)
				}
			}
			if strings.Contains(body, `class="article-actions"`) {
				t.Errorf("%s retains a standalone sharing row", file)
			}
		}
	}
}

func TestSharingToolsStayEquivalentAcrossThemes(t *testing.T) {
	for file, marker := range map[string]string{"templates/reading_tools.html": `{{define "site/reading_toc"}}`, "static/js/reading.js": "  const toc"} {
		folio, err := os.ReadFile(filepath.Join("..", "resource/themes/site/folio", file))
		if err != nil {
			t.Fatal(err)
		}
		cactus, err := os.ReadFile(filepath.Join("..", "resource/themes/site/cactus", file))
		if err != nil {
			t.Fatal(err)
		}
		folioEnd, cactusEnd := bytes.Index(folio, []byte(marker)), bytes.Index(cactus, []byte(marker))
		if folioEnd < 0 || cactusEnd < 0 || !bytes.Equal(folio[:folioEnd], cactus[:cactusEnd]) {
			t.Errorf("sharing tool behavior diverged: %s", file)
		}
	}
}
