package model

import (
	"context"

	"gorm.io/gorm"

	"github.com/naiba/solitudes/pkg/content"
)

type ArticleVisibility string

const (
	VisibilityPublic  ArticleVisibility = "public"
	VisibilityMembers ArticleVisibility = "members"
	VisibilityEditors ArticleVisibility = "editors"
	VisibilityPrivate ArticleVisibility = "private"
)

func (v ArticleVisibility) Valid() bool {
	return v == VisibilityPublic || v == VisibilityMembers || v == VisibilityEditors || v == VisibilityPrivate
}

func (a Article) Public() bool { return a.Visibility == "" || a.Visibility == VisibilityPublic }

func (a *Article) CanRead(account *Account) bool {
	if a.Public() {
		return true
	}
	return a.Allows(string(a.Visibility), account)
}

func (a *Article) Allows(level string, account *Account) bool {
	if account == nil || account.DisabledAt != nil {
		return false
	}
	if account.Role.IsAdmin() {
		return true
	}
	switch ArticleVisibility(level) {
	case VisibilityMembers:
		return account.EmailVerifiedAt != nil
	case VisibilityEditors:
		return account.Role.CanPublish()
	case VisibilityPrivate:
		return a.AuthorID != nil && *a.AuthorID == account.ID
	}
	return false
}

// ContentFor is also used by feeds and other non-template consumers.
func (a *Article) ContentFor(account *Account, notice func(string) string) string {
	if !a.CanRead(account) {
		return ""
	}
	result, err := content.Filter(a.Content, func(level string) bool { return a.Allows(level, account) }, notice)
	if err != nil {
		return ""
	}
	return result
}

type articleReaderKey struct{}
type articleReader struct {
	account *Account
	notice  func(string) string
}

// ReadableArticles binds both row visibility and content redaction to a query.
// Raw editorial queries deliberately do not use this scope.
func ReadableArticles(db *gorm.DB, account *Account, notice func(string) string) *gorm.DB {
	ctx := context.WithValue(db.Statement.Context, articleReaderKey{}, articleReader{account, notice})
	db = db.WithContext(ctx)
	if account == nil || account.DisabledAt != nil {
		return db.Where("visibility = ?", VisibilityPublic)
	}
	if account.Role.IsAdmin() {
		return db
	}
	levels := []ArticleVisibility{VisibilityPublic}
	if account.EmailVerifiedAt != nil {
		levels = append(levels, VisibilityMembers)
	}
	if account.Role.CanPublish() {
		levels = append(levels, VisibilityEditors)
	}
	if account.ID == "" {
		return db.Where("visibility IN ?", levels)
	}
	return db.Where("(visibility IN ? OR (visibility = ? AND author_id = ?))", levels, VisibilityPrivate, account.ID)
}

func (a *Article) redactForQuery(tx *gorm.DB) {
	if viewer, ok := tx.Statement.Context.Value(articleReaderKey{}).(articleReader); ok {
		a.Content = a.ContentFor(viewer.account, viewer.notice)
	}
}
