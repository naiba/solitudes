package router

import (
	"github.com/gofiber/fiber/v2"
	"github.com/mojocn/base64Captcha"
)

var captchaStore = base64Captcha.DefaultMemStore

// generateCaptcha generates a new captcha image and ID
func generateCaptcha(c *fiber.Ctx) error {
	// Create math captcha config (more secure than digits)
	// Parameters: height=80, width=240, noiseCount=5, showLineOptions=hollow|slime|sine, use default bg color, fonts, and fonts storage
	driver := base64Captcha.NewDriverMath(80, 240, 5, 2|4|8, nil, nil, nil)
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
