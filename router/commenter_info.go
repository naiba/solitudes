package router

import (
	"time"

	"github.com/gofiber/fiber/v2"
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

func commenterInfoHandler(c *fiber.Ctx) error {
	nickname := c.Query("nickname")
	email := c.Query("email")
	website := c.Query("website")

	if nickname == "" && email == "" && website == "" {
		return c.JSON(commenterInfoResponse{
			Success: false,
			Message: "at least one field is required",
		})
	}

	if nickname != "" && email != "" && website != "" {
		return c.JSON(commenterInfoResponse{
			Success: false,
			Message: "all fields are filled",
		})
	}

	oneYearAgo := time.Now().AddDate(-1, 0, 0)

	if email != "" {
		var comment model.Comment
		query := solitudes.System.DB.
			Select("nickname, email, website").
			Where("email = ? AND created_at >= ?", email, oneYearAgo)

		if nickname != "" {
			query = query.Where("nickname = ?", nickname)
		} else if website != "" {
			query = query.Where("website = ?", website)
		}

		err := query.Order("created_at DESC").First(&comment).Error

		if err != nil {
			return c.JSON(commenterInfoResponse{
				Success: false,
				Message: "no matching comment found",
			})
		}

		return c.JSON(commenterInfoResponse{
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
			Where("website = ? AND created_at >= ?", website, oneYearAgo)

		if nickname != "" {
			query = query.Where("nickname = ?", nickname)
		}

		err := query.Order("created_at DESC").First(&comment).Error

		if err != nil {
			return c.JSON(commenterInfoResponse{
				Success: false,
				Message: "no matching comment found",
			})
		}

		return c.JSON(commenterInfoResponse{
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
			Where("nickname = ? AND created_at >= ?", nickname, oneYearAgo).
			Order("created_at DESC").
			Limit(10).
			Find(&comments).Error

		if err != nil {
			return c.JSON(commenterInfoResponse{
				Success: false,
				Message: "database error",
			})
		}

		if len(comments) == 0 {
			return c.JSON(commenterInfoResponse{
				Success: false,
				Message: "no comments found for this nickname",
			})
		}

		if len(comments) == 1 {
			return c.JSON(commenterInfoResponse{
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
			return c.JSON(commenterInfoResponse{
				Success: true,
				Data: commenterInfo{
					Nickname: comments[0].Nickname,
					Website:  comments[0].Website,
				},
			})
		}

		return c.JSON(commenterInfoResponse{
			Success: false,
			Message: "multiple commenters found, please provide email",
		})
	}

	return c.JSON(commenterInfoResponse{
		Success: false,
		Message: "invalid request",
	})
}
