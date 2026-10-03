package router

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/naiba/solitudes/internal/model"
)

func TestPostgresBookTreeVisibilityBatchingAndCycles(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.Article{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	admin := model.Account{Email: "books@example.test", Role: model.RoleAdmin, EmailVerifiedAt: &now}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	if err := seedBookFixture(db, admin); err != nil {
		t.Fatal(err)
	}
	var root, hidden, part model.Article
	for slug, target := range map[string]*model.Article{"visual-book": &root, "visual-private-part": &hidden, "visual-book-part": &part} {
		if err := db.Take(target, "slug = ?", slug).Error; err != nil {
			t.Fatal(err)
		}
	}
	// A public descendant of a hidden book must not leak through tree traversal.
	child := model.Article{BookRefer: &hidden.ID, Slug: "hidden-parent-public-child", Title: "Hidden subtree", Content: "do not fetch this body"}
	if err := db.Create(&child).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Article{}).Where("1=1").Updates(map[string]interface{}{"read_num": 1, "comment_num": 2}).Error; err != nil {
		t.Fatal(err)
	}
	tracker := &performanceSQL{Interface: logger.Default.LogMode(logger.Silent)}
	queryDB := db.Session(&gorm.Session{Logger: tracker})
	for _, tc := range []struct {
		name    string
		account *model.Account
		total   uint
	}{
		{"guest", nil, 5},
		{"reader", &model.Account{Role: model.RoleUser, EmailVerifiedAt: &now}, 6},
		{"editor", &model.Account{Role: model.RoleEditor, EmailVerifiedAt: &now}, 7},
		{"admin", &admin, 9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scoped := model.ReadableArticles(queryDB, tc.account, nil)
			items := []model.Article{root, part, {Title: "not a book", ReadNum: 99}}
			for repeat := 0; repeat < 2; repeat++ {
				tracker.count.Store(0)
				if err := model.AggregateBookCounts(scoped, items); err != nil {
					t.Fatal(err)
				}
				if tracker.count.Load() != 1 {
					t.Fatalf("counter query count = %d", tracker.count.Load())
				}
				if items[0].ReadNum != tc.total || items[0].CommentNum != tc.total*2 || items[1].ReadNum != 2 || items[2].ReadNum != 99 {
					t.Fatalf("incorrect totals: %+v", items)
				}
			}
			a := root
			for repeat := 0; repeat < 2; repeat++ {
				tracker.count.Store(0)
				if err := a.LoadChapters(scoped); err != nil {
					t.Fatal(err)
				}
				if tracker.count.Load() != 1 || a.ReadNum != tc.total || a.CommentNum != 2*tc.total {
					t.Fatal("tree query count or totals changed")
				}
				if a.Content != root.Content {
					t.Fatal("root body lost")
				}
				seen := map[string]bool{a.ID: true}
				var visit func(*model.Article)
				visit = func(parent *model.Article) {
					for i, n := range parent.Chapters {
						if seen[n.ID] {
							t.Fatal("cycle in rendered tree")
						}
						seen[n.ID] = true
						if n.Content != "" {
							t.Fatal("loaded chapter body")
						}
						if !n.CanRead(tc.account) {
							t.Fatal("unreadable chapter leaked")
						}
						if i > 0 {
							prev := parent.Chapters[i-1]
							if n.CreatedAt.Before(prev.CreatedAt) || (n.CreatedAt.Equal(prev.CreatedAt) && n.ID < prev.ID) {
								t.Fatal("unstable chapter ordering")
							}
						}
						visit(n)
					}
				}
				visit(&a)
				if uint(len(seen)) != tc.total {
					t.Fatal("missing or duplicate tree node")
				}
				if seen[child.ID] != (tc.name == "admin") {
					t.Fatal("hidden parent subtree escaped scope")
				}
			}
		})
	}
	// Corrupt historical cycles terminate in SQL and are not recreated in Go.
	if err := db.Model(&model.Article{}).Where("id = ?", root.ID).Update("book_refer", part.ID).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := root.LoadChapters(queryDB.WithContext(ctx)); err != nil {
		t.Fatal(err)
	}
	if root.ReadNum != 9 || len(root.Chapters) != 6 || len(root.Chapters[2].Chapters) != 1 {
		t.Fatal("cycle traversal returned incorrect tree")
	}
	if err := model.AggregateBookCounts(queryDB.WithContext(ctx), []model.Article{root, part}); err != nil {
		t.Fatal(err)
	}
	if err := root.LoadChapters(queryDB.Where("missing_column = 1")); err == nil {
		t.Fatal("database error swallowed")
	}
	if err := model.AggregateBookCounts(queryDB.Where("missing_column = 1"), []model.Article{root}); err == nil {
		t.Fatal("aggregate database error swallowed")
	}
	tracker.count.Store(0)
	if err := model.AggregateBookCounts(queryDB, []model.Article{{Title: "ordinary article"}}); err != nil || tracker.count.Load() != 0 {
		t.Fatal("non-books issued a traversal")
	}
}
