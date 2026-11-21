package model

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/naiba/solitudes/internal/theme"
)

func TestConfigSaveRestrictsSecretFilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conf.yml")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	config := &Config{ConfigFilePath: path}
	if err := config.Save(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("config with login secrets is readable by other users: %v", info.Mode().Perm())
	}
}

// The file holds the database DSN and every secret. Truncating it and writing
// in place leaves it empty or half written if the process dies mid-write, and
// the site cannot start again. The new content has to be written beside it
// and swapped in.
func TestConfigSaveReplacesTheFileInOneStep(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "conf.yml")
	if err := os.WriteFile(path, []byte("database: old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	config := &Config{ConfigFilePath: path, Database: "postgres://new"}
	if err := config.Save(); err != nil {
		t.Fatal(err)
	}

	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Error("the existing file was overwritten in place instead of being replaced by a complete new file")
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "postgres://new") {
		t.Errorf("saved config = %q", saved)
	}
	if after.Mode().Perm() != 0600 {
		t.Errorf("saved config mode = %v, want 0600", after.Mode().Perm())
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("config directory holds %d entries after saving, want only conf.yml", len(entries))
	}
}

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
	availableAdmin := map[string]bool{"default": true}

	ApplyThemeFallback(cfg, "cactus", "default", availableSite, availableAdmin)

	if cfg.Site.Theme != "cactus" || cfg.Admin.Theme != "default" {
		t.Fatalf("fallback failed, got site=%q admin=%q", cfg.Site.Theme, cfg.Admin.Theme)
	}

	// ensure existing valid theme stays untouched
	availableAdmin["custom-admin"] = true
	cfg.Admin.Theme = "custom-admin"
	ApplyThemeFallback(cfg, "cactus", "default", availableSite, availableAdmin)

	if cfg.Admin.Theme != "custom-admin" {
		t.Fatalf("valid admin theme should not change, got %q", cfg.Admin.Theme)
	}
}

func TestRemovedAstroPaperFallsBackToCactus(t *testing.T) {
	cfg := &Config{}
	cfg.Site.Theme = "astro-paper"
	ApplyThemeFallback(cfg, "cactus", "default", map[string]bool{"cactus": true, "folio": true}, map[string]bool{"default": true})
	if cfg.Site.Theme != "cactus" {
		t.Fatalf("removed theme fallback = %q, want cactus", cfg.Site.Theme)
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

// When the new content cannot be written (full disk, I/O error) saving must
// fail and leave the existing file alone. Falling back to writing in place
// truncates the file first and then fails the same way, leaving it empty.
func TestConfigSaveKeepsTheExistingFileWhenTheNewContentCannotBeWritten(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "conf.yml")
	const existing = "database: old\n"
	if err := os.WriteFile(path, []byte(existing), 0600); err != nil {
		t.Fatal(err)
	}
	originalCreate := createConfigTemporaryFile
	t.Cleanup(func() { createConfigTemporaryFile = originalCreate })
	createConfigTemporaryFile = func(dir, pattern string) (*os.File, error) {
		created, err := os.CreateTemp(dir, pattern)
		if err != nil {
			return nil, err
		}
		name := created.Name()
		if err := created.Close(); err != nil {
			return nil, err
		}
		// A handle that cannot be written to stands in for a failing disk.
		return os.Open(name)
	}

	config := &Config{ConfigFilePath: path, Database: "postgres://new"}
	if err := config.Save(); err == nil {
		t.Fatal("Save reported success although the new content could not be written")
	}

	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(saved) != existing {
		t.Errorf("config after the failed save = %q, want the previous content %q", saved, existing)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("config directory holds %d entries after the failed save, want only conf.yml", len(entries))
	}
}
