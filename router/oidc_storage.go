package router

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/hashicorp/go-uuid"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/naiba/solitudes/internal/model"
)

const accessTokenLifetime = time.Hour
const refreshTokenLifetime = 30 * 24 * time.Hour

type oidcStorage struct{ db *gorm.DB }

var _ op.Storage = (*oidcStorage)(nil)

type oidcAuthRequest struct {
	model.OIDCAuthRequest
	req oidc.AuthRequest
}

func (r *oidcAuthRequest) GetID() string         { return r.ID }
func (r *oidcAuthRequest) GetACR() string        { return "" }
func (r *oidcAuthRequest) GetAMR() []string      { return nil }
func (r *oidcAuthRequest) GetAudience() []string { return []string{r.ClientID} }
func (r *oidcAuthRequest) GetAuthTime() time.Time {
	if r.AuthTime == nil {
		return time.Time{}
	}
	return *r.AuthTime
}
func (r *oidcAuthRequest) GetClientID() string { return r.ClientID }
func (r *oidcAuthRequest) GetCodeChallenge() *oidc.CodeChallenge {
	if r.req.CodeChallenge == "" {
		return nil
	}
	return &oidc.CodeChallenge{Challenge: r.req.CodeChallenge, Method: r.req.CodeChallengeMethod}
}
func (r *oidcAuthRequest) GetNonce() string                   { return r.req.Nonce }
func (r *oidcAuthRequest) GetRedirectURI() string             { return r.req.RedirectURI }
func (r *oidcAuthRequest) GetResponseType() oidc.ResponseType { return r.req.ResponseType }
func (r *oidcAuthRequest) GetResponseMode() oidc.ResponseMode { return r.req.ResponseMode }
func (r *oidcAuthRequest) GetScopes() []string                { return r.req.Scopes }
func (r *oidcAuthRequest) GetState() string                   { return r.req.State }
func (r *oidcAuthRequest) GetSubject() string {
	if r.AccountID == nil {
		return ""
	}
	return *r.AccountID
}
func (r *oidcAuthRequest) Done() bool { return r.Approved }

func oidcRequestFromRow(row model.OIDCAuthRequest) (*oidcAuthRequest, error) {
	var request oidc.AuthRequest
	if err := json.Unmarshal(row.RequestJSON, &request); err != nil {
		return nil, err
	}
	return &oidcAuthRequest{OIDCAuthRequest: row, req: request}, nil
}

func (s *oidcStorage) CreateAuthRequest(ctx context.Context, request *oidc.AuthRequest, subject string) (op.AuthRequest, error) {
	if request.ResponseType != oidc.ResponseTypeCode || request.CodeChallengeMethod != oidc.CodeChallengeMethodS256 || request.CodeChallenge == "" {
		return nil, oidc.ErrInvalidRequest().WithDescription("authorization code with PKCE S256 required")
	}
	for _, prompt := range request.Prompt {
		if prompt == oidc.PromptNone {
			return nil, oidc.ErrLoginRequired()
		}
	}
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	id, err := uuid.GenerateUUID()
	if err != nil {
		return nil, err
	}
	row := model.OIDCAuthRequest{ID: id, ClientID: request.ClientID, RequestJSON: data,
		ExpiresAt: time.Now().Add(10 * time.Minute)}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return nil, err
	}
	return oidcRequestFromRow(row)
}

func (s *oidcStorage) AuthRequestByID(ctx context.Context, id string) (op.AuthRequest, error) {
	var row model.OIDCAuthRequest
	if err := s.db.WithContext(ctx).Where("id = ? AND expires_at > ?", id, time.Now()).Take(&row).Error; err != nil {
		return nil, err
	}
	return oidcRequestFromRow(row)
}

func (s *oidcStorage) AuthRequestByCode(ctx context.Context, code string) (op.AuthRequest, error) {
	var row model.OIDCAuthRequest
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"code_hash = ? AND code_used_at IS NULL AND approved = true AND expires_at > ?", secretHash(code), time.Now()).Take(&row).Error; err != nil {
			return err
		}
		return tx.Model(&row).Update("code_used_at", time.Now()).Error
	})
	if err != nil {
		return nil, err
	}
	return oidcRequestFromRow(row)
}

