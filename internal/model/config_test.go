package model

import (
	"errors"
	"testing"

	"github.com/naiba/solitudes/internal/theme"
)

func TestThemeConfig(t *testing.T) {
	cfg := &Config{
		Site: struct {
			SpaceName     string
			SpaceDesc     string
			SpaceKeywords string
			Domain        string
			Theme         string
			ThemeConfig   map[string]interface{} `yaml:"theme_config"`
		}{
			ThemeConfig: map[string]interface{}{
				"cactus.key": "value",
			},
		},
	}

	if cfg.Site.ThemeConfig == nil {
		t.Error("ThemeConfig should not be nil")
	}

	if _, ok := cfg.Site.ThemeConfig["cactus.key"]; !ok {
		t.Error("ThemeConfig should contain cactus.key")
	}
}

func TestThemeFallback(t *testing.T) {
	cfg := &Config{}
	cfg.Site.Theme = "nonexistent"
	cfg.Admin.Theme = "nonexistent"

	availableSite := map[string]bool{"cactus": true}
	availableAdmin := map[string]bool{"cactus": true}

	ApplyThemeFallback(cfg, "cactus", "default", availableSite, availableAdmin)

	if cfg.Site.Theme != "cactus" || cfg.Admin.Theme != "default" {
		t.Fatalf("fallback failed, got site=%q admin=%q", cfg.Site.Theme, cfg.Admin.Theme)
	}

	// ensure existing valid theme stays untouched
	cfg.Site.Theme = "cactus"
	cfg.Admin.Theme = "cactus"
	ApplyThemeFallback(cfg, "cactus", "default", availableSite, availableAdmin)

	if cfg.Site.Theme != "cactus" || cfg.Admin.Theme != "default" {
		t.Fatalf("valid theme should not change, got site=%q admin=%q", cfg.Site.Theme, cfg.Admin.Theme)
	}
}

