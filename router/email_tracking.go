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

// trackEmailRead handles email read tracking requests
func trackEmailRead(c *fiber.Ctx) error {
	// Extract comment ID from URL parameter (remove .gif extension)
	commentID := c.Params("id")
	commentID = strings.TrimSuffix(commentID, ".gif")

	if commentID == "" {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid comment ID")
	}

	// Update comment email read status to "read"
	readStatus := "read"
	_ = solitudes.System.DB.Model(&model.Comment{}).
		Where("id = ? AND email_read_status = ?", commentID, "unread").
		Update("email_read_status", readStatus).Error

	// Always return 1x1 transparent GIF, even if update fails
	// We don't want to fail the email display

	// Return 1x1 transparent GIF
	c.Set("Content-Type", "image/gif")
	c.Set("Cache-Control", "no-cache, no-store, must-revalidate")
	c.Set("Pragma", "no-cache")
	c.Set("Expires", "0")
	return c.Send(trackingPixel)
}

// trackEmailReadRedirect handles email read tracking via link redirect
// This is more reliable than pixel tracking as it doesn't depend on image loading
func trackEmailReadRedirect(c *fiber.Ctx) error {
	// Extract comment ID from URL parameter
	commentID := c.Params("id")

	if commentID == "" {
		return c.Redirect("/", fiber.StatusFound)
	}

	// Query comment and its associated article
	var comment model.Comment
	err := solitudes.System.DB.Preload("Article").Take(&comment, "id = ?", commentID).Error
	if err != nil || comment.Article == nil {
		// Invalid comment ID or article not found, redirect to home
		return c.Redirect("/", fiber.StatusFound)
	}

	// Update comment email read status to "read"
	readStatus := "read"
	_ = solitudes.System.DB.Model(&model.Comment{}).
		Where("id = ? AND (email_read_status = ? OR email_read_status IS NULL)", commentID, "unread").
		Update("email_read_status", readStatus).Error

	// Redirect to the comment's article
	// Use 302 (Found) instead of 301 (Permanent) to allow tracking on each visit
	return c.Redirect("/"+comment.Article.Slug, fiber.StatusFound)
}
