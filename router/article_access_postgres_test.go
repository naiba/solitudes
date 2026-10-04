package router

import (
	"strings"
	"testing"
	"time"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

func TestPostgresArticleVisibilityAndQueryRedaction(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.Article{}, &model.Comment{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	owner := model.Account{Email: "owner@access.test", Role: model.RoleEditor, EmailVerifiedAt: &now}
	member := model.Account{Email: "member@access.test", Role: model.RoleUser, EmailVerifiedAt: &now}
	editor := model.Account{Email: "editor@access.test", Role: model.RoleEditor, EmailVerifiedAt: &now}
	admin := model.Account{Email: "admin@access.test", Role: model.RoleAdmin, EmailVerifiedAt: &now}
	for _, a := range []*model.Account{&owner, &member, &editor, &admin} {
		if err := db.Create(a).Error; err != nil {
			t.Fatal(err)
		}
	}
	var articles []model.Article
	for i, v := range []model.ArticleVisibility{model.VisibilityPublic, model.VisibilityMembers, model.VisibilityEditors, model.VisibilityPrivate} {
		a := model.Article{AuthorID: &owner.ID, Title: string(v), Slug: string(v), Visibility: v, TemplateID: solitudes.ArticleTemplateID, ReadNum: uint(10 - i),
			Content: "public-token\n\n```access:members\nmember-secret\n```\n\n```access:editors\neditor-secret\n```\n\n```access:private\nprivate-secret\n```"}
		if err := db.Create(&a).Error; err != nil {
			t.Fatal(err)
		}
		articles = append(articles, a)
	}
	for _, account := range []*model.Account{nil, &member, &editor, &owner, &admin} {
		q := &TemplateQueries{db: db, account: account}
		got, err := q.Articles("reads", 10)
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		for _, a := range articles {
			if a.CanRead(account) {
				want++
			}
		}
		if len(got) != want {
			t.Fatalf("visibility count=%d want %d", len(got), want)
		}
		for _, a := range got {
			for _, level := range []string{"members", "editors", "private"} {
				token := map[string]string{"members": "member-secret", "editors": "editor-secret", "private": "private-secret"}[level]
				if strings.Contains(a.Content, token) != a.Allows(level, account) {
					t.Fatalf("query redaction failed: %s %s", a.Slug, level)
				}
			}
		}
		// Queries and rendering must never mutate the persisted editorial source.
		var raw model.Article
		if err := db.First(&raw, "id = ?", articles[0].ID).Error; err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(raw.Content, "private-secret") {
			t.Fatal("redaction modified storage")
		}
		if _, err := q.Comments(articles[3].ID, 5); (err == nil) != articles[3].CanRead(account) {
			t.Fatal("comment query escaped article scope", err)
		}
		for _, order := range []string{"newest", "oldest", "updated", "comments"} {
			if _, err := q.Articles(order, 1); err != nil {
				t.Fatal(err)
			}
		}
	}
	q := &TemplateQueries{db: db}
	for _, order := range []string{"created_at; DROP TABLE articles", "random", ""} {
		if _, err := q.Articles(order, 2); err == nil {
			t.Fatal("unsafe order accepted")
		}
	}
	for _, filters := range [][]string{{"tag"}, {"sql", "true"}, {"offset", "-1"}, {"template", "x"}} {
		if _, err := q.Articles("newest", 5, filters...); err == nil {
			t.Fatal("invalid filter accepted", filters)
		}
	}
	for _, n := range []int{-1, 101} {
		if _, err := q.Articles("newest", n); err == nil {
			t.Fatal("invalid count accepted")
		}
	}
	if got, err := q.Articles("newest", 0); err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
}
