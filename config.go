package solitudes

// Config 系统配置
type Config struct {
	Debug         bool
	FrontendTheme string `mapstructure:"frontend_theme"`
	BackendTheme  string `mapstructure:"backend_theme"`
	Database      string
}