func (s *oidcStorage) SaveAuthCode(ctx context.Context, id, code string) error {
	result := s.db.WithContext(ctx).Model(&model.OIDCAuthRequest{}).
		Where("id = ? AND approved = true AND expires_at > ? AND code_hash IS NULL", id, time.Now()).Update("code_hash", secretHash(code))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("authorization request not approved")
	}
	return nil
}

func (s *oidcStorage) DeleteAuthRequest(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Delete(&model.OIDCAuthRequest{}, "id = ?", id).Error
}

type oidcRefreshRequest struct {
	token  model.OIDCRefreshToken
	scopes []string
}

func (r *oidcRefreshRequest) GetAMR() []string                 { return nil }
func (r *oidcRefreshRequest) GetAudience() []string            { return []string{r.token.ClientID} }
func (r *oidcRefreshRequest) GetAuthTime() time.Time           { return r.token.AuthTime }
func (r *oidcRefreshRequest) GetClientID() string              { return r.token.ClientID }
func (r *oidcRefreshRequest) GetScopes() []string              { return r.scopes }
func (r *oidcRefreshRequest) GetSubject() string               { return r.token.AccountID }
func (r *oidcRefreshRequest) SetCurrentScopes(scopes []string) { r.scopes = scopes }

func (s *oidcStorage) TokenRequestByRefreshToken(ctx context.Context, refreshToken string) (op.RefreshTokenRequest, error) {
	var row model.OIDCRefreshToken
	if err := s.db.WithContext(ctx).Where("token_hash = ? AND expires_at > ?", secretHash(refreshToken), time.Now()).Take(&row).Error; err != nil {
		return nil, op.ErrInvalidRefreshToken
	}
	if err := s.activeAccount(ctx, row.AccountID); err != nil {
		return nil, op.ErrInvalidRefreshToken
	}
	return &oidcRefreshRequest{token: row, scopes: strings.Fields(row.Scopes)}, nil
}

func newOIDCAccess(ctx context.Context, tx *gorm.DB, request op.TokenRequest, clientID string) (*model.OIDCAccessToken, error) {
	id, err := uuid.GenerateUUID()
	if err != nil {
		return nil, err
	}
	row := &model.OIDCAccessToken{
		ID: id, ClientID: clientID, AccountID: request.GetSubject(),
		Scopes: strings.Join(request.GetScopes(), " "), ExpiresAt: time.Now().Add(accessTokenLifetime),
	}
	if err := tx.WithContext(ctx).Create(row).Error; err != nil {
		return nil, err
	}
	return row, nil
}

func oidcClientID(request op.TokenRequest) string {
	if r, ok := request.(interface{ GetClientID() string }); ok {
		return r.GetClientID()
	}
	return ""
}

func (s *oidcStorage) CreateAccessToken(ctx context.Context, request op.TokenRequest) (string, time.Time, error) {
	row, err := newOIDCAccess(ctx, s.db, request, oidcClientID(request))
	if err != nil {
		return "", time.Time{}, err
	}
	return row.ID, row.ExpiresAt, nil
}

func (s *oidcStorage) CreateAccessAndRefreshTokens(ctx context.Context, request op.TokenRequest, current string) (string, string, time.Time, error) {
	clientID := oidcClientID(request)
	raw, err := newSecret()
	if err != nil {
		return "", "", time.Time{}, err
	}
	refreshID, err := uuid.GenerateUUID()
	if err != nil {
		return "", "", time.Time{}, err
	}
	var access *model.OIDCAccessToken
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if current != "" {
			var old model.OIDCRefreshToken
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("token_hash = ? AND client_id = ? AND expires_at > ?", secretHash(current), clientID, time.Now()).Take(&old).Error; err != nil {
				return op.ErrInvalidRefreshToken
			}
			if err := tx.Delete(&old).Error; err != nil {
				return err
			}
			if err := tx.Delete(&model.OIDCAccessToken{}, "id = ?", old.AccessID).Error; err != nil {
				return err
			}
		}
		access, err = newOIDCAccess(ctx, tx, request, clientID)
		if err != nil {
			return err
		}
		authTime := time.Now()
		if r, ok := request.(interface{ GetAuthTime() time.Time }); ok {
			authTime = r.GetAuthTime()
		}
		return tx.Create(&model.OIDCRefreshToken{ID: refreshID, TokenHash: secretHash(raw), ClientID: clientID,
			AccountID: request.GetSubject(), AccessID: access.ID, Scopes: strings.Join(request.GetScopes(), " "),
			AuthTime: authTime, ExpiresAt: time.Now().Add(refreshTokenLifetime)}).Error
	})
	if err != nil {
		return "", "", time.Time{}, err
	}
	return access.ID, raw, access.ExpiresAt, nil
}

