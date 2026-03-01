#!/bin/bash
set -e

echo "=== Taking screenshot for Folio ==="
sed -i 's/theme: astro-paper/theme: folio/g' /root/solitudes/data/conf.yml
pkill -f "go run cmd/web" || true
sleep 2
SOLITUDES_E2E=1 nohup go run cmd/web/main.go > /tmp/solitudes.log 2>&1 &
sleep 6

cat << 'EOF' > /root/solitudes/e2e/shot_folio.spec.ts
import { test } from '@playwright/test';
const BASE = 'http://localhost:8080';

// See previous seed logic
test('Shot Folio', async ({ page }) => {
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

  // Post Topic
  await page.goto(BASE + '/admin/publish');
  await page.waitForTimeout(1000);
  await page.evaluate(async () => {
    const form = new URLSearchParams();
    form.append('title', '');
    form.append('slug', 'a-new-topic');
    form.append('content', 'Just updated the blog themes to support 2-column layouts and better topic views. What do you guys think?');
    form.append('tags', 'Topic');
    form.append('template', '1');
    await fetch('/admin/publish', { method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body: form.toString() });
  });
  await page.waitForTimeout(1000);

  // Visit to bump read count
  await page.goto(BASE + '/a-new-topic');
  await page.waitForTimeout(500);

  // Take screenshot
  await page.setViewportSize({ width: 1920, height: 1080 });
  await page.goto(BASE + '/');
  await page.evaluate(() => { document.body.style.overflow = 'hidden'; });
  await page.waitForTimeout(2000);
  await page.screenshot({ path: '/root/solitudes/resource/themes/site/folio/screenshot.png', fullPage: false });
});
EOF

cd /root/solitudes/e2e
bunx playwright test shot_folio.spec.ts

echo "=== Taking screenshot for Astro-Paper ==="
sed -i 's/theme: folio/theme: astro-paper/g' /root/solitudes/data/conf.yml
pkill -f "go run cmd/web" || true
sleep 2
SOLITUDES_E2E=1 nohup go run cmd/web/main.go > /tmp/solitudes.log 2>&1 &
sleep 6

cat << 'EOF' > /root/solitudes/e2e/shot_astro.spec.ts
import { test } from '@playwright/test';
const BASE = 'http://localhost:8080';

test('Shot Astro', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 });
  await page.goto(BASE + '/');
  await page.evaluate(() => { document.body.style.overflow = 'hidden'; });
  await page.waitForTimeout(2000);
  await page.screenshot({ path: '/root/solitudes/resource/themes/site/astro-paper/screenshot.png', fullPage: false });
});
EOF

bunx playwright test shot_astro.spec.ts

echo "=== Taking screenshot for Glacie Admin ==="
cat << 'EOF' > /root/solitudes/e2e/shot_admin.spec.ts
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
EOF

bunx playwright test shot_admin.spec.ts
