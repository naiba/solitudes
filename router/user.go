package router

import (
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

type loginForm struct {
	Email     string `form:"email" validate:"required,email"`
	Password  string `form:"password" validate:"required"`
	Remember  string `form:"remember"`
	CaptchaID string `form:"captchaId" validate:"required"`
	Captcha   string `form:"captcha" validate:"required"`
	ReturnTo  string `form:"return_to"`
}

func loginHandler(writer http.ResponseWriter, httpRequest *http.Request) error {
	var lf loginForm
	if err := decodeRequestBody(httpRequest, &lf); err != nil {
		return err
	}
	if err := validator.StructCtx(httpRequest.Context(), &lf); err != nil {
		return err
	}
	// Verify captcha first
	if !verifyCaptcha(lf.CaptchaID, lf.Captcha) {
		requestValue(httpRequest, "audit_reason", "invalid_captcha")
		return newHTTPError(http.StatusBadRequest, "invalid captcha")
	}
	var account model.Account
	if err := solitudes.System.DB.Where("email = ? AND disabled_at IS NULL", strings.ToLower(strings.TrimSpace(lf.Email))).Take(&account).Error; err != nil ||
		bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(lf.Password)) != nil {
		requestValue(httpRequest, "audit_reason", "invalid_credentials")
		return newHTTPError(http.StatusUnauthorized, "invalid email or password")
	}
	if account.EmailVerifiedAt == nil {
		requestValue(httpRequest, "audit_reason", "email_unverified")
		return newHTTPError(http.StatusForbidden, "verify your email before signing in")
	}
	if err := issueLoginSession(writer, httpRequest, &account, lf.Remember == "on"); err != nil {
		return err
	}
	if ret := safeReturnPath(lf.ReturnTo); ret != "" {
		return redirectResponse(writer, httpRequest, ret, http.StatusFound)
	}
	return redirectResponse(writer, httpRequest, "/account", http.StatusFound)
}

func login(writer http.ResponseWriter, httpRequest *http.Request) error {
	writer.Header().Set("Cache-Control", "no-store")
	returnTo := safeReturnPath(queryValue(httpRequest, "return_to"))
	client, err := loginOIDCApplication(writer, httpRequest, returnTo)
	if err != nil {
		return err
	}
	if client != nil {
		writer.Header().Set("Cache-Control", "private, no-store")
	}
	return renderPage(writer, httpRequest, http.StatusOK, "site/login", injectSiteData(writer, httpRequest, viewData{
		"title":   requestValue(httpRequest, solitudes.CtxTranslator).(*translator.Translator).T("sign_in"),
		"noindex": true, "return_to": returnTo, "oidc_client": client,
		"verified": queryValue(httpRequest, "verified") == "1",
	}))
}

func logoutHandler(writer http.ResponseWriter, httpRequest *http.Request) error {
	if token := requestCookie(httpRequest, solitudes.AuthCookie); token != "" {
		if err := solitudes.System.DB.Where("token_hash = ?", secretHash(token)).Delete(&model.LoginSession{}).Error; err != nil {
			return err
		}
	}
	http.SetCookie(writer, &http.Cookie{
		Name:     solitudes.AuthCookie,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestScheme(httpRequest) == "https",
	})
	return redirectResponse(writer, httpRequest, "/", http.StatusFound)

}

func index(writer http.ResponseWriter, httpRequest *http.Request) error {
	return renderPage(writer, httpRequest, http.StatusOK, "site/index", injectSiteData(writer, httpRequest, viewData{}))
}

func count(writer http.ResponseWriter, httpRequest *http.Request) error {
	if queryValue(httpRequest, "slug") == "" {
		return nil
	}
	// FIXME 允许刷新增加计数
	// key := c.IP() + c.Query("slug")
	// if _, ok := solitudes.System.Cache.Get(key); ok {
	// 	return nil
	// }
	// solitudes.System.Cache.Set(key, nil, time.Hour*20)
	var latestArticle model.Article
	if err := readableArticles(solitudes.System.DB, currentAccount(writer, httpRequest)).Select("id").
		Order("created_at DESC").
		Take(&latestArticle, "slug = ?", queryValue(httpRequest, "slug")).Error; err != nil {
		return nil
	}

	solitudes.System.DB.Model(&model.Article{}).
		Where("id = ?", latestArticle.ID).
		UpdateColumn("read_num", gorm.Expr("read_num + ?", 1))
	return nil
}
