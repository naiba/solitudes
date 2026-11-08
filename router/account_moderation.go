package router

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const disabledAccountNickname = "已封禁用户"

func updateUserNickname(w http.ResponseWriter, r *http.Request) error {
	nickname := strings.TrimSpace(formValue(r, "nickname"))
	if nickname == "" || utf8.RuneCountInString(nickname) > 80 || strings.ContainsAny(nickname, "\r\n\x00") {
		return newHTTPError(http.StatusBadRequest, "invalid nickname")
	}
	return moderateAccount(w, r, false, nickname)
}

func disableUser(w http.ResponseWriter, r *http.Request) error {
	if formValue(r, "confirm") != "disable" {
		return newHTTPError(http.StatusBadRequest, "confirmation required")
	}
	return moderateAccount(w, r, true, disabledAccountNickname)
}

func moderateAccount(w http.ResponseWriter, r *http.Request, disable bool, nickname string) error {
	actor := currentAccount(w, r)
	id := routeParam(r, "id")
	if actor == nil {
		return newHTTPError(http.StatusForbidden)
	}
	if disable && actor.ID == id {
		return newHTTPError(http.StatusForbidden, "cannot disable yourself")
	}
	err := solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		// Lock both accounts in stable order: concurrent administrators must not ban
		// each other using permission snapshots taken before either ban committed.
		var accounts []model.Account
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", []string{actor.ID, id}).Order("id").Find(&accounts).Error; err != nil {
			return err
		}
		var owner, target *model.Account
		for i := range accounts {
			if accounts[i].ID == actor.ID {
				owner = &accounts[i]
			}
			if accounts[i].ID == id {
				target = &accounts[i]
			}
		}
		if owner == nil || owner.DisabledAt != nil || owner.Role != model.RoleAdmin {
			return newHTTPError(http.StatusForbidden)
		}
		if target == nil {
			return newHTTPError(http.StatusNotFound)
		}
		if target.DisabledAt != nil {
			if disable {
				return nil
			}
			return newHTTPError(http.StatusConflict, "disabled account nickname cannot be changed")
		}
		if !disable && target.Nickname == nickname {
			return nil
		}
		updates := map[string]interface{}{"nickname": nickname}
		action := "user.nickname.update"
		if disable {
			updates["disabled_at"] = time.Now()
			action = "user.disable"
		}
		if err := tx.Model(target).Updates(updates).Error; err != nil {
			return err
		}
		if disable {
			// Delete persisted credentials as well as rejecting disabled accounts so old
			// cookies and grants cannot become usable again after a future reinstatement.
			for _, entity := range []interface{}{&model.LoginSession{}, &model.OIDCAuthRequest{}, &model.OIDCAccessToken{}, &model.OIDCRefreshToken{}} {
				if err := tx.Where("account_id = ?", id).Delete(entity).Error; err != nil {
					return err
				}
			}
		}
		return auditMutation(w, r, tx, action, id, "", "")
	})
	if err != nil {
		return err
	}
	requestValue(r, "audit_recorded", true)
	return redirectResponse(w, r, "/admin/users/"+id, http.StatusSeeOther)
}
