package router

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

func accountPage(c *fiber.Ctx) error {
	account := currentAccount(c)
	page, err := listPage(c.Query("passkeys_page"))
	if err != nil {
		return err
	}
	var keys []model.Passkey
	if err := solitudes.System.DB.Where("account_id = ?", account.ID).Order("created_at DESC, id DESC").Limit(11).Offset((page - 1) * 10).Find(&keys).Error; err != nil {
		return err
	}
	more := len(keys) > 10
	if more {
		keys = keys[:10]
	}
	var identities []model.ExternalIdentity
	if err := solitudes.System.DB.Where("account_id = ?", account.ID).Order("provider").Find(&identities).Error; err != nil {
		return err
	}
	type connectedIdentity struct {
		Provider string
		Enabled  bool
	}
	connected := make([]connectedIdentity, 0, len(identities))
	linked := make(map[string]bool, len(identities))
	for _, identity := range identities {
		connected = append(connected, connectedIdentity{Provider: identity.Provider,
			Enabled: externalProviderEnabled(identity.Provider)})
		linked[identity.Provider] = true
	}
	c.Set("Cache-Control", "private, no-store")
	return c.Status(http.StatusOK).Render("site/account", injectSiteData(c, fiber.Map{
		"title": "Account", "noindex": true, "passkeys": keys, "identities": connected,
		"linked_github": linked["github"], "linked_google": linked["google"], "linked_oidc": linked["oidc"],
		"password_changed":    c.Query("password_changed") == "1",
		"profile_updated":     c.Query("profile_updated") == "1",
		"passkeys_navigation": pageNavigationFor(c, "passkeys_page", page, more, "passkeys"),
	}))
}

func updateAccountProfile(c *fiber.Ctx) error {
	account := currentAccount(c)
	nickname := strings.TrimSpace(c.FormValue("nickname"))
	bio := strings.TrimSpace(c.FormValue("bio"))
	visible := c.FormValue("directory_visible")
	if visible != "" && visible != "on" {
		return fiber.NewError(http.StatusBadRequest, "invalid reader circle preference")
	}
	if nickname == "" || utf8.RuneCountInString(nickname) > 80 || utf8.RuneCountInString(bio) > 500 ||
		strings.ContainsAny(nickname, "\r\n\x00") || strings.ContainsRune(bio, '\x00') {
		return fiber.NewError(http.StatusBadRequest, "invalid public profile")
	}
	if err := solitudes.System.DB.Model(&model.Account{}).Where("id = ?", account.ID).
		Updates(map[string]interface{}{"nickname": nickname, "bio": bio, "directory_hidden": visible != "on"}).Error; err != nil {
		return err
	}
	return c.Redirect("/account?profile_updated=1", http.StatusSeeOther)
}

func changeAccountPassword(c *fiber.Ctx) error {
	account := currentAccount(c)
	oldPassword := c.FormValue("old_password")
	newPassword := c.FormValue("new_password")
	if account.PasswordHash == "" || bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(oldPassword)) != nil {
		return fiber.NewError(http.StatusForbidden, "incorrect current password")
	}
	if len(newPassword) < 12 || len(newPassword) > 72 {
		return fiber.NewError(http.StatusBadRequest, "new password must be 12–72 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.Account{}).Where("id = ?", account.ID).Update("password_hash", string(hash)).Error; err != nil {
			return err
		}
		if err := tx.Where("account_id = ? AND token_hash <> ?", account.ID,
			secretHash(c.Cookies(solitudes.AuthCookie))).Delete(&model.LoginSession{}).Error; err != nil {
			return err
		}
		if err := tx.Where("account_id = ?", account.ID).Delete(&model.OIDCRefreshToken{}).Error; err != nil {
			return err
		}
		return tx.Where("account_id = ?", account.ID).Delete(&model.OIDCAccessToken{}).Error
	}); err != nil {
		return err
	}
	return c.Redirect("/account?password_changed=1", http.StatusSeeOther)
}

// An OAuth-only account must retain at least one usable sign-in method.
func unlinkAccountIdentity(c *fiber.Ctx) error {
	provider := c.Params("provider")
	if provider != "github" && provider != "google" && provider != "oidc" {
		return fiber.ErrNotFound
	}
	account := currentAccount(c)
	err := solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		var owner model.Account
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&owner, "id = ?", account.ID).Error; err != nil {
			return err
		}
		usableIdentity, err := hasUsableExternalIdentity(tx, account.ID, provider)
		if err != nil {
			return err
		}
		var keys int64
		if err := tx.Model(&model.Passkey{}).Where("account_id = ?", account.ID).Count(&keys).Error; err != nil {
			return err
		}
		if owner.PasswordHash == "" && keys == 0 && !usableIdentity {
			return fiber.NewError(http.StatusConflict, "cannot remove the last sign-in method")
		}
		result := tx.Where("account_id = ? AND provider = ?", account.ID, provider).Delete(&model.ExternalIdentity{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fiber.ErrNotFound
		}
		return nil
	})
	if err != nil {
		return err
	}
	return c.Redirect("/account", http.StatusSeeOther)
}

func hasUsableExternalIdentity(tx *gorm.DB, accountID, exceptProvider string) (bool, error) {
	var identities []model.ExternalIdentity
	if err := tx.Select("provider").Where("account_id = ? AND provider <> ?", accountID, exceptProvider).
		Find(&identities).Error; err != nil {
		return false, err
	}
	for _, identity := range identities {
		if externalProviderEnabled(identity.Provider) {
			return true, nil
		}
	}
	return false, nil
}

func externalProviderEnabled(provider string) bool {
	cfg := solitudes.System.Config.Auth
	switch provider {
	case "github":
		return cfg.GitHub.ClientID != "" && cfg.GitHub.ClientSecret != ""
	case "google":
		return cfg.Google.ClientID != "" && cfg.Google.ClientSecret != ""
	case "oidc":
		return cfg.OIDC.Issuer != "" && cfg.OIDC.ClientID != "" && cfg.OIDC.ClientSecret != ""
	default:
		return false
	}
}

func usersPage(c *fiber.Ctx) error {
	return adminUsersPage(c)
}

func setUserRole(c *fiber.Ctx) error {
	id := c.Params("id")
	role := model.Role(strings.TrimSpace(c.FormValue("role")))
	if role != model.RoleAdmin && role != model.RoleEditor && role != model.RoleUser {
		return fiber.NewError(http.StatusBadRequest, "invalid role")
	}
	if id == currentAccount(c).ID {
		return fiber.NewError(http.StatusForbidden, "cannot change your own role")
	}
	err := solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		var account model.Account
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&account, "id = ?", id).Error; err != nil {
			return err
		}
		previous := account.Role
		if err := tx.Model(&account).Update("role", role).Error; err != nil {
			return err
		}
		return auditMutation(c, tx, "user.role.update", account.ID, "", string(previous)+" -> "+string(role))
	})
	if err != nil {
		return err
	}
	c.Locals("audit_recorded", true)
	return c.Redirect("/admin/users", http.StatusSeeOther)
}
