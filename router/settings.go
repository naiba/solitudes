package router

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/crypto/bcrypt"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/internal/theme" // Add this import
	"github.com/naiba/solitudes/pkg/notify"
	"github.com/naiba/solitudes/pkg/translator"
)

func settings(c *fiber.Ctx) error {
	themesRoot := "resource/themes"
	availableThemes, err := theme.LoadThemes(themesRoot)
	if err != nil {
		availableThemes = &theme.ThemeList{}
	}
	c.Status(http.StatusOK).Render("admin/settings", injectSiteData(c, fiber.Map{
		"title":          c.Locals(solitudes.CtxTranslator).(*translator.Translator).T("site_settings"),
		"themeListSite":  availableThemes.Site,
		"themeListAdmin": availableThemes.Admin,
	}))
	return nil
}

type settingsRequest struct {
	SiteTitle    string `json:"site_title,omitempty" form:"site_title"`
	SiteDesc     string `json:"site_desc,omitempty" form:"site_desc"`
	TgBotToken   string `json:"tg_bot_token,omitempty" form:"tg_bot_token"`
	TgChatId     string `json:"tg_chat_id,omitempty" form:"tg_chat_id"`
	MailServer   string `json:"mail_server,omitempty" form:"mail_server"`
	MailPort     int    `json:"mail_port,omitempty" form:"mail_port"`
	MailUser     string `json:"mail_user,omitempty" form:"mail_user"`
	MailPassword string `json:"mail_password,omitempty" form:"mail_password"`
	MailSSL      bool   `json:"mail_ssl,omitempty" form:"mail_ssl"`
	Akismet      string `json:"akismet,omitempty" form:"akismet"`
	SiteDomain   string `json:"site_domain,omitempty" form:"site_domain"`
	SiteKeywords string `json:"site_keywords,omitempty" form:"site_keywords"`
	SiteTheme    string `json:"site_theme,omitempty" form:"site_theme"`
	Email        string `json:"email,omitempty" form:"email" validate:"email"`
	Nickname     string `json:"nickname,omitempty" form:"nickname" validate:"trim"`
	OldPassword  string `json:"old_password,omitempty" form:"old_password" validate:"trim"`
	NewPassword  string `json:"new_password,omitempty" form:"new_password" validate:"trim"`
	AdminTheme   string `json:"admin_theme,omitempty" form:"admin_theme"`
	ThemeConfig  string `json:"theme_config,omitempty" form:"theme_config"`
}

