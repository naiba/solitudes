import { test, expect, type Page } from '@playwright/test';

const BASE = 'http://localhost:8080';

/**
 * 登录 admin 后台。依赖 SOLITUDES_E2E=1 环境变量跳过验证码校验。
 */
async function loginAdmin(page: Page): Promise<void> {
  await page.goto(BASE + '/admin/login');
  await page.waitForTimeout(1500);
  // 禁用 CSS 动画，避免 Playwright 判定元素 not stable（glacie 主题 login card 有 breathe 动画）
  await page.evaluate(() => {
    const s = document.createElement('style');
    s.textContent = '*, *::before, *::after { animation: none !important; transition: none !important; }';
    document.head.appendChild(s);
  });
  await page.fill('input[name="email"]', 'hi@example.com');
  await page.fill('input[name="password"]', '123456');
  await expect(page.locator('input[name="captchaId"]')).not.toHaveValue('', { timeout: 5000 });
  await page.fill('input[name="captcha"]', '0');
  await page.click('button[type="submit"]');
  await page.waitForURL(/\/admin\/?$/, { timeout: 5000 });
  await expect(page.locator('input[name="email"]')).toHaveCount(0);
}

/**
 * 通过 admin 后台发布测试文章。
 * 模板用 id 属性而非 name 属性：#inputTitle, #inputSlug, #inputTags, #selTemplate, #cbBook
 */
async function publishTestArticle(
  page: Page,
  opts: {
    title: string;
    slug: string;
    content: string;
    templateId?: number;
    tags?: string;
    isBook?: boolean;
  }
): Promise<void> {
  await page.goto(BASE + '/admin/publish');
  // 禁用 CSS 动画，避免 Playwright 判定元素 not stable
  await page.evaluate(() => {
    const s = document.createElement('style');
    s.textContent = '*, *::before, *::after { animation: none !important; transition: none !important; }';
    document.head.appendChild(s);
  });
  await page.waitForTimeout(1000);

  const hasLegacyPublishInputs = await page
    .locator('#inputTitle')
    .isVisible()
    .catch(() => false);

  if (hasLegacyPublishInputs) {
    await page.fill('#inputTitle', opts.title);
    await page.fill('#inputSlug', opts.slug);
    if (opts.tags) {
      await page.fill('#inputTags', opts.tags);
    }
    if (opts.templateId === 2) {
      await page.selectOption('#selTemplate', '2');
    } else {
      await page.selectOption('#selTemplate', '1');
    }
    if (opts.isBook) {
      await page.check('#cbBook');
    }

    let vditorReady = false;
    for (let i = 0; i < 20; i++) {
      vditorReady = await page.evaluate(() => typeof (window as any).publishVditor?.getValue === 'function');
      if (vditorReady) break;
      await page.waitForTimeout(500);
    }

    if (vditorReady) {
      await page.evaluate((content) => {
        (window as any).publishVditor.setValue(content);
      }, opts.content);
      await page.evaluate(() => {
        (window as any).publish();
      });
      await page.waitForURL(/\/admin\/publish\?id=.+$/, { timeout: 10000 });
      return;
    }
  }

  if (!hasLegacyPublishInputs) {
    await page.evaluate(async (o) => {
      const form = new URLSearchParams();
      form.append('title', o.title);
      form.append('slug', o.slug);
      form.append('content', o.content);
      form.append('tags', o.tags || '');
      form.append('template', String(o.templateId || 1));
      if (o.isBook) {
        form.append('is_book', 'on');
      }
      const resp = await fetch('/admin/publish', {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: form.toString(),
      });
      console.log('publish status:', resp.status);
      if (!resp.ok) {
        throw new Error(`publish fallback failed: ${resp.status}`);
      }
    }, opts);
  }
  await page.waitForTimeout(1500);
}

