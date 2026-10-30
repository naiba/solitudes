package router

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

func accountPage(writer http.ResponseWriter, httpRequest *http.Request) error {
	account := currentAccount(writer, httpRequest)
	page, err := listPage(queryValue(httpRequest, "passkeys_page"))
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
	writer.Header().Set("Cache-Control", "private, no-store")
	return renderPage(writer, httpRequest, http.StatusOK, "site/account", injectSiteData(writer, httpRequest, viewData{
		"title": "Account", "noindex": true, "passkeys": keys, "identities": connected,
		"linked_github": linked["github"], "linked_google": linked["google"], "linked_oidc": linked["oidc"],
		"password_changed":    queryValue(httpRequest, "password_changed") == "1",
		"profile_updated":     queryValue(httpRequest, "profile_updated") == "1",
		"passkeys_navigation": pageNavigationFor(writer, httpRequest, "passkeys_page", page, more, "passkeys"),
	}))
}

func updateAccountProfile(writer http.ResponseWriter, httpRequest *http.Request) error {
	account := currentAccount(writer, httpRequest)
	nickname := strings.TrimSpace(formValue(httpRequest, "nickname"))
	bio := strings.TrimSpace(formValue(httpRequest, "bio"))
	visible := formValue(httpRequest, "directory_visible")
	if visible != "" && visible != "on" {
		return newHTTPError(http.StatusBadRequest, "invalid reader circle preference")
	}
	if nickname == "" || utf8.RuneCountInString(nickname) > 80 || utf8.RuneCountInString(bio) > 500 ||
		strings.ContainsAny(nickname, "\r\n\x00") || strings.ContainsRune(bio, '\x00') {
		return newHTTPError(http.StatusBadRequest, "invalid public profile")
	}
	err := solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		var stored model.Account
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&stored, "id = ?", account.ID).Error; err != nil {
			return err
		}
		// Compare locked database state, not a potentially stale browser session.
		if stored.Nickname == nickname && stored.Bio == bio && stored.DirectoryHidden == (visible != "on") {
			return nil
		}
		if err := tx.Model(&stored).Updates(map[string]interface{}{"nickname": nickname, "bio": bio, "directory_hidden": visible != "on"}).Error; err != nil {
			return err
		}
		return auditMutation(writer, httpRequest, tx, "account.profile.update", account.ID, "", "")
	})
	if err != nil {
		return err
	}
	requestValue(httpRequest, "audit_recorded", true)
	return redirectResponse(writer, httpRequest, "/account?profile_updated=1", http.StatusSeeOther)
}

func changeAccountPassword(writer http.ResponseWriter, httpRequest *http.Request) error {
	account := currentAccount(writer, httpRequest)
	oldPassword := formValue(httpRequest, "old_password")
	newPassword := formValue(httpRequest, "new_password")
	if len(newPassword) < 12 || len(newPassword) > 72 {
		return newHTTPError(http.StatusBadRequest, "new password must be 12–72 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		var stored model.Account
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&stored, "id = ?", account.ID).Error; err != nil {
			return err
		}
		if stored.DisabledAt != nil || stored.EmailVerifiedAt == nil {
			return newHTTPError(http.StatusForbidden)
		}
		// OAuth accounts can set their first password without an old one. Check
		// locked database state so a stale passwordless request cannot overwrite
		// a password that another request has just set.
		if stored.PasswordHash != "" && bcrypt.CompareHashAndPassword([]byte(stored.PasswordHash), []byte(oldPassword)) != nil {
			return newHTTPError(http.StatusForbidden, "incorrect current password")
		}
		if err := tx.Model(&stored).Update("password_hash", string(hash)).Error; err != nil {
			return err
		}
		if err := tx.Where("account_id = ? AND token_hash <> ?", account.ID,
			secretHash(requestCookie(httpRequest, solitudes.AuthCookie))).Delete(&model.LoginSession{}).Error; err != nil {
			return err
		}
		if err := tx.Where("account_id = ?", account.ID).Delete(&model.OIDCRefreshToken{}).Error; err != nil {
			return err
		}
		return tx.Where("account_id = ?", account.ID).Delete(&model.OIDCAccessToken{}).Error
	}); err != nil {
		return err
	}
	return redirectResponse(writer, httpRequest, "/account?password_changed=1", http.StatusSeeOther)
}

// An OAuth-only account must retain at least one usable sign-in method.
func unlinkAccountIdentity(writer http.ResponseWriter, httpRequest *http.Request) error {
	provider := routeParam(httpRequest, "provider")
	if provider != "github" && provider != "google" && provider != "oidc" {
		return newHTTPError(http.StatusNotFound)
	}
	account := currentAccount(writer, httpRequest)
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
			return newHTTPError(http.StatusConflict, "cannot remove the last sign-in method")
		}
		result := tx.Where("account_id = ? AND provider = ?", account.ID, provider).Delete(&model.ExternalIdentity{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return newHTTPError(http.StatusNotFound)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return redirectResponse(writer, httpRequest, "/account", http.StatusSeeOther)
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

func usersPage(writer http.ResponseWriter, httpRequest *http.Request) error {
	return adminUsersPage(writer, httpRequest)
}

func setUserRole(writer http.ResponseWriter, httpRequest *http.Request) error {
	id := routeParam(httpRequest, "id")
	role := model.Role(strings.TrimSpace(formValue(httpRequest, "role")))
	if role != model.RoleAdmin && role != model.RoleEditor && role != model.RoleUser {
		return newHTTPError(http.StatusBadRequest, "invalid role")
	}
	if id == currentAccount(writer, httpRequest).ID {
		return newHTTPError(http.StatusForbidden, "cannot change your own role")
	}
	err := solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		var account model.Account
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&account, "id = ?", id).Error; err != nil {
			return err
		}
		previous := account.Role
		// Re-saving an unchanged role is not a privilege change.
		if previous == role {
			return nil
		}
		if err := tx.Model(&account).Update("role", role).Error; err != nil {
			return err
		}
		return auditMutation(writer, httpRequest, tx, "user.role.update", account.ID, "", string(previous)+" -> "+string(role))
	})
	if err != nil {
		return err
	}
	requestValue(httpRequest, "audit_recorded", true)
	return redirectResponse(writer, httpRequest, "/admin/users", http.StatusSeeOther)
}
