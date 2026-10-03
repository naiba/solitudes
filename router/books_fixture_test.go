package router

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

func seedBookFixture(db *gorm.DB, admin model.Account) error {
	stamp := time.Date(2022, 3, 4, 10, 0, 0, 0, time.UTC)
	book := model.Article{ID: "30000000-0000-4000-8000-000000000001", AuthorID: &admin.ID, Slug: "visual-book", Title: "A field guide to thoughtful writing", Content: "An introduction to the series. Choose a chapter below.", IsBook: true, TemplateID: solitudes.ArticleTemplateID, CreatedAt: stamp, Version: 1}
	part := model.Article{ID: "30000000-0000-4000-8000-000000000004", AuthorID: &admin.ID, BookRefer: &book.ID, Slug: "visual-book-part", Title: "Part two: writing together", Content: "A nested section of the series.", IsBook: true, TemplateID: solitudes.ArticleTemplateID, CreatedAt: stamp.Add(time.Minute), Version: 1}
	if err := db.Create(&book).Error; err != nil {
		return err
	}
	if err := db.Create(&part).Error; err != nil {
		return err
	}
	for _, chapter := range []model.Article{
		{ID: "30000000-0000-4000-8000-000000000002", BookRefer: &book.ID, Slug: "visual-chapter", Title: "Chapter 1: Reading on every screen", Content: "The first chapter.", CreatedAt: stamp, ReadNum: 10, CommentNum: 2},
		{ID: "30000000-0000-4000-8000-000000000003", BookRefer: &book.ID, Slug: "visual-chapter-two", Title: "Chapter 2: Turning a very long working title into a readable chapter heading", Content: "The second chapter.", CreatedAt: stamp},
		{BookRefer: &part.ID, Slug: "visual-nested-chapter", Title: "Chapter 3: A conversation with readers", Content: "A chapter inside a nested part.", CreatedAt: stamp},
		{BookRefer: &book.ID, Slug: "visual-members-chapter", Title: "Members chapter", Content: "member-book-secret", Visibility: model.VisibilityMembers, CreatedAt: stamp.Add(2 * time.Minute)},
		{BookRefer: &book.ID, Slug: "visual-editors-chapter", Title: "Editors chapter", Content: "editor-book-secret", Visibility: model.VisibilityEditors, CreatedAt: stamp.Add(3 * time.Minute)},
		{BookRefer: &book.ID, Slug: "visual-private-part", Title: "Private part", Content: "private-book-secret", IsBook: true, Visibility: model.VisibilityPrivate, CreatedAt: stamp.Add(4 * time.Minute)},
		{Slug: "visual-empty-book", Title: "A new series, coming soon", Content: "Chapters will be published here.", IsBook: true, CreatedAt: stamp},
	} {
		chapter.AuthorID = &admin.ID
		chapter.TemplateID = solitudes.ArticleTemplateID
		chapter.Version = 1
		if err := db.Create(&chapter).Error; err != nil {
			return err
		}
	}
	return nil
}

// A separate, public three-level series leaves the permission/timestamp fixture
// above stable while exercising real navigation and deep comment threads.
func seedDeepBookFixture(db *gorm.DB, admin, editor, reader model.Account) error {
	stamp := time.Date(2021, 6, 1, 12, 0, 0, 0, time.UTC)
	var parent *string
	for level := 1; level <= 3; level++ {
		book := model.Article{ID: fmt.Sprintf("50000000-0000-4000-8000-%012d", level), AuthorID: &admin.ID, BookRefer: parent,
			Slug: fmt.Sprintf("deep-book-%d", level), Title: fmt.Sprintf("Level %d · Building a thoughtful publication / 深入写作", level),
			Content: "## About this collection\n\nRead the chapters below, then explore the next collection.\n\n## Reading together\n\nEach section includes its own articles and conversations.\n\n## Start reading\n\nChoose a chapter from the list.",
			IsBook:  true, TemplateID: solitudes.ArticleTemplateID, Version: 1, CreatedAt: stamp.Add(time.Duration(level) * time.Hour)}
		if err := db.Create(&book).Error; err != nil {
			return err
		}
		for n := 1; n <= 2; n++ {
			author := admin
			if n == 2 {
				author = editor
			}
			chapter := model.Article{AuthorID: &author.ID, BookRefer: &book.ID, Slug: fmt.Sprintf("deep-chapter-%d-%d", level, n),
				Title:      fmt.Sprintf("%d.%d · Notes on reading, writing and conversations that continue across chapters", level, n),
				Content:    "## A starting point\n\nA chapter can be read on its own or as part of a longer series.\n\n## An example\n\n- Leave enough room for the text.\n- Keep the next step easy to find.\n\n## Join the conversation\n\nReaders can reply to a comment and continue the discussion without losing the original context.",
				TemplateID: solitudes.ArticleTemplateID, Version: 1, CreatedAt: book.CreatedAt.Add(time.Duration(n) * time.Minute), CommentNum: 4}
			if err := db.Create(&chapter).Error; err != nil {
				return err
			}
			root := model.Comment{ArticleID: &chapter.ID, Nickname: "Curious guest", Content: "Level 1: How would you apply this idea to a small community?", Version: 1, CreatedAt: chapter.CreatedAt}
			if err := db.Create(&root).Error; err != nil {
				return err
			}
			reply := model.Comment{ArticleID: &chapter.ID, AccountID: &reader.ID, ReplyTo: &root.ID, Content: "Level 2: Start with a short series and invite readers to respond. 我们可以先从一个小问题聊起。", Version: 1, CreatedAt: chapter.CreatedAt.Add(time.Minute)}
			if err := db.Create(&reply).Error; err != nil {
				return err
			}
			for _, comment := range []model.Comment{
				{ArticleID: &chapter.ID, AccountID: &editor.ID, ReplyTo: &reply.ID, Content: "Level 3: That works well. Keep the context visible, including on a phone, so a new reader can follow the conversation.", Version: 1, CreatedAt: chapter.CreatedAt.Add(2 * time.Minute)},
				{ArticleID: &chapter.ID, AccountID: &admin.ID, ReplyTo: &root.ID, Content: "Another level 2 reply: Everyone is welcome to share a different perspective.", Version: 1, CreatedAt: chapter.CreatedAt.Add(3 * time.Minute)},
			} {
				if err := db.Create(&comment).Error; err != nil {
					return err
				}
			}
		}
		parent = &book.ID
	}
	return nil
}