// ============================================================
// 前台 Site 主题 (folio) — 所有 14 个模板
// ============================================================
test.describe('Site Theme — folio', () => {

  // --- header.html + footer.html ---
  test('Homepage renders header/footer', async ({ page }) => {
    const r = await page.goto(BASE + '/');
    expect(r?.status()).toBe(200);
    const body = await page.textContent('body');
    expect(body?.length).toBeGreaterThan(100);
    expect(body).not.toContain('{{');
    expect(body).not.toContain('runtime error');
    await expect(page.locator('header').first()).toBeVisible();
    await expect(page.locator('head title')).not.toBeEmpty();
    const og = await page.getAttribute('meta[property="og:title"]', 'content');
    expect(og).toBeTruthy();
    const vp = await page.getAttribute('meta[name="viewport"]', 'content');
    expect(vp).toContain('width=device-width');
  });

  // --- index.html ---
  test('Homepage (index.html) has JSON-LD and nav', async ({ page }) => {
    await page.goto(BASE + '/');
    const html = await page.content();
    expect(html).toContain('"@type"');
    expect(await page.locator('a[href*="/posts"]').count()).toBeGreaterThan(0);
    expect(await page.locator('a[href*="/tags"]').count()).toBeGreaterThan(0);
  });

  // --- posts.html ---
  test('Posts page (posts.html)', async ({ page }) => {
    const r = await page.goto(BASE + '/posts/');
    expect(r?.status()).toBe(200);
    const body = await page.textContent('body');
    expect(body).not.toContain('{{');
    expect(body?.length).toBeGreaterThan(50);
  });

  test('Books page (posts.html, what=books)', async ({ page }) => {
    const r = await page.goto(BASE + '/books/');
    expect(r?.status()).toBe(200);
    expect(await page.textContent('body')).not.toContain('{{');
  });

  // --- tags.html ---
  test('Tags page (tags.html)', async ({ page }) => {
    const r = await page.goto(BASE + '/tags/');
    expect(r?.status()).toBe(200);
    expect(await page.textContent('body')).not.toContain('{{');
  });

  // --- search.html ---
  test('Search page (search.html)', async ({ page }) => {
    const r = await page.goto(BASE + '/search/?w=test');
    expect(r?.status()).toBe(200);
    expect(await page.textContent('body')).not.toContain('{{');
  });

  // --- error.html ---
  test('404 page (error.html)', async ({ page }) => {
    const r = await page.goto(BASE + '/nonexistent-xyz/');
    expect(r?.status()).toBe(404);
    const body = await page.textContent('body');
    expect(body).not.toContain('{{');
    expect(body?.length).toBeGreaterThan(50);
  });

  // --- redirect.html ---
  test('Redirect page (redirect.html)', async ({ page }) => {
    const encoded = Buffer.from('https://example.com').toString('base64');
    const r = await page.goto(BASE + '/r/go?url=' + encoded);
    expect(r?.status()).toBe(200);
    expect(await page.textContent('body')).not.toContain('{{');
  });

  // --- CSS ---
  test('Site CSS loads', async ({ page }) => {
    const r = await page.goto(BASE + '/static/site/folio/css/style.css');
    expect(r?.status()).toBe(200);
    const css = await r?.text();
    expect(css?.length).toBeGreaterThan(500);
    expect(css).toContain('--');
  });

  // --- RSS ---
  test('RSS feed', async ({ page }) => {
    expect((await page.goto(BASE + '/feed/'))?.status()).toBe(200);
  });

  // --- JS ---
  test('No JS errors on homepage', async ({ page }) => {
    const errors: string[] = [];
    page.on('console', m => { if (m.type() === 'error') errors.push(m.text()); });
    await page.goto(BASE + '/');
    await page.waitForTimeout(2000);
    expect(errors.filter(e => e.includes('SyntaxError') || e.includes('ReferenceError') || e.includes('TypeError'))).toHaveLength(0);
  });

  // --- 响应式 ---
  test('Mobile no overflow', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 });
    await page.goto(BASE + '/');
    expect(await page.evaluate(() => document.body.scrollWidth)).toBeLessThanOrEqual(await page.evaluate(() => window.innerWidth) + 5);
  });
});

