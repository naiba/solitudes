package router

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/hashicorp/go-uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

const passkeyCookie = "solitudes_passkey_challenge"

type passkeyUser struct {
	account *model.Account
	keys    []model.Passkey
}

func (u passkeyUser) WebAuthnID() []byte          { return []byte(u.account.ID) }
func (u passkeyUser) WebAuthnName() string        { return u.account.Email }
func (u passkeyUser) WebAuthnDisplayName() string { return u.account.Nickname }
func (u passkeyUser) WebAuthnCredentials() []webauthn.Credential {
	credentials := make([]webauthn.Credential, 0, len(u.keys))
	for _, key := range u.keys {
		credentials = append(credentials, webauthn.Credential{
			ID: key.CredentialID, PublicKey: key.PublicKey, AttestationType: key.AttestationType,
			Authenticator: webauthn.Authenticator{AAGUID: key.AAGUID, SignCount: key.SignCount},
			Flags: webauthn.CredentialFlags{UserPresent: key.UserPresent, UserVerified: key.UserVerified,
				BackupEligible: key.BackupEligible, BackupState: key.BackupState},
		})
	}
	return credentials
}

func passkeyProvider() (*webauthn.WebAuthn, error) {
	cfg := solitudes.System.Config.Auth.WebAuthn
	if cfg.RPID == "" || cfg.Origin == "" {
		return nil, newHTTPError(http.StatusServiceUnavailable, "WebAuthn is not configured")
	}
	u, err := url.Parse(cfg.Origin)
	if err != nil || u.Hostname() == "" || u.User != nil ||
		(u.Scheme != "https" && !(u.Scheme == "http" && u.Hostname() == "localhost")) ||
		!strings.EqualFold(u.Hostname(), cfg.RPID) {
		return nil, newHTTPError(http.StatusServiceUnavailable, "invalid WebAuthn RP ID or origin")
	}
	return webauthn.New(&webauthn.Config{
		RPID: cfg.RPID, RPDisplayName: solitudes.System.Config.Site.SpaceName,
		RPOrigins:              []string{cfg.Origin},
		AuthenticatorSelection: protocol.AuthenticatorSelection{UserVerification: protocol.VerificationRequired},
	})
}

func loadPasskeyUser(account *model.Account) (passkeyUser, error) {
	var keys []model.Passkey
	err := solitudes.System.DB.Where("account_id = ?", account.ID).Find(&keys).Error
	return passkeyUser{account: account, keys: keys}, err
}

func savePasskeyCeremony(writer http.ResponseWriter, httpRequest *http.Request, accountID, purpose string, session *webauthn.SessionData) error {
	raw, err := json.Marshal(session)
	if err != nil {
		return err
	}
	secret, err := newSecret()
	if err != nil {
		return err
	}
	id, err := uuid.GenerateUUID()
	if err != nil {
		return err
	}
	if err := solitudes.System.DB.Create(&model.PasskeyCeremony{
		ID: id, AccountID: accountID, CookieHash: secretHash(secret), Purpose: purpose,
		SessionJSON: raw, ExpiresAt: time.Now().Add(5 * time.Minute),
	}).Error; err != nil {
		return err
	}
	http.SetCookie(writer, &http.Cookie{Name: passkeyCookie, Value: secret, Path: "/", MaxAge: 300,
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: requestScheme(httpRequest) == "https"})
	return nil
}

func consumePasskeyCeremony(writer http.ResponseWriter, httpRequest *http.Request, purpose string) (*model.PasskeyCeremony, webauthn.SessionData, error) {
	secret := requestCookie(httpRequest, passkeyCookie)
	if len(secret) != 64 {
		return nil, webauthn.SessionData{}, newHTTPError(http.StatusBadRequest)
	}
	http.SetCookie(writer, &http.Cookie{Name: passkeyCookie, Path: "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: requestScheme(httpRequest) == "https"})
	var record model.PasskeyCeremony
	err := solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("cookie_hash = ? AND purpose = ? AND expires_at > ?",
			secretHash(secret), purpose, time.Now()).Take(&record).Error; err != nil {
			return err
		}
		return tx.Delete(&record).Error
	})
	if err != nil {
		return nil, webauthn.SessionData{}, newHTTPError(http.StatusBadRequest)
	}
	var session webauthn.SessionData
	if err := json.Unmarshal(record.SessionJSON, &session); err != nil {
		return nil, session, err
	}
	return &record, session, nil
}

func beginPasskeyRegistration(writer http.ResponseWriter, httpRequest *http.Request) error {
	provider, err := passkeyProvider()
	if err != nil {
		return err
	}
	account := currentAccount(writer, httpRequest)
	user, err := loadPasskeyUser(account)
	if err != nil {
		return err
	}
	excluded := make([]protocol.CredentialDescriptor, 0, len(user.keys))
	for _, key := range user.keys {
		excluded = append(excluded, protocol.CredentialDescriptor{
			Type: protocol.PublicKeyCredentialType, CredentialID: key.CredentialID,
		})
	}
	options, session, err := provider.BeginRegistration(user, webauthn.WithExclusions(excluded))
	if err != nil {
		return err
	}
	if err := savePasskeyCeremony(writer, httpRequest, account.ID, "register", session); err != nil {
		return err
	}
	return writeJSON(writer, http.StatusOK, options)
}

