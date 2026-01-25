package theme

// ThemeMeta represents the metadata for a single theme.
type ThemeMeta struct {
	ID          string `json:"id" yaml:"id"`
	Name        string `json:"name" yaml:"name"`
	Author      string `json:"author" yaml:"author"`
	Version     string `json:"version" yaml:"version"`
	Description string `json:"description" yaml:"description"`
	Link        string `json:"link" yaml:"link"`
	Config      map[string]interface{} `json:"config" yaml:"config"`
}

// ThemeList holds separate lists for site and admin themes.
type ThemeList struct {
	Site  []ThemeMeta
	Admin []ThemeMeta
}
