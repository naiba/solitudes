package router

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

func TestPostgresFeedsCreditEachAuthorWithoutPublishingAdminEmail(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.Article{}); err != nil {
		t.Fatal(err)
	}
	previous := solitudes.System
	t.Cleanup(func() { solitudes.System = previous })
	config := &model.Config{}
	config.Site.Domain = "localhost:8080"
	config.Site.SpaceName = "A many-author publication"
	solitudes.System = &solitudes.SysVariable{DB: db, Config: config}
	now := time.Now()
	admin := model.Account{Nickname: "Publisher Alice", Email: "alice@example.test", Role: model.RoleAdmin, EmailVerifiedAt: &now}
	editor := model.Account{Nickname: "Editor Bob", Email: "bob@example.test", Role: model.RoleEditor, EmailVerifiedAt: &now}
	for _, account := range []*model.Account{&admin, &editor} {
		if err := db.Create(account).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, article := range []*model.Article{
		{AuthorID: &admin.ID, Title: "Alice's post", Slug: "alice-public", Content: "Written by Alice"},
		{AuthorID: &editor.ID, Title: "Bob's post", Slug: "bob-public", Content: "Written by Bob"},
		{AuthorID: &admin.ID, Title: "Not public", Slug: "secret", Content: "secret-feed-text", Visibility: model.VisibilityPrivate},
	} {
		if err := db.Create(article).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, format := range []string{"rss", "atom", "json"} {
		t.Run(format, func(t *testing.T) {
			output, err := generateFeed(format)
			if err != nil {
				t.Fatal(err)
			}
			body := output.(*feedOutput).body
			if strings.Contains(body, admin.Email) || strings.Contains(body, editor.Email) ||
				strings.Contains(body, "secret-feed-text") || !strings.Contains(body, "Publisher Alice") || !strings.Contains(body, "Editor Bob") {
				t.Fatalf("wrong author or leaked private content in %s: %s", format, body)
			}
			if format == "json" {
				var feed struct {
					Authors []interface{} `json:"authors"`
					Items   []struct {
						Title   string `json:"title"`
						Authors []struct {
							Name string `json:"name"`
							URL  string `json:"url"`
						} `json:"authors"`
					} `json:"items"`
				}
				if err := json.Unmarshal([]byte(body), &feed); err != nil {
					t.Fatal(err)
				}
				if len(feed.Authors) != 0 || len(feed.Items) != 2 || len(feed.Items[0].Authors) != 1 ||
					len(feed.Items[1].Authors) != 1 || feed.Items[0].Authors[0].Name == feed.Items[1].Authors[0].Name ||
					feed.Items[0].Authors[0].URL != "https://localhost:8080/users/"+editor.ID ||
					feed.Items[1].Authors[0].URL != "https://localhost:8080/users/"+admin.ID {
					t.Fatalf("JSON feed must use distinct per-item authors: %+v", feed)
				}
			} else if format == "atom" && (!strings.Contains(body, "<uri>https://localhost:8080/users/"+admin.ID+"</uri>") ||
				!strings.Contains(body, "<uri>https://localhost:8080/users/"+editor.ID+"</uri>")) {
				t.Fatalf("Atom feed must link each author to their public profile: %s", body)
			}
		})
	}
}
