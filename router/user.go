package router

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/naiba/solitudes/pkg/pagination"
	"github.com/naiba/solitudes/pkg/translator"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

type loginForm struct {
	Email    string `form:"email" validate:"required,email"`
	Password string `form:"password" validate:"required"`
	Remember string `form:"remember"`
}

func loginHandler(c *fiber.Ctx) error {
	var lf loginForm
	if err := c.BodyParser(&lf); err != nil {
		return err
	}
	if err := validator.StructCtx(c.Context(), &lf); err != nil {
		return err
	}
	if lf.Email != solitudes.System.Config.User.Email ||
		bcrypt.CompareHashAndPassword([]byte(solitudes.System.Config.User.Password),
			[]byte(lf.Password)) != nil {
		return errors.New("invalid email or password")
	}
	token, err := bcrypt.GenerateFromPassword([]byte(fmt.Sprintf("%s%d", lf.Password, time.Now().UnixMicro())), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	solitudes.System.Config.User.Token = string(token)
	var expires time.Time
	if lf.Remember == "on" {
		expires = time.Now().AddDate(0, 3, 0)
	} else {
		expires = time.Now().Add(time.Hour * 4)
	}
	solitudes.System.Config.User.TokenExpires = expires.Unix()
	c.Cookie(&fiber.Cookie{
		Name:    solitudes.AuthCookie,
		Value:   string(token),
		Expires: expires,
	})
	solitudes.System.Config.Save()
	c.Redirect("/admin/", http.StatusFound)
	return nil
}

func login(c *fiber.Ctx) error {
	c.Status(http.StatusOK).Render("admin/login", injectSiteData(c, fiber.Map{}))
	return nil
}

func logoutHandler(c *fiber.Ctx) error {
	solitudes.System.Config.User.TokenExpires = time.Now().Unix()
	solitudes.System.Config.User.Token = ""
	solitudes.System.Config.Save()
	c.Redirect("/", http.StatusFound)
	return nil
}

func index(c *fiber.Ctx) error {
	var articles []model.Article
	var topics []model.Article
	var mostRead []model.Article

	solitudes.System.DB.Where("tags @> ARRAY[?]::varchar[]", "Topic").Order("created_at DESC").Limit(3).Find(&topics)
	for i := range topics {
		pagination.Paging(&pagination.Param{
			DB:      solitudes.System.DB.Where("reply_to is null and article_id = ?", topics[i].ID),
			Limit:   5,
			OrderBy: []string{"created_at DESC"},
		}, &topics[i].Comments)
	}

	// Fetch top 3 most read articles and books
	solitudes.System.DB.Where("template_id = ?", 1).Where("array_length(tags, 1) is null OR NOT tags @> ARRAY[?]::varchar[]", "Topic").Order("read_num DESC").Limit(3).Find(&mostRead)
	for i := range mostRead {
		mostRead[i].RelatedCount(solitudes.System.DB)
	}

	articleCount := 16 - len(topics)*2
	solitudes.System.DB.Where("array_length(tags, 1) is null").Or("NOT tags @> ARRAY[?]::varchar[]", "Topic").Order("created_at DESC").Limit(articleCount).Find(&articles)
	for i := range articles {
		articles[i].RelatedCount(solitudes.System.DB)
	}
	tr := c.Locals(solitudes.CtxTranslator).(*translator.Translator)

	// Only show "Most Read" section if we have at least 3 items
	var mostReadData interface{}
	if len(mostRead) >= 3 {
		mostReadData = mostRead
	}

	c.Status(http.StatusOK).Render("default/index", injectSiteData(c, fiber.Map{
		"title":    tr.T("home"),
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
	solitudes.System.DB.Model(model.Article{}).
		Where("slug = ?", c.Query("slug")).
		UpdateColumn("read_num", gorm.Expr("read_num + ?", 1))
	return nil
}
