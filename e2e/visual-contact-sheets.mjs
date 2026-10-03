import { chromium } from '@playwright/test';
import { readFileSync, readdirSync, existsSync } from 'node:fs';
import path from 'node:path';

const root = process.env.SOLITUDES_VISUAL_DIR;
if (!root || !existsSync(root)) throw new Error('Set SOLITUDES_VISUAL_DIR to a generated visual-audit directory');

const browser = await chromium.launch({ executablePath: process.env.E2E_CHROMIUM_EXECUTABLE || undefined });
try {
  for (const pair of readdirSync(root)) {
    const directory = path.join(root, pair);
    if (!existsSync(path.join(directory, 'manifest.json'))) continue;
    const items = JSON.parse(readFileSync(path.join(directory, 'manifest.json'), 'utf8'));
    for (const viewport of ['desktop', 'mobile']) {
      for (const section of ['site', 'admin']) {
        const entries = items.filter(item => item.viewport === viewport &&
          (section === 'site' ? item.role.includes('site') : !item.role.includes('site')));
        if (!entries.length) continue;
        const page = await browser.newPage({ viewport: { width: 1420, height: 900 }, deviceScaleFactor: 1 });
        const cells = entries.map(item => {
          const data = readFileSync(item.image).toString('base64');
          return `<figure><div><img src="data:image/jpeg;base64,${data}"></div><figcaption>${item.role} / ${item.page} (${item.status})</figcaption></figure>`;
        }).join('');
        await page.setContent(`<html><head><style>
          * { box-sizing: border-box } body { font: 15px system-ui; margin: 12px; color: #222; background: #eef1f5 }
          h1 { font-size: 22px } section { display: grid; grid-template-columns: repeat(4, 1fr); gap: 12px }
          figure { margin: 0; padding: 6px; background: white; border: 1px solid #ccd1d8; border-radius: 8px; overflow: hidden }
          figure div { height: 228px; overflow: hidden; background: #eee }
          img { display: block; width: 100%; height: 228px; object-fit: cover; object-position: top }
          figcaption { font-size: 12px; line-height: 1.4; padding: 6px 2px; overflow-wrap: anywhere }
        </style></head><body><h1>${pair}: ${viewport} ${section} (${entries.length} pages)</h1><section>${cells}</section></body></html>`);
        await page.locator('img').evaluateAll(images => Promise.all(images.map(img => img.complete ? Promise.resolve() : new Promise(resolve => {
          img.onload = resolve; img.onerror = resolve;
        }))));
        const target = path.join(directory, `contact-${viewport}-${section}.jpg`);
        await page.screenshot({ path: target, type: 'jpeg', quality: 75, fullPage: true });
        console.log(target);
        await page.close();
      }
    }
  }
} finally {
  await browser.close();
}
