package theme

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const metadataFileName = "metadata.json"

// LoadThemes scans the provided root directory for site and admin themes.
func LoadThemes(root string) (*ThemeList, error) {
	siteThemes, err := loadThemeList(root, "site")
	if err != nil {
		return nil, err
	}
	adminThemes, err := loadThemeList(root, "admin")
	if err != nil {
		return nil, err
	}
	return &ThemeList{Site: siteThemes, Admin: adminThemes}, nil
}

func loadThemeList(root, kind string) ([]ThemeMeta, error) {
	themeRoot := filepath.Join(root, kind)
	log("loading kind", kind, "from", themeRoot)
	entries, err := os.ReadDir(themeRoot)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var metas []ThemeMeta
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		metaPath := filepath.Join(themeRoot, entry.Name(), metadataFileName)
		data, err := os.ReadFile(metaPath)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("read metadata %s: %w", metaPath, err)
		}
		var meta ThemeMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			return nil, fmt.Errorf("parse metadata %s: %w", metaPath, err)
		}
		if meta.ID == "" {
			meta.ID = entry.Name()
		}
		metas = append(metas, meta)
	}
	return metas, nil
}

func log(args ...interface{}) {}
