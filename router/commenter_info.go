package router

import (
	"net/http"
	"time"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

type commenterInfoResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data"`
	Message string      `json:"message,omitempty"`
}

type commenterInfo struct {
	Nickname string `json:"nickname"`
	Website  string `json:"website"`
}

func commenterInfoHandler(writer http.ResponseWriter, httpRequest *http.Request) error {
	nickname := queryValue(httpRequest, "nickname")
	email := queryValue(httpRequest, "email")
	website := queryValue(httpRequest, "website")

	if nickname == "" && email == "" && website == "" {
		return writeJSON(writer, http.StatusOK, commenterInfoResponse{
			Success: false,
			Message: "at least one field is required",
		})
	}

	if nickname != "" && email != "" && website != "" {
		return writeJSON(writer, http.StatusOK, commenterInfoResponse{
			Success: false,
			Message: "all fields are filled",
		})
	}

	oneYearAgo := time.Now().AddDate(-1, 0, 0)

	if email != "" {
		var comment model.Comment
		query := solitudes.System.DB.
			Select("nickname, email, website").
			Where("is_spam = ? AND email = ? AND created_at >= ?", false, email, oneYearAgo)

		if nickname != "" {
			query = query.Where("nickname = ?", nickname)
		} else if website != "" {
			query = query.Where("website = ?", website)
		}

		err := query.Order("created_at DESC").First(&comment).Error

		if err != nil {
			return writeJSON(writer, http.StatusOK, commenterInfoResponse{
				Success: false,
				Message: "no matching comment found",
			})
		}

		return writeJSON(writer, http.StatusOK, commenterInfoResponse{
			Success: true,
			Data: commenterInfo{
				Nickname: comment.Nickname,
				Website:  comment.Website,
			},
		})
	}

	if website != "" {
		var comment model.Comment
		query := solitudes.System.DB.
			Select("nickname, email, website").
			Where("is_spam = ? AND website = ? AND created_at >= ?", false, website, oneYearAgo)

		if nickname != "" {
			query = query.Where("nickname = ?", nickname)
		}

		err := query.Order("created_at DESC").First(&comment).Error

		if err != nil {
			return writeJSON(writer, http.StatusOK, commenterInfoResponse{
				Success: false,
				Message: "no matching comment found",
			})
		}

		return writeJSON(writer, http.StatusOK, commenterInfoResponse{
			Success: true,
			Data: commenterInfo{
				Nickname: comment.Nickname,
				Website:  comment.Website,
			},
		})
	}

	if nickname != "" {
		var comments []model.Comment
		err := solitudes.System.DB.
			Select("nickname, email, website").
			Where("is_spam = ? AND nickname = ? AND created_at >= ?", false, nickname, oneYearAgo).
			Order("created_at DESC").
			Limit(10).
			Find(&comments).Error

		if err != nil {
			return writeJSON(writer, http.StatusOK, commenterInfoResponse{
				Success: false,
				Message: "database error",
			})
		}

		if len(comments) == 0 {
			return writeJSON(writer, http.StatusOK, commenterInfoResponse{
				Success: false,
				Message: "no comments found for this nickname",
			})
		}

		if len(comments) == 1 {
			return writeJSON(writer, http.StatusOK, commenterInfoResponse{
				Success: true,
				Data: commenterInfo{
					Nickname: comments[0].Nickname,
					Website:  comments[0].Website,
				},
			})
		}

		firstEmail := comments[0].Email
		firstWebsite := comments[0].Website
		allSame := true

		for _, cm := range comments {
			if cm.Email != firstEmail || cm.Website != firstWebsite {
				allSame = false
				break
			}
		}

		if allSame {
			return writeJSON(writer, http.StatusOK, commenterInfoResponse{
				Success: true,
				Data: commenterInfo{
					Nickname: comments[0].Nickname,
					Website:  comments[0].Website,
				},
			})
		}

		return writeJSON(writer, http.StatusOK, commenterInfoResponse{
			Success: false,
			Message: "multiple commenters found, please provide email",
		})
	}

	return writeJSON(writer, http.StatusOK, commenterInfoResponse{
		Success: false,
		Message: "invalid request",
	})
}
