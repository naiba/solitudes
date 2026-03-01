import { test } from '@playwright/test';
const BASE = 'http://localhost:8080';
test('Shot Folio', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 });
  await page.goto(BASE + '/');
  await page.evaluate(() => { document.body.style.overflow = 'hidden'; });
  await page.waitForTimeout(2000);
  await page.screenshot({ path: '/root/solitudes/resource/themes/site/folio/screenshot.png', fullPage: false });
});
