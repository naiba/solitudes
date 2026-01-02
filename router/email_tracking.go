package router

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

// 1x1 transparent GIF pixel
var trackingPixel = []byte{
	0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00,
	0x01, 0x00, 0x80, 0x00, 0x00, 0xFF, 0xFF, 0xFF,
	0x00, 0x00, 0x00, 0x21, 0xF9, 0x04, 0x01, 0x00,
	0x00, 0x00, 0x00, 0x2C, 0x00, 0x00, 0x00, 0x00,
	0x01, 0x00, 0x01, 0x00, 0x00, 0x02, 0x02, 0x44,
	0x01, 0x00, 0x3B,
}

// trackEmailRead handles email read tracking via pixel tracking
// Updates status only if token is valid
func trackEmailRead(c *fiber.Ctx) error {
	// Extract token from URL parameter (format: /static/i/{token}.gif)
	fullToken := c.Params("token")
	// Remove .gif extension if present
	trackingToken := strings.TrimSuffix(fullToken, ".gif")

	if trackingToken == "" {
		// Still return pixel for privacy
		c.Set("Content-Type", "image/gif")
		c.Set("Cache-Control", "no-cache, no-store, must-revalidate")
		c.Set("Pragma", "no-cache")
		c.Set("Expires", "0")
		return c.Send(trackingPixel)
	}

	// Query comment by token (token is unique in database)
	var comment model.Comment
	err := solitudes.System.DB.Take(&comment, "email_tracking_token = ?", trackingToken).Error
	if err == nil {
		// Token found and valid, update status
		readStatus := "read"
		_ = solitudes.System.DB.Model(&model.Comment{}).
			Where("email_tracking_token = ? AND (email_read_status = ? OR email_read_status IS NULL)", trackingToken, "unread").
			Update("email_read_status", readStatus).Error
	}

	// Always return the tracking pixel, even if token is invalid
	// We don't want to fail the email display
	c.Set("Content-Type", "image/gif")
	c.Set("Cache-Control", "no-cache, no-store, must-revalidate")
	c.Set("Pragma", "no-cache")
	c.Set("Expires", "0")
	return c.Send(trackingPixel)
}

// trackEmailReadRedirect handles email read tracking via link redirect
// This is more reliable than pixel tracking as it doesn't depend on image loading
func trackEmailReadRedirect(c *fiber.Ctx) error {
	// Extract tracking token from URL
	trackingToken := c.Params("token")

	if trackingToken == "" {
		return c.Redirect("/", fiber.StatusFound)
	}

	// Query comment by token and load related article
	var comment model.Comment
	err := solitudes.System.DB.Preload("Article").Take(&comment, "email_tracking_token = ?", trackingToken).Error
	if err != nil || comment.Article == nil {
		// Invalid token or article not found, redirect to home
		return c.Redirect("/", fiber.StatusFound)
	}

	// Token is valid, update comment email read status to "read"
	readStatus := "read"
	_ = solitudes.System.DB.Model(&model.Comment{}).
		Where("email_tracking_token = ? AND (email_read_status = ? OR email_read_status IS NULL)", trackingToken, "unread").
		Update("email_read_status", readStatus).Error

	// Redirect to the comment's article
	// Use 302 (Found) instead of 301 (Permanent) to allow tracking on each visit
	return c.Redirect("/"+comment.Article.Slug, fiber.StatusFound)
}
