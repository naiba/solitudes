package router

import (
	"context"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"reflect"
	"regexp"
	"sync"
	"syscall"
	"time"

	"github.com/88250/lute"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/microcosm-cc/bluemonday"

	"github.com/naiba/com"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/pkg/soligin"
)

var ugcPolict = bluemonday.UGCPolicy()
var luteEngine = lute.New()

func init() {
	if solitudes.System.Config.Debug {
		pprofPrefix := `^\/debug/pprof`
		pprofRouters := []shitGin{
			{
				Match: regexp.MustCompile(pprofPrefix + `/$`),
				Routes: map[string]gin.HandlerFunc{
					http.MethodGet: pprofHandler(pprof.Index),
				},
			},
			{
				Match: regexp.MustCompile(pprofPrefix + `/cmdline$`),
				Routes: map[string]gin.HandlerFunc{
					http.MethodGet: pprofHandler(pprof.Cmdline),
				},
			},
			{
				Match: regexp.MustCompile(pprofPrefix + `/symbol$`),
				Routes: map[string]gin.HandlerFunc{
					http.MethodGet:  pprofHandler(pprof.Symbol),
					http.MethodPost: pprofHandler(pprof.Symbol),
				},
			},
			{
				Match: regexp.MustCompile(pprofPrefix + `/trace$`),
				Routes: map[string]gin.HandlerFunc{
					http.MethodGet: pprofHandler(pprof.Trace),
				},
			},
			{
				Match: regexp.MustCompile(pprofPrefix + `/block$`),
				Routes: map[string]gin.HandlerFunc{
					http.MethodGet: pprofHandler(pprof.Handler("block").ServeHTTP),
				},
			},
			{
				Match: regexp.MustCompile(pprofPrefix + `/goroutine$`),
				Routes: map[string]gin.HandlerFunc{
					http.MethodGet: pprofHandler(pprof.Handler("goroutine").ServeHTTP),
				},
			},
			{
				Match: regexp.MustCompile(pprofPrefix + `/heap$`),
				Routes: map[string]gin.HandlerFunc{
					http.MethodGet: pprofHandler(pprof.Handler("heap").ServeHTTP),
				},
			},
			{
				Match: regexp.MustCompile(pprofPrefix + `/mutex$`),
				Routes: map[string]gin.HandlerFunc{
					http.MethodGet: pprofHandler(pprof.Handler("mutex").ServeHTTP),
				},
			},
			{
				Match: regexp.MustCompile(pprofPrefix + `/threadcreate$`),
				Routes: map[string]gin.HandlerFunc{
					http.MethodGet: pprofHandler(pprof.Handler("threadcreate").ServeHTTP),
				},
			},
		}
		shits = append(pprofRouters, shits...)
	}
}

var shits = []shitGin{
	{
		Match: regexp.MustCompile(`^\/$`),
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet: index,
		},
	},
	{
		Match: regexp.MustCompile(`^\/feed/([^\/]{1,})$`),
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet: feedHandler,
		},
	},
	{
		Match: regexp.MustCompile(`^\/archives/(\d*)/?$`),
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet: archive,
		},
	},
	{
		Match: regexp.MustCompile(`^\/search/$`),
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet: search,
		},
	},
	{
		Match: regexp.MustCompile(`^\/tags/([^\/]*)/(\d*)/?$`),
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet: tags,
		},
	},
	{
		Match: regexp.MustCompile(`^\/login$`),
		Pre: []gin.HandlerFunc{
			soligin.Authorize,
			soligin.Limit(soligin.LimitOption{NeedGuest: true}),
		},
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet:  login,
			http.MethodPost: loginHandler,
		},
	},
	{
		Match: regexp.MustCompile(`^\/logout$`),
		Pre: []gin.HandlerFunc{
			soligin.Authorize,
			soligin.Limit(soligin.LimitOption{NeedLogin: true}),
		},
		Routes: map[string]gin.HandlerFunc{
			http.MethodPost: logoutHandler,
		},
	},
	{
		Match: regexp.MustCompile(`^\/count$`),
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet: count,
		},
	},
	{
		Match: regexp.MustCompile(`^\/comment$`),
		Pre: []gin.HandlerFunc{
			soligin.Authorize,
		},
		Routes: map[string]gin.HandlerFunc{
			http.MethodPost: commentHandler,
		},
	},
	{
		Match: regexp.MustCompile(`^\/admin\/$`),
		Pre: []gin.HandlerFunc{
			soligin.Authorize,
			soligin.Limit(soligin.LimitOption{NeedLogin: true}),
		},
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet: manager,
		},
	},
	{
		Match: regexp.MustCompile(`^\/admin\/publish$`),
		Pre: []gin.HandlerFunc{
			soligin.Authorize,
			soligin.Limit(soligin.LimitOption{NeedLogin: true}),
		},
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet:  publish,
			http.MethodPost: publishHandler,
		},
	},
	{
		Match: regexp.MustCompile(`^\/admin\/rebuild-riot$`),
		Pre: []gin.HandlerFunc{
			soligin.Authorize,
			soligin.Limit(soligin.LimitOption{NeedLogin: true}),
		},
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet: rebuildRiotData,
		},
	},
	{
		Match: regexp.MustCompile(`^\/admin\/upload$`),
		Pre: []gin.HandlerFunc{
			soligin.Authorize,
			soligin.Limit(soligin.LimitOption{NeedLogin: true}),
		},
		Routes: map[string]gin.HandlerFunc{
			http.MethodPost: upload,
		},
	},
	{
		Match: regexp.MustCompile(`^\/admin\/comments$`),
		Pre: []gin.HandlerFunc{
			soligin.Authorize,
			soligin.Limit(soligin.LimitOption{NeedLogin: true}),
		},
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet:    comments,
			http.MethodDelete: deleteComment,
		},
	},
	{
		Match: regexp.MustCompile(`^\/admin\/articles$`),
		Pre: []gin.HandlerFunc{
			soligin.Authorize,
			soligin.Limit(soligin.LimitOption{NeedLogin: true}),
		},
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet:    manageArticle,
			http.MethodDelete: deleteArticle,
		},
	},
	{
		Match: regexp.MustCompile(`^\/admin\/media$`),
		Pre: []gin.HandlerFunc{
			soligin.Authorize,
			soligin.Limit(soligin.LimitOption{NeedLogin: true}),
		},
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet:    media,
			http.MethodDelete: mediaHandler,
		},
	},
	{
		Match: regexp.MustCompile(`^\/static\/`),
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet: static("resource/static"),
		},
	},
	{
		Match: regexp.MustCompile(`^\/upload\/`),
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet: static("data/upload"),
		},
	},
	{
		Match: regexp.MustCompile(`^\/([^\/]*)\/v(\d*)$`),
		Pre: []gin.HandlerFunc{
			soligin.Authorize,
		},
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet: article,
		},
	},
	{
		Match: regexp.MustCompile(`^\/([^\/]*)$`),
		Pre: []gin.HandlerFunc{
			soligin.Authorize,
		},
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet: article,
		},
	},
}