func settingsHandler(c *fiber.Ctx) error {
	var err error
	var themeChanged bool
	var originalSiteTheme, originalAdminTheme string

	defer func() {
		if err == nil {
			// 同步主题配置
			themesRoot := "resource/themes"
			availableThemes, themeErr := theme.LoadThemes(themesRoot)
			if themeErr == nil {
				model.SyncThemeConfig(solitudes.System.Config, availableThemes)
			}

			// 如果主题发生了变化，重新加载模板
			if themeChanged {
				if reloadErr := ReloadTemplates(); reloadErr != nil {
					log.Printf("Failed to reload templates after theme change: %v", reloadErr)
				} else {
					// 更新已运行app的Views配置
					// 注意：这需要在应用启动后才能调用
					log.Printf("Templates reloaded after theme change")
				}
			}

			err = solitudes.System.Config.Save()
		}
	}()

	// Store current themes for change detection
	originalSiteTheme = solitudes.System.Config.Site.Theme
	originalAdminTheme = solitudes.System.Config.Admin.Theme

	var sr settingsRequest
	if err := c.BodyParser(&sr); err != nil {
		return err
	}

	// 检查主题是否发生变化
	if sr.SiteTheme != "" && sr.SiteTheme != originalSiteTheme {
		themeChanged = true
	}
	if sr.AdminTheme != "" && sr.AdminTheme != originalAdminTheme {
		themeChanged = true
	}

	// Handle Logo Upload
	if file, err := c.FormFile("logo"); err == nil {
		if err := c.SaveFile(file, "data/upload/logo.png"); err != nil {
			return err
		}
	}

	// Handle Favicon Upload
	if file, err := c.FormFile("favicon"); err == nil {
		if err := c.SaveFile(file, "data/upload/favicon.ico"); err != nil {
			return err
		}
	}

	// Load available themes
	themesRoot := "resource/themes"
	availableThemes, err := theme.LoadThemes(themesRoot)
	if err != nil {
		return fiber.NewError(http.StatusInternalServerError, "Failed to load themes: "+err.Error())
	}

	// Temporarily update themes for validation (defer will handle rollback on error)
	if sr.SiteTheme != "" {
		solitudes.System.Config.Site.Theme = sr.SiteTheme
	}
	if sr.AdminTheme != "" {
		solitudes.System.Config.Admin.Theme = sr.AdminTheme
	}

	// Find active theme meta (使用当前主题或新主题)
	var activeThemeMeta theme.ThemeMeta
	foundActiveTheme := false
	targetTheme := sr.SiteTheme
	if targetTheme == "" {
		targetTheme = solitudes.System.Config.Site.Theme
	}
	for _, t := range availableThemes.Site {
		if t.ID == targetTheme {
			activeThemeMeta = t
			foundActiveTheme = true
			break
		}
	}

	// 检查 Telegram 配置是否发生变化
	tgTokenChanged := solitudes.System.Config.TGBotToken != sr.TgBotToken && sr.TgBotToken != ""
	tgChatIDChanged := solitudes.System.Config.TGChatID != sr.TgChatId && sr.TgChatId != ""

	solitudes.System.Config.Site.SpaceName = sr.SiteTitle
	solitudes.System.Config.Site.SpaceDesc = sr.SiteDesc
	solitudes.System.Config.TGBotToken = sr.TgBotToken
	solitudes.System.Config.TGChatID = sr.TgChatId
	solitudes.System.Config.Email.Host = sr.MailServer
	solitudes.System.Config.Email.Port = sr.MailPort
	solitudes.System.Config.Email.User = sr.MailUser
	solitudes.System.Config.Email.Pass = sr.MailPassword
	solitudes.System.Config.Email.SSL = sr.MailSSL
	solitudes.System.Config.Akismet = sr.Akismet
	solitudes.System.Config.Site.Domain = sr.SiteDomain
	solitudes.System.Config.Site.SpaceKeywords = sr.SiteKeywords
	solitudes.System.Config.User.Nickname = sr.Nickname
	solitudes.System.Config.User.Email = sr.Email

	if solitudes.System.Config.Site.ThemeConfig == nil {
		solitudes.System.Config.Site.ThemeConfig = make(map[string]interface{})
	}

	var newConfig map[string]interface{}
	if sr.ThemeConfig != "" {
		if err := json.Unmarshal([]byte(sr.ThemeConfig), &newConfig); err != nil {
			return fiber.NewError(http.StatusBadRequest, "Invalid theme config JSON: "+err.Error())
		}

		// 补充 theme.config 中存在但用户未提交的 key
		if foundActiveTheme && activeThemeMeta.Config != nil {
			for k, defaultValue := range activeThemeMeta.Config {
				if _, exists := newConfig[k]; !exists {
					newConfig[k] = defaultValue
				}
			}
		}

		backupThemeConfig := make(map[string]interface{})
		for k, v := range solitudes.System.Config.Site.ThemeConfig {
			backupThemeConfig[k] = v
		}

		for k, v := range newConfig {
			solitudes.System.Config.Site.ThemeConfig[k] = v
		}

		if err := model.ValidateThemeConfig(solitudes.System.Config, availableThemes); err != nil {
			solitudes.System.Config.Site.ThemeConfig = backupThemeConfig
			return fiber.NewError(http.StatusBadRequest, "Theme config validation failed: "+err.Error())
		}
	} else {
		// 如果用户没有提交 theme_config，使用 theme.config 中的默认值补充
		if foundActiveTheme && activeThemeMeta.Config != nil {
			if solitudes.System.Config.Site.ThemeConfig == nil {
				solitudes.System.Config.Site.ThemeConfig = make(map[string]interface{})
			}
			for k, defaultValue := range activeThemeMeta.Config {
				if _, exists := solitudes.System.Config.Site.ThemeConfig[k]; !exists {
					solitudes.System.Config.Site.ThemeConfig[k] = defaultValue
				}
			}
		}

		backupThemeConfig := make(map[string]interface{})
		for k, v := range solitudes.System.Config.Site.ThemeConfig {
			backupThemeConfig[k] = v
		}

		if err := model.ValidateThemeConfig(solitudes.System.Config, availableThemes); err != nil {
			solitudes.System.Config.Site.ThemeConfig = backupThemeConfig
			return fiber.NewError(http.StatusBadRequest, "Theme config validation failed: "+err.Error())
		}
	}

	if len(sr.OldPassword) > 0 && len(sr.NewPassword) > 0 {
		if bcrypt.CompareHashAndPassword([]byte(solitudes.System.Config.User.Password), []byte(sr.OldPassword)) != nil {
			return errors.New("invalid email or password")
		}
		b, err := bcrypt.GenerateFromPassword([]byte(sr.NewPassword), 1)
		if err != nil {
			return err
		}
		solitudes.System.Config.User.Password = string(b)
	}

	if (tgTokenChanged || tgChatIDChanged) && solitudes.System.Config.TGBotToken != "" && solitudes.System.Config.TGChatID != "" {
		sendTelegramTestMessage()
	}

	return nil
}

func sendTelegramTestMessage() {
	testComment := &model.Comment{
		Nickname: "System",
		Email:    "system@test.com",
		Content:  "🎉 Telegram notification has been configured successfully! Your bot is now ready to send notifications.",
		IsAdmin:  false, // 设置为 false 确保消息会被发送
	}

	testArticle := &model.Article{
		Title: "Telegram Configuration Test",
	}

	go notify.TGNotify(testComment, testArticle, nil)
}
