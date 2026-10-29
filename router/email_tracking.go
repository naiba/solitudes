package router

import (
	"net/http"
	"strings"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/siteurl"
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
func trackEmailRead(writer http.ResponseWriter, httpRequest *http.Request) error {
	// Extract token from URL parameter (format: /static/i/{token}.gif)
	fullToken := routeParam(httpRequest, "token")
	// Remove .gif extension if present
	trackingToken := strings.TrimSuffix(fullToken, ".gif")

	if trackingToken == "" {
		// Still return pixel for privacy
		writer.Header().Set("Content-Type", "image/gif")
		writer.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		writer.Header().Set("Pragma", "no-cache")
		writer.Header().Set("Expires", "0")
		return writeBytes(writer, http.StatusOK, trackingPixel)
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
	writer.Header().Set("Content-Type", "image/gif")
	writer.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	writer.Header().Set("Pragma", "no-cache")
	writer.Header().Set("Expires", "0")
	return writeBytes(writer, http.StatusOK, trackingPixel)
}

// trackEmailReadRedirect handles email read tracking via link redirect
// This is more reliable than pixel tracking as it doesn't depend on image loading
func trackEmailReadRedirect(writer http.ResponseWriter, httpRequest *http.Request) error {
	// Extract tracking token from URL
	trackingToken := routeParam(httpRequest, "token")

	if trackingToken == "" {
		return redirectResponse(writer, httpRequest, "/", http.StatusFound)
	}

	// Query comment by token and load related article
	var comment model.Comment
	err := solitudes.System.DB.Preload("Article").Take(&comment, "email_tracking_token = ?", trackingToken).Error
	if err != nil || comment.Article == nil {
		// Invalid token or article not found, redirect to home
		return redirectResponse(writer, httpRequest, "/", http.StatusFound)
	}

	// Token is valid, update comment email read status to "read"
	readStatus := "read"
	_ = solitudes.System.DB.Model(&model.Comment{}).
		Where("email_tracking_token = ? AND (email_read_status = ? OR email_read_status IS NULL)", trackingToken, "unread").
		Update("email_read_status", readStatus).Error

	// Redirect to the comment's article
	// Use 302 (Found) instead of 301 (Permanent) to allow tracking on each visit
	return redirectResponse(writer, httpRequest, siteurl.ArticlePath(comment.Article.Slug), http.StatusFound)
}
