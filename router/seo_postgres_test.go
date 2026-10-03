package router

import (
	"encoding/xml"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

func TestPostgresSitemapOnlyIncludesPublicURLsAndEscapesXML(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.Article{}); err != nil {
		t.Fatal(err)
	}
	previous := solitudes.System
	t.Cleanup(func() { solitudes.System = previous })
	config := &model.Config{}
	config.Site.Domain = "community.example"
	solitudes.System = &solitudes.SysVariable{Config: config, DB: db}
	created := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	public := model.Article{Title: "Public", Slug: "public&story", RawTags: "Reading & writing,中文,shared", CreatedAt: created}
	private := model.Article{Title: "Secret", Slug: "secret-story", RawTags: "private-only,shared", Visibility: model.VisibilityPrivate}
	for _, a := range []*model.Article{&public, &private} {
		if err := db.Create(a).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Legacy rows without an update time use their creation date, never year 0001.
	if err := db.Model(&public).UpdateColumn("updated_at", nil).Error; err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	app.Get("/sitemap.xml", sitemapHandler)
	readLocations := func() map[string]string {
		t.Helper()
		resp, err := app.Test(httptest.NewRequest("GET", "/sitemap.xml", nil), -1)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Type"), "application/xml") ||
			strings.Contains(string(body), "private-only") || strings.Contains(string(body), "secret-story") {
			t.Fatalf("invalid or private sitemap: status=%d body=%s", resp.StatusCode, body)
		}
		var sitemap struct {
			XMLName xml.Name
			URLs    []struct {
				Location string `xml:"loc"`
				LastMod  string `xml:"lastmod"`
			} `xml:"url"`
		}
		if err := xml.Unmarshal(body, &sitemap); err != nil {
			t.Fatalf("sitemap is not valid XML: %v: %s", err, body)
		}
		if sitemap.XMLName.Space != "http://www.sitemaps.org/schemas/sitemap/0.9" {
			t.Fatalf("invalid sitemap namespace: %+v", sitemap.XMLName)
		}
		locations := make(map[string]string)
		for _, entry := range sitemap.URLs {
			if _, exists := locations[entry.Location]; exists {
				t.Fatalf("duplicate sitemap URL: %s", entry.Location)
			}
			locations[entry.Location] = entry.LastMod
		}
		return locations
	}
	locations := readLocations()
	for _, path := range []string{"/", "/posts/", "/books/", "/tags/", "/public&story", "/tags/Reading%20&%20writing/", "/tags/%E4%B8%AD%E6%96%87/", "/tags/shared/"} {
		if _, exists := locations["https://community.example"+path]; !exists {
			t.Errorf("missing public URL %s: %+v", path, locations)
		}
	}
	if locations["https://community.example/public&story"] != "2026-01-02" {
		t.Fatal("missing creation-date fallback")
	}
	if _, exists := locations["https://community.example/readers/"]; exists {
		t.Fatal("empty reader directory should not be indexed")
	}
	member := model.Account{Nickname: "Reader", Email: "reader@example.test", Role: model.RoleUser, EmailVerifiedAt: &created}
	if err := db.Create(&member).Error; err != nil {
		t.Fatal(err)
	}
	if _, exists := readLocations()["https://community.example/readers/"]; !exists {
		t.Fatal("public reader directory missing")
	}
}
