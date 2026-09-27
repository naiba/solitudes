package router

import (
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

func accountPage(c *fiber.Ctx) error {
	account := currentAccount(c)
	var keys []model.Passkey
	if err := solitudes.System.DB.Where("account_id = ?", account.ID).Order("created_at DESC").Find(&keys).Error; err != nil {
		return err
	}
	return c.Status(http.StatusOK).Render("admin/account", injectSiteData(c, fiber.Map{
		"title": "Account", "passkeys": keys,
	}))
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

func usersPage(c *fiber.Ctx) error {
	var accounts []model.Account
	if err := solitudes.System.DB.Order("created_at DESC").Find(&accounts).Error; err != nil {
		return err
	}
	return c.Status(http.StatusOK).Render("admin/users", injectSiteData(c, fiber.Map{
		"title": "Users", "users": accounts,
	}))
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
		return tx.Model(&account).Update("role", role).Error
	})
	if err != nil {
		return err
	}
	return c.Redirect("/admin/users", http.StatusSeeOther)
}