func (s *oidcStorage) TerminateSession(ctx context.Context, subject, clientID string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("account_id = ? AND client_id = ?", subject, clientID).Delete(&model.OIDCRefreshToken{}).Error; err != nil {
			return err
		}
		return tx.Where("account_id = ? AND client_id = ?", subject, clientID).Delete(&model.OIDCAccessToken{}).Error
	})
}

func (s *oidcStorage) GetRefreshTokenInfo(ctx context.Context, clientID, raw string) (string, string, error) {
	var token model.OIDCRefreshToken
	if err := s.db.WithContext(ctx).Where("client_id = ? AND token_hash = ? AND expires_at > ?", clientID, secretHash(raw), time.Now()).Take(&token).Error; err != nil {
		return "", "", op.ErrInvalidRefreshToken
	}
	return token.AccountID, token.ID, nil
}

func (s *oidcStorage) RevokeToken(ctx context.Context, tokenOrID, subject, clientID string) *oidc.Error {
	if subject != "" {
		var token model.OIDCAccessToken
		err := s.db.WithContext(ctx).Where("id = ?", tokenOrID).Take(&token).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return oidc.ErrServerError()
		}
		if token.ClientID != clientID {
			return oidc.ErrInvalidClient()
		}
		if err := s.db.WithContext(ctx).Delete(&token).Error; err != nil {
			return oidc.ErrServerError()
		}
		return nil
	}
	var token model.OIDCRefreshToken
	err := s.db.WithContext(ctx).Where("token_hash = ?", secretHash(tokenOrID)).Take(&token).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return oidc.ErrServerError()
	}
	if token.ClientID != clientID {
		return oidc.ErrInvalidClient()
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&token).Error; err != nil {
			return err
		}
		return tx.Delete(&model.OIDCAccessToken{}, "id = ?", token.AccessID).Error
	}); err != nil {
		return oidc.ErrServerError()
	}
	return nil
}

func (s *oidcStorage) activeAccount(ctx context.Context, id string) error {
	var account model.Account
	return s.db.WithContext(ctx).Select("id").Where("id = ? AND disabled_at IS NULL AND email_verified_at IS NOT NULL", id).Take(&account).Error
}

