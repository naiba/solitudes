package router

import (
	"encoding/json"
	"net/http"

	"github.com/gofiber/fiber"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/pkg/translator"
	"golang.org/x/crypto/bcrypt"
)

func settings(c *fiber.Ctx) {
	c.Status(http.StatusOK).Render("admin/settings", injectSiteData(c, fiber.Map{
		"title": c.Locals(solitudes.CtxTranslator).(*translator.Translator).T("site_settings"),
	}))
}

type settingsRequest struct {
	SiteTitle             string `json:"site_title,omitempty"`
	SiteDesc              string `json:"site_desc,omitempty"`
	ServerChan            string `json:"server_chan,omitempty"`
	MailServer            string `json:"mail_server,omitempty"`
	MailPort              int    `json:"mail_port,omitempty"`
	MailUser              string `json:"mail_user,omitempty"`
	MailPassword          string `json:"mail_password,omitempty"`
	MailSSL               bool   `json:"mail_ssl,omitempty"`
	Akismet               string `json:"akismet,omitempty"`
	SiteDomain            string `json:"site_domain,omitempty"`
	SiteKeywords          string `json:"site_keywords,omitempty"`
	SiteHeaderMenus       string `json:"site_header_menus,omitempty"`
	SiteFooterMenus       string `json:"site_footer_menus,omitempty"`
	SiteTheme             string `json:"site_theme,omitempty"`
	SiteHomeTopContent    string `json:"site_home_top_content,omitempty"`
	SiteHomeBottomContent string `json:"site_home_bottom_content,omitempty"`
	OldPassword           string `json:"old_password,omitempty" validate:"trim"`
	NewPassword           string `json:"new_password,omitempty" validate:"trim"`
}

func settingsHandler(c *fiber.Ctx) {
	var flag bool
	defer func() {
		err := solitudes.System.Config.Save()
		if err != nil && flag {
			c.Status(http.StatusBadRequest).Write(err.Error())
			return
		}
	}()
	var sr settingsRequest
	if err := c.BodyParser(&sr); err != nil {
		c.Status(http.StatusBadRequest).Write(err.Error())
		return
	}

	solitudes.System.Config.Site.SpaceName = sr.SiteTitle
	solitudes.System.Config.Site.SpaceDesc = sr.SiteDesc
	solitudes.System.Config.ServerChan = sr.ServerChan
	solitudes.System.Config.Email.Host = sr.MailServer
	solitudes.System.Config.Email.Port = sr.MailPort
	solitudes.System.Config.Email.User = sr.MailUser
	solitudes.System.Config.Email.Pass = sr.MailPassword
	solitudes.System.Config.Email.SSL = sr.MailSSL
	solitudes.System.Config.Akismet = sr.Akismet
	solitudes.System.Config.Site.Domain = sr.SiteDomain
	solitudes.System.Config.Site.SpaceKeywords = sr.SiteKeywords
	err := json.Unmarshal([]byte(sr.SiteHeaderMenus), &solitudes.System.Config.Site.HeaderMenus)
	if err != nil {
		c.Status(http.StatusBadRequest).Write(err.Error())
		return
	}
	err = json.Unmarshal([]byte(sr.SiteFooterMenus), &solitudes.System.Config.Site.FooterMenus)
	if err != nil {
		c.Status(http.StatusBadRequest).Write(err.Error())
		return
	}
	solitudes.System.Config.Site.Theme = sr.SiteTheme
	solitudes.System.Config.Site.HomeTopContent = sr.SiteHomeTopContent
	solitudes.System.Config.Site.HomeBottomContent = sr.SiteHomeBottomContent

	if len(sr.OldPassword) > 0 && len(sr.NewPassword) > 0 {
		if bcrypt.CompareHashAndPassword([]byte(solitudes.System.Config.User.Password), []byte(sr.OldPassword)) != nil {
			c.Status(http.StatusInternalServerError).Write("Invalid email or password")
			return
		}
		b, err := bcrypt.GenerateFromPassword([]byte(sr.NewPassword), 1)
		if err != nil {
			c.Status(http.StatusInternalServerError).Write(err.Error())
			return
		}
		solitudes.System.Config.User.Password = string(b)
	}

	flag = true
}
