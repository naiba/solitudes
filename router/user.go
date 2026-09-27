package router

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/naiba/solitudes/pkg/pagination"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
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
		return errors.New("invalid captcha")
	}
	var account model.Account
	if err := solitudes.System.DB.Where("email = ? AND disabled_at IS NULL", strings.ToLower(strings.TrimSpace(lf.Email))).Take(&account).Error; err != nil ||
		bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(lf.Password)) != nil {
		return errors.New("invalid email or password")
	}
	if account.EmailVerifiedAt == nil {
		return fiber.NewError(http.StatusForbidden, "verify your email before signing in")
	}
	if err := issueLoginSession(c, &account, lf.Remember == "on"); err != nil {
		return err
	}
	if ret := safeReturnPath(lf.ReturnTo); ret != "" {
		return c.Redirect(ret, http.StatusFound)
	}
	if account.Role.CanPublish() {
		if account.Role == model.RoleEditor {
			return c.Redirect("/admin/articles", http.StatusFound)
		}
		return c.Redirect("/admin", http.StatusFound)
	}
	return c.Redirect("/account", http.StatusFound)
}

func login(c *fiber.Ctx) error {

	return c.Status(http.StatusOK).Render("admin/login", injectSiteData(c, fiber.Map{
		"return_to": safeReturnPath(c.Query("return_to")),
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
	var articles []model.Article
	var topics []model.Article
	var mostRead []model.Article
	db := readableArticles(solitudes.System.DB, currentAccount(c)).Preload("Author")

	db.Where("tags @> ARRAY[?]::varchar[]", "Topic").Order("created_at DESC").Limit(5).Find(&topics)
	for i := range topics {
		pagination.Paging(&pagination.Param{
			DB:      visibleComments(solitudes.System.DB).Where("reply_to is null and article_id = ?", topics[i].ID),
			Limit:   5,
			OrderBy: []string{"created_at DESC"},
		}, &topics[i].Comments)
	}

	// Fetch top 3 most read articles and books
	db.Where("template_id = ? AND (array_length(tags, 1) is null OR NOT tags @> ARRAY[?]::varchar[])", solitudes.ArticleTemplateID, "Topic").Order("read_num DESC").Limit(3).Find(&mostRead)
	for i := range mostRead {
		mostRead[i].RelatedCount(solitudes.System.DB)
	}

	articleCount := 16 - len(topics)*2
	db.Where("(array_length(tags, 1) is null OR NOT tags @> ARRAY[?]::varchar[])", "Topic").Order("created_at DESC").Limit(articleCount).Find(&articles)
	for i := range articles {
		articles[i].RelatedCount(solitudes.System.DB)
	}
	// Only show "Most Read" section if we have at least 3 items
	var mostReadData interface{}
	if len(mostRead) >= 3 {
		mostReadData = mostRead
	}

	c.Status(http.StatusOK).Render("site/index", injectSiteData(c, fiber.Map{
		"articles": articles,
		"topics":   topics,
		"mostRead": mostReadData,
	}))
	return nil
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
	if err := solitudes.System.DB.Select("id").
		Order("created_at DESC").
		Take(&latestArticle, "slug = ?", c.Query("slug")).Error; err != nil {
		return nil
	}

	solitudes.System.DB.Model(&model.Article{}).
		Where("id = ?", latestArticle.ID).
		UpdateColumn("read_num", gorm.Expr("read_num + ?", 1))
	return nil
}
