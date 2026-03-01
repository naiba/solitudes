import { test } from '@playwright/test';

const BASE = 'http://localhost:8080';

async function seedData(page: any) {
  // Login
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

  // Post Article 1 (Hero)
  await page.goto(BASE + '/admin/publish');
  await page.waitForTimeout(1000);
  await page.evaluate(async () => {
    const form = new URLSearchParams();
    form.append('title', 'The Art of Minimalist Web Design');
    form.append('slug', 'minimalist-web-design');
    form.append('content', 'In an era of digital noise, minimalism is not just an aesthetic choice, but a functional necessity. By stripping away the non-essential, we allow the core message to breathe and resonate with the reader. This article explores the principles of whitespace, typography, and visual hierarchy.');
    form.append('tags', 'design,web');
    form.append('template', '1');
    await fetch('/admin/publish', { method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body: form.toString() });
  });
  await page.waitForTimeout(1000);

  // Post Article 2 (Recent list)
  await page.goto(BASE + '/admin/publish');
  await page.waitForTimeout(1000);
  await page.evaluate(async () => {
    const form = new URLSearchParams();
    form.append('title', 'Go 1.24 Release Notes Overview');
    form.append('slug', 'go-1-24-release');
    form.append('content', 'Go 1.24 introduces several highly anticipated features, including improved loop semantics and enhanced generic type inference. Here is a quick breakdown of what you need to know before upgrading your production services.');
    form.append('tags', 'golang,programming');
    form.append('template', '1');
    await fetch('/admin/publish', { method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body: form.toString() });
  });
  await page.waitForTimeout(1000);

  // Post Topic (Bibi)
  await page.goto(BASE + '/admin/publish');
  await page.waitForTimeout(1000);
  await page.evaluate(async () => {
    const form = new URLSearchParams();
    form.append('title', '');
    form.append('slug', 'server-update-topic');
    form.append('content', 'Just migrated the database to PostgreSQL 16. The performance improvements are noticeable right out of the box, especially for complex joins in the tagging system. #devops');
    form.append('tags', 'Topic');
    form.append('template', '1');
    await fetch('/admin/publish', { method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body: form.toString() });
  });
  await page.waitForTimeout(1000);

  // Visit articles to increase read count (for mostRead section)
  await page.goto(BASE + '/minimalist-web-design');
  await page.waitForTimeout(500);
  await page.goto(BASE + '/go-1-24-release');
  await page.waitForTimeout(500);
  
  // Create a comment on the Topic
  await page.goto(BASE + '/server-update-topic');
  await page.waitForTimeout(1000);
  await page.fill('input[name="nickname"]', 'Fan');
  await page.fill('input[name="email"]', 'fan@example.com');
  await page.fill('textarea[name="content"]', 'Nice update! Is it faster now?');
  await page.click('button:has-text("Submit"), button[type="submit"]');
  await page.waitForTimeout(1000);
}

// Ensure clean DB
test.beforeAll(async () => {
  const { execSync } = require('child_process');
  try {
    execSync('su - postgres -c "psql solitudes -c \\"DELETE FROM comments; DELETE FROM article_histories; DELETE FROM articles;\\""');
  } catch(e) {}
});

test('Screenshot site theme - folio', async ({ page }) => {
  await seedData(page);
  await page.setViewportSize({ width: 1920, height: 1080 });
  await page.goto(BASE + '/');
  await page.evaluate(() => { document.body.style.overflow = 'hidden'; });
  await page.waitForTimeout(2000);
  await page.screenshot({ path: '/root/solitudes/resource/themes/site/folio/screenshot.png', fullPage: false });
});

test('Screenshot site theme - astro-paper', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 });
  
  // Switch theme via DB directly to avoid UI interactions
  const { execSync } = require('child_process');
  execSync('sed -i "s/theme: folio/theme: astro-paper/g" /root/solitudes/data/conf.yml');
  // Wait for the hot-reload or just trigger it via API? Actually we need to restart server.
});

// Since changing theme requires server restart, we do it in a separate bash script
