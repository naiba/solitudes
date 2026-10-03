package router

import (
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

type loginForm struct {
	Email     string `form:"email" validate:"required,email"`
	Password  string `form:"password" validate:"required"`
	Remember  string `form:"remember"`
	CaptchaID string `form:"captchaId" validate:"required"`
	Captcha   string `form:"captcha" validate:"required"`
	ReturnTo  string `form:"return_to"`
}

func loginHandler(c *fiber.Ctx) error {
	var lf loginForm
	if err := c.BodyParser(&lf); err != nil {
		return err
	}
	if err := validator.StructCtx(c.Context(), &lf); err != nil {
		return err
	}
	// Verify captcha first
	if !verifyCaptcha(lf.CaptchaID, lf.Captcha) {
		c.Locals("audit_reason", "invalid_captcha")
		return fiber.NewError(http.StatusBadRequest, "invalid captcha")
	}
	var account model.Account
	if err := solitudes.System.DB.Where("email = ? AND disabled_at IS NULL", strings.ToLower(strings.TrimSpace(lf.Email))).Take(&account).Error; err != nil ||
		bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(lf.Password)) != nil {
		c.Locals("audit_reason", "invalid_credentials")
		return fiber.NewError(http.StatusUnauthorized, "invalid email or password")
	}
	if account.EmailVerifiedAt == nil {
		c.Locals("audit_reason", "email_unverified")
		return fiber.NewError(http.StatusForbidden, "verify your email before signing in")
	}
	if err := issueLoginSession(c, &account, lf.Remember == "on"); err != nil {
		return err
	}
	if ret := safeReturnPath(lf.ReturnTo); ret != "" {
		return c.Redirect(ret, http.StatusFound)
	}
	return c.Redirect("/account", http.StatusFound)
}

func login(c *fiber.Ctx) error {
	c.Set("Cache-Control", "no-store")
	returnTo := safeReturnPath(c.Query("return_to"))
	client, err := loginOIDCApplication(c, returnTo)
	if err != nil {
		return err
	}
	if client != nil {
		c.Set("Cache-Control", "private, no-store")
	}
	return c.Status(http.StatusOK).Render("site/login", injectSiteData(c, fiber.Map{
		"title":   c.Locals(solitudes.CtxTranslator).(*translator.Translator).T("sign_in"),
		"noindex": true, "return_to": returnTo, "oidc_client": client,
		"verified": c.Query("verified") == "1",
	}))
}

func logoutHandler(c *fiber.Ctx) error {
	if token := c.Cookies(solitudes.AuthCookie); token != "" {
		if err := solitudes.System.DB.Where("token_hash = ?", secretHash(token)).Delete(&model.LoginSession{}).Error; err != nil {
			return err
		}
	}
	c.Cookie(&fiber.Cookie{
		Name:     solitudes.AuthCookie,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HTTPOnly: true,
		SameSite: fiber.CookieSameSiteLaxMode,
		Secure:   c.Protocol() == "https",
	})
	c.Redirect("/", http.StatusFound)
	return nil
}

func index(c *fiber.Ctx) error {
	return c.Render("site/index", injectSiteData(c, fiber.Map{}))
}

func count(c *fiber.Ctx) error {
	if c.Query("slug") == "" {
		return nil
	}
	// FIXME 允许刷新增加计数
	// key := c.IP() + c.Query("slug")
	// if _, ok := solitudes.System.Cache.Get(key); ok {
	// 	return nil
	// }
	// solitudes.System.Cache.Set(key, nil, time.Hour*20)
	var latestArticle model.Article
	if err := readableArticles(solitudes.System.DB, currentAccount(c)).Select("id").
		Order("created_at DESC").
		Take(&latestArticle, "slug = ?", c.Query("slug")).Error; err != nil {
		return nil
	}

	solitudes.System.DB.Model(&model.Article{}).
		Where("id = ?", latestArticle.ID).
		UpdateColumn("read_num", gorm.Expr("read_num + ?", 1))
	return nil
}
