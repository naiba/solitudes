package model

import (
	"testing"
	"time"
)

func TestArticleVisibilityMatrix(t *testing.T) {
	now := time.Now()
	owner := "owner"
	for _, tc := range []struct {
		name    string
		account *Account
		want    []bool
	}{
		{"guest", nil, []bool{true, false, false, false}},
		{"unverified", &Account{ID: "reader", Role: RoleUser}, []bool{true, false, false, false}},
		{"member", &Account{ID: "reader", Role: RoleUser, EmailVerifiedAt: &now}, []bool{true, true, false, false}},
		{"editor", &Account{ID: "editor", Role: RoleEditor, EmailVerifiedAt: &now}, []bool{true, true, true, false}},
		{"owner", &Account{ID: owner, Role: RoleUser, EmailVerifiedAt: &now}, []bool{true, true, false, true}},
		{"admin", &Account{Role: RoleAdmin, EmailVerifiedAt: &now}, []bool{true, true, true, true}},
		{"disabled admin", &Account{Role: RoleAdmin, DisabledAt: &now}, []bool{true, false, false, false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for i, v := range []ArticleVisibility{VisibilityPublic, VisibilityMembers, VisibilityEditors, VisibilityPrivate} {
				a := Article{Visibility: v, AuthorID: &owner}
				if a.CanRead(tc.account) != tc.want[i] {
					t.Fatalf("%s access", v)
				}
			}
		})
	}
}

func TestTOCUsesMarkdownAST(t *testing.T) {
	a := Article{Content: "# Visible\n\n```text\n# Not a heading\n```\n\nVisible setext\n---\n\n## Nested"}
	a.GenTOC()
	a.GenTOC()
	if len(a.Toc) != 1 || len(a.Toc[0].SubTitles) != 2 {
		t.Fatalf("wrong/idempotence TOC: %+v", a.Toc)
	}
}
