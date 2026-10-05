package router

import (
	"time"

	"gorm.io/gorm"

	"github.com/naiba/solitudes/internal/model"
)

type oidcClientUsage struct {
	ID          string
	LoginUsers  int64
	ActiveUsers int64
}

// Aggregate only the owner's current page. Existing client/account indexes
// and login summaries provide these counts without another statistics table.
func ownedClientUsage(db *gorm.DB, ownerID string, ids []string, now time.Time) (map[string]oidcClientUsage, error) {
	result := make(map[string]oidcClientUsage, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	var rows []oidcClientUsage
	err := db.Raw(`SELECT c.id,
 (SELECT COUNT(DISTINCT actor_id) FROM `+model.LoginFactsSQL+` AS logins
  WHERE action = 'oidc.login' AND client_id = c.id AND actor_id <> '') AS login_users,
 CASE WHEN c.disabled_at IS NOT NULL THEN 0 ELSE (
  SELECT COUNT(DISTINCT credentials.account_id) FROM (
   SELECT account_id FROM o_id_c_access_tokens WHERE client_id = c.id AND expires_at > ?
   UNION ALL
   SELECT account_id FROM o_id_c_refresh_tokens WHERE client_id = c.id AND expires_at > ?
  ) AS credentials JOIN accounts a ON a.id = credentials.account_id
  WHERE a.disabled_at IS NULL AND a.email_verified_at IS NOT NULL
 ) END AS active_users
 FROM o_id_c_clients c WHERE c.owner_id = ? AND c.id IN ?`, now, now, ownerID, ids).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.ID] = row
	}
	return result, nil
}
