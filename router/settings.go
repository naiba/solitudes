package router

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/internal/theme"
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
	var sr settingsRequest
	if err := c.BodyParser(&sr); err != nil {
		return err
	}

	// Load available themes
	themesRoot := "resource/themes"
	availableThemes, err := theme.LoadThemes(themesRoot)
	if err != nil {
		return fiber.NewError(http.StatusInternalServerError, "Failed to load themes: "+err.Error())
	}

	// Validate on an isolated copy: invalid input must never change the running config.
	previous := *solitudes.System.Config
	candidate := previous
	candidate.Site.ThemeConfig = make(map[string]interface{}, len(previous.Site.ThemeConfig))
	for key, value := range previous.Site.ThemeConfig {
		candidate.Site.ThemeConfig[key] = value
	}

	if sr.SiteTheme != "" {
		candidate.Site.Theme = sr.SiteTheme
	}
	if sr.AdminTheme != "" {
		candidate.Admin.Theme = sr.AdminTheme
	}

	// Find active theme meta (使用当前主题或新主题)
	var activeThemeMeta theme.ThemeMeta
	foundActiveTheme := false
	targetTheme := sr.SiteTheme
	if targetTheme == "" {
		targetTheme = candidate.Site.Theme
	}
	for _, t := range availableThemes.Site {
		if t.ID == targetTheme {
			activeThemeMeta = t
			foundActiveTheme = true
			break
		}
	}

	// 检查 Telegram 配置是否发生变化
	tgTokenChanged := candidate.TGBotToken != sr.TgBotToken && sr.TgBotToken != ""
	tgChatIDChanged := candidate.TGChatID != sr.TgChatId && sr.TgChatId != ""

	candidate.Site.SpaceName = sr.SiteTitle
	candidate.Site.SpaceDesc = sr.SiteDesc
	candidate.TGBotToken = sr.TgBotToken
	candidate.TGChatID = sr.TgChatId
	candidate.Email.Host = sr.MailServer
	candidate.Email.Port = sr.MailPort
	candidate.Email.User = sr.MailUser
	candidate.Email.Pass = sr.MailPassword
	candidate.Email.SSL = sr.MailSSL
	candidate.Akismet = sr.Akismet
	candidate.Site.Domain = sr.SiteDomain
	candidate.Site.SpaceKeywords = sr.SiteKeywords
	candidate.User.Nickname = sr.Nickname
	candidate.User.Email = sr.Email

	var newConfig map[string]interface{}
	if sr.ThemeConfig != "" {
		if err := yaml.Unmarshal([]byte(sr.ThemeConfig), &newConfig); err != nil {
			return fiber.NewError(http.StatusBadRequest, "Invalid theme config YAML: "+err.Error())
		}
		if newConfig == nil {
			newConfig = make(map[string]interface{})
		}

		// 补充 theme.config 中存在但用户未提交的 key
		if foundActiveTheme && activeThemeMeta.Config != nil {
			for k, defaultValue := range activeThemeMeta.Config {
				if _, exists := newConfig[k]; !exists {
					newConfig[k] = defaultValue
				}
			}
		}

		for k, v := range newConfig {
			candidate.Site.ThemeConfig[k] = v
		}
	} else {
		// 如果用户没有提交 theme_config，使用 theme.config 中的默认值补充
		if foundActiveTheme && activeThemeMeta.Config != nil {
			for k, defaultValue := range activeThemeMeta.Config {
				if _, exists := candidate.Site.ThemeConfig[k]; !exists {
					candidate.Site.ThemeConfig[k] = defaultValue
				}
			}
		}
	}

	if err := model.ValidateThemeConfig(&candidate, availableThemes); err != nil {
		return fiber.NewError(http.StatusBadRequest, "Theme config validation failed: "+err.Error())
	}

	if sr.NewPassword != "" && sr.OldPassword == "" {
		return fiber.NewError(http.StatusBadRequest, "old password required")
	}
	var changedPassword string
	if len(sr.OldPassword) > 0 && len(sr.NewPassword) > 0 {
		passwordHash := previous.User.Password
		if account := currentAccount(c); account != nil {
			passwordHash = account.PasswordHash
		}
		if bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(sr.OldPassword)) != nil {
			return errors.New("invalid email or password")
		}
		b, err := bcrypt.GenerateFromPassword([]byte(sr.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		candidate.User.Password = string(b)
		changedPassword = string(b)
	}

	for _, fileInput := range []struct {
		field, destination string
		limit              int64
	}{
		{"logo", "data/upload/logo.png", 5 * 1024 * 1024},
		{"favicon", "data/upload/favicon.ico", 1 * 1024 * 1024},
	} {
		file, err := c.FormFile(fileInput.field)
		if err != nil {
			continue
		}
		contentType := file.Header.Get("Content-Type")
		if file.Size > fileInput.limit || (!strings.HasPrefix(contentType, "image/") && contentType != "application/octet-stream") {
			return fiber.NewError(fiber.StatusBadRequest, "invalid "+fileInput.field+" file")
		}
		if err := c.SaveFile(file, fileInput.destination); err != nil {
			return err
		}
	}

	model.SyncThemeConfig(&candidate, availableThemes)
	*solitudes.System.Config = candidate
	if err := candidate.Save(); err != nil {
		*solitudes.System.Config = previous
		return fmt.Errorf("save settings: %w", err)
	}
	if changedPassword != "" {
		if account := currentAccount(c); account != nil {
			if err := solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
				if err := tx.Model(&model.Account{}).Where("id = ?", account.ID).Update("password_hash", changedPassword).Error; err != nil {
					return err
				}
				for _, entry := range []interface{}{&model.LoginSession{}, &model.OIDCRefreshToken{}, &model.OIDCAccessToken{}} {
					if err := tx.Where("account_id = ?", account.ID).Delete(entry).Error; err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				return fmt.Errorf("update administrator password and revoke sessions: %w", err)
			}
		}
	}
	if candidate.Site.Theme != previous.Site.Theme || candidate.Admin.Theme != previous.Admin.Theme {
		if err := ReloadTemplates(); err != nil {
			log.Printf("Failed to reload templates after theme change: %v", err)
		}
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
