package router

import (
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/hashicorp/go-uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/notify"
	"github.com/naiba/solitudes/pkg/translator"
)

type registrationForm struct {
	Email     string `form:"email"`
	Nickname  string `form:"nickname"`
	Password  string `form:"password"`
	Captcha   string `form:"captcha"`
	CaptchaID string `form:"captchaId"`
}

func registerPage(c *fiber.Ctx) error {
	c.Set("Cache-Control", "no-store")
	return c.Status(http.StatusOK).Render("site/register", injectSiteData(c, fiber.Map{
		"title": c.Locals(solitudes.CtxTranslator).(*translator.Translator).T("create_account"), "noindex": true,
		"registration_available": validSiteDomain() && solitudes.System.Config.Email.Host != "" && solitudes.System.Config.Email.User != "",
		"return_to":              safeReturnPath(c.Query("return_to")),
	}))
}

func normalizeRegistration(form *registrationForm) error {
	form.Email = strings.ToLower(strings.TrimSpace(form.Email))
	form.Nickname = strings.TrimSpace(form.Nickname)
	address, err := mail.ParseAddress(form.Email)
	if err != nil || address.Address != form.Email || len(form.Email) > 254 || len([]rune(form.Nickname)) > 80 || form.Nickname == "" || len(form.Password) < 12 || len(form.Password) > 72 {
		return fiber.NewError(http.StatusBadRequest, "invalid email, nickname or password (12–72 characters)")
	}
	return nil
}

func registerHandler(c *fiber.Ctx) error {
	if !validSiteDomain() {
		return fiber.NewError(http.StatusServiceUnavailable, "registration requires a public site domain")
	}
	if solitudes.System.Config.Email.Host == "" || solitudes.System.Config.Email.User == "" {
		return fiber.NewError(http.StatusServiceUnavailable, "registration requires SMTP configuration")
	}
	var form registrationForm
	if err := c.BodyParser(&form); err != nil {
		return err
	}
	if !verifyCaptcha(form.CaptchaID, form.Captcha) {
		return fiber.NewError(http.StatusBadRequest, "invalid captcha")
	}
	if err := normalizeRegistration(&form); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(form.Password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	id, err := uuid.GenerateUUID()
	if err != nil {
		return err
	}
	account := model.Account{ID: id, Email: form.Email, Nickname: form.Nickname, PasswordHash: string(hash), Role: model.RoleUser}
	var already int64
	if err := solitudes.System.DB.Model(&account).Where("email = ?", form.Email).Count(&already).Error; err != nil {
		return err
	}
	if already != 0 {
		return fiber.NewError(http.StatusConflict, "email already registered")
	}
	if err := solitudes.System.DB.Create(&account).Error; err != nil {
		return fmt.Errorf("create account: %w", err)
	}
	if err := sendVerification(&account, safeReturnPath(c.FormValue("return_to"))); err != nil {
		return fiber.NewError(http.StatusServiceUnavailable, "account created but verification email could not be sent; try resending")
	}
	return c.Status(http.StatusAccepted).SendString("Check your email to verify your account before signing in.")
}

func sendVerification(account *model.Account, returnTo string) error {
	token, err := newSecret()
	if err != nil {
		return err
	}
	id, err := uuid.GenerateUUID()
	if err != nil {
		return err
	}
	if err := solitudes.System.DB.Create(&model.EmailAction{
		ID: id, AccountID: account.ID, TokenHash: secretHash(token), Purpose: "verify_email",
		ExpiresAt: time.Now().Add(time.Hour),
	}).Error; err != nil {
		return err
	}
	link := publicBaseURL() + "/verify-email?token=" + token
	if returnTo != "" {
		link += "&return_to=" + url.QueryEscape(returnTo)
	}
	return notify.SendVerificationEmail(account.Email, link)
}

func resendVerification(c *fiber.Ctx) error {
	if !validSiteDomain() || solitudes.System.Config.Email.Host == "" || solitudes.System.Config.Email.User == "" {
		return fiber.NewError(http.StatusServiceUnavailable, "email verification is not configured")
	}
	var form registrationForm
	if err := c.BodyParser(&form); err != nil {
		return err
	}
	if !verifyCaptcha(form.CaptchaID, form.Captcha) {
		return fiber.NewError(http.StatusBadRequest, "invalid captcha")
	}
	var account model.Account
	err := solitudes.System.DB.Where("email = ? AND email_verified_at IS NULL AND disabled_at IS NULL", strings.ToLower(strings.TrimSpace(form.Email))).Take(&account).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err == nil {
		var recent int64
		if err := solitudes.System.DB.Model(&model.EmailAction{}).
			Where("account_id = ? AND created_at > ?", account.ID, time.Now().Add(-time.Hour)).Count(&recent).Error; err != nil {
			return err
		}
		if recent < 3 {
			if err := sendVerification(&account, safeReturnPath(c.FormValue("return_to"))); err != nil {
				return fiber.NewError(http.StatusServiceUnavailable, "verification email unavailable")
			}
		}
	}
	return c.SendString("If an unverified account exists, a confirmation link has been sent.")
}

func verifyEmailHandler(c *fiber.Ctx) error {
	c.Set("Referrer-Policy", "no-referrer")
	c.Set("Cache-Control", "no-store")
	if err := verifyAccountEmail(c.Query("token")); err != nil {
		return fiber.NewError(http.StatusBadRequest, "invalid or expired verification link")
	}
	redirect := "/login?verified=1"
	if target := safeReturnPath(c.Query("return_to")); target != "" {
		redirect += "&return_to=" + url.QueryEscape(target)
	}
	return c.Redirect(redirect, http.StatusFound)
}