func (s *oidcStorage) SetUserinfoFromScopes(ctx context.Context, info *oidc.UserInfo, subject, _ string, scopes []string) error {
	return s.setUserInfo(ctx, info, subject, scopes)
}
func (s *oidcStorage) SetUserinfoFromRequest(ctx context.Context, info *oidc.UserInfo, request op.IDTokenRequest, scopes []string) error {
	return s.setUserInfo(ctx, info, request.GetSubject(), scopes)
}
func (s *oidcStorage) setUserInfo(ctx context.Context, info *oidc.UserInfo, subject string, scopes []string) error {
	var account model.Account
	if err := s.db.WithContext(ctx).Where("id = ? AND email_verified_at IS NOT NULL AND disabled_at IS NULL", subject).Take(&account).Error; err != nil {
		return err
	}
	info.Subject = account.ID
	for _, scope := range scopes {
		switch scope {
		case "email":
			info.Email = account.Email
			info.EmailVerified = true
		case "profile":
			info.Name = account.Nickname
			info.Nickname = account.Nickname
		}
	}
	return nil
}
func (s *oidcStorage) SetUserinfoFromToken(ctx context.Context, info *oidc.UserInfo, tokenID, subject, origin string) error {
	var token model.OIDCAccessToken
	if err := s.db.WithContext(ctx).Where("id = ? AND account_id = ? AND expires_at > ?", tokenID, subject, time.Now()).Take(&token).Error; err != nil {
		return err
	}
	return s.setUserInfo(ctx, info, subject, strings.Fields(token.Scopes))
}
func (s *oidcStorage) SetIntrospectionFromToken(ctx context.Context, result *oidc.IntrospectionResponse, tokenID, subject, clientID string) error {
	var token model.OIDCAccessToken
	if err := s.db.WithContext(ctx).Where("id = ? AND account_id = ? AND client_id = ? AND expires_at > ?", tokenID, subject, clientID, time.Now()).Take(&token).Error; err != nil {
		return err
	}
	info := &oidc.UserInfo{}
	if err := s.setUserInfo(ctx, info, subject, strings.Fields(token.Scopes)); err != nil {
		return err
	}
	result.SetUserInfo(info)
	result.ClientID = clientID
	result.Expiration = oidc.FromTime(token.ExpiresAt)
	result.Scope = strings.Fields(token.Scopes)
	return nil
}
func (s *oidcStorage) GetPrivateClaimsFromScopes(context.Context, string, string, []string) (map[string]any, error) {
	return map[string]any{}, nil
}
func (s *oidcStorage) GetKeyByIDAndClientID(context.Context, string, string) (*jose.JSONWebKey, error) {
	return nil, errors.New("private_key_jwt is not supported")
}
func (s *oidcStorage) ValidateJWTProfileScopes(context.Context, string, []string) ([]string, error) {
	return nil, errors.New("JWT profile is not supported")
}
func (s *oidcStorage) Health(ctx context.Context) error {
	return s.db.WithContext(ctx).Exec("SELECT 1").Error
}

type oidcSigningKey struct {
	id  string
	key *rsa.PrivateKey
}

func (k oidcSigningKey) ID() string                                  { return k.id }
func (k oidcSigningKey) SignatureAlgorithm() jose.SignatureAlgorithm { return jose.RS256 }
func (k oidcSigningKey) Key() any                                    { return k.key }

type oidcPublicKey struct {
	id  string
	key *rsa.PublicKey
}

func (k oidcPublicKey) ID() string                         { return k.id }
func (k oidcPublicKey) Algorithm() jose.SignatureAlgorithm { return jose.RS256 }
func (k oidcPublicKey) Use() string                        { return "sig" }
func (k oidcPublicKey) Key() any                           { return k.key }

func decodeSigningKey(data []byte) (*rsa.PrivateKey, error) {
	parsed, err := x509.ParsePKCS8PrivateKey(data)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("OIDC signing key is not RSA")
	}
	return key, nil
}

func (s *oidcStorage) SigningKey(ctx context.Context) (op.SigningKey, error) {
	var row model.OIDCSigningKey
	if err := s.db.WithContext(ctx).Where("active = true").Order("created_at DESC").Take(&row).Error; err != nil {
		return nil, err
	}
	key, err := decodeSigningKey(row.KeyDER)
	if err != nil {
		return nil, err
	}
	return oidcSigningKey{id: row.ID, key: key}, nil
}
func (s *oidcStorage) SignatureAlgorithms(context.Context) ([]jose.SignatureAlgorithm, error) {
	return []jose.SignatureAlgorithm{jose.RS256}, nil
}
func (s *oidcStorage) KeySet(ctx context.Context) ([]op.Key, error) {
	var rows []model.OIDCSigningKey
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, err
	}
	keys := make([]op.Key, 0, len(rows))
	for _, row := range rows {
		privateKey, err := decodeSigningKey(row.KeyDER)
		if err != nil {
			return nil, err
		}
		keys = append(keys, oidcPublicKey{id: row.ID, key: &privateKey.PublicKey})
	}
	return keys, nil
}

func generateSigningKey(db *gorm.DB) error {
	key, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return fmt.Errorf("generate signing key: %w", err)
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	id, err := newSecret()
	if err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.OIDCSigningKey{}).Where("active = true").Update("active", false).Error; err != nil {
			return err
		}
		return tx.Create(&model.OIDCSigningKey{ID: id, KeyDER: encoded, Active: true}).Error
	})
}

