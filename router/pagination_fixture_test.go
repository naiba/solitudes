package router

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

// Deliberately exceeds every page size and uses equal timestamps to test the
// ID tie-breaker. Only used in isolated test schemas, never the preview DB.
func seedPaginationFixture(db *gorm.DB, admin, reader model.Account) (string, error) {
	stamp := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	var main model.Article
	for i := 0; i < 112; i++ {
		a := model.Article{AuthorID: &admin.ID, Title: fmt.Sprintf("pagingneedle document %03d", i), Slug: fmt.Sprintf("paging-%03d", i), Content: "pagingneedle public text", TemplateID: solitudes.ArticleTemplateID, CreatedAt: stamp, Version: 1, IsBook: i < 22, RawTags: "Paging & test"}
		if i == 0 {
			for j := 0; j < 51; j++ {
				a.RawTags += fmt.Sprintf(",paging-tag-%02d", j)
			}
		}
		if err := db.Create(&a).Error; err != nil {
			return "", err
		}
		if i == 0 {
			main = a
		}
	}
	private := model.Article{AuthorID: &admin.ID, Title: "pagingneedle private-only", Slug: "paging-private", Content: "pagingneedle confidential", Visibility: model.VisibilityPrivate, TemplateID: solitudes.ArticleTemplateID, Version: 1, RawTags: "private-only-tag"}
	if err := db.Create(&private).Error; err != nil {
		return "", err
	}
	var root model.Comment
	for i := 0; i < 23; i++ {
		comment := model.Comment{ArticleID: &main.ID, AccountID: &reader.ID, Content: fmt.Sprintf("paging root %02d", i), CreatedAt: stamp, Version: 1}
		if err := db.Create(&comment).Error; err != nil {
			return "", err
		}
		if i == 0 {
			root = comment
		}
	}
	for i := 0; i < 23; i++ {
		comment := model.Comment{ArticleID: &main.ID, ReplyTo: &root.ID, AccountID: &reader.ID, Content: fmt.Sprintf("paging reply %02d", i), CreatedAt: stamp, Version: 1}
		if err := db.Create(&comment).Error; err != nil {
			return "", err
		}
	}
	if err := db.Model(&main).Update("comment_num", 23).Error; err != nil {
		return "", err
	}
	for i := 0; i < 28; i++ {
		client := model.OIDCClient{ID: fmt.Sprintf("paging-client-%02d", i), OwnerID: &reader.ID, Name: fmt.Sprintf("Paging app %02d", i), CreatedAt: stamp, Public: true, HomepageURL: "https://example.test", RedirectURIsJSON: `["https://example.test/callback"]`}
		if err := db.Create(&client).Error; err != nil {
			return "", err
		}
		user := model.Account{Email: fmt.Sprintf("paging-%02d@example.test", i), Nickname: fmt.Sprintf("Paging member %02d", i), Role: model.RoleUser, CreatedAt: stamp, EmailVerifiedAt: &stamp}
		if err := db.Create(&user).Error; err != nil {
			return "", err
		}
		event := model.AuditEvent{ID: fmt.Sprintf("paging-audit-%02d", i), ActorID: user.ID, ClientID: "paging-client-00", Action: "oidc.login", Outcome: "success", CreatedAt: stamp}
		if err := db.Create(&event).Error; err != nil {
			return "", err
		}
	}
	for i := 0; i < 12; i++ {
		key := model.Passkey{AccountID: admin.ID, PublicKey: []byte{1}, CredentialID: []byte(fmt.Sprintf("paging-credential-%02d", i)), Name: fmt.Sprintf("Paging key %02d", i), CreatedAt: stamp}
		if err := db.Create(&key).Error; err != nil {
			return "", err
		}
	}
	return root.ID, nil
}
