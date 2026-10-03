package model

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

const maintenanceBatchSize = 1000
const maintenanceMaxBatches = 20

// LoginFactsSQL combines disjoint live details and compacted history. Consumers
// must SUM(login_count), not COUNT(*); the union is read with one MVCC snapshot
// so compaction cannot transiently duplicate or lose totals.
const LoginFactsSQL = `(SELECT action, client_id, actor_id, 1::bigint AS login_count, created_at AS last_login_at
 FROM audit_events WHERE outcome = 'success' AND action IN ('oidc.login', 'session.login')
 UNION ALL
 SELECT action, client_id, actor_id, login_count, last_login_at FROM login_summaries)`

// MaintainDatabase bounds both transaction size and work per scheduled run.
// It never removes accounts, credentials, signing keys, content, or revisions.
// auditDays=0 keeps all audit details; positive days explicitly enable deletion.
func MaintainDatabase(ctx context.Context, db *gorm.DB, now time.Time, auditDays int) (int64, error) {
	if auditDays < 0 || auditDays > 36500 {
		return 0, fmt.Errorf("invalid audit retention days")
	}
	db = db.WithContext(ctx)
	schema, err := currentDatabaseSchema(db)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, table := range []string{"login_sessions", "email_actions", "o_auth_attempts", "passkey_ceremonies", "o_id_c_auth_requests", "o_id_c_refresh_tokens", "o_id_c_access_tokens", "feed_visits"} {
		column, cutoff := "expires_at", now
		if table == "feed_visits" {
			column, cutoff = "created_at", now.AddDate(0, 0, -7)
		}
		for batch := 0; batch < maintenanceMaxBatches; batch++ {
			qualified := schema + "." + table // Table names are a fixed allowlist.
			result := db.Exec("WITH expired AS (SELECT id FROM "+qualified+" WHERE "+column+" <= ? ORDER BY "+column+", id LIMIT ? FOR UPDATE SKIP LOCKED) DELETE FROM "+qualified+" AS t USING expired WHERE t.id = expired.id", cutoff, maintenanceBatchSize)
			if result.Error != nil {
				return total, fmt.Errorf("clean %s: %w", table, result.Error)
			}
			total += result.RowsAffected
			if result.RowsAffected < maintenanceBatchSize {
				break
			}
		}
	}
	if auditDays == 0 {
		return total, nil
	}
	for batch := 0; batch < maintenanceMaxBatches; batch++ {
		n, err := CompactAuditBatch(db, now.AddDate(0, 0, -auditDays))
		if err != nil {
			return total, err
		}
		total += n
		if n < maintenanceBatchSize {
			break
		}
	}
	return total, nil
}

// CompactAuditBatch atomically removes a bounded batch and transfers successful
// login facts to a compact ledger. Failed insertion rolls the deletion back.
// Concurrent workers skip locked details and serialize conflicting summary rows.
func CompactAuditBatch(db *gorm.DB, cutoff time.Time) (int64, error) {
	schema, err := currentDatabaseSchema(db)
	if err != nil {
		return 0, err
	}
	var count int64
	err = db.Raw(`WITH expired AS (
 SELECT id FROM `+schema+`.audit_events WHERE created_at < ? AND id <> 'audit-initialized'
 ORDER BY created_at, id LIMIT ? FOR UPDATE SKIP LOCKED
), removed AS (
 DELETE FROM `+schema+`.audit_events AS e USING expired WHERE e.id = expired.id RETURNING e.*
), summarized AS (
 INSERT INTO `+schema+`.login_summaries (action, client_id, actor_id, login_count, last_login_at)
 SELECT action, COALESCE(client_id, ''), COALESCE(actor_id, ''), COUNT(*), MAX(created_at) FROM removed
 WHERE outcome = 'success' AND action IN ('session.login', 'oidc.login')
 GROUP BY action, COALESCE(client_id, ''), COALESCE(actor_id, '')
 ORDER BY action, COALESCE(client_id, ''), COALESCE(actor_id, '')
 ON CONFLICT (action, client_id, actor_id) DO UPDATE
 SET login_count = login_summaries.login_count + EXCLUDED.login_count,
 last_login_at = GREATEST(login_summaries.last_login_at, EXCLUDED.last_login_at)
 RETURNING 1
) SELECT COUNT(*) FROM removed`, cutoff, maintenanceBatchSize).Scan(&count).Error
	if err != nil {
		return 0, fmt.Errorf("compact audit events: %w", err)
	}
	return count, nil
}
