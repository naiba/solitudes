package model

import (
	"errors"
	"fmt"
	"log"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/naiba/solitudes/internal/theme"
)

// 定义自定义错误
var (
	ErrInvalidSiteTheme  = errors.New("invalid site theme")
	ErrInvalidAdminTheme = errors.New("invalid admin theme")
)

// Config 系统配置
type Config struct {
	Debug bool

	EnableTrustedProxyCheck bool
	TrustedProxies          []string
	ProxyHeader             string

	TGBotToken string
	TGChatID   string

	Database string
	Akismet  string
	Email    struct {
		Host string
		Port int
		User string
		Pass string
		SSL  bool
	}
	Site struct {
		SpaceName     string
		SpaceDesc     string
		SpaceKeywords string
		Domain        string
		Theme         string
		ThemeConfig   map[string]interface{} `yaml:"theme_config"`
	}

	Admin struct {
		Theme string
	}

	User           User
	ConfigFilePath string
}

// Save ..
func (c *Config) Save() error {
	b, err := yaml.Marshal(&c)
	if err != nil {
		return err
	}
	return os.WriteFile(c.ConfigFilePath, b, os.FileMode(0655))
}

// ApplyThemeFallback ensures configured themes exist in the available list.
func ApplyThemeFallback(cfg *Config, defaultSiteTheme, defaultAdminTheme string, availableSite, availableAdmin map[string]bool) {
	if cfg == nil {
		return
	}
	if defaultSiteTheme != "" {
		if !availableSite[cfg.Site.Theme] {
			cfg.Site.Theme = defaultSiteTheme
		}
	}
	if defaultAdminTheme != "" {
		if !availableAdmin[cfg.Admin.Theme] {
			cfg.Admin.Theme = defaultAdminTheme
		}
	}
}

// ValidateThemeConfigStartup 启动时校验主题配置，只记录日志不返回错误
func ValidateThemeConfigStartup(cfg *Config, availableThemes *theme.ThemeList) {
	if err := validateThemeConfigInternal(cfg, availableThemes); err != nil {
		log.Printf("Theme config validation warning: %v", err)
	}
}

// ValidateThemeConfig 校验主题配置是否合法，遇到错误时返回错误
func ValidateThemeConfig(cfg *Config, availableThemes *theme.ThemeList) error {
	return validateThemeConfigInternal(cfg, availableThemes)
}

// validateThemeConfigInternal 内部验证逻辑
func validateThemeConfigInternal(cfg *Config, availableThemes *theme.ThemeList) error {
	// If Site.Theme is not empty, check if it exists in available site themes
	if cfg.Site.Theme != "" {
		found := false
		for _, t := range availableThemes.Site {
			if t.ID == cfg.Site.Theme {
				found = true
				break
			}
		}
		if !found {
			return ErrInvalidSiteTheme
		}
	}

	// If Admin.Theme is not empty, check if it exists in available admin themes
	if cfg.Admin.Theme != "" {
		found := false
		for _, t := range availableThemes.Admin {
			if t.ID == cfg.Admin.Theme {
				found = true
				break
			}
		}
		if !found {
			return ErrInvalidAdminTheme
		}
	}

	// 校验 ThemeConfig 中的键是否满足主题 metadata 中定义的配置
	if cfg.Site.Theme == "" || cfg.Site.ThemeConfig == nil {
		return nil
	}

	// 找到当前激活的主题
	var activeThemeMeta theme.ThemeMeta
	found := false
	for _, t := range availableThemes.Site {
		if t.ID == cfg.Site.Theme {
			activeThemeMeta = t
			found = true
			break
		}
	}
	if !found {
		return ErrInvalidSiteTheme
	}

	// 如果主题没有定义配置，允许任何配置
	if activeThemeMeta.Config == nil {
		return nil
	}

	// 检查 metadata 中定义的必需键是否都在 ThemeConfig 中存在
	for requiredKey := range activeThemeMeta.Config {
		if _, exists := cfg.Site.ThemeConfig[requiredKey]; !exists {
			return fmt.Errorf("missing required theme config key: %s (defined in theme metadata)", requiredKey)
		}
	}

	// ThemeConfig 可以包含额外的未知键，不需要检查

	return nil
}

// SyncThemeConfig 同步主题配置，将 metadata.config 中定义但 ThemeConfig 中缺失的键添加进去
func SyncThemeConfig(cfg *Config, availableThemes *theme.ThemeList) {
	if cfg.Site.Theme == "" {
		return
	}

	// 找到当前激活的主题
	var activeThemeMeta theme.ThemeMeta
	found := false
	for _, t := range availableThemes.Site {
		if t.ID == cfg.Site.Theme {
			activeThemeMeta = t
			found = true
			break
		}
	}
	if !found || activeThemeMeta.Config == nil {
		return
	}

	// 初始化 ThemeConfig（如果为 nil）
	if cfg.Site.ThemeConfig == nil {
		cfg.Site.ThemeConfig = make(map[string]interface{})
	}

	// 只添加缺失的键，不修改已存在的键
	for requiredKey, defaultValue := range activeThemeMeta.Config {
		if _, exists := cfg.Site.ThemeConfig[requiredKey]; !exists {
			cfg.Site.ThemeConfig[requiredKey] = defaultValue
		}
	}
}
