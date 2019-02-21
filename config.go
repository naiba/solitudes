package solitudes

// Menu 自定义菜单
type Menu struct {
	Name  string
	Link  string
	Icon  string
	Black bool
}

// Config 系统配置
type Config struct {
	Debug     bool
	SpaceName string `mapstructure:"space_name"`
	SpaceDesc string `mapstructure:"space_desc"`
	Web       struct {
		Bio           string
		Database      string
		User          User
		Theme         string `mapstructure:"theme"`
		HeaderMenus   []Menu `mapstructure:"header_menus"`
		FooterMenus   []Menu `mapstructure:"footer_menus"`
		SpaceKeywords string `mapstructure:"space_keywords"`
	}
}