func pprofHandler(h http.HandlerFunc) gin.HandlerFunc {
	handler := http.HandlerFunc(h)
	return func(c *gin.Context) {
		handler.ServeHTTP(c.Writer, c.Request)
	}
}

// Serve web service
func Serve() {
	if !solitudes.System.Config.Debug {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.SetFuncMap(template.FuncMap{
		"md5": func(origin string) string {
			return com.MD5(origin)
		},
		"add": func(a, b int) int {
			return a + b
		},
		"uint2str": func(i uint) string {
			return fmt.Sprintf("%d", i)
		},
		"int2str": func(i int) string {
			return fmt.Sprintf("%d", i)
		},
		"unsafe": func(raw string) template.HTML {
			return template.HTML(raw)
		},
		"tf": func(t time.Time, f string) string {
			return t.Format(f)
		},
		"ugcPolicy": func(raw string) string {
			return ugcPolict.Sanitize(raw)
		},
		"md": func(id string, raw string) string {
			return luteEngine.MarkdownStr(id, raw)
		},
		"last": func(x int, a interface{}) bool {
			return x == reflect.ValueOf(a).Len()-1
		},
	})
	r.LoadHTMLGlob("resource/theme/**/*")
	store := cookie.NewStore([]byte("secret"))
	r.Use(sessions.Sessions("solisession", store))
	r.Use(soligin.Translator)

	r.Any("/*shit", routerSwitch)

	srv := &http.Server{
		Addr:    ":8080",
		Handler: r,
	}

	go func() {
		// service connections
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutdown Server ...")
	solitudes.System.Search.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server Shutdown: ", err)
	}

	log.Println("Server exiting")
}

type shitGin struct {
	Match  *regexp.Regexp
	Pre    []gin.HandlerFunc
	Routes map[string]gin.HandlerFunc
}

func routerSwitch(c *gin.Context) {
	var params []string
	for j := 0; j < len(shits); j++ {
		params = shits[j].Match.FindStringSubmatch(c.Request.URL.Path)
		if len(params) == 0 {
			continue
		}
		if f, ok := shits[j].Routes[c.Request.Method]; ok {
			c.Set(solitudes.CtxRequestParams, params)
			for i := 0; i < len(shits[j].Pre); i++ {
				shits[j].Pre[i](c)
			}
			if len(shits[j].Pre) > 0 && !c.MustGet(solitudes.CtxPassPreHandler).(bool) {
				// 如果没有通过 pre handler
				return
			}
			f(c)
			return
		}
		c.HTML(http.StatusMethodNotAllowed, "default/error", soligin.Soli(c, gin.H{
			"title": c.MustGet(solitudes.CtxTranslator).(*solitudes.Translator).T("method_not_allowed"),
			"msg":   c.MustGet(solitudes.CtxTranslator).(*solitudes.Translator).T("are_you_lost"),
		}))
		return
	}
	tr := c.MustGet(solitudes.CtxTranslator).(*solitudes.Translator)
	c.HTML(http.StatusNotFound, "default/error", soligin.Soli(c, gin.H{
		"title": tr.T("404_title"),
		"msg":   tr.T("404_msg"),
	}))
}

func checkPoolSubmit(wg *sync.WaitGroup, err error) {
	if err != nil {
		log.Println(err)
		if wg != nil {
			wg.Done()
		}
	}
}
