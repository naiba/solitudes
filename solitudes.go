package solitudes

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/patrickmn/go-cache"
	"github.com/robfig/cron/v3"
	"github.com/yanyiwu/gojieba"
	"go.uber.org/dig"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
	"gopkg.in/yaml.v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/internal/theme"
	_ "github.com/naiba/solitudes/pkg/blevejieba"
)

// Constants
const (
	// CtxAuthorized 用户已认证
	CtxAuthorized = "cazed"
	// CtxAccount is the authenticated database account, if one exists.
	CtxAccount = "account"
	// CtxTranslator 翻译
	CtxTranslator = "ct"
	// AuthCookie 用户认证使用的Cookie名
	AuthCookie = "i_like_solitude"
	// CacheKeyPrefixRelatedChapters 缓存键前缀：章节
	CacheKeyPrefixRelatedChapters = "ckprc"
	// CacheKeyPrefixRelatedArticle 缓存键前缀：文章
	CacheKeyPrefixRelatedArticle = "ckpra"
	// CacheKeyPrefixRelatedSiblingArticle 缓存键前缀：相邻文章
	CacheKeyPrefixRelatedSiblingArticle = "ckprsa"
)

// SysVariable 全局变量
type SysVariable struct {
	Config    *model.Config
	DB        *gorm.DB
	Cache     *cache.Cache
	Search    bleve.Index
	SafeCache *singleflight.Group
}

const fullTextSearchIndexPath = "data/bleve"

// Injector 运行时依赖注入
var Injector *dig.Container

// System 全局变量
var System *SysVariable

// BuildVersion 构建版本
var BuildVersion = "_BuildVersion_"

const (
	// ArticleTemplateID represents the article template ID
	ArticleTemplateID byte = 1
	// PageTemplateID represents the page template ID
	PageTemplateID byte = 2
)

// Templates 文章模板
var Templates = map[byte]string{
	ArticleTemplateID: "Article template",
	PageTemplateID:    "Page template",
}

// TemplateIndex 模板索引
var TemplateIndex = map[byte]string{
	ArticleTemplateID: "article",
	PageTemplateID:    "page",
}

type articleSearchDocument struct {
	Slug      string
	Version   uint
	Title     string
	Content   string
	IsPrivate bool
}

func newArticleSearchDocument(article *model.Article, version uint, content string) articleSearchDocument {
	if version != article.Version {
		content = ""
	}
	view := *article
	view.Content = content
	content = view.ContentFor(nil, nil)
	if !article.Public() {
		content = ""
	}
	return articleSearchDocument{
		Slug:      article.Slug,
		Version:   version,
		Title:     article.Title,
		Content:   content,
		IsPrivate: !article.Public(),
	}
}

func indexArticleVersion(index bleve.Index, article *model.Article, version uint, content string) error {
	document := newArticleSearchDocument(article, version, content)
	indexID := fmt.Sprintf("%s.%d", article.ID, version)
	if err := index.Index(indexID, document); err != nil {
		return fmt.Errorf("failed to index article %s: %w", indexID, err)
	}
	return nil
}

func indexArticle(index bleve.Index, article *model.Article) error {
	for version := uint(1); version < article.Version; version++ {
		if err := indexArticleVersion(index, article, version, ""); err != nil {
			return err
		}
	}
	return indexArticleVersion(index, article, article.Version, article.Content)
}

// Rewrite persisted pre-access-control indexes once. A crash retries the scrub
// on the next start; the marker is written only after every batch succeeds.
func upgradeArticleAccessIndex() error {
	const key = "article-access-schema-v1"
	marker, err := System.Search.GetInternal([]byte(key))
	if err != nil || len(marker) > 0 {
		return err
	}
	var articles []model.Article
	if err := System.DB.FindInBatches(&articles, 100, func(tx *gorm.DB, batch int) error {
		for i := range articles {
			if err := indexArticle(System.Search, &articles[i]); err != nil {
				return err
			}
		}
		return nil
	}).Error; err != nil {
		return err
	}
	return System.Search.SetInternal([]byte(key), []byte("1"))
}

// IndexArticle updates the full-text search document for an article. Private
// articles remain discoverable by title, but their body is never persisted in
// the search index.
func IndexArticle(article *model.Article) error {
	if System == nil || System.Search == nil {
		return fmt.Errorf("search index is not initialized")
	}
	return indexArticle(System.Search, article)
}

