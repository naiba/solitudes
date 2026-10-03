package model

import (
	"fmt"

	"github.com/lib/pq"
	"gorm.io/gorm"
)

// MigrateDatabasePolicy is an explicit, repeatable PostgreSQL migration run
// after AutoMigrate and legacy data backfills. Never deduplicate or discard
// conflicting user data to make a constraint succeed: abort startup instead.
func MigrateDatabasePolicy(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		// DDL must never fall through search_path into another schema when a
		// legacy index is absent locally (including repeat migrations/tests).
		schema, err := currentDatabaseSchema(tx)
		if err != nil {
			return err
		}
		if err := tx.Exec("SET LOCAL search_path TO " + schema).Error; err != nil {
			return err
		}
		if err := tx.Exec(`SET LOCAL lock_timeout = '5s'`).Error; err != nil {
			return err
		}
		for _, item := range []struct{ table, name, definition string }{
			{"accounts", "accounts_role_valid", "CHECK (role IN ('admin', 'editor', 'user'))"},
			{"articles", "articles_visibility_valid", "CHECK (visibility IN ('public', 'members', 'editors', 'private'))"},
			{"article_histories", "history_editor_account", "FOREIGN KEY (editor_id) REFERENCES accounts(id) ON DELETE RESTRICT"},
			{"login_sessions", "session_account", "FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE"},
			{"email_actions", "email_action_account", "FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE"},
			{"external_identities", "external_identity_account", "FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE"},
			{"passkeys", "passkey_account", "FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE"},
			{"passkey_ceremonies", "ceremony_account", "FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE"},
			{"o_auth_attempts", "oauth_attempt_account", "FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE"},
			{"o_id_c_clients", "client_owner", "FOREIGN KEY (owner_id) REFERENCES accounts(id) ON DELETE RESTRICT"},
			{"o_id_c_auth_requests", "auth_request_account", "FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE"},
			{"o_id_c_auth_requests", "auth_request_client", "FOREIGN KEY (client_id) REFERENCES o_id_c_clients(id) ON DELETE CASCADE"},
			{"o_id_c_access_tokens", "access_token_account", "FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE"},
			{"o_id_c_access_tokens", "access_token_client", "FOREIGN KEY (client_id) REFERENCES o_id_c_clients(id) ON DELETE CASCADE"},
			{"o_id_c_refresh_tokens", "refresh_token_account", "FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE"},
			{"o_id_c_refresh_tokens", "refresh_token_client", "FOREIGN KEY (client_id) REFERENCES o_id_c_clients(id) ON DELETE CASCADE"},
		} {
			var exists bool
			if err := tx.Raw(`SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = to_regclass(?) AND conname = ?)`, item.table, item.name).Scan(&exists).Error; err != nil {
				return err
			}
			if !exists {
				// All identifiers and expressions are static developer-owned SQL.
				if err := tx.Exec("ALTER TABLE " + item.table + " ADD CONSTRAINT " + item.name + " " + item.definition).Error; err != nil {
					return fmt.Errorf("add %s (repair conflicting data before retrying): %w", item.name, err)
				}
			}
		}
		for _, ddl := range []string{
			// The model's unique slug index supersedes this old constraint.
			`ALTER TABLE articles DROP CONSTRAINT IF EXISTS uni_articles_slug`,
			`CREATE INDEX IF NOT EXISTS idx_articles_author_page ON articles (author_id, created_at DESC, id DESC)`,
			`CREATE INDEX IF NOT EXISTS idx_articles_page ON articles (created_at DESC, id DESC)`,
			`CREATE INDEX IF NOT EXISTS idx_comments_thread_page ON comments (article_id, reply_to, created_at DESC, id DESC) WHERE is_spam = false`,
			`CREATE INDEX IF NOT EXISTS idx_accounts_page ON accounts (created_at DESC, id DESC)`,
			`CREATE INDEX IF NOT EXISTS idx_clients_owner_page ON o_id_c_clients (owner_id, created_at DESC, id)`,
			`CREATE INDEX IF NOT EXISTS idx_clients_page ON o_id_c_clients (created_at DESC, id)`,
			`CREATE INDEX IF NOT EXISTS idx_audit_page ON audit_events (created_at DESC, id DESC)`,
			`CREATE INDEX IF NOT EXISTS idx_audit_session_actor ON audit_events (actor_id, created_at DESC) WHERE action = 'session.login' AND outcome = 'success'`,
			`CREATE INDEX IF NOT EXISTS idx_audit_oidc_client_actor ON audit_events (client_id, actor_id, created_at DESC) WHERE action = 'oidc.login' AND outcome = 'success'`,
			`CREATE INDEX IF NOT EXISTS idx_login_summary_actor ON login_summaries (action, actor_id)`,
			`CREATE INDEX IF NOT EXISTS idx_ceremony_account ON passkey_ceremonies (account_id)`,
			`CREATE INDEX IF NOT EXISTS idx_oauth_attempt_account ON o_auth_attempts (account_id)`,
			// Replace indexes from shipped schemas, not development-only states.
			`DROP INDEX IF EXISTS idx_articles_tags`,
			`DROP INDEX IF EXISTS idx_article_histories_article_id`,
			`DROP INDEX IF EXISTS idx_article_histories_version`,
			`DROP INDEX IF EXISTS idx_nickname`,
			`DROP INDEX IF EXISTS idx_articles_author_id`,
			`DROP INDEX IF EXISTS idx_o_id_c_clients_owner_id`,
		} {
			if err := tx.Exec(ddl).Error; err != nil {
				return fmt.Errorf("database index migration: %w", err)
			}
		}
		return nil
	})
}

func currentDatabaseSchema(db *gorm.DB) (string, error) {
	var schema string
	if err := db.Raw(`SELECT current_schema()`).Scan(&schema).Error; err != nil {
		return "", err
	}
	if schema == "" {
		return "", fmt.Errorf("database operation requires a current schema")
	}
	return pq.QuoteIdentifier(schema), nil
}
