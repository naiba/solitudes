//go:build !postgres_test

package router

import (
	"context"
	"testing"
	"time"

	"github.com/naiba/solitudes/internal/model"
)

func TestValidClientHomepage(t *testing.T) {
	for _, tc := range []struct {
		url   string
		valid bool
	}{
		{"https://example.com/", true},
		{"https://example.com/path?lang=zh", true},
		{"http://localhost:8080/", true},
		{"http://127.0.0.1:8080/", true},
		{"", false},
		{"/users/123", false},
		{"javascript:alert(1)", false},
		{"data:text/html,hello", false},
		{"http://example.com/", false},
		{"http://localhost.evil.test/", false},
		{"https://user:password@example.com/", false},
		{"https://example.com/#login", false},
		{"https://example.com\\@evil.test/", false},
		{"https://example.com/\nmalicious", false},
	} {
		if got := validClientHomepage(tc.url); got != tc.valid {
			t.Errorf("validClientHomepage(%q) = %v, want %v", tc.url, got, tc.valid)
		}
	}
}

func TestOIDCRevocationDoesNotRevealOrRemoveAnotherClientsTokens(t *testing.T) {
	db := newIdentityTestDB(t)
	storage := &oidcStorage{db: db}
	access := model.OIDCAccessToken{ID: "access-id", ClientID: "owner-client", AccountID: "user-id", ExpiresAt: time.Now().Add(time.Hour)}
	refresh := model.OIDCRefreshToken{ID: "refresh-id", TokenHash: secretHash("refresh-secret"),
		ClientID: "owner-client", AccountID: "user-id", AccessID: access.ID, ExpiresAt: time.Now().Add(time.Hour)}
	if err := db.Create(&access).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&refresh).Error; err != nil {
		t.Fatal(err)
	}
	for _, token := range []struct{ value, subject string }{{access.ID, "user-id"}, {"refresh-secret", ""}} {
		if got := storage.RevokeToken(context.Background(), token.value, token.subject, "different-client"); got != nil {
			t.Fatalf("another client's token leaked via revocation response: %v", got)
		}
	}
	var accessCount, refreshCount int64
	if err := db.Model(&model.OIDCAccessToken{}).Where("id = ?", access.ID).Count(&accessCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.OIDCRefreshToken{}).Where("id = ?", refresh.ID).Count(&refreshCount).Error; err != nil {
		t.Fatal(err)
	}
	if accessCount != 1 || refreshCount != 1 {
		t.Fatalf("another client's tokens changed: access=%d refresh=%d", accessCount, refreshCount)
	}
}