func TestValidateThemeConfig(t *testing.T) {
	// Simulate available themes
	availableThemes := &theme.ThemeList{
		Site: []theme.ThemeMeta{
			{ID: "cactus"},
		},
		Admin: []theme.ThemeMeta{
			{ID: "default"},
		},
	}

	// Test case 1: Valid themes
	cfg1 := &Config{
		Site: struct {
			SpaceName     string
			SpaceDesc     string
			SpaceKeywords string
			Domain        string
			Theme         string
			ThemeConfig   map[string]interface{} `yaml:"theme_config"`
		}{
			Theme: "cactus",
		},
		Admin: struct {
			Theme string
		}{
			Theme: "default",
		},
	}
	if err := ValidateThemeConfig(cfg1, availableThemes); err != nil {
		t.Errorf("ValidateThemeConfig failed for valid themes: %v", err)
	}

	// Test case 2: Invalid site theme
	cfg2 := &Config{
		Site: struct {
			SpaceName     string
			SpaceDesc     string
			SpaceKeywords string
			Domain        string
			Theme         string
			ThemeConfig   map[string]interface{} `yaml:"theme_config"`
		}{
			Theme: "nonexistent-site-theme",
		},
		Admin: struct {
			Theme string
		}{
			Theme: "default",
		},
	}
	err := ValidateThemeConfig(cfg2, availableThemes)
	if err == nil {
		t.Error("ValidateThemeConfig should have failed for invalid site theme, but got nil")
	} else if !errors.Is(err, ErrInvalidSiteTheme) {
		t.Errorf("ValidateThemeConfig failed with wrong error for invalid site theme: got %v, want %v", err, ErrInvalidSiteTheme)
	}

	// Test case 3: Invalid admin theme
	cfg3 := &Config{
		Site: struct {
			SpaceName     string
			SpaceDesc     string
			SpaceKeywords string
			Domain        string
			Theme         string
			ThemeConfig   map[string]interface{} `yaml:"theme_config"`
		}{
			Theme: "cactus",
		},
		Admin: struct {
			Theme string
		}{
			Theme: "nonexistent-admin-theme",
		},
	}
	err = ValidateThemeConfig(cfg3, availableThemes)
	if err == nil {
		t.Error("ValidateThemeConfig should have failed for invalid admin theme, but got nil")
	} else if !errors.Is(err, ErrInvalidAdminTheme) {
		t.Errorf("ValidateThemeConfig failed with wrong error for invalid admin theme: got %v, want %v", err, ErrInvalidAdminTheme)
	}

	// Test case 4: Empty themes (should be valid if default fallback is handled elsewhere)
	cfg4 := &Config{
		Site: struct {
			SpaceName     string
			SpaceDesc     string
			SpaceKeywords string
			Domain        string
			Theme         string
			ThemeConfig   map[string]interface{} `yaml:"theme_config"`
		}{
			Theme: "",
		},
		Admin: struct {
			Theme string
		}{
			Theme: "",
		},
	}
	if err := ValidateThemeConfig(cfg4, availableThemes); err != nil {
		t.Errorf("ValidateThemeConfig failed for empty themes: %v", err)
	}

	// Test case 5: Valid theme config keys
	availableThemesWithConfig := &theme.ThemeList{
		Site: []theme.ThemeMeta{
			{
				ID: "cactus",
				Config: map[string]interface{}{
					"color":    "string",
					"fontSize": "number",
				},
			},
		},
		Admin: []theme.ThemeMeta{
			{ID: "default"},
		},
	}
	cfg5 := &Config{
		Site: struct {
			SpaceName     string
			SpaceDesc     string
			SpaceKeywords string
			Domain        string
			Theme         string
			ThemeConfig   map[string]interface{} `yaml:"theme_config"`
		}{
			Theme: "cactus",
			ThemeConfig: map[string]interface{}{
				"color":    "blue",
				"fontSize": 16,
			},
		},
		Admin: struct {
			Theme string
		}{
			Theme: "default",
		},
	}
	if err := ValidateThemeConfig(cfg5, availableThemesWithConfig); err != nil {
		t.Errorf("ValidateThemeConfig failed for valid theme config: %v", err)
	}

	// Test case 6: Missing required theme config key
	cfg6 := &Config{
		Site: struct {
			SpaceName     string
			SpaceDesc     string
			SpaceKeywords string
			Domain        string
			Theme         string
			ThemeConfig   map[string]interface{} `yaml:"theme_config"`
		}{
			Theme: "cactus",
			ThemeConfig: map[string]interface{}{
				"color": "blue", // missing fontSize
			},
		},
		Admin: struct {
			Theme string
		}{
			Theme: "default",
		},
	}
	err = ValidateThemeConfig(cfg6, availableThemesWithConfig)
	if err == nil {
		t.Error("ValidateThemeConfig should have failed for missing required config key")
	}
	if err != nil && err.Error() != "missing required theme config key: fontSize (defined in theme metadata)" {
		t.Errorf("ValidateThemeConfig failed with wrong error message: got %v", err)
	}

	// Test case 7: Unknown theme config keys (should be allowed)
	cfg7 := &Config{
		Site: struct {
			SpaceName     string
			SpaceDesc     string
			SpaceKeywords string
			Domain        string
			Theme         string
			ThemeConfig   map[string]interface{} `yaml:"theme_config"`
		}{
			Theme: "cactus",
			ThemeConfig: map[string]interface{}{
				"color":      "blue",
				"fontSize":   16,
				"unknownKey": "extra value", // unknown key should be allowed
			},
		},
		Admin: struct {
			Theme string
		}{
			Theme: "default",
		},
	}
	err = ValidateThemeConfig(cfg7, availableThemesWithConfig)
	if err != nil {
		t.Errorf("ValidateThemeConfig should allow unknown config keys, but got error: %v", err)
	}

	// Test case 8: Theme config when theme doesn't define any config (should allow any)
	availableThemesNoConfig := &theme.ThemeList{
		Site: []theme.ThemeMeta{
			{ID: "minimal"},
		},
		Admin: []theme.ThemeMeta{
			{ID: "default"},
		},
	}
	cfg8 := &Config{
		Site: struct {
			SpaceName     string
			SpaceDesc     string
			SpaceKeywords string
			Domain        string
			Theme         string
			ThemeConfig   map[string]interface{} `yaml:"theme_config"`
		}{
			Theme: "minimal",
			ThemeConfig: map[string]interface{}{
				"someKey": "value",
			},
		},
		Admin: struct {
			Theme string
		}{
			Theme: "default",
		},
	}
	err = ValidateThemeConfig(cfg8, availableThemesNoConfig)
	if err != nil {
		t.Errorf("ValidateThemeConfig should allow any config when theme doesn't define config, but got error: %v", err)
	}
}

func TestValidateThemeConfigStartup(t *testing.T) {
	// Test startup validation - should not return errors, just log
	availableThemesNoConfig := &theme.ThemeList{
		Site: []theme.ThemeMeta{
			{ID: "minimal"},
		},
		Admin: []theme.ThemeMeta{
			{ID: "default"},
		},
	}
	cfg := &Config{
		Site: struct {
			SpaceName     string
			SpaceDesc     string
			SpaceKeywords string
			Domain        string
			Theme         string
			ThemeConfig   map[string]interface{} `yaml:"theme_config"`
		}{
			Theme: "minimal",
			ThemeConfig: map[string]interface{}{
				"someKey": "value",
			},
		},
		Admin: struct {
			Theme string
		}{
			Theme: "default",
		},
	}

	// This should not panic or return error
	ValidateThemeConfigStartup(cfg, availableThemesNoConfig)

	// Also test with missing required key (should log but not return error)
	cfgMissingKey := &Config{
		Site: struct {
			SpaceName     string
			SpaceDesc     string
			SpaceKeywords string
			Domain        string
			Theme         string
			ThemeConfig   map[string]interface{} `yaml:"theme_config"`
		}{
			Theme:       "minimal",
			ThemeConfig: map[string]interface{}{
				// missing someKey if it were required
			},
		},
		Admin: struct {
			Theme string
		}{
			Theme: "default",
		},
	}

	// This should also not panic or return error
	ValidateThemeConfigStartup(cfgMissingKey, availableThemesNoConfig)
}
