import { test } from '@playwright/test';
const BASE = 'http://localhost:8080';

test('Shot Admin', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 });
  await page.goto(BASE + '/admin/login');
  await page.waitForTimeout(1000);
  await page.evaluate(() => {
    const s = document.createElement('style');
    s.textContent = '*, *::before, *::after { animation: none !important; transition: none !important; }';
    document.head.appendChild(s);
  });
  await page.fill('input[name="email"]', 'hi@example.com');
  await page.fill('input[name="password"]', '123456');
  await page.fill('input[name="captcha"]', '0');
  await page.click('button[type="submit"]');
  await page.waitForURL(/\/admin/, { timeout: 5000 });

  await page.goto(BASE + '/admin');
  await page.evaluate(() => { document.body.style.overflow = 'hidden'; });
  await page.waitForTimeout(2000);
  await page.screenshot({ path: '/root/solitudes/resource/themes/admin/glacie/screenshot.png', fullPage: false });
});
