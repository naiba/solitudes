package router

import (
	"github.com/gofiber/fiber/v2"
	"github.com/mojocn/base64Captcha"
)

var captchaStore = base64Captcha.DefaultMemStore

// generateCaptcha generates a new captcha image and ID
func generateCaptcha(c *fiber.Ctx) error {
	// Create digit captcha config
	driver := base64Captcha.NewDriverDigit(80, 240, 5, 0.7, 80)
	captcha := base64Captcha.NewCaptcha(driver, captchaStore)
	
	id, b64s, _, err := captcha.Generate()
	if err != nil {
		return err
	}
	
	return c.JSON(fiber.Map{
		"captchaId":    id,
		"captchaImage": b64s,
	})
}

// verifyCaptcha verifies the captcha answer
func verifyCaptcha(id, answer string) bool {
	return captchaStore.Verify(id, answer, true)
}
