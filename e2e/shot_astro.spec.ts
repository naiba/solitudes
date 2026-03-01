import { test } from '@playwright/test';
const BASE = 'http://localhost:8080';

async function seedData(page: any) {
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

  await page.goto(BASE + '/admin/publish');
  await page.waitForTimeout(1000);
  await page.evaluate(async () => {
    const form = new URLSearchParams();
    form.append('title', 'Welcome to the Dual Column Setup');
    form.append('slug', 'dual-column-setup');
    form.append('content', 'This is a test of the new Astro-Paper layout.');
    form.append('tags', 'design,web');
    form.append('template', '1');
    await fetch('/admin/publish', { method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body: form.toString() });
  });
  await page.waitForTimeout(1000);

  await page.goto(BASE + '/admin/publish');
  await page.waitForTimeout(1000);
  await page.evaluate(async () => {
    const form = new URLSearchParams();
    form.append('title', 'A second article to pad out the main column');
    form.append('slug', 'second-article');
    form.append('content', 'Content goes here.');
    form.append('tags', 'tech');
    form.append('template', '1');
    await fetch('/admin/publish', { method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body: form.toString() });
  });
  await page.waitForTimeout(1000);

  await page.goto(BASE + '/admin/publish');
  await page.waitForTimeout(1000);
  await page.evaluate(async () => {
    const form = new URLSearchParams();
    form.append('title', '');
    form.append('slug', 'a-new-topic');
    form.append('content', 'A quick thought about the new UI update! #Topic');
    form.append('tags', 'Topic');
    form.append('template', '1');
    await fetch('/admin/publish', { method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body: form.toString() });
  });
  await page.waitForTimeout(1000);

  // Visit to bump read count
  await page.goto(BASE + '/dual-column-setup');
  await page.waitForTimeout(500);
}

test('Shot Astro', async ({ page }) => {
  await seedData(page);
  await page.setViewportSize({ width: 1920, height: 1080 });
  await page.goto(BASE + '/');
  await page.evaluate(() => { document.body.style.overflow = 'hidden'; });
  await page.waitForTimeout(2000);
  await page.screenshot({ path: '/root/solitudes/resource/themes/site/astro-paper/screenshot.png', fullPage: false });
});
