package wengine

import (
	"net/http"
	"net/http/pprof"
	"regexp"

	"github.com/gin-gonic/gin"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/x/soligin"
)

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