func newBleveSearch() (bleve.Index, error) {
	_, err := os.Stat(fullTextSearchIndexPath)
	var index bleve.Index
	if err != nil {
		mapping := bleve.NewIndexMapping()
		mapping.DefaultAnalyzer = "jieba"
		if err := mapping.AddCustomTokenizer("jieba", map[string]interface{}{
			"type":         "jieba",
			"useHmm":       true,
			"tokenizeMode": float64(gojieba.SearchMode),
		}); err != nil {
			return nil, fmt.Errorf("failed to add custom tokenizer: %w", err)
		}
		if err := mapping.AddCustomAnalyzer("jieba", map[string]interface{}{
			"type":      "jieba",
			"tokenizer": "jieba",
		}); err != nil {
			return nil, fmt.Errorf("failed to add custom analyzer: %w", err)
		}
		index, err = bleve.New(fullTextSearchIndexPath, mapping)
		if err != nil {
			return nil, fmt.Errorf("failed to create new bleve index: %w", err)
		}
	} else {
		index, err = bleve.Open(fullTextSearchIndexPath)
		if err != nil {
			return nil, fmt.Errorf("failed to open bleve index: %w", err)
		}
	}
	count, err := index.DocCount()
	log.Println("Bleve: DocCount", count, err)
	return index, nil
}

func newCache() *cache.Cache {
	return cache.New(5*time.Minute, 10*time.Minute)
}

func newDatabase(conf *model.Config) (*gorm.DB, error) {
	if err := conf.ValidateDatabasePolicy(); err != nil {
		return nil, err
	}
	db, err := gorm.Open(postgres.Open(conf.Database), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	if conf.Debug {
		db = db.Debug()
	}
	pool, _ := conf.DatabasePool.WithDefaults() // Validated before connecting.
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("configure database pool: %w", err)
	}
	sqlDB.SetMaxOpenConns(pool.MaxOpen)
	sqlDB.SetMaxIdleConns(pool.MaxIdle)
	sqlDB.SetConnMaxLifetime(time.Duration(pool.LifetimeSeconds) * time.Second)
	sqlDB.SetConnMaxIdleTime(time.Duration(pool.IdleSeconds) * time.Second)
	return db, nil
}

func newConfig() (*model.Config, error) {
	configFile := "data/conf.yml"
	content, err := os.ReadFile(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	var c model.Config
	err = yaml.Unmarshal(content, &c)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	c.ConfigFilePath = configFile

	themesRoot := "resource/themes"
	themes, err := theme.LoadThemes(themesRoot)
	if err != nil {
		log.Printf("load themes failed: %v", err)
		themes = &theme.ThemeList{}
	}

	availableSite := make(map[string]bool)
	for _, meta := range themes.Site {
		if meta.ID == "" {
			continue
		}
		availableSite[meta.ID] = true
	}
	availableAdmin := make(map[string]bool)
	for _, meta := range themes.Admin {
		if meta.ID == "" {
			continue
		}
		availableAdmin[meta.ID] = true
	}

	model.ApplyThemeFallback(&c, "cactus", "default", availableSite, availableAdmin)

	// 校验主题配置
	model.ValidateThemeConfigStartup(&c, themes)

	// 同步主题配置
	model.SyncThemeConfig(&c, themes)

	log.Printf("Config loaded successfully")
	return &c, nil
}

func newSystem(c *model.Config, d *gorm.DB, h *cache.Cache,
	s bleve.Index) *SysVariable {
	return &SysVariable{
		Config:    c,
		DB:        d,
		Cache:     h,
		Search:    s,
		SafeCache: new(singleflight.Group),
	}
}

func migrate() error {
	if err := System.DB.AutoMigrate(&model.AuditEvent{}, &model.LoginSummary{}); err != nil {
		return fmt.Errorf("migrate security audit: %w", err)
	}
	if err := System.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.AuditEvent{
		ID: "audit-initialized", Action: "audit.enabled", Outcome: "success", CreatedAt: time.Now().UTC(),
	}).Error; err != nil {
		return fmt.Errorf("initialize security audit: %w", err)
	}
	if err := System.DB.Exec("CREATE EXTENSION IF NOT EXISTS \"uuid-ossp\";").Error; err != nil {
		return fmt.Errorf("failed to create uuid-ossp extension: %w", err)
	}
	if err := System.DB.AutoMigrate(&model.Account{}, &model.LoginSession{}, &model.EmailAction{}, &model.ExternalIdentity{}, &model.Passkey{}, &model.PasskeyCeremony{}, &model.OAuthAttempt{}, &model.OIDCClient{}, &model.OIDCAuthRequest{}, &model.OIDCAccessToken{}, &model.OIDCRefreshToken{}, &model.OIDCSigningKey{}, &model.OIDCCryptoKey{}, &model.Article{}, &model.ArticleHistory{}, &model.Comment{}, &model.FeedVisit{}); err != nil {
		return fmt.Errorf("failed to auto migrate models: %w", err)
	}
	if err := model.MigrateArticleVisibility(System.DB); err != nil {
		return fmt.Errorf("migrate article visibility: %w", err)
	}
	// A pre-existing installation owns its administrator identity in conf.yml.
	// Seed exactly that account, never promote the first public registrant.
	adminEmail := strings.ToLower(strings.TrimSpace(System.Config.User.Email))
	var admin model.Account
	err := System.DB.Where("role = ?", model.RoleAdmin).Order("created_at ASC").Take(&admin).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("load administrator account: %w", err)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		var accountCount int64
		if err := System.DB.Model(&model.Account{}).Count(&accountCount).Error; err != nil {
			return err
		}
		if accountCount == 0 && adminEmail != "" && System.Config.User.Password != "" {
			now := time.Now()
			nickname := strings.TrimSpace(System.Config.User.Nickname)
			if nickname == "" {
				nickname = "Administrator"
			}
			admin = model.Account{
				Email: adminEmail, Nickname: nickname,
				PasswordHash: System.Config.User.Password, Role: model.RoleAdmin,
				EmailVerifiedAt: &now,
			}
			if err := System.DB.Create(&admin).Error; err != nil {
				return fmt.Errorf("seed administrator: %w", err)
			}
		} else {
			return errors.New("no administrator account; configure the legacy user to bootstrap an empty database")
		}
	}
	// Existing articles keep their original owner through a repeatable backfill.
	if err := System.DB.Model(&model.Article{}).Where("author_id IS NULL").Update("author_id", admin.ID).Error; err != nil {
		return fmt.Errorf("backfill article authors: %w", err)
	}
	return model.MigrateDatabasePolicy(System.DB)
}