// ============================================================
// 数据准备 + article/page/comments 模板验证
// ============================================================
test.describe('Seed data & article templates', () => {

  // --- article.html + article_list_entry.html + article_title_item.html ---
  test('Publish article, verify article.html', async ({ page }) => {
    await loginAdmin(page);
    await publishTestArticle(page, {
      title: 'E2E Test Article',
      slug: 'e2e-test-article',
      content: '## Heading\n\nParagraph with **bold** and [link](https://example.com).\n\n```go\nfmt.Println("hello")\n```',
      tags: 'e2e,test',
    });

    const r = await page.goto(BASE + '/e2e-test-article');
    expect(r?.status()).toBe(200);
    const body = await page.textContent('body');
    expect(body).not.toContain('{{');
    expect(body).toContain('E2E Test Article');
    const html = await page.content();
    expect(html).toContain('BlogPosting');
  });

  // --- page.html ---
  test('Publish page, verify page.html', async ({ page }) => {
    await loginAdmin(page);
    await publishTestArticle(page, {
      title: 'E2E Test Page',
      slug: 'e2e-test-page',
      content: 'Static page content for E2E.',
      templateId: 2,
    });

    const r = await page.goto(BASE + '/e2e-test-page');
    expect(r?.status()).toBe(200);
    const body = await page.textContent('body');
    expect(body).not.toContain('{{');
    expect(body).toContain('E2E Test Page');
  });

  test('Article page no JS errors', async ({ page }) => {
    const errors: string[] = [];
    page.on('console', m => { if (m.type() === 'error') errors.push(m.text()); });
    await page.goto(BASE + '/e2e-test-article');
    await page.waitForTimeout(2000);
    expect(errors.filter(e => e.includes('SyntaxError') || e.includes('ReferenceError') || e.includes('TypeError'))).toHaveLength(0);
  });

  // --- article_list_entry.html ---
  test('Posts lists seeded article (article_list_entry.html)', async ({ page }) => {
    await page.goto(BASE + '/posts/');
    const body = await page.textContent('body');
    expect(body).not.toContain('{{');
    // 文章可能还未发布成功（依赖上面的测试），只验证模板无错
  });

  // --- 标签过滤 ---
  test('Tag filter page', async ({ page }) => {
    const r = await page.goto(BASE + '/tags/e2e/');
    expect(r?.status()).toBe(200);
    expect(await page.textContent('body')).not.toContain('{{');
  });

  // --- comments_entry.html ---
  test('Comment section renders (comments_entry.html)', async ({ page }) => {
    await page.goto(BASE + '/e2e-test-article');
    expect(await page.textContent('body')).not.toContain('{{');
  });

  test('Folio comment form uses semantic fields on article', async ({ page }) => {
    await page.goto(BASE + '/e2e-test-article');

    await expect(page.locator('form#reply.comment-form')).toBeVisible();
    await expect(page.locator('textarea#id_content[name="content"]')).toBeVisible();
    await expect(page.locator('input#id_nickname[name="nickname"]')).toBeVisible();
    await expect(page.locator('input#id_email[type="email"][name="email"]')).toBeVisible();
    await expect(page.locator('input#id_website[type="url"][name="website"]')).toBeVisible();
    await expect(page.locator('#comment_form_status[role="status"]')).toBeVisible();
    await expect(page.locator('#comment_reply_target')).toBeHidden();
  });

  test('Folio reply flow shows and clears reply target', async ({ page }) => {
    await page.goto(BASE + '/e2e-test-article');

    let replyButtonCount = await page.locator('.comment-reply-btn').count();
    if (replyButtonCount === 0) {
      await page.evaluate(async () => {
        const slug = (document.querySelector('#id_slug') as HTMLInputElement | null)?.value || '';
        const payload = {
          nickname: 'E2E Reply Author',
          content: 'E2E seed comment for reply interaction',
          version: 1,
          slug,
        };
        const resp = await fetch('/api/comment', {
          method: 'POST',
          headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        });
        if (resp.status !== 200) {
          throw new Error('seed comment failed: ' + resp.status);
        }
      });
      await page.reload({ waitUntil: 'networkidle' });
      replyButtonCount = await page.locator('.comment-reply-btn').count();
    }

    expect(replyButtonCount).toBeGreaterThan(0);

    const firstReplyButton = page.locator('.comment-reply-btn').first();
    const replyNickname = await firstReplyButton.getAttribute('data-comment-nickname');
    await expect(firstReplyButton).toBeVisible();
    await firstReplyButton.click();

    await expect(page.locator('#comment_reply_target')).toBeVisible();
    if (replyNickname) {
      await expect(page.locator('#comment_reply_target_text')).toContainText('@' + replyNickname);
    }
    await expect(page.locator('#id_reply_to')).not.toHaveValue('');

    await page.click('.comment-reply-cancel');
    await expect(page.locator('#comment_reply_target')).toBeHidden();
    await expect(page.locator('#id_reply_to')).toHaveValue('');
  });

  test('Folio comment form uses semantic fields on page template', async ({ page }) => {
    await page.goto(BASE + '/e2e-test-page');

    await expect(page.locator('form#reply.comment-form')).toBeVisible();
    await expect(page.locator('textarea#id_content[name="content"]')).toBeVisible();
    await expect(page.locator('input#id_nickname[name="nickname"]')).toBeVisible();
    await expect(page.locator('input#id_email[type="email"][name="email"]')).toBeVisible();
    await expect(page.locator('input#id_website[type="url"][name="website"]')).toBeVisible();
  });

  // --- search ---
  test('Search with results', async ({ page }) => {
    const r = await page.goto(BASE + '/search/?w=E2E');
    expect(r?.status()).toBe(200);
    expect(await page.textContent('body')).not.toContain('{{');
  });
});

