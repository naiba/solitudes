import { test, expect } from '@playwright/test';
import { readFileSync } from 'node:fs';
import path from 'node:path';

for (const theme of ['cactus', 'folio']) {
  for (const width of [1280, 390]) {
    test(`${theme} image captions and viewer at ${width}px`, async ({ page }) => {
      await page.setViewportSize({ width, height: 900 });
      const root = path.resolve(import.meta.dirname, '../resource/themes/site', theme);
      const tools = readFileSync(path.join(root, 'templates/reading_tools.html'), 'utf8');
      // Fail explicitly if the production template changes instead of silently testing an empty dialog.
      const dialogTemplate = tools.match(/{{define "site\/image_dialog"}}([\s\S]*?){{end}}/)?.[1];
      if (!dialogTemplate) throw new Error('Missing image dialog template');
      const dialog = dialogTemplate.replace(/{{\.Tr\.T "([^"]+)"}}/g, '$1');
      await page.route('https://images.example/**', route => route.fulfill({ contentType: 'image/svg+xml', body: '<svg xmlns="http://www.w3.org/2000/svg" width="1800" height="1200"><rect width="1800" height="1200" fill="teal"/></svg>' }));
      await page.setContent(`<style>body{margin:20px} [data-reading-content]{max-width:700px;margin:auto} :root{--reader-bg:white;--reader-text:black;--reader-border:gray;--reader-accent:teal}</style>
        <div data-reading-content><figure class="article-image"><img src="https://images.example/photo.svg" alt="Accessible description"><figcaption>Visible caption &amp; source</figcaption></figure>
        <figure class="article-image"><a href="https://images.example/source"><img src="https://images.example/linked.svg" alt="Linked image"></a></figure>
        <figure class="article-image"><img src="https://images.example/uncaptioned.svg" alt="Only alt"></figure></div>${dialog}`);
      for (const css of [theme === 'cactus' ? 'main.css' : 'style.css', 'reading.css']) await page.addStyleTag({ path: path.join(root, 'static/css', css) });
      await page.addScriptTag({ path: path.join(root, 'static/js/reading.js') });
      const image = page.locator('.article-image img').first();
      await expect.poll(() => image.evaluate((img: HTMLImageElement) => img.naturalWidth)).toBe(1800);
      const contentWidth = await page.locator('[data-reading-content]').evaluate(el => el.getBoundingClientRect().width);
      const imageWidth = (await image.boundingBox())!.width;
      expect(imageWidth / contentWidth).toBeCloseTo(width === 390 ? 1 : .88, 2);
      await expect(page.locator('figcaption')).toHaveCount(1);
      await expect(page.locator('figcaption')).toHaveText('Visible caption & source');
      await expect(page.locator('.article-image-open')).toHaveCount(2);
      await expect(page.locator('figure a')).toHaveAttribute('href', 'https://images.example/source');
      const opener = page.locator('.article-image-open').first();
      await opener.focus();
      await page.keyboard.press('Enter');
      const viewer = page.locator('#article-image-dialog');
      await expect(viewer).toBeVisible();
      await expect(page.locator('[data-image-caption]')).toHaveText('Visible caption & source');
      await expect(page.locator('[data-image-full]')).toHaveAttribute('src', 'https://images.example/photo.svg');
      await expect(page.locator('[data-image-full]')).toHaveAttribute('alt', 'Accessible description');
      await expect(page.locator('[data-image-close]')).toBeFocused();
      await page.keyboard.press('Tab');
      await expect(page.locator('[data-image-original]')).toBeFocused();
      await page.locator('[data-image-zoom]').click();
      await expect(page.locator('[data-image-zoom]')).toHaveAttribute('aria-pressed', 'true');
      await expect.poll(() => page.locator('[data-image-full]').evaluate(el => el.getBoundingClientRect().width)).toBe(1800);
      await page.keyboard.press('Escape');
      await expect(viewer).not.toBeVisible();
      await expect(opener).toBeFocused();
      // Native dialog dismissal restores focus before its queued close event
      // runs our scroll-lock cleanup; visibility alone is not completion.
      await expect.poll(() => page.evaluate(() => document.body.style.overflow)).toBe('');
      await expect(page.locator('[data-image-full]')).not.toHaveAttribute('src');
      await page.locator('.article-image-open').last().click();
      await expect(page.locator('[data-image-caption]')).toBeHidden();
      await page.locator('[data-image-close]').click();
      await expect(viewer).not.toBeVisible();
      await opener.click();
      await page.mouse.click(1, 1);
      await expect(viewer).not.toBeVisible();
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    });
  }
}