func ensureOIDCKeys(db *gorm.DB) ([32]byte, string, error) {
	var cryptoKey [32]byte
	var key model.OIDCCryptoKey
	err := db.Take(&key, "id = ?", "primary").Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		bytes := make([]byte, 32)
		if _, err := rand.Read(bytes); err != nil {
			return cryptoKey, "", err
		}
		key = model.OIDCCryptoKey{ID: "primary", Key: bytes}
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&key).Error; err != nil {
			return cryptoKey, "", err
		}
		if err := db.Take(&key, "id = ?", "primary").Error; err != nil {
			return cryptoKey, "", err
		}
	} else if err != nil {
		return cryptoKey, "", err
	}
	if len(key.Key) != 32 {
		return cryptoKey, "", errors.New("invalid OIDC crypto key length")
	}
	copy(cryptoKey[:], key.Key)
	var count int64
	if err := db.Model(&model.OIDCSigningKey{}).Where("active = true").Count(&count).Error; err != nil {
		return cryptoKey, "", err
	}
	if count == 0 {
		if err := generateSigningKey(db); err != nil {
			return cryptoKey, "", err
		}
	}
	return cryptoKey, key.ID, nil
}

type oidcClient struct {
	model.OIDCClient
	redirects []string
}

func (c *oidcClient) GetID() string                    { return c.ID }
func (c *oidcClient) RedirectURIs() []string           { return c.redirects }
func (c *oidcClient) PostLogoutRedirectURIs() []string { return nil }
func (c *oidcClient) ApplicationType() op.ApplicationType {
	if c.Public {
		return op.ApplicationTypeNative
	}
	return op.ApplicationTypeWeb
}
func (c *oidcClient) AuthMethod() oidc.AuthMethod {
	if c.Public {
		return oidc.AuthMethodNone
	}
	return oidc.AuthMethodBasic
}
func (c *oidcClient) ResponseTypes() []oidc.ResponseType {
	return []oidc.ResponseType{oidc.ResponseTypeCode}
}
func (c *oidcClient) GrantTypes() []oidc.GrantType {
	return []oidc.GrantType{oidc.GrantTypeCode, oidc.GrantTypeRefreshToken}
}
func (c *oidcClient) LoginURL(id string) string           { return "/oidc/consent?authRequestID=" + id }
func (c *oidcClient) AccessTokenType() op.AccessTokenType { return op.AccessTokenTypeBearer }
func (c *oidcClient) IDTokenLifetime() time.Duration      { return time.Hour }
func (c *oidcClient) DevMode() bool                       { return false }
func (c *oidcClient) RestrictAdditionalIdTokenScopes() func([]string) []string {
	return func(s []string) []string { return s }
}
func (c *oidcClient) RestrictAdditionalAccessTokenScopes() func([]string) []string {
	return func(s []string) []string { return s }
}
func (c *oidcClient) IsScopeAllowed(scope string) bool     { return false }
func (c *oidcClient) IDTokenUserinfoClaimsAssertion() bool { return false }
func (c *oidcClient) ClockSkew() time.Duration             { return 0 }

func (s *oidcStorage) GetClientByClientID(ctx context.Context, id string) (op.Client, error) {
	var row model.OIDCClient
	if err := s.db.WithContext(ctx).Where("id = ? AND disabled_at IS NULL", id).Take(&row).Error; err != nil {
		return nil, err
	}
	var redirects []string
	if err := json.Unmarshal([]byte(row.RedirectURIsJSON), &redirects); err != nil {
		return nil, err
	}
	return &oidcClient{OIDCClient: row, redirects: redirects}, nil
}
func (s *oidcStorage) AuthorizeClientIDSecret(ctx context.Context, id, secret string) error {
	var client model.OIDCClient
	if err := s.db.WithContext(ctx).Where("id = ? AND disabled_at IS NULL AND public = false", id).Take(&client).Error; err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(client.SecretHash), []byte(secret)) != nil {
		return errors.New("invalid client credentials")
	}
	return nil
}
