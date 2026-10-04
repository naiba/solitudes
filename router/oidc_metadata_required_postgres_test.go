package router

import (
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/naiba/solitudes/internal/model"
)

func TestPostgresOIDCRequiresOwnerNameAndSafeHomepage(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.OIDCClient{}); err != nil {
		t.Fatal(err)
	}
	owner := model.Account{Email: "owner@metadata.test", Nickname: "Owner", Role: model.RoleUser}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("test-client-secret"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	storage := &oidcStorage{db: db}
	for _, tc := range []struct {
		name  string
		alter func(*model.OIDCClient)
		valid bool
	}{
		{"valid", func(c *model.OIDCClient) {}, true},
		{"owner-missing", func(c *model.OIDCClient) { c.OwnerID = nil }, false},
		{"name-missing", func(c *model.OIDCClient) { c.Name = " " }, false},
		{"homepage-missing", func(c *model.OIDCClient) { c.HomepageURL = "" }, false},
		{"homepage-unsafe", func(c *model.OIDCClient) { c.HomepageURL = "javascript:alert(1)" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := model.OIDCClient{ID: tc.name, OwnerID: &owner.ID, Name: "Test app", HomepageURL: "https://app.example.test", SecretHash: string(hash), RedirectURIsJSON: `["https://app.example.test/callback"]`}
			tc.alter(&client)
			if err := db.Create(&client).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := storage.GetClientByClientID(t.Context(), client.ID); (err == nil) != tc.valid {
				t.Fatalf("client lookup accepted invalid metadata or rejected valid app: %v", err)
			}
			if err := storage.AuthorizeClientIDSecret(t.Context(), client.ID, "test-client-secret"); (err == nil) != tc.valid {
				t.Fatalf("client authentication accepted invalid metadata or rejected valid app: %v", err)
			}
		})
	}
}
