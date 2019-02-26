package wengine

import (
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"
	"github.com/naiba/solitudes/x/soligin"
)

var shits = []shitGin{
	shitGin{
		Match: regexp.MustCompile(`^\/$`),
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet: index,
		},
	},
	shitGin{
		Match: regexp.MustCompile(`^\/archives/(\d*)/?$`),
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet: archive,
		},
	},
	shitGin{
		Match: regexp.MustCompile(`^\/search/$`),
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet: search,
		},
	},
	shitGin{
		Match: regexp.MustCompile(`^\/tags/([^\/]*)/(\d*)/?$`),
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet: tags,
		},
	},
	shitGin{
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
	shitGin{
		Match: regexp.MustCompile(`^\/logout$`),
		Pre: []gin.HandlerFunc{
			soligin.Authorize,
			soligin.Limit(soligin.LimitOption{NeedLogin: true}),
		},
		Routes: map[string]gin.HandlerFunc{
			http.MethodPost: logoutHandler,
		},
	},
	shitGin{
		Match: regexp.MustCompile(`^\/count$`),
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet: count,
		},
	},
	shitGin{
		Match: regexp.MustCompile(`^\/comment$`),
		Pre: []gin.HandlerFunc{
			soligin.Authorize,
		},
		Routes: map[string]gin.HandlerFunc{
			http.MethodPost: commentHandler,
		},
	},
	shitGin{
		Match: regexp.MustCompile(`^\/admin\/$`),
		Pre: []gin.HandlerFunc{
			soligin.Authorize,
			soligin.Limit(soligin.LimitOption{NeedLogin: true}),
		},
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet: manager,
		},
	},
	shitGin{
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
	shitGin{
		Match: regexp.MustCompile(`^\/admin\/upload`),
		Pre: []gin.HandlerFunc{
			soligin.Authorize,
			soligin.Limit(soligin.LimitOption{NeedLogin: true}),
		},
		Routes: map[string]gin.HandlerFunc{
			http.MethodPost: upload,
		},
	},
	shitGin{
		Match: regexp.MustCompile(`^\/static\/`),
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet: static("resource/static"),
		},
	},
	shitGin{
		Match: regexp.MustCompile(`^\/upload\/`),
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet: static("data/upload"),
		},
	},
	shitGin{
		Match: regexp.MustCompile(`^\/(.*)$`),
		Pre: []gin.HandlerFunc{
			soligin.Authorize,
		},
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet: article,
		},
	},
}
