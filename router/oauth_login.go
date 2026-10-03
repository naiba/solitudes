package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/hashicorp/go-uuid"
	"github.com/zitadel/oidc/v3/pkg/client/rp"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

const oauthStateCookie = "solitudes_oauth_state"

type upstreamIdentity struct {
	Subject  string
	Email    string
	Name     string
	Verified bool
}

func upstreamConfig(provider string) (*oauth2.Config, error) {
	cfg := solitudes.System.Config.Auth
	callback := publicBaseURL() + "/auth/" + provider + "/callback"
	switch provider {
	case "github":
		if cfg.GitHub.ClientID == "" || cfg.GitHub.ClientSecret == "" {
			break
		}
		return &oauth2.Config{
			ClientID: cfg.GitHub.ClientID, ClientSecret: cfg.GitHub.ClientSecret,
			RedirectURL: callback, Scopes: []string{"read:user", "user:email"},
			Endpoint: oauth2.Endpoint{AuthURL: "https://github.com/login/oauth/authorize", TokenURL: "https://github.com/login/oauth/access_token"},
		}, nil
	case "google":
		if cfg.Google.ClientID == "" || cfg.Google.ClientSecret == "" {
			break
		}
		return &oauth2.Config{
			ClientID: cfg.Google.ClientID, ClientSecret: cfg.Google.ClientSecret,
			RedirectURL: callback, Scopes: []string{"openid", "email", "profile"},
			Endpoint: oauth2.Endpoint{AuthURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token"},
		}, nil
	}
	return nil, fiber.ErrNotFound
}

func upstreamOIDC(ctx context.Context) (rp.RelyingParty, error) {
	cfg := solitudes.System.Config.Auth.OIDC
	u, err := url.Parse(cfg.Issuer)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, fiber.ErrNotFound
	}
	return rp.NewRelyingPartyOIDC(ctx, cfg.Issuer, cfg.ClientID, cfg.ClientSecret,
		publicBaseURL()+"/auth/oidc/callback", []string{"openid", "email", "profile"})
}

func beginOAuthLogin(c *fiber.Ctx) error {
	return startOAuthLogin(c, false)
}

func beginOAuthLink(c *fiber.Ctx) error {
	return startOAuthLogin(c, true)
}

func startOAuthLogin(c *fiber.Ctx, link bool) error {
	if !validSiteDomain() {
		return fiber.NewError(http.StatusServiceUnavailable, "external login requires a public site domain")
	}
	provider := c.Params("provider")
	verifier := oauth2.GenerateVerifier()
	state, err := newSecret()
	if err != nil {
		return err
	}
	nonce, err := newSecret()
	if err != nil {
		return err
	}
	attempt := model.OAuthAttempt{
		Provider: provider, StateHash: secretHash(state), Verifier: verifier, Nonce: nonce,
		ExpiresAt: time.Now().Add(10 * time.Minute), ReturnTo: safeReturnPath(c.Query("return_to")),
	}
	if link {
		if currentAccount(c) == nil {
			return fiber.ErrUnauthorized
		}
		attempt.AccountID = &currentAccount(c).ID
	}
	var target string
	if provider == "oidc" {
		ctx, cancel := context.WithTimeout(c.UserContext(), 10*time.Second)
		defer cancel()
		party, err := upstreamOIDC(ctx)
		if err != nil {
			return err
		}
		target = rp.AuthURL(state, party, rp.WithCodeChallenge(oidc.NewSHACodeChallenge(verifier)),
			func() []oauth2.AuthCodeOption { return []oauth2.AuthCodeOption{oauth2.SetAuthURLParam("nonce", nonce)} })
	} else {
		config, err := upstreamConfig(provider)
		if err != nil {
			return err
		}
		target = config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
	}
	if err := solitudes.System.DB.Create(&attempt).Error; err != nil {
		return err
	}
	c.Cookie(&fiber.Cookie{
		Name: oauthStateCookie, Value: state, Path: "/auth/", MaxAge: 600,
		HTTPOnly: true, SameSite: fiber.CookieSameSiteLaxMode, Secure: c.Protocol() == "https",
	})
	return c.Redirect(target, http.StatusFound)
}

func oauthCallback(c *fiber.Ctx) error {
	state := c.Query("state")
	if len(state) != 64 || state != c.Cookies(oauthStateCookie) {
		return fiber.NewError(http.StatusBadRequest, "invalid OAuth state")
	}
	c.Cookie(&fiber.Cookie{Name: oauthStateCookie, Value: "", Path: "/auth/", MaxAge: -1,
		HTTPOnly: true, SameSite: fiber.CookieSameSiteLaxMode, Secure: c.Protocol() == "https"})
	var attempt model.OAuthAttempt
	err := solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("state_hash = ? AND provider = ? AND expires_at > ?", secretHash(state), c.Params("provider"), time.Now()).
			Take(&attempt).Error; err != nil {
			return err
		}
		return tx.Delete(&attempt).Error
	})
	if err != nil {
		return fiber.NewError(http.StatusBadRequest, "expired OAuth state")
	}
	if c.Query("error") != "" || c.Query("code") == "" {
		return fiber.NewError(http.StatusBadRequest, "OAuth authorization declined")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 12*time.Second)
	defer cancel()
	identity, err := exchangeUpstreamIdentity(ctx, attempt.Provider, c.Query("code"), attempt.Verifier, attempt.Nonce)
	if err != nil || !identity.Verified || identity.Subject == "" {
		return fiber.NewError(http.StatusUnauthorized, "unable to verify external identity and email")
	}
	account, err := findOrLinkExternalIdentity(&attempt, identity, currentAccount(c))
	if err != nil {
		return err
	}
	if attempt.AccountID == nil {
		if err := issueLoginSession(c, account, false); err != nil {
			return err
		}
	}
	if attempt.ReturnTo != "" {
		return c.Redirect(attempt.ReturnTo, http.StatusFound)
	}
	return c.Redirect("/account", http.StatusFound)
}