func finishPasskeyRegistration(writer http.ResponseWriter, httpRequest *http.Request) error {
	provider, err := passkeyProvider()
	if err != nil {
		return err
	}
	record, session, err := consumePasskeyCeremony(writer, httpRequest, "register")
	if err != nil {
		return err
	}
	account := currentAccount(writer, httpRequest)
	if account.ID != record.AccountID {
		return newHTTPError(http.StatusForbidden)
	}
	user, err := loadPasskeyUser(account)
	if err != nil {
		return err
	}
	key, err := provider.FinishRegistration(user, session, httpRequest)
	if err != nil {
		return newHTTPError(http.StatusBadRequest, "invalid passkey registration")
	}
	if err := solitudes.System.DB.Create(&model.Passkey{
		AccountID: account.ID, CredentialID: key.ID, PublicKey: key.PublicKey,
		AAGUID: key.Authenticator.AAGUID, SignCount: key.Authenticator.SignCount,
		UserPresent: key.Flags.UserPresent, UserVerified: key.Flags.UserVerified,
		BackupEligible: key.Flags.BackupEligible, BackupState: key.Flags.BackupState,
		AttestationType: key.AttestationType, Name: "Passkey",
	}).Error; err != nil {
		return err
	}
	return writeStatus(writer, http.StatusCreated)
}

func beginPasskeyLogin(writer http.ResponseWriter, httpRequest *http.Request) error {
	provider, err := passkeyProvider()
	if err != nil {
		return err
	}
	var input struct {
		Email string `json:"email"`
	}
	if err := decodeRequestBody(httpRequest, &input); err != nil {
		return newHTTPError(http.StatusBadRequest)
	}
	var account model.Account
	if err := solitudes.System.DB.Where("email = ? AND email_verified_at IS NOT NULL AND disabled_at IS NULL", strings.ToLower(strings.TrimSpace(input.Email))).Take(&account).Error; err != nil {
		return newHTTPError(http.StatusBadRequest, "no passkeys available")
	}
	user, err := loadPasskeyUser(&account)
	if err != nil {
		return err
	}
	if len(user.keys) == 0 {
		return newHTTPError(http.StatusBadRequest, "no passkeys available")
	}
	options, session, err := provider.BeginLogin(user, webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return err
	}
	if err := savePasskeyCeremony(writer, httpRequest, account.ID, "login", session); err != nil {
		return err
	}
	return writeJSON(writer, http.StatusOK, options)
}

func finishPasskeyLogin(writer http.ResponseWriter, httpRequest *http.Request) error {
	provider, err := passkeyProvider()
	if err != nil {
		return err
	}
	record, session, err := consumePasskeyCeremony(writer, httpRequest, "login")
	if err != nil {
		return err
	}
	var account model.Account
	if err := solitudes.System.DB.Where("id = ? AND email_verified_at IS NOT NULL AND disabled_at IS NULL", record.AccountID).Take(&account).Error; err != nil {
		return newHTTPError(http.StatusUnauthorized)
	}
	user, err := loadPasskeyUser(&account)
	if err != nil {
		return err
	}
	key, err := provider.FinishLogin(user, session, httpRequest)
	if err != nil {
		return newHTTPError(http.StatusUnauthorized, "invalid passkey assertion")
	}
	if key.Authenticator.CloneWarning {
		return newHTTPError(http.StatusUnauthorized, "passkey counter regression")
	}
	if err := solitudes.System.DB.Model(&model.Passkey{}).Where("account_id = ? AND credential_id = ?", account.ID, key.ID).
		Updates(map[string]interface{}{"sign_count": key.Authenticator.SignCount,
			"backup_state": key.Flags.BackupState, "user_verified": key.Flags.UserVerified}).Error; err != nil {
		return err
	}
	if err := issueLoginSession(writer, httpRequest, &account, false); err != nil {
		return err
	}
	var input struct {
		ReturnTo string `json:"return_to"`
	}
	_ = json.Unmarshal(requestBody(httpRequest), &input)
	if target := safeReturnPath(input.ReturnTo); target != "" {
		return writeJSON(writer, http.StatusOK, viewData{"redirect": target})
	}
	return writeJSON(writer, http.StatusOK, viewData{"redirect": "/account"})
}

func deletePasskey(writer http.ResponseWriter, httpRequest *http.Request) error {
	account := currentAccount(writer, httpRequest)
	err := solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		var owner model.Account
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&owner, "id = ?", account.ID).Error; err != nil {
			return err
		}
		usableIdentity, err := hasUsableExternalIdentity(tx, account.ID, "")
		if err != nil {
			return err
		}
		var keys int64
		if err := tx.Model(&model.Passkey{}).Where("account_id = ?", account.ID).Count(&keys).Error; err != nil {
			return err
		}
		if owner.PasswordHash == "" && !usableIdentity && keys <= 1 {
			return newHTTPError(http.StatusConflict, "cannot remove the last sign-in method")
		}
		result := tx.Where("account_id = ? AND id = ?", account.ID, routeParam(httpRequest, "id")).Delete(&model.Passkey{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return newHTTPError(http.StatusNotFound)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return writeStatus(writer, http.StatusNoContent)
}