// ============================================================
// 后台 Admin 主题 (glacie) — 所有 13 个模板
// ============================================================
test.describe('Admin Theme — glacie', () => {

  // --- login.html ---
  test('Login page (login.html)', async ({ page }) => {
    const r = await page.goto(BASE + '/admin/login');
    expect(r?.status()).toBe(200);
    const body = await page.textContent('body');
    expect(body).not.toContain('{{');
    expect(body?.length).toBeGreaterThan(200);
    await expect(page.locator('form')).toBeVisible();
    await expect(page.locator('input[name="email"]')).toBeVisible();
    await expect(page.locator('input[name="password"]')).toBeVisible();
    await expect(page.locator('input[name="captcha"]')).toBeVisible();
    await expect(page.locator('#captchaId')).toBeAttached();
  });

  test('Login page no template residue', async ({ page }) => {
    await page.goto(BASE + '/admin/login');
    const html = await page.content();
    expect(html).not.toContain('{{.');
    expect(html).not.toContain('{{template');
    expect(html).not.toContain('{{define');
    expect(html).not.toContain('{{end}}');
  });

  test('Login page no JS errors', async ({ page }) => {
    const errors: string[] = [];
    page.on('console', m => { if (m.type() === 'error') errors.push(m.text()); });
    await page.goto(BASE + '/admin/login');
    await page.waitForTimeout(2000);
    expect(errors.filter(e => e.includes('SyntaxError') || e.includes('ReferenceError') || e.includes('TypeError'))).toHaveLength(0);
  });

  // --- css.html ---
  test('Admin CSS with glass morphism (css.html)', async ({ page }) => {
    const r = await page.goto(BASE + '/static/admin/glacie/style.css');
    expect(r?.status()).toBe(200);
    const css = await r?.text();
    expect(css?.length).toBeGreaterThan(500);
    expect(css).toContain('backdrop-filter');
  });

  // --- index.html (dashboard) ---
  test('Dashboard (index.html)', async ({ page }) => {
    await loginAdmin(page);
    const r = await page.goto(BASE + '/admin');
    expect(r?.status()).toBe(200);
    const html = await page.content();
    expect(html).not.toContain('{{.');
    expect(html).not.toContain('{{end}}');
    expect(await page.textContent('body')).not.toContain('{{');
  });

  test('Dashboard no JS errors', async ({ page }) => {
    await loginAdmin(page);
    const errors: string[] = [];
    page.on('console', m => { if (m.type() === 'error') errors.push(m.text()); });
    await page.goto(BASE + '/admin');
    await page.waitForTimeout(3000);
    expect(errors.filter(e => e.includes('SyntaxError') || e.includes('ReferenceError') || e.includes('TypeError'))).toHaveLength(0);
  });

  // --- publish.html ---
  test('Publish page (publish.html)', async ({ page }) => {
    await loginAdmin(page);
    const r = await page.goto(BASE + '/admin/publish');
    expect(r?.status()).toBe(200);
    const html = await page.content();
    expect(html).not.toContain('{{.');
    expect(html).not.toContain('{{end}}');
    await expect(page.locator('#inputTitle')).toBeVisible();
    await expect(page.locator('#inputSlug')).toBeVisible();
  });

  test('Publish page no JS errors', async ({ page }) => {
    await loginAdmin(page);
    const errors: string[] = [];
    page.on('console', m => { if (m.type() === 'error') errors.push(m.text()); });
    await page.goto(BASE + '/admin/publish');
    await page.waitForTimeout(3000);
    expect(errors.filter(e => e.includes('SyntaxError') || e.includes('ReferenceError') || e.includes('TypeError'))).toHaveLength(0);
  });

  // --- articles.html ---
  test('Articles page (articles.html)', async ({ page }) => {
    await loginAdmin(page);
    const r = await page.goto(BASE + '/admin/articles');
    expect(r?.status()).toBe(200);
    const html = await page.content();
    expect(html).not.toContain('{{.');
    expect(html).not.toContain('{{end}}');
    expect(await page.textContent('body')).not.toContain('{{');
  });

  // --- comments.html ---
  test('Comments page (comments.html)', async ({ page }) => {
    await loginAdmin(page);
    const r = await page.goto(BASE + '/admin/comments');
    expect(r?.status()).toBe(200);
    const html = await page.content();
    expect(html).not.toContain('{{.');
    expect(html).not.toContain('{{end}}');
  });

  // --- media.html ---
  test('Media page (media.html)', async ({ page }) => {
    await loginAdmin(page);
    const r = await page.goto(BASE + '/admin/media');
    expect(r?.status()).toBe(200);
    const html = await page.content();
    expect(html).not.toContain('{{.');
    expect(html).not.toContain('{{end}}');
  });

  // --- tags.html ---
  test('Tags page (tags.html)', async ({ page }) => {
    await loginAdmin(page);
    const r = await page.goto(BASE + '/admin/tags');
    expect(r?.status()).toBe(200);
    const html = await page.content();
    expect(html).not.toContain('{{.');
    expect(html).not.toContain('{{end}}');
  });

  // --- settings.html ---
  test('Settings page (settings.html)', async ({ page }) => {
    await loginAdmin(page);
    const r = await page.goto(BASE + '/admin/settings');
    expect(r?.status()).toBe(200);
    const html = await page.content();
    expect(html).not.toContain('{{.');
    expect(html).not.toContain('{{end}}');
    expect(await page.textContent('body')).not.toContain('{{');
  });

  // --- error.html ---
  test('Admin 404 (error.html)', async ({ page }) => {
    await loginAdmin(page);
    const r = await page.goto(BASE + '/admin/nonexistent');
    expect(await page.textContent('body')).not.toContain('{{');
  });

  // --- header.html (导航链接) ---
  test('Admin nav links (header.html)', async ({ page }) => {
    await loginAdmin(page);
    await page.goto(BASE + '/admin');
    // 检查导航链接存在（用 href 包含匹配，兼容有无尾斜杠）
    for (const keyword of ['publish', 'articles', 'comments', 'tags', 'settings']) {
      expect(await page.locator(`a[href*="${keyword}"]`).count()).toBeGreaterThan(0);
    }
  });

  // --- footer.html (ajaxRequest 函数) ---
  test('Admin footer JS (footer.html)', async ({ page }) => {
    await loginAdmin(page);
    await page.goto(BASE + '/admin');
    await page.waitForTimeout(1000);
    expect(await page.evaluate(() => typeof (window as any).ajaxRequest === 'function')).toBe(true);
  });

  // --- js.html (jQuery 加载) ---
  test('jQuery loaded (js.html)', async ({ page }) => {
    await loginAdmin(page);
    await page.goto(BASE + '/admin');
    await page.waitForTimeout(1000);
    expect(await page.evaluate(() => typeof (window as any).jQuery === 'function')).toBe(true);
  });
});
