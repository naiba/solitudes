package wengine

import (
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"
	"github.com/naiba/solitudes/x/soligin"
)

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
		Match: regexp.MustCompile(`^\/admin\/rebuild-bleve$`),
		Pre: []gin.HandlerFunc{
			soligin.Authorize,
			soligin.Limit(soligin.LimitOption{NeedLogin: true}),
		},
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet: rebuildBleveData,
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
		Match: regexp.MustCompile(`^\/([^\/]*)\/version/(\d*)$`),
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
