package solitudes

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/naiba/solitudes/internal/model"
)

var errAdministratorInitialized = errors.New("accounts already exist; init-admin never resets passwords or promotes existing users")

func administratorIdentity(email, nickname string) (string, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	nickname = strings.TrimSpace(nickname)
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 254 || !strings.Contains(email, "@") {
		return "", "", errors.New("a valid administrator email is required")
	}
	if nickname == "" || utf8.RuneCountInString(nickname) > 64 || strings.ContainsAny(nickname, "\r\n\x00") {
		return "", "", errors.New("administrator nickname must be 1–64 characters without control characters")
	}
	return email, nickname, nil
}

// CreateInitialAdministrator is a local-only setup operation. The generated
// password is returned once to the CLI; it is never written to configuration.
func CreateInitialAdministrator(email, nickname string) (string, error) {
	email, nickname, err := administratorIdentity(email, nickname)
	if err != nil {
		return "", err
	}
	conf, err := newConfig()
	if err != nil {
		return "", err
	}
	conf.Debug = false
	db, err := newDatabase(conf)
	if err != nil {
		return "", err
	}
	pool, err := db.DB()
	if err != nil {
		return "", err
	}
	defer pool.Close()
	if err := initializeDatabase(db); err != nil {
		return "", err
	}
	return createInitialAdministrator(db, email, nickname)
}

func createInitialAdministrator(db *gorm.DB, email, nickname string) (string, error) {
	email, nickname, err := administratorIdentity(email, nickname)
	if err != nil {
		return "", err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	password := base64.RawURLEncoding.EncodeToString(secret)
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		// Serializes concurrent setup commands and excludes registration writes.
		if err := tx.Exec("SET LOCAL lock_timeout = '5s'").Error; err != nil {
			return err
		}
		if err := tx.Exec("LOCK TABLE accounts IN EXCLUSIVE MODE").Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&model.Account{}).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return errAdministratorInitialized
		}
		now := time.Now().UTC()
		account := model.Account{Email: email, Nickname: nickname, PasswordHash: string(hash), Role: model.RoleAdmin, EmailVerifiedAt: &now}
		if err := tx.Create(&account).Error; err != nil {
			return err
		}
		return tx.Create(&model.AuditEvent{ID: account.ID, Action: "account.initialized", Outcome: "success", ActorID: account.ID, CreatedAt: now}).Error
	})
	if err != nil {
		return "", err
	}
	return password, nil
}

func requireAdministrator(db *gorm.DB) error {
	var count int64
	if err := db.Model(&model.Account{}).Where("role = ? AND disabled_at IS NULL", model.RoleAdmin).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("no active administrator: initialize an empty installation with solitudes init-admin --email you@example.com --nickname Administrator; existing accounts require operator recovery")
	}
	return nil
}