func maintainDatabase() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	count, err := model.MaintainDatabase(ctx, System.DB, time.Now().UTC(), System.Config.AuditRetentionDays)
	if err != nil {
		log.Printf("Database maintenance failed after removing %d expired records: %v", count, err)
		return
	}
	log.Printf("Database maintenance removed %d expired records", count)
}

func provide() error {
	var providers = []interface{}{
		newCache,
		newConfig,
		newDatabase,
		newSystem,
		newBleveSearch,
	}
	for _, provider := range providers {
		if err := Injector.Provide(provider); err != nil {
			return fmt.Errorf("failed to provide %T: %w", provider, err)
		}
	}
	if err := Injector.Invoke(func(s *SysVariable) {
		System = s
	}); err != nil {
		return fmt.Errorf("failed to invoke system initialization: %w", err)
	}
	return nil
}

// BuildArticleIndex 重建索引
func BuildArticleIndex() {
	System.Search.Close()
	if err := os.RemoveAll(fullTextSearchIndexPath); err != nil {
		panic(err)
	}
	var err error
	System.Search, err = newBleveSearch()
	if err != nil {
		panic(err)
	}
	var as []model.Article
	var hs []model.ArticleHistory
	var g errgroup.Group
	g.Go(func() error {
		return System.DB.Find(&as).Error
	})
	g.Go(func() error {
		return System.DB.Preload("Article").Find(&hs).Error
	})
	if err := g.Wait(); err != nil {
		log.Printf("Failed to fetch data for indexing: %v\n", err)
		return
	}
	for i := range as {
		if err := indexArticleVersion(System.Search, &as[i], as[i].Version, as[i].Content); err != nil {
			log.Printf("Failed to index article %s: %v\n", as[i].ID, err)
		}
	}
	for i := range hs {
		if hs[i].Article.ID == "" {
			log.Printf("Failed to index article history %s.%d: article not found\n", hs[i].ArticleID, hs[i].Version)
			continue
		}
		if err := indexArticleVersion(System.Search, &hs[i].Article, hs[i].Version, hs[i].Content); err != nil {
			log.Printf("Failed to index article history %s.%d: %v\n", hs[i].ArticleID, hs[i].Version, err)
		}
	}
	num, err := System.Search.DocCount()
	log.Printf("Doc indexed %d %+v\n", num, err)
}

func Init() {
	BuildVersion = BuildVersion[:8]
	Injector = dig.New()
	if err := provide(); err != nil {
		log.Fatalf("Initialization failed (DI): %v", err)
	}
	if System.DB != nil {
		if err := migrate(); err != nil {
			log.Fatalf("Database migration failed: %v", err)
		}
		if err := upgradeArticleAccessIndex(); err != nil {
			log.Fatalf("Article index access upgrade failed: %v", err)
		}
	}

	c := cron.New()
	_, err := c.AddFunc("17 * * * *", maintainDatabase)
	if err != nil {
		log.Printf("Failed to start cron job: %v", err)
	} else {
		c.Start()
		log.Println("Cron job started: bounded database maintenance hourly")
	}
}
