package model

import "testing"

func TestCommentCountsTowardArticle(t *testing.T) {
	replyID := "00000000-0000-0000-0000-000000000001"
	tests := []struct {
		name    string
		comment Comment
		want    bool
	}{
		{name: "visible root", comment: Comment{}, want: true},
		{name: "spam root", comment: Comment{IsSpam: true}, want: false},
		{name: "visible reply", comment: Comment{ReplyTo: &replyID}, want: false},
		{name: "spam reply", comment: Comment{ReplyTo: &replyID, IsSpam: true}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.comment.CountsTowardArticle(); got != tt.want {
				t.Fatalf("CountsTowardArticle() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCommentPublicRoleUsesAccountNotClaimedIdentity(t *testing.T) {
	id := "10000000-0000-4000-8000-000000000001"
	for _, tt := range []struct {
		name string
		cm   Comment
		want string
	}{
		{"visitor", Comment{Nickname: "Administrator", Email: "admin@example.com"}, "guest"},
		{"authenticated member", Comment{AccountID: &id, Account: &Account{Role: RoleUser}}, "user"},
		{"authenticated editor", Comment{AccountID: &id, Account: &Account{Role: RoleEditor}}, "editor"},
		{"authenticated administrator", Comment{AccountID: &id, Account: &Account{Role: RoleAdmin}}, "admin"},
		{"removed account", Comment{AccountID: &id}, "guest"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cm.PublicRole(); got != tt.want {
				t.Fatalf("PublicRole() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCommentPublicNameFollowsVerifiedAccount(t *testing.T) {
	id := "10000000-0000-4000-8000-000000000001"
	cm := Comment{Nickname: "Previous Name", AccountID: &id, Account: &Account{Nickname: "Current Name"}}
	if got := cm.PublicName(); got != "Current Name" {
		t.Fatalf("PublicName() = %q", got)
	}
	cm.Account = nil
	if got := cm.PublicName(); got != "Previous Name" {
		t.Fatalf("missing account fallback = %q", got)
	}
}
