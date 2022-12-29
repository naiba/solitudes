package model

import (
	"io/ioutil"
	"os"

	"gopkg.in/yaml.v3"
)

// Menu 自定义菜单
type Menu struct {
	Name  string
	Link  string
	Icon  string
	Black bool
}

// Config 系统配置
type Config struct {
	Debug bool

	EnableTrustedProxyCheck bool
	TrustedProxies          []string
	ProxyHeader             string

	WxpusherAppToken string
	WxpusherUID      string

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
		SpaceName         string
		SpaceDesc         string
		SpaceKeywords     string
		HomeTopContent    string
		HomeBottomContent string
		Domain            string
		Theme             string
		HeaderMenus       []Menu
		FooterMenus       []Menu
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
	return ioutil.WriteFile(c.ConfigFilePath, b, os.FileMode(0655))
}