func findOrLinkExternalIdentity(attempt *model.OAuthAttempt, identity upstreamIdentity, loggedIn *model.Account) (*model.Account, error) {
	email := strings.ToLower(strings.TrimSpace(identity.Email))
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || len(email) > 254 {
		return nil, fiber.ErrUnauthorized
	}
	if attempt.AccountID != nil && (loggedIn == nil || loggedIn.ID != *attempt.AccountID) {
		return nil, fiber.ErrForbidden
	}
	var account model.Account
	err = solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		var linked model.ExternalIdentity
		err := tx.Where("provider = ? AND subject = ?", attempt.Provider, identity.Subject).Take(&linked).Error
		if err == nil {
			if attempt.AccountID != nil && linked.AccountID != *attempt.AccountID {
				return fiber.ErrConflict
			}
			return tx.Take(&account, "id = ? AND disabled_at IS NULL AND email_verified_at IS NOT NULL", linked.AccountID).Error
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if attempt.AccountID != nil {
			account = *loggedIn
		} else {
			err = tx.Where("email = ?", email).Take(&account).Error
			if err == nil {
				return fiber.NewError(http.StatusConflict, "email already registered; sign in and link this provider")
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			now := time.Now()
			name := strings.TrimSpace(identity.Name)
			if name == "" {
				name = strings.Split(email, "@")[0]
			}
			id, err := uuid.GenerateUUID()
			if err != nil {
				return err
			}
			account = model.Account{ID: id, Email: email, Nickname: name, Role: model.RoleUser, EmailVerifiedAt: &now}
			if err := tx.Create(&account).Error; err != nil {
				return err
			}
		}
		id, err := uuid.GenerateUUID()
		if err != nil {
			return err
		}
		return tx.Create(&model.ExternalIdentity{ID: id, AccountID: account.ID, Provider: attempt.Provider, Subject: identity.Subject}).Error
	})
	if err != nil {
		return nil, err
	}
	return &account, nil
}

func exchangeUpstreamIdentity(ctx context.Context, provider, code, verifier, nonce string) (upstreamIdentity, error) {
	if provider == "oidc" {
		party, err := upstreamOIDC(ctx)
		if err != nil {
			return upstreamIdentity{}, err
		}
		tokens, err := rp.CodeExchange[*oidc.IDTokenClaims](ctx, code, party, rp.WithCodeVerifier(verifier))
		if err != nil || tokens.IDTokenClaims == nil {
			return upstreamIdentity{}, fmt.Errorf("verify OIDC ID token: %w", err)
		}
		claims := tokens.IDTokenClaims
		if claims.Nonce != nonce {
			return upstreamIdentity{}, errors.New("OIDC nonce mismatch")
		}
		return upstreamIdentity{Subject: claims.Subject, Email: claims.Email,
			Name: claims.Name, Verified: bool(claims.EmailVerified)}, nil
	}
	config, err := upstreamConfig(provider)
	if err != nil {
		return upstreamIdentity{}, err
	}
	token, err := config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return upstreamIdentity{}, err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	if provider == "google" {
		var user struct {
			Sub           string `json:"sub"`
			Email         string `json:"email"`
			Name          string `json:"name"`
			EmailVerified bool   `json:"email_verified"`
		}
		if err := oauthGetJSON(ctx, client, "https://openidconnect.googleapis.com/v1/userinfo", token.AccessToken, &user); err != nil {
			return upstreamIdentity{}, err
		}
		return upstreamIdentity{user.Sub, user.Email, user.Name, user.EmailVerified}, nil
	}
	var user struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Login string `json:"login"`
	}
	if err := oauthGetJSON(ctx, client, "https://api.github.com/user", token.AccessToken, &user); err != nil {
		return upstreamIdentity{}, err
	}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := oauthGetJSON(ctx, client, "https://api.github.com/user/emails", token.AccessToken, &emails); err != nil {
		return upstreamIdentity{}, err
	}
	for _, e := range emails {
		if e.Primary && e.Verified {
			name := user.Name
			if name == "" {
				name = user.Login
			}
			return upstreamIdentity{strconv.FormatInt(user.ID, 10), e.Email, name, true}, nil
		}
	}
	return upstreamIdentity{}, errors.New("GitHub has no verified primary email")
}

func oauthGetJSON(ctx context.Context, client *http.Client, endpoint, accessToken string, target interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("identity endpoint status %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(target)
}
