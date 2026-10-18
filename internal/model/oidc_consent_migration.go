package model

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

// DatabaseMigration records completed data migrations independently of audit
// retention. Never remove these markers during routine database maintenance.
type DatabaseMigration struct {
	Name        string    `gorm:"primaryKey"`
	CompletedAt time.Time `gorm:"not null"`
}

const oidcConsentMigrationName = "oidc_persistent_consent"

// MigrateOIDCConsent imports live legacy credentials once, after schema creation
// and before serving requests. Deployments must not run mixed-version writers.
func MigrateOIDCConsent(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		schema, err := currentDatabaseSchema(tx)
		if err != nil {
			return err
		}
		// A missing local table must fail closed, never read or mark a fallback
		// schema. The relation lock serializes only this schema's migrations.
		if err := tx.Exec("SET LOCAL search_path TO " + schema).Error; err != nil {
			return err
		}
		if err := tx.Exec("LOCK TABLE database_migrations IN EXCLUSIVE MODE").Error; err != nil {
			return err
		}
		var completed int64
		if err := tx.Model(&DatabaseMigration{}).Where("name = ?", oidcConsentMigrationName).Count(&completed).Error; err != nil {
			return err
		}
		if completed != 0 {
			return nil
		}

		// Both token kinds use exactly the same cutoff, including strict expiry
		// exclusion at equality. GORM's clock also makes this boundary testable.
		cutoff := tx.NowFunc().UTC()
		var credentials []struct{ AccountID, ClientID, Scopes string }
		if err := tx.Raw(`SELECT account_id, client_id, scopes FROM o_id_c_access_tokens WHERE expires_at > ?
			UNION ALL
			SELECT account_id, client_id, scopes FROM o_id_c_refresh_tokens WHERE expires_at > ?`, cutoff, cutoff).Scan(&credentials).Error; err != nil {
			return fmt.Errorf("load legacy OIDC permissions: %w", err)
		}
		type accountClient struct{ accountID, clientID string }
		grants := make(map[accountClient]map[string]struct{})
		for _, credential := range credentials {
			key := accountClient{credential.AccountID, credential.ClientID}
			if grants[key] == nil {
				grants[key] = make(map[string]struct{})
			}
			for _, scope := range strings.Fields(credential.Scopes) {
				grants[key][scope] = struct{}{}
			}
		}
		for key, permissions := range grants {
			scopes := make([]string, 0, len(permissions))
			for scope := range permissions {
				scopes = append(scopes, scope)
			}
			sort.Strings(scopes)
			consent := OIDCConsent{AccountID: key.accountID, ClientID: key.clientID,
				Scopes: strings.Join(scopes, " "), CreatedAt: cutoff, UpdatedAt: cutoff}
			if err := tx.Create(&consent).Error; err != nil {
				return fmt.Errorf("import legacy OIDC consent: %w", err)
			}
		}
		// Persist even an empty import, in the same transaction as all grants.
		// Otherwise a restart could resurrect explicitly revoked permissions.
		return tx.Create(&DatabaseMigration{Name: oidcConsentMigrationName, CompletedAt: cutoff}).Error
	})
}
