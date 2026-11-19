import { expect, test } from '@playwright/test';
import path from 'node:path';

// Opt-in capture driven by TestCaptureSiteThemeScreenshots, which serves a
// showcase publication on an isolated database. The regular suite never
// rewrites theme assets.
const siteTheme = process.env.E2E_SITE_THEME;

test.use({ viewport: { width: 1440, height: 900 }, colorScheme: 'light', locale: 'en-US' });

test('site theme screenshot shows the current home page', async ({ page }) => {
  test.skip(process.env.SOLITUDES_CAPTURE_SITE_THEME !== '1' || !siteTheme, 'Only rewrites screenshots on request.');
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  expect((await page.goto('/', { waitUntil: 'networkidle' }))?.status()).toBe(200);
  await page.evaluate(async () => { await document.fonts.ready; });
  await expect(page.locator('#main-content')).toBeVisible();
  await expect(page.getByTestId('reader-circle-home')).toBeVisible();
  await expect(page.locator(`link[href*="/static/site/${siteTheme}/"]`).first()).toHaveCount(1);
  // A preview with missing icons or a horizontal scrollbar misrepresents the theme.
  await expect.poll(() => page.evaluate(() => document.fonts.check('900 1em "Font Awesome 6 Free"'))).toBe(true);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(1440);
  expect(errors).toEqual([]);
  // The admin theme screenshot is 1440×900 as well, so the picker shows equal cards.
  await page.screenshot({
    path: path.resolve(`../resource/themes/site/${siteTheme}/screenshot.png`),
    animations: 'disabled',
  });
});
