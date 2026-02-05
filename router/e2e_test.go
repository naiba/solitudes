//go:build e2e

package router

import (
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

func TestE2EPages(t *testing.T) {
	baseURL := "http://localhost:8080"

	// 1. 获取一个有效的文章 Slug
	resp, err := http.Get(baseURL + "/sitemap.xml")
	if err != nil {
		t.Fatalf("Server is not running at %s: %v", baseURL, err)
	}
	defer resp.Body.Close()
	sitemap, _ := io.ReadAll(resp.Body)

	// 从 sitemap 提取第一个文章链接
	re := regexp.MustCompile(`<loc>https://[^/]+/([^/]+)/</loc>`)
	matches := re.FindStringSubmatch(string(sitemap))
	articleSlug := ""
	if len(matches) > 1 {
		articleSlug = matches[1]
	}

	pages := []struct {
		name string
		path string
	}{
		{"Home", "/"},
		{"Posts", "/posts/"},
		{"Tags", "/tags/"},
	}

	if articleSlug != "" {
		pages = append(pages, struct {
			name string
			path string
		}{"Article", "/" + articleSlug})
	} else {
		t.Log("Warning: No article slug found in sitemap, skipping article E2E check")
	}

	for _, pg := range pages {
		t.Run(pg.name, func(t *testing.T) {
			targetURL := baseURL + pg.path
			res, err := http.Get(targetURL)
			if err != nil {
				t.Fatalf("Failed to fetch %s: %v", targetURL, err)
			}
			defer res.Body.Close()

			if res.StatusCode != http.StatusOK {
				t.Errorf("%s returned status %d, want 200", targetURL, res.StatusCode)
			}

			body, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatalf("Failed to read body of %s: %v", targetURL, err)
			}

			content := string(body)
			if len(content) < 1000 {
				t.Errorf("Page %s seems too short (%d bytes), possibly blank or error page", targetURL, len(content))
			}

			// 检查是否包含常见的错误字样（根据项目中的翻译 Key 判断）
			if strings.Contains(content, "404") || strings.Contains(content, "Internal Server Error") {
				t.Errorf("Page %s contains error indicators", targetURL)
			}

			t.Logf("Successfully verified %s (%d bytes)", targetURL, len(content))
		})
	}
}
