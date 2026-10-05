import { expect, test, type Locator, type Page } from '@playwright/test';
import path from 'node:path';
import { createHash, createPublicKey, verify } from 'node:crypto';

async function capturePagination(page: Page, name: string) {
  if (!process.env.SOLITUDES_PAGINATION_SCREENSHOTS) return;
  const original=page.viewportSize();
  for (const width of [1440,390]) {
    await page.setViewportSize({width,height:900});
    await page.screenshot({path:path.join(process.env.SOLITUDES_PAGINATION_SCREENSHOTS,`${siteTheme}-${name}-${width}.png`),fullPage:true,animations:'disabled'});
  }
  if(original) await page.setViewportSize(original);
}

test('pagination contract public archives, search, tags, profiles and replies', async ({page}) => {
  test.skip(!process.env.E2E_PAGING_THREAD, 'Requires multi-page fixtures.');
  test.setTimeout(90000);
  for (const route of ['/posts/','/books/','/tags/Paging%20%26%20test/']) {
    await page.goto(route);
    const first = await page.getByTestId('article-list-link').allTextContents();
    expect(first.length).toBe(20);
    await page.getByTestId('pagination-next').click();
    await expect(page.getByTestId('article-list-link')).toHaveCount(route === '/books/' ? 2 : 20);
    const second = await page.getByTestId('article-list-link').allTextContents();
    expect(second.length).toBeGreaterThan(0);
    expect(second.some(value => first.includes(value))).toBe(false);
    if (route.includes('tags')) expect(decodeURIComponent(new URL(page.url()).pathname)).toBe('/tags/Paging & test/2/');
    await page.getByTestId('pagination-previous').click();
    await expect(page.getByTestId('article-list-link')).toHaveCount(20);
    expect(await page.getByTestId('article-list-link').allTextContents()).toEqual(first);
  }
  await page.goto('/tags/');
  const firstTags = await page.getByTestId('tag-entry').allTextContents();
  expect(firstTags.length).toBe(50);
  await page.getByTestId('pagination-next').click();
  await expect(page.getByTestId('tag-entry').first()).toBeVisible();
  expect((await page.getByTestId('tag-entry').allTextContents()).some(value => firstTags.includes(value))).toBe(false);
  await expect(page.getByTestId('pagination-next')).toHaveCount(0);
  await expect(page.locator('main')).not.toContainText('private-only-tag');
  await page.goto('/search/?w=pagingneedle');
  const seen = new Set<string>();
  for (let n=1;n<=12;n++) {
    await expect(page.locator('.search-result-title')).toHaveCount(n===12 ? 2 : 10);
    const titles = await page.locator('.search-result-title').allTextContents();
    expect(titles.length).toBe(n===12 ? 2 : 10);
    for (const title of titles) { expect(seen.has(title)).toBe(false); expect(title).not.toContain('private-only'); seen.add(title); }
    if (n<12) { await page.getByTestId('pagination-next').click(); expect(new URL(page.url()).searchParams.get('w')).toBe('pagingneedle'); }
  }
  expect(seen.size).toBe(112);
  await expect(page.getByTestId('pagination-next')).toHaveCount(0);
  await page.goto('/readers/');
  const readers = await page.getByTestId('reader-circle-latest').allTextContents();
  await page.getByTestId('pagination-next').click();
  await expect(page.getByTestId('reader-circle-latest')).toHaveCount(12);
  expect((await page.getByTestId('reader-circle-latest').allTextContents()).some(value => readers.includes(value))).toBe(false);
  await page.goto('/users/'+process.env.E2E_ADMIN_ID);
  const articles = await page.getByTestId('public-profile-article').allTextContents();
  await page.locator('#profile-articles').getByTestId('pagination-next').click();
  await expect(page.getByTestId('public-profile-article')).toHaveCount(12);
  expect((await page.getByTestId('public-profile-article').allTextContents()).some(value => articles.includes(value))).toBe(false);
  await page.goto('/users/'+process.env.E2E_READER_ID+'?articles_page=2');
  const comments = await page.getByTestId('public-profile-comment').allTextContents();
  await page.locator('#profile-comments').getByTestId('pagination-next').click();
  await expect(page.getByTestId('public-profile-comment')).toHaveCount(12);
  expect(new URL(page.url()).searchParams.get('articles_page')).toBe('2');
  expect((await page.getByTestId('public-profile-comment').allTextContents()).some(value => comments.includes(value))).toBe(false);
  await page.goto('/paging-000');
  expect(await page.getByTestId('comment-entry').count()).toBeLessThanOrEqual(23);
  await page.getByTestId('pagination-next').click();
  await expect(page.getByTestId('pagination-next')).toHaveCount(0);
  await page.goto('/paging-000?thread='+process.env.E2E_PAGING_THREAD);
  await expect(page.getByTestId('comment-entry')).toHaveCount(21);
  const replies = await page.getByTestId('comment-entry').allTextContents();
  await page.getByTestId('pagination-next').click();
  await expect(page.getByTestId('comment-entry')).toHaveCount(4);
  await capturePagination(page,'replies');
  const nextReplies = await page.getByTestId('comment-entry').allTextContents();
  expect(nextReplies.slice(1).some(value => replies.slice(1).includes(value))).toBe(false);
  await expect(page.getByTestId('pagination-next')).toHaveCount(0);
  await page.getByTestId('comments-back').click();
  expect(new URL(page.url()).searchParams.has('thread')).toBe(false);
  await page.goto('/paging-000?thread='+process.env.E2E_PAGING_THREAD+'&replies_page=2');
  await page.getByTestId('comment-nickname').fill('Pagination guest');
  await page.getByTestId('comment-content').fill('New comment remains reachable after pagination '+siteTheme);
  let payload!: {id:string};
  // Read the real response before forwarding it: the browser immediately
  // navigates away and may discard its old response-body handle.
  await page.route('**/api/comment',async route=>{
    const response=await route.fetch();
    expect(response.status()).toBe(200);
    payload=await response.json();
    await route.fulfill({response});
  },{times:1});
  await page.getByTestId('comment-submit').click();
  await expect(page).toHaveURL(/\?thread=[^#]+#comment-/);
  expect(Object.keys(payload)).toEqual(['id']);
  await expect(page).toHaveURL(new RegExp('thread='+payload.id+'#comment-'+payload.id+'$'));
  await expect(page.locator('#comment-'+payload.id)).toContainText('New comment remains reachable');
});

test('pagination contract account applications, passkeys and administrative lists', async ({page}) => {
  test.skip(!process.env.E2E_PAGING_THREAD, 'Requires multi-page fixtures.');
  test.setTimeout(90000);
  await signIn(page, readerEmail, readerPassword);
  await page.goto('/account/oidc/clients');
  await expect(page.getByTestId('account-client-row')).toHaveCount(20);
  const clients = await page.getByTestId('account-client-row').allTextContents();
  await page.getByTestId('pagination-next').click();
  await expect(page.getByTestId('account-client-row').first()).toBeVisible();
  await capturePagination(page,'clients');
  expect((await page.getByTestId('account-client-row').allTextContents()).some(value => clients.includes(value))).toBe(false);
  await expect(page.getByTestId('pagination-next')).toHaveCount(0);
  expect((await page.request.get('/admin/users?page=2')).status()).toBe(403);
  await page.goto('/account');
  await page.getByTestId('account-logout').click();
  await signIn(page);
  await page.goto('/account');
  await expect(page.getByTestId('account-passkey-row')).toHaveCount(10);
  await page.locator('#passkeys').getByTestId('pagination-next').click();
  await expect(page.getByTestId('account-passkey-row')).toHaveCount(2);
  await expect(page.locator('#passkeys').getByTestId('pagination-next')).toHaveCount(0);
  await page.goto('/admin/media');
  await expect(page.locator('a[href^="/upload/"]')).toHaveCount(15);
  await page.getByTestId('pagination-next').click();
  await expect(page.locator('a[href^="/upload/"]')).toHaveCount(15);
  await page.getByTestId('pagination-next').click();
  await expect(page.locator('a[href^="/upload/"]')).toHaveCount(1);
  await expect(page.getByTestId('pagination-next')).toHaveCount(0);
  for (const route of ['/admin/tags','/admin/oidc/clients/paging-client-00']) {
    await page.goto(route);
    await page.getByTestId('pagination-next').click();
    await expect(page.getByTestId('pagination-previous')).toBeVisible();
    await expect(page.getByTestId('pagination-next')).toHaveCount(0);
  }
  for (const route of ['/admin/users?q=Paging&role=user','/admin/oidc/clients?owner_id='+process.env.E2E_READER_ID,'/admin/audit?action=oidc.login&client_id=paging-client-00']) {
    await page.goto(route);
    const before = new URL(page.url());
    await page.getByTestId('identity-next').click();
    const after = new URL(page.url());
    for (const [key,value] of before.searchParams) expect(after.searchParams.get(key)).toBe(value);
    expect(after.searchParams.get('page')).toBe('2');
    await expect(page.getByTestId('identity-next')).toHaveCount(0);
  }
  for (const route of ['/admin/articles','/admin/comments?status=visible']) {
    await page.goto(route);
    const first=await page.locator('tbody tr').allTextContents();
    await page.locator('.admin-pagination a').last().click();
    expect((await page.locator('tbody tr').allTextContents()).some(value=>first.includes(value))).toBe(false);
    if(route.includes('comments')) expect(new URL(page.url()).searchParams.get('status')).toBe('visible');
  }
  for (const route of ['/posts/1001/','/books/-1/','/tags/Paging%20%26%20test/1001/','/admin/media?page=1001','/admin/tags?page=0','/account?passkeys_page=-1','/account/oidc/clients?page=1001','/search/?w=pagingneedle&page=1001','/paging-000?comment_page=1001']) {
    expect((await page.request.get(route)).status(),route).toBe(400);
  }
  for (const width of [1440,768,390]) {
    await page.setViewportSize({width,height:900});
    await page.goto('/account/oidc/clients?page=2');
    expect(await page.evaluate(()=>document.documentElement.scrollWidth)).toBeLessThanOrEqual(width+1);
  }
  await page.goto('/account');
  await page.getByTestId('account-logout').click();
  await signIn(page,editorEmail);
  const editorArticles=await page.request.get('/admin/articles?page=2');
  expect(editorArticles.status()).toBe(200);
  expect(await editorArticles.text()).not.toContain('pagingneedle');
  expect((await page.request.get('/admin/users?page=2')).status()).toBe(403);
  await page.goto('/account/oidc/clients?page=2');
  await expect(page.getByTestId('account-client-row')).toHaveCount(0);
});

for (const locale of ['zh-CN','en']) test.describe(`revision language ${locale}`, () => {
  test.use({locale});
  test('revision controls are separate from prose and accessible across layouts', async ({page}) => {
    test.skip(!process.env.E2E_ADMIN_EMAIL, 'Requires isolated fixtures.');
    test.setTimeout(60000);
    const capture = async (name: string) => {
      if (process.env.SOLITUDES_REVISION_SCREENSHOTS) await page.screenshot({path:path.join(process.env.SOLITUDES_REVISION_SCREENSHOTS, `${siteTheme}-${name}.png`),animations:'disabled'});
    };
    const checkRevisionUnderline = async (link: Locator, name: string) => {
      if (siteTheme !== 'folio') return;
      await page.mouse.move(0, 0);
      await expect(link).toHaveCSS('background-image', 'none');
      await expect(link).toHaveCSS('text-decoration-line', 'none');
      await link.hover();
      await expect(link).toHaveCSS('text-decoration-line', 'underline');
      await expect(link).toHaveCSS('background-image', 'none');
      if (locale === 'zh-CN') await capture(name);
      await page.mouse.move(0, 0);
    };
    for (const [width, scheme] of [[1440,'light'],[390,'light'],[320,'dark']] as const) {
      await page.setViewportSize({width,height:900});
      await page.emulateMedia({colorScheme:scheme});
      await page.goto('/visual-revisions', {waitUntil:'domcontentloaded'});
      const history = page.getByTestId('article-history');
      await expect(history.locator('summary')).toHaveText(locale === 'zh-CN' ? '历史版本 · 50' : 'Revision history · 50');
      await expect(history).not.toHaveAttribute('open');
      await expect(page.locator('[data-reading-content] [data-testid="article-history"]')).toHaveCount(0);
      const prefix = `${locale}-${width}-${scheme}`;
      if (locale === 'zh-CN') await capture(prefix+'-collapsed');
      const summary = history.locator('summary');
      const summaryBox = (await summary.boundingBox())!;
      const historyBox = (await history.boundingBox())!;
      expect(Math.abs(summaryBox.x + summaryBox.width - historyBox.x - historyBox.width)).toBeLessThanOrEqual(2);
      expect(summaryBox.width).toBeGreaterThanOrEqual(historyBox.width - 2);
      expect(summaryBox.height).toBeGreaterThanOrEqual(44);
      const closedArrow = await summary.evaluate(el => getComputedStyle(el, '::after').transform);
      // The whitespace in the middle and the far-right end are both targets,
      // not just the label or the disclosure arrow.
      await summary.hover({position:{x:summaryBox.width / 2,y:summaryBox.height / 2}});
      await expect(summary).toHaveCSS('cursor', 'pointer');
      expect(await summary.evaluate(el => getComputedStyle(el).backgroundColor)).not.toBe('rgba(0, 0, 0, 0)');
      if (locale === 'zh-CN') await capture(prefix+'-summary-hover');
      await summary.click({position:{x:summaryBox.width / 2,y:summaryBox.height / 2}});
      await expect(history).toHaveAttribute('open', '');
      expect(await summary.evaluate(el => getComputedStyle(el, '::after').transform)).not.toBe(closedArrow);
      await summary.click({position:{x:summaryBox.width - 4,y:summaryBox.height / 2}});
      await expect(history).not.toHaveAttribute('open');
      await summary.focus();
      await page.keyboard.press('Enter');
      await expect(history).toHaveAttribute('open', '');
      await expect(history.locator('nav a')).toHaveCount(50);
      await expect(history.locator('nav a').first()).toBeVisible();
      await expect(history.locator('nav a').first()).toHaveText('v50');
      const list = history.locator('nav');
      const listSize = await list.evaluate(el => ({height:el.clientHeight, fullHeight:el.scrollHeight, width:el.clientWidth, fullWidth:el.scrollWidth}));
      expect(listSize.height).toBeLessThanOrEqual(152);
      expect(listSize.fullWidth).toBeLessThanOrEqual(listSize.width);
      if (width <= 390) expect(listSize.fullHeight).toBeGreaterThan(listSize.height);
      expect(await page.evaluate(()=>document.documentElement.scrollWidth)).toBeLessThanOrEqual(width+1);
      if (locale === 'zh-CN') await capture(prefix+'-expanded');
      await checkRevisionUnderline(history.locator('nav a').first(), prefix+'-history-hover');
      // Tab to an off-screen revision: native focus must scroll the list,
      // without increasing the space reserved above the article body.
      const bodyTop = (await page.locator('[data-reading-content]').boundingBox())!.y;
      await history.locator('a[href="/visual-revisions/v2"]').focus();
      await page.keyboard.press('Tab');
      await expect(history.locator('a[href="/visual-revisions/v1"]')).toBeFocused();
      await expect(history.locator('nav a').last()).toHaveCSS('outline-style', 'solid');
      await expect(history.locator('nav a').last()).toHaveCSS('outline-width', '2px');
      if (width <= 390) expect(await list.evaluate(el => el.scrollTop)).toBeGreaterThan(0);
      const lastLink = await history.locator('nav a').last().boundingBox();
      const listBox = await list.boundingBox();
      expect(lastLink!.y).toBeGreaterThanOrEqual(listBox!.y);
      expect(lastLink!.y + lastLink!.height).toBeLessThanOrEqual(listBox!.y + listBox!.height + 1);
      expect((await page.locator('[data-reading-content]').boundingBox())!.y).toBeCloseTo(bodyTop, 0);
      if (locale === 'zh-CN') await capture(prefix+'-scrolled');
      await page.keyboard.press('Enter');
      await expect(page).toHaveURL(/\/visual-revisions\/v1$/);
      const notice = page.getByTestId('article-revision-notice');
      await expect(notice).toBeVisible();
      await expect(notice).toContainText(locale === 'zh-CN' ? '正在阅读历史版本 v1' : 'You’re reading an earlier version, v1');
      await expect(page.locator('[data-reading-content] [data-testid="article-revision-notice"]')).toHaveCount(0);
      expect(await page.evaluate(()=>document.documentElement.scrollWidth)).toBeLessThanOrEqual(width+1);
      if (locale === 'zh-CN') await capture(prefix+'-old');
      await checkRevisionUnderline(page.getByTestId('article-latest-version'), prefix+'-latest-hover');
      await page.getByTestId('article-latest-version').click();
      await expect(page).toHaveURL(/\/visual-revisions$/);
      await expect(page.getByTestId('article-revision-notice')).toHaveCount(0);
    }
    await page.goto('/e2e-theme-article');
    await expect(page.getByTestId('article-history')).toHaveCount(0);
    await expect(page.getByTestId('article-revision-notice')).toHaveCount(0);
  });
});

for (const role of ['guest', 'member', 'editor', 'admin'] as const) {
  test(`article access boundaries for ${role}`, async ({page, context}) => {
    test.skip(!process.env.E2E_ADMIN_EMAIL, 'Requires isolated fixtures.');
    if (role !== 'guest') await signIn(page, role === 'member' ? readerEmail : role === 'editor' ? editorEmail : adminEmail);
    const allowed = {public: true, members: role !== 'guest', editors: role === 'editor' || role === 'admin', private: role === 'admin'};
    for (const [level, canRead] of Object.entries(allowed)) {
      const response = await page.goto('/access-' + level, {waitUntil:'domcontentloaded'});
      expect(response?.status()).toBe(canRead ? 200 : 404);
      const html = await response!.text();
      for (const [token, visible] of [['member-access-secret', allowed.members], ['editor-access-secret', allowed.editors], ['author-access-secret', allowed.private]] as const) {
        expect(html.includes(token)).toBe(canRead && visible);
      }
      expect(html.includes('hidden-attachment.example')).toBe(canRead && allowed.private);
      if (role !== 'guest') expect(response!.headers()['cache-control']).toContain('no-store');
      if (canRead) {
        const historyEntry = page.getByTestId('article-history');
        await expect(historyEntry).not.toHaveAttribute('open');
        await historyEntry.locator('summary').click();
        await expect(historyEntry.locator(`a[href="/access-${level}/v1"]`)).toBeVisible();
      }
      const history = await page.request.get('/access-' + level + '/v1');
      expect(history.status()).toBe(canRead ? 200 : 404);
      const historicalHTML = await history.text();
      expect(historicalHTML.includes('legacy-unguarded-secret')).toBe(canRead);
      for (const [token, visible] of [['history-member-secret', allowed.members], ['history-editor-secret', allowed.editors], ['history-author-secret', allowed.private]] as const) {
        expect(historicalHTML.includes(token)).toBe(canRead && visible);
      }
      expect(historicalHTML.includes('history-hidden-attachment.example')).toBe(canRead && allowed.private);
      const latest = await page.request.get('/access-' + level + '/v2', {maxRedirects:0});
      expect(latest.status()).toBe(canRead ? 301 : 404);
    }
    await page.goto('/access-public/v1', {waitUntil:'domcontentloaded'});
    await expect(page.locator('meta[name="robots"]')).toHaveAttribute('content', /noindex/);
    await expect(page.locator('[data-reading-content]')).toContainText('legacy-unguarded-secret');
    if (role === 'guest') {
      if (siteTheme === 'cactus') {
        await expect(page.locator('#toc .toc-text')).toHaveText(['Historical public heading']);
        await expect(page.locator('#toc-footer .toc-text')).toHaveText(['Historical public heading']);
      } else await expect(page.getByTestId('article-toc')).toHaveCount(0);
    }
    await page.goto('/access-public', {waitUntil:'domcontentloaded'});
    if (role === 'guest') {
      await expect(page.locator('[data-reading-content] a[href^="/login"]')).toBeVisible();
      if (siteTheme === 'cactus') {
        await expect(page.locator('#toc .toc-text')).toHaveText(['Public heading']);
        await expect(page.locator('#toc-footer .toc-text')).toHaveText(['Public heading']);
      } else await expect(page.getByTestId('article-toc')).toHaveCount(0);
    }
    for (const url of ['/', '/posts/', '/tags/Access/', '/feed/rss', '/feed/atom', '/feed/json', '/sitemap.xml', '/search/?w=member-access-secret']) {
      const response = await page.request.get(url);
      expect(response.status()).toBe(200);
      const body = await response.text();
      if (role === 'guest' || url.startsWith('/feed/') || url.includes('sitemap') || url.includes('search')) {
        for (const token of ['member-access-secret','editor-access-secret','author-access-secret','legacy-unguarded-secret']) expect(body).not.toContain(token);
      }
    }
    if (role === 'guest' || role === 'member') {
      const response = await page.request.post('/admin/publish', {maxRedirects:0, form:{title:'Forbidden',slug:'forbidden-access',content:'not allowed',template:'1',visibility:'public'},headers:{Origin:new URL(page.url()).origin}});
      expect(response.status()).not.toBe(200);
    }
    if (process.env.SOLITUDES_ACCESS_SCREENSHOTS) {
      for (const width of [1280,390]) {
        await page.setViewportSize({width,height:900});
        await page.goto('/access-public', {waitUntil:'domcontentloaded'});
        await page.screenshot({path:path.join(process.env.SOLITUDES_ACCESS_SCREENSHOTS, `${siteTheme}-${role}-${width}.png`),fullPage:true,animations:"disabled"});
      }
    }
  });
}

async function useRealEditor(page: Page) {
  await page.route('https://cdn.jsdelivr.net/npm/vditor@4.0.0/dist/**', route => {
    const relative = new URL(route.request().url()).pathname.split('/dist/')[1]!;
    return route.fulfill({path:path.resolve('node_modules/vditor/dist',relative)});
  });
}

async function waitForEditor(page: Page) {
  await page.waitForFunction(() => {
    try { return typeof eval('vditor.getValue()') === 'string'; } catch { return false; }
  });
}

async function expectUnsavedWarning(page: Page, leave: () => Promise<unknown>) {
  const pendingDialog = page.waitForEvent('dialog');
  const navigation = leave().catch(() => {}); // Chromium aborts a cancelled navigation.
  const dialog = await pendingDialog;
  expect(dialog.type()).toBe('beforeunload');
  await dialog.dismiss();
  await navigation;
  expect(page.isClosed()).toBe(false);
}

// page.reload() waits for a load that intentionally never occurs when the
// beforeunload dialog is cancelled. Trigger the same browser reload without
// waiting for that cancelled document's lifecycle.
async function refreshWithUnsavedChanges(page: Page) {
  return expectUnsavedWarning(page, () => page.evaluate(() => { setTimeout(() => location.reload(), 0); }));
}

for (const role of ['admin', 'editor'] as const) {
  test(`publish unsaved protection covers refresh, navigation and closing for ${role}`, async ({page}) => {
    test.skip(!adminEmail, 'Requires isolated fixtures.');
    await useRealEditor(page);
    await signIn(page, role === 'admin' ? adminEmail : editorEmail);
    await page.goto('/admin/publish', {waitUntil:'domcontentloaded'});
    await waitForEditor(page);
    // An untouched editor must not warn, including after clicking into it.
    await page.getByTestId('publish-title').click();
    const unexpected: string[] = [];
    const acceptUnexpected = async (dialog: import('@playwright/test').Dialog) => {
      unexpected.push(dialog.type()); await dialog.accept();
    };
    page.on('dialog', acceptUnexpected);
    await page.reload({waitUntil:'domcontentloaded'});
    expect(unexpected).toEqual([]);
    page.off('dialog', acceptUnexpected);
    await waitForEditor(page);
    // Every persisted metadata field participates, and reverting is clean.
    for (const selector of ['#inputTitle','#inputSlug','#inputTags','#inputCID']) {
      const field = page.locator(selector);
      const original = await field.inputValue();
      await field.fill('Unsaved metadata');
      await refreshWithUnsavedChanges(page);
      await expect(field).toHaveValue('Unsaved metadata');
      await field.fill(original);
    }
    for (const selector of ['#cbBook','#cbDisableComment','#cbNewVersion']) {
      await page.locator(selector).check();
      await refreshWithUnsavedChanges(page);
      await page.locator(selector).uncheck();
    }
    for (const selector of ['#selTemplate','#articleVisibility']) {
      const field = page.locator(selector);
      const original = await field.inputValue();
      const other = await field.locator('option').evaluateAll((items, current) =>
        items.map(el => (el as HTMLOptionElement).value).find(value => value !== current)!, original);
      await field.selectOption(other);
      await refreshWithUnsavedChanges(page);
      await field.selectOption(original);
    }
    expect(await page.evaluate(() => (window as any).SolitudesPublishGuard.changed())).toBe(false);
    // Use real keyboard input in all three Vditor editing modes.
    for (const mode of ['sv','ir','wysiwyg']) {
      await page.locator('[data-type="edit-mode"]').click();
      await page.locator(`[data-mode="${mode}"]`).click();
      const surface = page.locator(mode === 'sv' ? '#editSection textarea.vditor-sv' : `#editSection .vditor-${mode} [contenteditable="true"]`).first();
      await surface.click();
      await page.keyboard.type('Do not lose this paragraph.');
      await refreshWithUnsavedChanges(page);
      expect(await page.evaluate(() => eval('vditor.getValue()'))).toContain('Do not lose this paragraph.');
    }
    await expectUnsavedWarning(page, () => page.locator('#admin-navigation a[href="/admin/articles"]').click({noWaitAfter:true}));
    await expect(page).toHaveURL(/\/admin\/publish$/);
    await expectUnsavedWarning(page, () => page.close({runBeforeUnload:true}));
  });
}

test('publish unsaved protection survives delayed editor initialization', async ({page}) => {
  test.skip(!adminEmail, 'Requires isolated fixtures.');
  await useRealEditor(page);
  let release!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/dist/js/lute/lute.min.js', async route => {
    await gate;
    await route.fulfill({path:path.resolve('node_modules/vditor/dist/js/lute/lute.min.js')});
  });
  await signIn(page);
  await page.goto('/admin/publish', {waitUntil:'domcontentloaded'});
  try { await page.getByTestId('publish-title').fill('Written while the editor loads'); }
  finally { release(); }
  await waitForEditor(page);
  await refreshWithUnsavedChanges(page);
  await expect(page.getByTestId('publish-title')).toHaveValue('Written while the editor loads');
});

test('publish unsaved protection remains active when the editor CDN fails', async ({page}) => {
  test.skip(!adminEmail, 'Requires isolated fixtures.');
  await page.route('https://cdn.jsdelivr.net/npm/vditor@4.0.0/dist/**', route => route.abort());
  await signIn(page);
  await page.goto('/admin/publish', {waitUntil:'domcontentloaded'});
  await page.getByTestId('publish-title').fill('Keep my title even without the editor');
  await refreshWithUnsavedChanges(page);
  await expect(page.getByTestId('publish-title')).toHaveValue('Keep my title even without the editor');
  // The user can still explicitly choose to discard changes and leave.
  page.once('dialog', dialog => dialog.accept());
  await page.locator('#admin-navigation a[href="/admin/articles"]').click();
  await expect(page).toHaveURL(/\/admin\/articles$/);
});

test('publish unsaved protection survives save failure and edits during saving', async ({page}) => {
  test.skip(!adminEmail, 'Requires isolated fixtures.');
  await useRealEditor(page);
  await signIn(page);
  await page.goto('/admin/publish', {waitUntil:'domcontentloaded'});
  await waitForEditor(page);
  const slug = `unsaved-${siteTheme}-${Date.now()}`;
  await page.getByTestId('publish-title').fill('Unsaved regression');
  await page.getByTestId('publish-slug').fill(slug);
  await page.evaluate(() => eval('vditor.setValue("Submitted paragraph")'));
  await page.route('**/admin/publish', route => route.fulfill({status:500,body:'Save failed'}), {times:1});
  const failure = page.waitForEvent('dialog');
  const failedSubmit = page.getByTestId('publish-submit').click();
  const error = await failure;
  expect(error.type()).toBe('alert');
  await error.accept();
  await failedSubmit;
  await refreshWithUnsavedChanges(page);
  // Save the actual request, but delay its response while the author keeps writing.
  let release!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  let persisted!: () => void;
  const saved = new Promise<void>(resolve => { persisted = resolve; });
  await page.route('**/admin/publish', async route => {
    const response = await route.fetch();
    persisted();
    await gate;
    await route.fulfill({response});
  }, {times:1});
  await page.getByTestId('publish-submit').click();
  await saved;
  try { await page.evaluate(() => eval('vditor.setValue("Submitted paragraph\\n\\nNew unsaved paragraph")')); }
  finally { release(); }
  await expect(page.getByTestId('publish-status')).not.toBeEmpty();
  await expect(page).toHaveURL(/\/admin\/publish$/);
  await expect(page.locator('#inputID')).not.toHaveValue('');
  const articleID = await page.locator('#inputID').inputValue();
  await refreshWithUnsavedChanges(page);
  // A successful save with no newer changes redirects without a warning.
  const warnings: string[] = [];
  page.on('dialog', async dialog => { warnings.push(dialog.type()); await dialog.accept(); });
  await page.getByTestId('publish-submit').click();
  await expect(page).toHaveURL(new RegExp('/'+slug+'$'));
  expect(warnings).toEqual([]);
  await expect(page.getByTestId('site-article')).toContainText('New unsaved paragraph');
  await page.goto('/admin/publish?id='+articleID, {waitUntil:'domcontentloaded'});
  await waitForEditor(page);
  await page.getByTestId('publish-title').click();
  await page.reload({waitUntil:'domcontentloaded'});
  expect(warnings).toEqual([]);
});

test('real Vditor inserts and publishes restricted Markdown', async ({page, browser}) => {
  test.skip(!adminEmail, 'Requires isolated fixtures.');
  // Serve the actual pinned editor distribution locally, including Lute and
  // language assets. No widget stub, and CI does not depend on a CDN request.
  await page.route('https://cdn.jsdelivr.net/npm/vditor@4.0.0/dist/**', route => {
    const relative = new URL(route.request().url()).pathname.split('/dist/')[1]!;
    return route.fulfill({path:path.resolve('node_modules/vditor/dist',relative)});
  });
  await signIn(page);
  await page.goto('/admin/publish', {waitUntil:'domcontentloaded'});
  const tool = page.locator('[data-type="restricted-content"]');
  await expect(tool).toBeVisible({timeout:30000});
  await page.evaluate(() => eval('vditor.setValue("Before\\n\\nselected-secret\\n\\nAfter")'));
  await page.locator('[data-type="edit-mode"]').click();
  await page.locator('[data-mode="sv"]').click();
  const source = page.locator('#editSection textarea.vditor-sv');
  await source.evaluate((el: HTMLTextAreaElement) => {
    el.focus(); const start = el.value.indexOf('selected-secret');
    el.setSelectionRange(start, start+'selected-secret'.length);
  });
  await tool.click();
  await expect(page.getByTestId('access-dialog')).toBeVisible();
  await expect(page.getByTestId('access-content')).toHaveValue('selected-secret');
  await page.getByTestId('access-level').selectOption('members');
  await page.getByTestId('access-content').fill('## UI protected heading\n\nreal-vditor-secret\nselected-secret\n\n```go\nfmt.Println("nested example")\n```');
  if (process.env.SOLITUDES_ACCESS_SCREENSHOTS) await page.screenshot({path:path.join(process.env.SOLITUDES_ACCESS_SCREENSHOTS, `${siteTheme}-access-editor.png`),fullPage:false});
  await page.getByTestId('access-insert').click();
  await expect(page.getByTestId('access-dialog')).not.toBeVisible();
  for (const mode of ['wysiwyg','ir','sv']) {
    await page.locator('[data-type="edit-mode"]').click();
    await page.locator(`[data-mode="${mode}"]`).click();
    const roundtrip = await page.evaluate(() => eval('vditor.getValue()'));
    expect(roundtrip).toContain('access:members');
    expect(roundtrip.match(/selected-secret/g)).toHaveLength(1);
  }
  const value = await page.evaluate(() => eval('vditor.getValue()'));
  expect(value).toContain('````access:members');
  const slug='real-access-'+siteTheme+'-'+Date.now();
  await page.getByTestId('publish-title').fill('Restricted UI test');
  await page.getByTestId('publish-slug').fill(slug);
  await page.getByTestId('publish-visibility').selectOption('public');
  await page.getByTestId('publish-submit').click();
  await expect(page).toHaveURL(new RegExp('/'+slug+'$'));
  await expect(page.getByTestId('site-article')).toContainText('real-vditor-secret');
  const guest = await browser.newContext();
  const response = await guest.request.get(new URL('/'+slug,page.url()).toString());
  expect(response.status()).toBe(200);
  expect(await response.text()).not.toContain('real-vditor-secret');
  expect(await response.text()).not.toContain('selected-secret');
  await guest.close();
});

test('quick Topic real Vditor protects fragments across roles and public surfaces', async ({page, browser}) => {
  test.skip(!adminEmail || !readerEmail || !editorEmail);
  await page.route('https://cdn.jsdelivr.net/npm/vditor@4.0.0/dist/**', route => {
    const relative = new URL(route.request().url()).pathname.split('/dist/')[1]!;
    return route.fulfill({path:path.resolve('node_modules/vditor/dist',relative)});
  });
  await signIn(page);
  await page.goto('/admin/');
  const tool = page.locator('[data-type="restricted-content"]');
  await expect(tool).toBeVisible({timeout:30000});
  await page.evaluate(() => eval('vditor.setValue("Quick public topic\\n\\nselected-topic-secret")'));
  await page.locator('[data-type="edit-mode"]').click();
  await page.locator('[data-mode="sv"]').click();
  await page.locator('#topicContent textarea.vditor-sv').evaluate((el: HTMLTextAreaElement) => {
    el.focus(); el.setSelectionRange(el.value.indexOf('selected-topic-secret'), el.value.length);
  });
  await tool.click();
  await expect(page.getByTestId('access-content')).toHaveValue(/selected-topic-secret/);
  await page.getByTestId('access-level').selectOption('members');
  await page.getByTestId('access-content').fill('topic-member-secret\n\n```access:editors\ntopic-editor-secret\n```\n\n```access:private\ntopic-private-secret\n```');
  await page.getByTestId('access-insert').click();
  expect(await page.evaluate(() => eval('vditor.getValue()'))).toContain('````access:members');
  page.once('dialog', dialog => dialog.accept());
  await page.getByTestId('topic-publish-submit').click();
  await expect(page.getByTestId('site-article')).toContainText('topic-private-secret');
  const slug = new URL(page.url()).pathname;
  const baseURL = new URL(page.url()).origin;
  for (const [role, email, password, visible] of [
    ['guest', '', '', 0], ['member', readerEmail!, readerPassword!, 1],
    ['editor', editorEmail!, readerPassword!, 2], ['admin', adminEmail!, adminPassword!, 3]
  ] as const) {
    const context = await browser.newContext({baseURL});
    const visitor = await context.newPage();
    if (email) await signIn(visitor, email, password);
    for (const route of ['/', '/tags/Topic/', slug, '/feed/rss', '/feed/atom', '/feed/json']) {
      const response = await context.request.get(route);
      expect(response.status(), `${role} ${route}`).toBe(200);
      const body = await response.text();
      expect(body, `${role} ${route}`).toContain('Quick public topic');
      for (const [i, marker] of ['topic-member-secret','topic-editor-secret','topic-private-secret'].entries()) {
        // Feeds are always public, even with a signed-in browser cookie.
        if (!route.startsWith('/feed') && i < visible) expect(body, `${role} ${route}`).toContain(marker);
        else expect(body, `${role} ${route}`).not.toContain(marker);
      }
    }
    await context.close();
  }
});

test('account columns remain compact across roles and screen widths', async ({page}) => {
  test.skip(!adminEmail || !readerEmail || !editorEmail);
  for (const [role, email, password] of [
    ['member',readerEmail!,readerPassword!],['editor',editorEmail!,readerPassword!],['admin',adminEmail!,adminPassword!]
  ]) {
    await signIn(page, email, password);
    await expect(page.getByTestId('account-authorizations')).toBeVisible();
    for (const width of [1440,390]) {
      await page.setViewportSize({width,height:1000});
      const left = await page.getByTestId('account-profile-column').boundingBox();
      const right = await page.getByTestId('account-security-column').boundingBox();
      expect(left).not.toBeNull(); expect(right).not.toBeNull();
      if (width > 767) {
        expect(Math.abs(left!.y-right!.y)).toBeLessThan(2);
        expect(right!.x).toBeGreaterThan(left!.x+left!.width);
      } else expect(right!.y).toBeGreaterThanOrEqual(left!.y+left!.height);
      for (const column of ['account-profile-column','account-security-column']) {
        const cards = await page.getByTestId(column).locator('.account-card').all();
        for (let i=1;i<cards.length;i++) {
          const previous = await cards[i-1]!.boundingBox(); const next = await cards[i]!.boundingBox();
          expect(next!.y-previous!.y-previous!.height).toBeLessThan(25);
        }
      }
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
      if (process.env.SOLITUDES_ACCOUNT_SCREENSHOTS) await page.screenshot({path:path.join(process.env.SOLITUDES_ACCOUNT_SCREENSHOTS,`${siteTheme}-${role}-${width}.png`),fullPage:true,animations:'disabled'});
    }
    await page.getByTestId('account-logout').click();
  }
});

test('member app authorization can be revoked and owner deletion invalidates credentials', async ({page}) => {
  test.skip(!readerEmail || !editorEmail || !oidcRedirectURI);
  await signIn(page, readerEmail!, readerPassword!);
  await page.getByTestId('account-oidc-clients').click();
  await page.getByTestId('oidc-client-name').fill('Revocable application');
  await page.getByTestId('oidc-client-homepage').fill('https://external.example.test/');
  await page.getByTestId('oidc-client-redirects').fill(oidcRedirectURI!);
  await page.getByTestId('oidc-client-public').check();
  await page.getByTestId('oidc-client-submit').click();
  const clientID = (await page.getByTestId('oidc-created-id').innerText()).trim();
  await page.goto('/account'); await page.getByTestId('account-logout').click();
  await signIn(page, editorEmail!, readerPassword!);
  const verifier = 'g'.repeat(64);
  const authorize = async () => {
    await page.goto('/authorize?'+new URLSearchParams({client_id:clientID,redirect_uri:oidcRedirectURI!,response_type:'code',scope:'openid email profile offline_access',
      code_challenge:createHash('sha256').update(verifier).digest('base64url'),code_challenge_method:'S256'}));
    await page.getByTestId('oidc-consent-allow').click();
    await expect(page).toHaveURL(/callback\?code=/);
    return new URL(page.url()).searchParams.get('code')!;
  };
  const exchange = (code: string) => page.request.post('/oauth/token',{form:{grant_type:'authorization_code',client_id:clientID,redirect_uri:oidcRedirectURI!,code,code_verifier:verifier}});
  const tokensResponse = await exchange(await authorize());
  expect(tokensResponse.status()).toBe(200);
  const tokens = await tokensResponse.json();
  expect(tokens.refresh_token).toBeTruthy();
  const pendingCode = await authorize();
  await page.goto('/account'); await page.getByTestId('account-authorizations').click();
  const row = page.locator(`[data-testid="authorization-row"][data-client-id="${clientID}"]`);
  await expect(row).toContainText('Revocable application');
  await expect(row.locator(`a[href="/users/${process.env.E2E_READER_ID}"]`)).toBeVisible();
  await expect(row).toContainText('email');
  for (const width of [1440,390]) {
    await page.setViewportSize({width,height:900});
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    if (process.env.SOLITUDES_ACCOUNT_SCREENSHOTS) await page.screenshot({path:path.join(process.env.SOLITUDES_ACCOUNT_SCREENSHOTS,`${siteTheme}-authorizations-${width}.png`),fullPage:true,animations:'disabled'});
  }
  page.once('dialog', dialog => dialog.dismiss());
  await row.getByTestId('authorization-revoke').click();
  await expect(row).toBeVisible();
  page.once('dialog', dialog => dialog.accept());
  await row.getByTestId('authorization-revoke').click();
  await expect(page.getByTestId('authorization-revoked')).toBeVisible();
  await expect(row).toHaveCount(0);
  expect((await exchange(pendingCode)).status()).toBe(400);
  expect((await page.request.get('/userinfo',{headers:{Authorization:'Bearer '+tokens.access_token}})).status()).toBe(403);
  expect((await page.request.post('/oauth/token',{form:{grant_type:'refresh_token',client_id:clientID,refresh_token:tokens.refresh_token}})).status()).toBe(400);
  const freshResponse = await exchange(await authorize());
  expect(freshResponse.status()).toBe(200);
  const fresh = await freshResponse.json();
  expect((await page.request.get('/userinfo',{headers:{Authorization:'Bearer '+fresh.access_token}})).status()).toBe(200);
  await page.goto('/account'); await page.getByTestId('account-logout').click();
  await signIn(page, readerEmail!, readerPassword!);
  await page.getByTestId('account-oidc-clients').click();
  const owned = page.getByTestId('account-client-row').filter({hasText:'Revocable application'});
  page.once('dialog', dialog => dialog.accept());
  await owned.getByTestId('oidc-client-delete').click();
  await expect(owned).toHaveCount(0);
  expect((await page.request.get('/userinfo',{headers:{Authorization:'Bearer '+fresh.access_token}})).status()).toBe(403);
});

const siteTheme = process.env.E2E_SITE_THEME || 'cactus';
const adminTheme = process.env.E2E_ADMIN_THEME || 'default';
const adminEmail = process.env.E2E_ADMIN_EMAIL;
const adminPassword = process.env.E2E_ADMIN_PASSWORD;
const readerEmail = process.env.E2E_READER_EMAIL;
const readerPassword = process.env.E2E_READER_PASSWORD;
const editorEmail = process.env.E2E_EDITOR_EMAIL;
const oidcClientID = process.env.E2E_OIDC_CLIENT_ID;
const oidcRedirectURI = process.env.E2E_OIDC_REDIRECT_URI;
const articleSlug = process.env.E2E_ARTICLE_SLUG;
const articleTitle = process.env.E2E_ARTICLE_TITLE;

test.beforeEach(async ({ page }) => {
  // Keep the test independent of the jQuery CDN. Production pages still use
  // their normal URL; only browser test requests receive this local fixture.
  await page.route('https://cdnjs.cloudflare.com/ajax/libs/jquery/3.3.1/jquery.min.js', route =>
    route.fulfill({ path: path.resolve('node_modules/jquery/dist/jquery.min.js'), contentType: 'application/javascript' })
  );
});

function articleShareTrigger(page: Page) {
  return page.getByTestId(siteTheme === 'cactus' && page.viewportSize()!.width >= 900 ? 'article-share-menu' : 'article-share');
}

async function signIn(page: Page, email = adminEmail!, password = adminPassword!, destination = /\/account(?:\/|\?|$)/) {
  await page.goto('/login', { waitUntil: 'domcontentloaded' });
  await page.addStyleTag({ content: '*, *::before, *::after { animation: none !important; transition: none !important; }' });
  await page.getByTestId('auth-email').fill(email);
  await page.getByTestId('auth-password').fill(password);
  await page.getByTestId('auth-captcha').fill('0');
  await expect(page.locator('input[name="captchaId"]')).not.toHaveValue('');
  // The isolated Go browser test enables SOLITUDES_E2E for the captcha.
  await page.getByTestId('auth-submit').click();
  await expect(page).toHaveURL(destination);
}

test('Folio editorial front page keeps unique stories above compact community', async ({ page }) => {
  test.skip(siteTheme !== 'folio');
  for (const width of [1280, 768, 390, 320]) {
    await page.setViewportSize({ width, height: 900 });
    for (const colorScheme of ['light', 'dark'] as const) {
      await page.emulateMedia({ colorScheme, reducedMotion: 'reduce' });
      expect((await page.goto('/'))?.status()).toBe(200);
      await expect(page.locator('main h1')).toHaveCount(1);
      const stories = page.getByTestId('folio-story');
      const slugs = await stories.evaluateAll(nodes => nodes.map(n => n.getAttribute('data-story-slug')));
      expect(slugs.length).toBeGreaterThan(1);
      expect(new Set(slugs).size).toBe(slugs.length);
      expect(slugs.length).toBeLessThanOrEqual(7);
      const recommendations = await page.getByTestId('folio-recommendation').evaluateAll(nodes => nodes.map(n => n.getAttribute('data-story-slug')));
      expect(recommendations).toHaveLength(2);
      expect(new Set(recommendations).size).toBe(2);
      expect(recommendations).not.toContain(slugs[0]);
      expect(await page.getByTestId('folio-latest').getByTestId('folio-story').count()).toBe(slugs.length - 1);
      const front = await page.getByTestId('folio-frontpage').boundingBox();
      const community = await page.getByTestId('folio-community').boundingBox();
      expect(community!.y).toBeGreaterThan(front!.y + front!.height);
      const columns = await page.getByTestId('folio-frontpage').evaluate(el => getComputedStyle(el).gridTemplateColumns.split(' ').length);
      expect(columns).toBe(width >= 768 ? 2 : 1);
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width + 1);
    }
  }
  await page.getByTestId('folio-frontpage').locator('h2 a').first().click();
  await expect(page.getByTestId('site-article')).toBeVisible();
  await page.goto(`/${articleSlug}`);
  await expect(page.getByTestId('article-toc')).toHaveCount(0);
  for (const link of await page.locator('.sibling-nav a').all()) {
    await expect(link.locator('.sibling-title')).not.toBeEmpty();
  }
  await page.goto('/visual-long-article');
  const toc = page.getByTestId('article-toc').locator('details');
  await expect(toc).not.toHaveAttribute('open');
  await toc.locator('summary').click();
  await expect(toc).toHaveAttribute('open', '');
  await expect(toc.locator('a')).toHaveCount(3);
  await expect(page.locator('.reading-content pre')).toHaveCSS('background-color', 'rgb(34, 39, 46)');
  await page.goto('/visual-page');
  await expect(page.getByTestId('comment-sign-in')).toBeVisible();
  await expect(page.getByTestId('comment-register')).toBeVisible();
  expect((await page.getByTestId('comment-sign-in').boundingBox())!.height).toBeGreaterThanOrEqual(44);
});

test('modern admin drawer traps focus, closes predictably and preserves appearance', async ({ page }) => {
  test.skip(!adminEmail, 'Requires isolated administrator.');
  await page.setViewportSize({ width: 390, height: 844 });
  await signIn(page);
  await page.goto('/admin/users');
  const sidebar = page.getByTestId('admin-sidebar');
  const toggle = page.getByTestId('admin-nav-toggle');
  await expect(sidebar).toBeHidden();
  await expect(page.locator('#admin-workspace')).not.toHaveAttribute('inert');
  await toggle.click();
  await expect(sidebar).toHaveAttribute('aria-modal', 'true');
  await expect(page.locator('#admin-workspace')).toHaveAttribute('inert', '');
  await expect(page.getByTestId('admin-nav-close')).toBeFocused();
  await sidebar.locator('button[type="submit"]').focus();
  await page.keyboard.press('Tab');
  await expect(sidebar.locator('.admin-logo')).toBeFocused();
  await page.keyboard.press('Shift+Tab');
  await expect(sidebar.locator('button[type="submit"]')).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(sidebar).toBeHidden();
  await expect(toggle).toBeFocused();
  await toggle.click();
  await page.locator('.sidebar-backdrop').click({ position: { x: 380, y: 400 } });
  await expect(sidebar).toBeHidden();
  await toggle.click();
  await page.getByTestId('admin-nav-providers').click();
  await expect(page).toHaveURL(/\/admin\/auth\/providers$/);
  await expect(sidebar).toBeHidden();
  await page.setViewportSize({ width: 1366, height: 900 });
  await expect(sidebar).toBeVisible();
  await expect(sidebar).not.toHaveAttribute('inert');
  await expect(page.getByTestId('admin-nav-providers')).toHaveAttribute('aria-current', 'page');
  await page.goto('/admin/users');
  await page.evaluate(() => { localStorage.setItem('solitudes_theme', 'light'); });
  await page.reload();
  await page.getByTestId('admin-theme-toggle').click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await expect(page.getByTestId('admin-theme-toggle')).toHaveAttribute('aria-pressed', 'true');
  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await page.getByTestId('admin-theme-toggle').click();
  await page.emulateMedia({ colorScheme: 'dark' });
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
});

test('modern admin page layouts stay usable across desktop tablet and phone', async ({ page }) => {
  test.skip(!adminEmail, 'Requires isolated administrator.');
  test.setTimeout(90000);
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  await signIn(page);
  for (const width of [1440, 1024, 768, 390]) {
    await page.setViewportSize({ width, height: 900 });
    for (const route of ['/admin/', '/admin/articles', '/admin/comments', '/admin/media', '/admin/tags',
      '/admin/publish', '/admin/users', '/admin/oidc/clients', '/admin/auth/providers', '/admin/audit', '/admin/settings']) {
      expect((await page.goto(route, { waitUntil: 'domcontentloaded' }))?.status(), route).toBe(200);
      await expect(page.locator('main h1')).toHaveCount(1);
      await expect(page.locator('#admin-current-page')).toHaveText(await page.locator('main h1').innerText());
      expect(await page.evaluate(() => document.documentElement.scrollWidth), route + ' at ' + width).toBeLessThanOrEqual(width + 1);
      if (route === '/admin/comments' && width >= 1200) {
        const author = await page.locator('.comment-table tbody tr').first().locator('td').nth(2).boundingBox();
        expect(author!.width, 'comment author metadata must not collapse into single-character columns').toBeGreaterThan(150);
      }
      if (width >= 1024) {
        const box = await page.locator('main').boundingBox();
        const nav = await page.getByTestId('admin-sidebar').boundingBox();
        expect(box!.x).toBeGreaterThanOrEqual(nav!.width);
      }
    }
    await expect(page.getByTestId('admin-theme-select').locator('input:checked')).toHaveValue(adminTheme);
  }
  expect(errors).toEqual([]);
});

test('admin theme selection remains available and is saved by the settings form', async ({ page }) => {
  test.skip(!adminEmail, 'Requires isolated administrator.');
  await signIn(page);
  await page.goto('/admin/settings');
  for (const id of ['inputNickname','inputEmail','inputOldPassword','inputNewPassword','inputConfirmPassword']) {
    await expect(page.locator('#'+id)).toHaveCount(0);
  }
  const selector = page.getByTestId('admin-theme-select');
  await expect(selector).toHaveAttribute('role', 'radiogroup');
  const choice = selector.locator(`input[value="${adminTheme}"]`);
  await expect(choice).toBeEnabled();
  await expect(choice).toBeChecked();
  await selector.locator(`label[for="theme-admin-${adminTheme}"]`).click();
  const saved = page.waitForResponse(response => response.url().endsWith('/admin/settings') && response.request().method() === 'POST');
  await page.getByTestId('admin-settings-save').click();
  const response = await saved;
  expect(response.status()).toBe(200);
  expect(response.request().postData()).toContain('name="admin_theme"');
  for (const field of ['nickname','email','old_password','new_password']) {
    expect(response.request().postData()).not.toContain(`name="${field}"`);
  }
  await page.waitForLoadState('domcontentloaded');
  await expect(page.getByTestId('admin-theme-select').locator('input:checked')).toHaveValue(adminTheme);
  await page.reload();
  await expect(page.getByTestId('admin-theme-select').locator('input:checked')).toHaveValue(adminTheme);
  await page.getByTestId('admin-account-nav').click();
  await expect(page).toHaveURL(/\/account$/);
  await expect(page.getByTestId('account-profile-nickname')).toBeVisible();
  await expect(page.getByTestId('account-new-password')).toBeVisible();
});

test('site and admin themes use the same accessible screenshot cards', async ({page}) => {
  test.skip(!adminEmail, 'Requires isolated administrator.');
  await signIn(page);
  if (process.env.SOLITUDES_CAPTURE_ADMIN_THEME === '1' && siteTheme === 'cactus') {
    await page.setViewportSize({width:1440,height:900});
    await page.goto('/admin/', {waitUntil:'networkidle'});
    await page.screenshot({path:path.resolve('../resource/themes/admin/default/screenshot.png'),animations:'disabled'});
  }
  await page.goto('/admin/settings');
  for (const kind of ['site','admin']) {
    const picker=page.getByTestId(`${kind}-theme-select`);
    await expect(picker).toHaveAttribute('role','radiogroup');
    expect(await picker.getByTestId('theme-card').count()).toBeGreaterThan(0);
    await expect(picker.locator('input:checked')).toHaveCount(1);
    for (const card of await picker.getByTestId('theme-card').all()) {
      await expect(card.locator('.theme-name')).not.toBeEmpty();
      await expect(card.locator('.theme-description')).not.toBeEmpty();
      await expect(card.locator('.theme-version')).not.toBeEmpty();
      await expect(card.locator('.theme-author')).not.toBeEmpty();
      await expect(card.getByTestId('theme-preview')).toHaveAttribute('src',new RegExp(`/admin/theme/preview/${kind}/`));
      await expect.poll(()=>card.getByTestId('theme-preview').evaluate((img:HTMLImageElement)=>img.naturalWidth)).toBeGreaterThan(0);
    }
  }
  const site=page.getByTestId('site-theme-select');
  const selected=site.locator('input:checked');
  const original=await selected.inputValue();
  await selected.focus();
  await page.keyboard.press('ArrowRight');
  await expect(site.locator('input:checked')).not.toHaveValue(original);
  await expect(page.getByTestId('admin-theme-select').locator('input:checked')).toHaveValue(adminTheme);
  await page.keyboard.press('ArrowLeft');
  await expect(site.locator('input:checked')).toHaveValue(original);
  // Missing screenshots use a local fallback, never an external image service.
  await page.route('**/admin/theme/preview/admin/**',route=>route.fulfill({status:404,body:''}));
  page.on('dialog',dialog=>dialog.accept());
  await page.reload();
  const admin=page.getByTestId('admin-theme-select');
  await expect(admin.getByTestId('theme-preview')).toBeHidden();
  await expect(admin.locator('.theme-preview-fallback')).toBeVisible();
  await page.unroute('**/admin/theme/preview/admin/**');
  await page.reload();
  for (const width of [1440,768,390]) {
    await page.setViewportSize({width,height:900});
    for (const appearance of ['light','dark']) {
      await page.evaluate(mode=>{
        localStorage.setItem('solitudes_theme',mode);
        document.documentElement.dataset.theme=mode;
      },appearance);
      expect(await page.evaluate(()=>document.documentElement.scrollWidth)).toBeLessThanOrEqual(width+1);
      if(process.env.SOLITUDES_THEME_SCREENSHOTS) {
        for (const kind of ['site','admin']) await page.locator(`[data-theme-kind="${kind}"]`).screenshot({path:path.join(process.env.SOLITUDES_THEME_SCREENSHOTS,`${siteTheme}-${kind}-${width}-${appearance}.png`),animations:'disabled'});
      }
    }
  }
});

test('modern admin navigation respects editor and reader roles', async ({ page }) => {
  test.skip(!editorEmail || !readerEmail, 'Requires isolated roles.');
  await signIn(page, editorEmail!, readerPassword!);
  await page.goto('/admin/articles');
  await expect(page.getByTestId('admin-nav-publish')).toBeVisible();
  await expect(page.getByTestId('admin-nav-identity-menu')).toHaveCount(0);
  await expect(page.locator('#admin-navigation a')).toHaveCount(2);
  await page.goto('/account');
  await page.getByTestId('account-logout').click();
  await signIn(page, readerEmail!, readerPassword!);
  expect((await page.goto('/admin/users'))?.status()).toBe(403);
  await expect(page.getByTestId('admin-nav-publish')).toHaveCount(0);
  await expect(page.getByTestId('admin-nav-identity-menu')).toHaveCount(0);
});

test.describe('Chinese administration', () => {
test.use({ locale: 'zh-CN' });
test('modern admin localization and active navigation follow each request', async ({ page }) => {
  test.skip(!adminEmail, 'Requires isolated administrator.');
  await signIn(page);
  for (const route of ['/admin', '/admin/users', '/admin/articles', '/admin/comments']) {
    const response = await page.goto(route);
    expect(response?.status()).toBe(200);
    expect((await response!.request().allHeaders())['accept-language']).toContain('zh');
    expect(await response!.text()).toContain('<html lang="zh"');
    await expect(page.locator('html')).toHaveAttribute('lang', 'zh');
    await expect(page.locator('#admin-navigation [aria-current="page"]')).toHaveAttribute('href', route);
  }
});
});

test('modern admin comments resolve registered authors without a guest nickname snapshot', async ({ page }) => {
  test.skip(!adminEmail, 'Requires isolated comments and accounts.');
  await signIn(page);
  await page.goto('/admin/comments');
  for (const [prefix, name, email] of [
    ['Admin topic reply:', 'Browser Admin', adminEmail],
    ['Editor topic reply:', 'Browser Editor', editorEmail],
    ['Member topic reply:', 'Browser Reader', readerEmail],
  ]) {
    const row = page.locator('.comment-table tbody tr').filter({ hasText: prefix! });
    await expect(row.getByTestId('admin-comment-author')).toHaveText(name!);
    await expect(row).toContainText(email!);
    await expect(row.getByTestId('admin-comment-author')).toHaveAttribute('href', /^\/admin\/users\/[^/]+$/);
  }
});

test('public metadata is canonical and private drafts cannot be indexed or cached', async ({ page }) => {
  await page.goto('/?utm_source=seo-check', { waitUntil: 'domcontentloaded' });
  // Custom home HTML and Markdown topics can contain their own headings.
  // Check the site-owned page title, without imposing limits on user content.
  await expect(page.locator(siteTheme === 'cactus' ? 'main > h1' : '.journal-masthead h1')).toHaveCount(1);
  await expect(page.locator('link[rel="canonical"]')).toHaveAttribute('href', /^https:\/\/[^/?]+\/$/);
  const structuredData = await page.locator('script[type="application/ld+json"]').allTextContents();
  expect(structuredData.length).toBeGreaterThan(0);
  for (const entry of structuredData) expect(() => JSON.parse(entry)).not.toThrow();
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.goto('/posts/', { waitUntil: 'domcontentloaded' });
  const postList = page.locator(siteTheme === 'cactus' ? '#posts .post-list' : '.year-group .article-list').first();
  expect((await postList.evaluate(node => getComputedStyle(node).gridTemplateColumns)).split(' ')).toHaveLength(2);
  expect(errors, 'single-page archive must not bind a missing pagination button').toEqual([]);
  await page.goto('/search/?w=article', { waitUntil: 'domcontentloaded' });
  await expect(page.locator('.search-result-list li').first()).toBeVisible();
  expect((await page.locator('.search-result-list').evaluate(node => getComputedStyle(node).gridTemplateColumns)).split(' ')).toHaveLength(2);
  await expect(page.locator('meta[name="robots"]')).toHaveAttribute('content', 'noindex');
  await page.goto('/visual-editor-post', { waitUntil: 'domcontentloaded' });
  await expect(page.locator('meta[property="article:author"]')).toHaveAttribute('content', /^https:\/\/[^/]+\/users\/[^/]+$/);
  for (const width of [768, 390]) {
    await page.setViewportSize({ width, height: 900 });
    for (const route of ['/', '/readers/']) {
      await page.goto(route, { waitUntil: 'domcontentloaded' });
      expect(await page.evaluate(() => document.documentElement.scrollWidth), `${route} at ${width}px`).toBeLessThanOrEqual(width + 1);
      if (route === '/readers/' && width === 768) {
        expect((await page.locator('.reader-circle-layout').evaluate(node => getComputedStyle(node).gridTemplateColumns)).split(' ')).toHaveLength(2);
      }
      if (route === '/' && width === 768) {
        expect((await page.locator(siteTheme === 'cactus' ? '.home-grid-layout' : '.home-grid').evaluate(node => getComputedStyle(node).gridTemplateColumns)).split(' ')).toHaveLength(2);
      }
    }
  }
  test.skip(!adminEmail || !adminPassword, 'Requires isolated private article and administrator fixtures.');
  expect((await page.request.get('/e2e-private-profile-draft')).status()).toBe(404);
  await signIn(page);
  const privateResponse = await page.goto('/e2e-private-profile-draft', { waitUntil: 'domcontentloaded' });
  expect(privateResponse?.status()).toBe(200);
  expect(privateResponse?.headers()['x-robots-tag']).toBe('noindex, nofollow');
  expect(privateResponse?.headers()['cache-control']).toBe('private, no-store');
  await expect(page.locator('meta[name="robots"]')).toHaveAttribute('content', 'noindex');
  await expect(page.locator('link[rel="canonical"]')).toHaveCount(0);
});

test('guest can find login, registration, and resend controls', async ({ page }) => {
  const login = await page.goto('/login', { waitUntil: 'domcontentloaded' });
  await page.addStyleTag({ content: '*, *::before, *::after { animation: none !important; transition: none !important; }' });
  expect(login?.status()).toBe(200);
  await expect(page.getByTestId('site-auth-page')).toBeVisible();
  await expect(page.locator('meta[name="robots"]')).toHaveAttribute('content', 'noindex');
  for (const route of ['/admin/login', '/admin/register', '/admin/verify-email', '/admin/resend-verification']) {
    expect((await page.request.get(route, { maxRedirects: 0 })).status(), `GET ${route}`).toBe(404);
    expect((await page.request.post(route, {
      headers: { Origin: new URL(page.url()).origin }, maxRedirects: 0,
      form: { email: 'unused@example.test', password: 'not-a-password' },
    })).status(), `POST ${route}`).toBe(404);
  }
  await expect(page.getByTestId('oidc-application-info')).toHaveCount(0);
  for (const id of ['auth-login-form', 'auth-email', 'auth-password', 'auth-captcha', 'auth-submit']) {
    await expect(page.getByTestId(id)).toBeVisible();
  }
  await page.goto('/login?return_to=' + encodeURIComponent('/oidc/consent?authRequestID=not-a-real-request'));
  await page.addStyleTag({ content: '*, *::before, *::after { animation: none !important; transition: none !important; }' });
  await expect(page.getByTestId('oidc-application-info')).toHaveCount(0);
  await page.getByTestId('auth-register-link').click();
  await expect(page).toHaveURL(/\/register\?return_to=/);
  await expect(page.getByTestId('register-reader-circle-notice')).toBeVisible();
  expect(new URL(page.url()).searchParams.get('return_to')).toBe('/oidc/consent?authRequestID=not-a-real-request');
  await expect(page.locator('input[name="return_to"]').first()).toHaveValue('/oidc/consent?authRequestID=not-a-real-request');
  await expect(page.getByTestId('site-auth-page')).toBeVisible();
  await page.locator('.auth-resend summary').click();
  for (const id of ['register-form', 'register-email', 'register-nickname', 'register-password',
    'register-captcha', 'register-submit', 'resend-form', 'resend-email', 'resend-captcha', 'resend-submit']) {
    await expect(page.getByTestId(id)).toBeVisible();
  }
});

test('registration delivers a verification email before a new reader can sign in', async ({ page }) => {
  test.skip(!adminEmail || !oidcRedirectURI, 'Requires the in-process SMTP catcher and isolated browser server.');
  const email = `new-reader-${siteTheme}-${adminTheme}-${Date.now()}@example.com`;
  const nickname = `New Browser Reader ${siteTheme}-${adminTheme}`;
  const password = 'browser-registration-password';
  await page.goto('/register?return_to=%2Faccount', { waitUntil: 'domcontentloaded' });
  await page.addStyleTag({ content: '*, *::before, *::after { animation: none !important; transition: none !important; }' });
  await page.getByTestId('register-email').fill(email);
  await page.getByTestId('register-nickname').fill(nickname);
  await page.getByTestId('register-password').fill(password);
  await page.getByTestId('register-captcha').fill('0');
  await expect(page.locator('input[name="captchaId"]').first()).not.toHaveValue('');
  const registered = page.waitForResponse(response => response.url().endsWith('/register') && response.request().method() === 'POST');
  await page.getByTestId('register-submit').click();
  expect((await registered).status()).toBe(202);
  await page.goto('/login', { waitUntil: 'domcontentloaded' });
  await page.addStyleTag({ content: '*, *::before, *::after { animation: none !important; transition: none !important; }' });
  await page.getByTestId('auth-email').fill(email);
  await page.getByTestId('auth-password').fill(password);
  await page.getByTestId('auth-captcha').fill('0');
  await expect(page.locator('input[name="captchaId"]')).not.toHaveValue('');
  const denied = page.waitForResponse(response => response.url().endsWith('/login') && response.request().method() === 'POST');
  await page.getByTestId('auth-submit').click();
  const deniedResponse = await denied;
  expect(deniedResponse.status()).toBe(403);
  let link = '';
  await expect.poll(async () => {
    const response = await page.request.get('/__e2e/verification-mail');
    if (response.status() === 200) link = await response.text();
    return link;
  }).toMatch(/^http:\/\/localhost:\d+\/verify-email\?token=[a-f0-9]{64}&return_to=%2Faccount$/);
  await page.goto(link, { waitUntil: 'domcontentloaded' });
  await expect(page).toHaveURL(/\/login\?verified=1&return_to=%2Faccount$/);
  await signIn(page, email, password, /\/account(?:\?|$)/);
  await expect(page.getByTestId('account-logout')).toBeVisible();
  await expect(page.getByTestId('account-directory-listed')).toBeChecked();
  await page.goto('/readers/', { waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('reader-circle-latest').filter({ has: page.getByRole('link', { name: nickname, exact: true }) })).toHaveCount(1);
  await expect(page.locator('body')).not.toContainText(email);
  await page.goto('/account', { waitUntil: 'domcontentloaded' });
  const ownerProfile = await page.getByTestId('account-public-profile').getAttribute('href');
  const emptyProfile = await page.request.get(ownerProfile!);
  expect(emptyProfile.headers()['x-robots-tag']).toBe('noindex, follow');
  await page.goto(ownerProfile!, { waitUntil: 'domcontentloaded' });
  await expect(page.locator('meta[name="robots"]')).toHaveAttribute('content', 'noindex');
  await expect(page.locator('link[rel="canonical"]')).toHaveCount(0);
  await page.goto('/account', { waitUntil: 'domcontentloaded' });
  await page.getByTestId('account-profile-bio').fill('An introduction, but no public activity yet.');
  await page.getByTestId('account-profile-save').click();
  await page.goto(ownerProfile!, { waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('public-profile-bio')).toContainText('An introduction');
  await expect(page.locator('meta[name="robots"]')).toHaveAttribute('content', 'noindex');
  await page.goto('/account', { waitUntil: 'domcontentloaded' });
  await page.getByTestId('account-oidc-clients').click();
  const invalidHomepage = await page.request.post('/account/oidc/clients', {
    headers: { Origin: new URL(page.url()).origin }, maxRedirects: 0,
    form: { name: 'Unsafe app', homepage_url: 'javascript:alert(1)', redirect_uris: oidcRedirectURI! },
  });
  expect(invalidHomepage.status()).toBe(400);
  const invalidLogout = await page.request.post('/account/oidc/clients', {
    headers: { Origin: new URL(page.url()).origin }, maxRedirects: 0,
    form: { name: 'Unsafe logout', homepage_url: 'https://reader.example.test/', redirect_uris: oidcRedirectURI!,
      post_logout_uris: 'https://reader.example.test/logout#fragment' },
  });
  expect(invalidLogout.status()).toBe(400);
  await page.getByTestId('oidc-client-name').fill('New reader application');
  await page.getByTestId('oidc-client-description').fill('Use the blog identity to read articles');
  await page.getByTestId('oidc-client-homepage').fill('https://reader.example.test/app');
  await page.getByTestId('oidc-client-redirects').fill(oidcRedirectURI!);
  await page.getByTestId('oidc-client-logout-uris').fill('https://reader.example.test/signed-out');
  await page.getByTestId('oidc-client-public').check();
  await page.getByTestId('oidc-client-submit').click();
  const newClientID = (await page.getByTestId('oidc-created-id').textContent())!;
  expect(newClientID).toBeTruthy();

  await page.goto('/account');
  await page.getByTestId('account-logout').click();
  const verifier = 'v'.repeat(64);
  const query = new URLSearchParams({ client_id: newClientID, redirect_uri: oidcRedirectURI!,
    response_type: 'code', scope: 'openid email', state: 'new-reader',
    code_challenge: createHash('sha256').update(verifier).digest('base64url'), code_challenge_method: 'S256' });
  await page.goto('/authorize?' + query);
  await expect(page).toHaveURL(/\/login\?return_to=/);
  expect((await page.request.get(page.url())).headers()['cache-control']).toContain('no-store');
  await page.addStyleTag({ content: '*, *::before, *::after { animation: none !important; transition: none !important; }' });
  await expect(page.getByTestId('oidc-application-name')).toHaveText('New reader application');
  await expect(page.getByTestId('oidc-application-description')).toContainText('Use the blog identity');
  await expect(page.getByTestId('oidc-application-owner')).toHaveAttribute('href', ownerProfile!);
  await expect(page.getByTestId('oidc-application-homepage')).toHaveAttribute('href', 'https://reader.example.test/app');
  await expect(page.getByTestId('oidc-application-owner')).toHaveAttribute('target', '_blank');
  await expect(page.getByTestId('oidc-application-homepage')).toHaveAttribute('rel', /noopener noreferrer/);
  await page.getByTestId('auth-email').fill(email);
  await page.getByTestId('auth-password').fill(password);
  await page.getByTestId('auth-captcha').fill('0');
  await expect(page.locator('input[name="captchaId"]')).not.toHaveValue('');
  await page.getByTestId('auth-submit').click();
  await expect(page).toHaveURL(/\/oidc\/consent\?/);
  expect((await page.request.get(page.url())).headers()['cache-control']).toContain('no-store');
  await page.addStyleTag({ content: '*, *::before, *::after { animation: none !important; transition: none !important; }' });
  await expect(page.getByTestId('oidc-application-owner')).toHaveAttribute('href', ownerProfile!);
  await expect(page.getByTestId('oidc-application-homepage')).toHaveAttribute('href', 'https://reader.example.test/app');
  await expect(page.getByTestId('oidc-requested-scopes')).toContainText('Verify your blog identity');
  await page.getByTestId('oidc-consent-allow').click();
  const code = new URL(page.url()).searchParams.get('code');
  expect(code).toBeTruthy();
  const token = await page.request.post('/oauth/token', { form: {
    grant_type: 'authorization_code', client_id: newClientID, redirect_uri: oidcRedirectURI!,
    code: code!, code_verifier: verifier,
  } });
  expect(token.status()).toBe(200);
  const userInfo = await page.request.get('/userinfo', {
    headers: { Authorization: `Bearer ${(await token.json()).access_token}` },
  });
  expect((await userInfo.json()).email).toBe(email);
});

test('site navigation and search form work in both site themes', async ({ page }) => {
  const home = await page.goto('/', { waitUntil: 'domcontentloaded' });
  expect(home?.status()).toBe(200);
  await expect(page.locator('footer')).not.toContainText('_BuildVersion_');
  await expect(page.getByTestId('site-account-nav')).toHaveAttribute('href', '/login');
  if (siteTheme === 'folio') await expect(page.getByTestId('site-account-nav-mobile')).toHaveAttribute('href', '/login');
  // The front page intentionally contains a bounded, newest-first selection;
  // publishing another fixture may push the original story into the archive.
  await expect(page.locator(siteTheme === 'folio' ? '[data-testid="folio-story"]' : '.home-articles-section .post-title-row').first()).toBeVisible();
  await expect(page.locator(`link[href*="/static/site/${siteTheme}/"]`).first()).toHaveCount(1);
  for (const route of ['/posts/', '/books/', '/tags/']) {
    expect((await page.goto(route, { waitUntil: 'domcontentloaded' }))?.status()).toBe(200);
  }
  await page.goto('/search/', { waitUntil: 'domcontentloaded' });
  await page.getByTestId('site-search-input').fill('Solitudes');
  await page.getByTestId('site-search-submit').click();
  await expect(page).toHaveURL(/\/search\/\?w=Solitudes/);
  await expect(page.getByTestId('site-search-input')).toHaveValue('Solitudes');
  expect((await page.goto('/does-not-exist-for-e2e', { waitUntil: 'domcontentloaded' }))?.status()).toBe(404);
});

test('public pages load local assets without script errors and redirect safely', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  for (const route of ['/', '/posts/', '/books/', '/tags/', '/search/', '/visual-page', '/does-not-exist-for-e2e']) {
    const response = await page.goto(route, { waitUntil: 'load' });
    expect(response?.status()).toBe(route.includes('does-not-exist') ? 404 : 200);
    await expect(page.locator('#main-content')).toBeVisible();
    const assets = await page.locator('link[rel="stylesheet"][href^="/static/"], script[src^="/static/"]').evaluateAll(nodes =>
      nodes.map(node => node.getAttribute('href') || node.getAttribute('src')!)
    );
    expect(assets.length).toBeGreaterThan(0);
    for (const asset of assets) expect((await page.request.get(asset)).status(), asset).toBe(200);
  }
  // Keep the external target local to the test: do not navigate to another site.
  const target = 'https://external.example.test/read?from=blog';
  await page.goto('/r/go?url=' + Buffer.from(target).toString('base64url'));
  await expect(page.locator('#continue-link')).toHaveAttribute('href', target);
  await expect(page.locator('#target-url')).toHaveText(target);
  await page.goto('/r/go?url=' + Buffer.from('javascript:alert(1)').toString('base64url'));
  await expect(page.locator('#continue-link')).toHaveCount(0);
  await expect(page.locator('#redirect-container')).toContainText('Invalid URL');
  expect((await page.goto('/r/go'))?.status()).toBe(400);
  await expect(page.locator('body')).toHaveText('Missing url parameter');
  expect(errors).toEqual([]);
});

test('front account entry remains reachable on mobile', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/', { waitUntil: 'domcontentloaded' });
  if (siteTheme === 'folio') {
    await page.locator('#folio-menu-toggle').click();
    await page.getByTestId('site-account-nav-mobile').click();
  } else {
    await page.locator('#cactus-navigation .icon button').click();
    await page.getByTestId('site-account-nav').click();
  }
  await expect(page).toHaveURL(/\/login$/);
  if (siteTheme === 'cactus' && articleSlug) {
    await page.goto('/' + articleSlug, { waitUntil: 'domcontentloaded' });
    await page.locator('#menu-footer').click();
    await expect(page.getByTestId('site-account-nav-mobile')).toBeVisible();
    await expect(page.locator('#menu-footer')).toHaveAttribute('aria-expanded', 'true');
  }
});

test('post listings remain visible when motion is disabled', async ({ page }) => {
  test.skip(!articleTitle, 'Requires isolated article fixture.');
  await page.addInitScript(() => {
    const style = document.createElement('style');
    style.textContent = '*, *::before, *::after { animation: none !important; transition: none !important; }';
    document.addEventListener('DOMContentLoaded', () => document.head.append(style), { once: true });
  });
  for (const route of ['/', '/posts/']) {
    await page.goto(route, { waitUntil: 'domcontentloaded' });
    const article = route === '/' ? page.locator(siteTheme === 'folio' ? '[data-testid="folio-story"]' : '.home-articles-section .post-title-row').first() : page.getByText(articleTitle!, { exact: true }).first();
    await expect(article).toBeVisible();
    if (siteTheme === 'folio' && await page.locator('.article-list-item').count()) {
      await expect(page.locator('.article-list-item').first()).toHaveCSS('opacity', '1');
    }
  }
});

test('mobile admin tables expose actions without horizontal page overflow', async ({ page }) => {
  test.skip(!adminEmail || !adminPassword, 'Requires isolated admin fixture.');
  await page.setViewportSize({ width: 390, height: 844 });
  await signIn(page);
  for (const [route, action] of [['/admin/articles', 'article-edit'], ['/admin/users', 'user-role-save']] as const) {
    await page.goto(route, { waitUntil: 'domcontentloaded' });
    await expect(page.getByTestId(action).first()).toBeVisible();
    const width = await page.evaluate(() => document.documentElement.scrollWidth);
    expect(width, `${adminTheme} ${route} has horizontal overflow`).toBeLessThanOrEqual(394);
    const box = await page.getByTestId(action).first().boundingBox();
    expect(box!.x + box!.width).toBeLessThanOrEqual(390);
  }
  await page.goto('/admin/articles', { waitUntil: 'domcontentloaded' });
  const toggle = page.locator('.admin-mobile-toggle');
  await toggle.click();
  const nav = page.locator('#admin-navigation');
  await expect(nav).toBeVisible();
  await expect(nav).toHaveCSS('background-color', /^(?!rgba\(0, 0, 0, 0\))[^\n]+$/);
  if (adminTheme === 'default') await expect(page.getByTestId('admin-account-nav')).toBeVisible();
});

test('restricted pages explain access and return each role to a reachable destination', async ({ page }) => {
  test.skip(!editorEmail || !readerEmail || !readerPassword, 'Requires isolated roles.');
  await signIn(page, editorEmail!, readerPassword!);
  expect((await page.goto('/admin/users', { waitUntil: 'domcontentloaded' }))?.status()).toBe(403);
  await expect(page.locator('body')).toContainText('403 Access denied');
  await expect(page.locator('body')).not.toContainText('administrator required');
  await expect(page.getByTestId('error-recovery-link')).toHaveAttribute('href', '/admin/articles');
  await page.getByTestId('error-recovery-link').click();
  await expect(page).toHaveURL(/\/admin\/articles$/);
  await page.goto('/account', { waitUntil: 'domcontentloaded' });
  await page.getByTestId('account-logout').click();

  await signIn(page, readerEmail!, readerPassword!, /\/account/);
  expect((await page.goto('/admin/publish', { waitUntil: 'domcontentloaded' }))?.status()).toBe(403);
  await expect(page.locator('body')).toContainText('403 Access denied');
  await expect(page.getByTestId('error-recovery-link')).toHaveAttribute('href', '/account');
  await page.getByTestId('error-recovery-link').click();
  await expect(page).toHaveURL(/\/account$/);
});

test('article and page editing affordances match the server authorization for all roles', async ({ page }) => {
  test.skip(!articleSlug || !editorEmail || !readerEmail || !readerPassword || !adminEmail,
    'Requires isolated admin, editor, and reader fixtures.');
  const edit = page.locator('[data-testid="article-edit-link"], [data-testid="page-edit-link"]');
  for (const slug of [articleSlug!, 'visual-editor-post', 'visual-page']) {
    await page.goto('/' + slug, { waitUntil: 'domcontentloaded' });
    await expect(edit).toHaveCount(0);
  }

  await signIn(page, readerEmail!, readerPassword!);
  for (const slug of [articleSlug!, 'visual-editor-post', 'visual-page']) {
    await page.goto('/' + slug, { waitUntil: 'domcontentloaded' });
    await expect(edit).toHaveCount(0);
  }
  await page.goto('/account');
  await page.getByTestId('account-logout').click();

  await signIn(page, editorEmail!, readerPassword!);
  for (const slug of [articleSlug!, 'visual-page']) {
    await page.goto('/' + slug, { waitUntil: 'domcontentloaded' });
    await expect(edit).toHaveCount(0);
  }
  await page.goto('/visual-editor-post', { waitUntil: 'domcontentloaded' });
  await expect(edit).toBeVisible();
  const ownEditURL = await edit.getAttribute('href');
  expect(ownEditURL).toMatch(/^\/admin\/publish\?id=/);
  expect((await page.goto(ownEditURL!, { waitUntil: 'domcontentloaded' }))?.status()).toBe(200);
  await page.goto('/account');
  await page.getByTestId('account-logout').click();

  await signIn(page);
  for (const slug of [articleSlug!, 'visual-editor-post', 'visual-page']) {
    await page.goto('/' + slug, { waitUntil: 'domcontentloaded' });
    await expect(edit).toBeVisible();
  }
});

test('account identity separates name, email and translated role on narrow screens', async ({ page }) => {
  test.skip(!adminEmail || !editorEmail || !readerEmail || !readerPassword,
    'Requires isolated admin, editor and member accounts.');
  await page.setViewportSize({ width: 390, height: 844 });
  for (const [email, label] of [[readerEmail!, 'Member'], [editorEmail!, 'Editor'], [adminEmail!, 'Administrator']] as const) {
    await signIn(page, email, email === adminEmail ? adminPassword! : readerPassword!);
    await page.goto('/account', { waitUntil: 'domcontentloaded' });
    await expect(page.getByTestId('account-role')).toHaveText(label);
    await expect(page.locator('.account-summary-email')).toHaveText(email);
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(394);
    await page.getByTestId('account-logout').click();
  }
});

test('home reader names flow at natural widths and truncate without wrapping role badges', async ({ browser }) => {
  const names = ['这是一位昵称很长很长的读者也喜欢读书交流', 'AnExtremelyLongUnbrokenReaderNickname', 'A reader with a very long display name 🌱'].map(name=>name.repeat(4));
  for (const language of ['zh-CN', 'en']) {
    const context = await browser.newContext({locale:language,baseURL:process.env.E2E_BASE_URL || 'http://localhost:8080'});
    const page = await context.newPage();
    try {
    for (const width of [320,390,768,1280]) {
      await page.setViewportSize({width,height:900});
      expect((await page.goto('/', {waitUntil:'domcontentloaded'}))?.status()).toBe(200);
      const links = page.getByTestId('reader-circle-member');
      expect(await links.count()).toBeGreaterThan(0);
      await expect(page.locator('.reader-home-list .comment-role').first()).toHaveText(language==='zh-CN' ? /管理员|编辑|注册会员/ : /Administrator|Editor|Member/);
      // Stress just the layout with long names; do not mutate shared accounts.
      for (let i=0;i<await links.count();i++) {
        const link=links.nth(i);
        await expect(link).toHaveAttribute('title', (await link.textContent())!);
        await link.evaluate((element,name) => {element.textContent=name; element.setAttribute('title',name);}, names[i%names.length]!);
      }
      const rows=await page.locator('.reader-home-list li').evaluateAll(elements=>elements.map(row=>{
        const link=row.querySelector('a')!;
        const role=row.querySelector('.comment-role')!;
        const a=link.getBoundingClientRect(), b=role.getBoundingClientRect(), r=row.getBoundingClientRect();
        return {nameWidth:a.width,centerDelta:Math.abs((a.top+a.bottom-b.top-b.bottom)/2),
          badgeRight:b.right,rowRight:r.right,overflow:link.scrollWidth>link.clientWidth,
          ellipsis:getComputedStyle(link).textOverflow,shrink:getComputedStyle(role).flexShrink};
      }));
      for (const row of rows) {
        expect(row.nameWidth).toBeGreaterThan(0);
        expect(row.centerDelta).toBeLessThan(1);
        expect(row.badgeRight).toBeLessThanOrEqual(row.rowRight+1);
        expect(row.overflow).toBe(true);
        expect(row.ellipsis).toBe('ellipsis');
        expect(row.shrink).toBe('0');
      }
      expect(await page.evaluate(()=>document.documentElement.scrollWidth)).toBeLessThanOrEqual(width+1);
      // Short names and their badges form compact units, not stretched columns.
      for (let i=0;i<await links.count();i++) {
        const name=['叶','Ada','认真读书也喜欢交流的朋友',names[0]!][i%4]!;
        await links.nth(i).evaluate((element,value)=>{element.textContent=value; element.setAttribute('title',value);},name);
      }
      const list=page.locator('.reader-home-list');
      await expect(list).toHaveCSS('display','flex');
      await expect(list).toHaveCSS('flex-wrap','wrap');
      const compact=await list.locator('li').evaluateAll(elements=>elements.slice(0,2).map(row=>{
        const link=row.querySelector('a')!, badge=row.querySelector('.comment-role')!;
        const range=document.createRange(); range.selectNodeContents(link);
        const text=range.getBoundingClientRect(), a=link.getBoundingClientRect(), b=badge.getBoundingClientRect(), r=row.getBoundingClientRect();
        return {textWidth:text.width,nameWidth:a.width,badgeGap:b.x-a.right,rightGap:r.right-b.right,x:r.x,y:r.y,width:r.width};
      }));
      for (const item of compact) {
        expect(Math.abs(item.nameWidth-item.textWidth),'short name is not stretched').toBeLessThan(1);
        expect(item.badgeGap).toBeGreaterThan(0);
        expect(item.badgeGap).toBeLessThanOrEqual(8);
        expect(Math.abs(item.rightGap),'no trailing empty column').toBeLessThan(1);
      }
      const listWidth=(await list.boundingBox())!.width;
      if (compact.length===2 && compact[0]!.width+compact[1]!.width+16<=listWidth) {
        expect(compact[0]!.y,'short entries pack into the same row when they fit').toBe(compact[1]!.y);
      }
      expect(await page.evaluate(()=>document.documentElement.scrollWidth)).toBeLessThanOrEqual(width+1);
      if (process.env.SOLITUDES_READER_SCREENSHOTS && language==='zh-CN' && [390,1280].includes(width)) {
        await page.getByTestId('reader-circle-home').screenshot({path:path.join(process.env.SOLITUDES_READER_SCREENSHOTS,`${siteTheme}-${width}.png`),animations:'disabled'});
      }
    }
    } finally { await context.close(); }
  }
});

test('reader circle defaults to visible and supports opt-out and rejoin without revealing email', async ({ page }) => {
  test.skip(!readerEmail || !readerPassword || !editorEmail || !adminEmail || !adminPassword,
    'Requires isolated reader, editor, and admin fixtures.');
  const readerCard = () => page.getByTestId('reader-circle-latest').filter({
    has: page.getByRole('link', { name: 'Browser Reader', exact: true }),
  });
  await page.goto('/', { waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('reader-circle-home')).toBeVisible();
  await expect(page.getByTestId('reader-circle-home')).toContainText('newest readers');
  await expect(page.getByTestId('reader-circle-more')).toHaveAttribute('href', '/readers/');
  await expect(page.getByTestId('reader-circle-more')).toHaveText(siteTheme === 'folio' ? /View [Mm]ore\s*→/ : /View [Mm]ore\s*➤/);
  const homeHeading = page.getByTestId('reader-circle-home').locator('.home-community-heading');
  const homeTitle = await homeHeading.locator('a').first().boundingBox();
  const homeMore = await page.getByTestId('reader-circle-more').boundingBox();
  expect(homeTitle && homeMore && homeMore.x - homeTitle.x - homeTitle.width).toBeGreaterThan(8);
  await expect(page.locator('.reader-home-list')).toHaveCSS('flex-wrap','wrap');
  if (siteTheme === 'cactus') {
    const search = await page.locator('.home-search-section').boundingBox();
    const writing = await page.locator('.home-articles-section').boundingBox();
    expect(search && writing && writing.y - search.y - search.height, 'writing follows search without a fixed empty grid row').toBeLessThan(70);
    if (await page.locator('.home-topics-section').count()) {
      await expect(page.locator('.home-topics-section')).toHaveCSS('max-height', 'none');
    }
  }
  await expect(page.getByTestId('site-reader-circle-nav')).toHaveAttribute('href', '/readers/');
  await expect(page.getByTestId('site-manage-nav')).toHaveCount(0);
  await page.getByTestId('site-reader-circle-nav').click();
  await expect(page).toHaveURL(/\/readers\/$/);
  await expect(page.getByTestId('reader-circle-activity').first()).toBeVisible();
  expect((await page.locator('.reader-circle-layout').evaluate(node => getComputedStyle(node).gridTemplateColumns)).split(' ')).toHaveLength(2);
  const layoutBox = await page.locator('.reader-circle-layout').boundingBox();
  const feedBox = await page.locator('.circle-feed').boundingBox();
  expect(layoutBox && feedBox && Math.abs(layoutBox.width - feedBox.width), 'activity uses the full available width').toBeLessThan(2);
  const firstActivity = await page.getByTestId('reader-circle-activity').nth(0).boundingBox();
  const secondActivity = await page.getByTestId('reader-circle-activity').nth(1).boundingBox();
  expect(firstActivity && secondActivity && Math.abs(firstActivity.y - secondActivity.y), 'activity cards share one row').toBeLessThan(2);
  const activeSection = await page.locator('section[aria-labelledby="circle-active-title"]').boundingBox();
  const newSection = await page.locator('section[aria-labelledby="circle-new-title"]').boundingBox();
  expect(activeSection && newSection && Math.abs(activeSection.y - newSection.y), 'active and new members share one row').toBeLessThan(2);
  await expect(page.locator('body')).not.toContainText('Spam comment content');
  await expect(page.locator('body')).not.toContainText('Private profile draft');
  await expect(page.getByTestId('reader-circle-active').filter({ hasText: 'Browser Editor' })).toHaveCount(1);
  await expect(readerCard()).toHaveCount(1);
  await expect(page.locator('body')).not.toContainText(editorEmail!);
  await expect(page.locator('body')).not.toContainText(readerEmail!);
  expect((await page.goto('/readers/?page=1001', { waitUntil: 'domcontentloaded' }))?.status()).toBe(400);

  await page.goto('/visual-editor-post', { waitUntil: 'domcontentloaded' });
  const guestText = `Guest home conversation ${Date.now()}`;
  const guestComment = await page.request.post('/api/comment', {
    headers: { Origin: new URL(page.url()).origin },
    data: { nickname: 'Guest Neighbour', email: 'private-guest@example.test', content: guestText,
      version: 1, slug: 'visual-editor-post' },
  });
  expect(guestComment.status()).toBe(200);
  await page.goto('/', { waitUntil: 'domcontentloaded' });
  const guestHomeComment = page.getByTestId('home-recent-comment').filter({ hasText: guestText });
  await expect(guestHomeComment).toHaveCount(1);
  await expect(guestHomeComment.getByTestId('home-comment-link')).toHaveAttribute('href', /^\/visual-editor-post\?thread=[^#]+#comment-/);
  await expect(guestHomeComment.getByTestId('home-comment-author')).toHaveCount(0);
  await expect(guestHomeComment).toContainText('Guest Neighbour');
  await expect(page.getByTestId('home-recent-comments')).not.toContainText('private-guest@example.test');
  const primary = page.locator(siteTheme === 'cactus' ? '.home-primary' : '.journal-community .home-topics-section');
  const secondary = page.locator(siteTheme === 'cactus' ? '.home-secondary' : '.journal-community-side');
  await expect(primary.getByTestId('topic-entry').first()).toBeVisible();
  await expect(secondary.getByTestId('home-recent-comments')).toBeVisible();
  const primaryBox = (await primary.boundingBox())!;
  const secondaryBox = (await secondary.boundingBox())!;
  expect(primaryBox.x).toBeLessThan(secondaryBox.x);
  expect(primaryBox.width).toBeGreaterThan(secondaryBox.width * 1.5);
  expect(await guestHomeComment.getByTestId('home-comment-link').evaluate(node => parseFloat(getComputedStyle(node).fontSize))).toBeLessThanOrEqual(14);

  await signIn(page, readerEmail!, readerPassword!);
  await expect(page.getByTestId('site-manage-nav')).toHaveCount(0);
  await page.goto('/visual-editor-post', { waitUntil: 'domcontentloaded' });
  const commentText = `<img src=x onerror=alert(1)> Reader circle test conversation ${Date.now()}`;
  const posted = await page.request.post('/api/comment', {
    headers: { Origin: new URL(page.url()).origin },
    data: { content: commentText, version: 1, slug: 'visual-editor-post' },
  });
  expect(posted.status()).toBe(200);
  const readerActivity = () => page.getByTestId('reader-circle-activity').filter({ hasText: commentText });
  await page.goto('/readers/', { waitUntil: 'domcontentloaded' });
  await expect(readerActivity()).toHaveCount(1);
  await expect(readerActivity().locator('img')).toHaveCount(0);
  await expect(readerActivity().getByTestId('reader-circle-activity-author')).toHaveAttribute('href', /^\/users\//);
  await expect(readerActivity().getByTestId('reader-circle-activity-link')).toHaveAttribute('href', /^\/visual-editor-post\?thread=[^#]+#comment-/);
  await page.goto('/', { waitUntil: 'domcontentloaded' });
  const memberHomeComment = page.getByTestId('home-recent-comment').filter({ hasText: commentText });
  await expect(memberHomeComment).toHaveCount(1);
  await expect(memberHomeComment.locator('img')).toHaveCount(0);
  await expect(memberHomeComment.getByTestId('home-comment-author')).toHaveAttribute('href', /^\/users\//);
  await expect(memberHomeComment.getByTestId('home-comment-link')).toHaveAttribute('href', /^\/visual-editor-post\?thread=[^#]+#comment-/);
  await expect(page.getByTestId('home-recent-comments')).not.toContainText(readerEmail!);
  await page.goto('/account', { waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('account-workspace')).toHaveCount(0);
  await expect(page.getByTestId('account-directory-listed')).toBeChecked();
  await page.getByTestId('account-profile-bio').fill('I like talking about good books.');
  await page.getByTestId('account-profile-save').click();
  await expect(page.getByTestId('account-directory-listed')).toBeChecked();
  await page.goto('/readers/', { waitUntil: 'domcontentloaded' });
  await expect(readerCard()).toContainText('good books');
  await expect(page.locator('body')).not.toContainText(readerEmail!);
  await page.goto('/account');
  await page.getByTestId('account-directory-listed').uncheck();
  await page.getByTestId('account-profile-save').click();
  await page.goto('/readers/', { waitUntil: 'domcontentloaded' });
  await expect(readerCard()).toHaveCount(0);
  await expect(readerActivity()).toHaveCount(0);
  await expect(page.getByTestId('reader-circle-join')).toHaveAttribute('href', '/account');
  await page.goto('/account');
  await expect(page.getByTestId('account-directory-listed')).not.toBeChecked();
  await page.getByTestId('account-directory-listed').check();
  await page.getByTestId('account-profile-save').click();
  await page.goto('/readers/', { waitUntil: 'domcontentloaded' });
  await expect(readerCard()).toHaveCount(1);
  await expect(readerActivity()).toHaveCount(1);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.reload({ waitUntil: 'domcontentloaded' });
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(394);
  expect((await page.locator('.reader-circle-layout').evaluate(node => getComputedStyle(node).gridTemplateColumns)).split(' ')).toHaveLength(1);
  expect((await page.locator('.circle-feed-list').evaluate(node => getComputedStyle(node).gridTemplateColumns)).split(' ')).toHaveLength(1);
  await page.goto('/account');
  await page.getByTestId('account-logout').click();
  await page.setViewportSize({ width: 1280, height: 900 });

  await signIn(page, editorEmail!, readerPassword!);
  await expect(page.getByTestId('site-manage-nav')).toHaveCount(0);
  await expect(page.getByTestId('site-manage-nav-mobile')).toHaveCount(0);
  await page.getByTestId('site-account-nav').click();
  await expect(page.getByTestId('account-workspace')).toBeVisible();
  await page.getByTestId('account-manage-articles').click();
  await expect(page).toHaveURL(/\/admin\/articles$/);
  await page.goto('/account');
  await expect(page.getByTestId('account-compose')).toHaveAttribute('href', '/admin/publish');
  await expect(page.getByTestId('account-admin-dashboard')).toHaveCount(0);
  await page.getByTestId('account-logout').click();

  await signIn(page, adminEmail!, adminPassword!);
  await expect(page.getByTestId('site-manage-nav')).toHaveCount(0);
  await expect(page.getByTestId('site-manage-nav-mobile')).toHaveCount(0);
  await page.goto('/account');
  await expect(page.getByTestId('account-admin-dashboard')).toHaveAttribute('href', '/admin/');
  await page.setViewportSize({ width: 390, height: 844 });
  await page.reload({ waitUntil: 'domcontentloaded' });
  if (siteTheme === 'folio') {
    await page.locator('#folio-menu-toggle').click();
    await expect(page.getByTestId('site-manage-nav-mobile')).toHaveCount(0);
    await expect(page.getByTestId('site-account-nav-mobile')).toBeVisible();
    await expect(page.getByTestId('site-reader-circle-nav-mobile')).toBeVisible();
  }
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(394);
});

test('public feeds credit the post authors without exposing the legacy admin address', async ({ page }) => {
  test.skip(!adminEmail || !articleSlug, 'Requires isolated multi-author feed fixtures.');
  const response = await page.request.get('/feed/json');
  expect(response.status()).toBe(200);
  const feed = await response.json();
  expect(feed.authors).toBeUndefined();
  expect(feed.items.length).toBeGreaterThan(0);
  expect(feed.items.length).toBeLessThanOrEqual(20);
  const includedAuthors = new Set<string>();
  for (const item of feed.items) {
    const author = item.authors?.[0];
    expect(author?.name).toMatch(/^Browser (Admin|Editor)$/);
    expect(author?.url).toMatch(/\/users\/[^/]+$/);
    includedAuthors.add(author.name);
  }
  const adminStory = feed.items.find((item: { url: string }) => item.url.endsWith('/' + articleSlug));
  const editorStory = feed.items.find((item: { url: string }) => item.url.endsWith('/visual-editor-post'));
  if (adminStory) expect(adminStory.authors[0].name).toBe('Browser Admin');
  if (editorStory) expect(editorStory.authors[0].name).toBe('Browser Editor');
  expect(JSON.stringify(feed)).not.toContain(adminEmail!);
  expect(JSON.stringify(feed)).not.toContain('Private profile draft');
  for (const format of ['rss', 'atom']) {
    const result = await page.request.get(`/feed/${format}`);
    expect(result.status()).toBe(200);
    const body = await result.text();
    for (const author of includedAuthors) expect(body).toContain(author);
    expect(body).not.toContain(adminEmail!);
    expect(body).not.toContain('Private profile draft');
  }
});

test('featured story gives readers a linked author before its date', async ({ page }) => {
  test.skip(siteTheme !== 'folio' || !articleSlug, 'Folio featured story requires the isolated browser fixture.');
  await page.goto('/', { waitUntil: 'domcontentloaded' });
  const featured = page.locator('.hero-featured');
  await expect(featured.getByRole('heading')).toBeVisible();
  const author = featured.getByTestId('article-author-link');
  await expect(author).toHaveAttribute('rel', 'author');
  await expect(author).toHaveAttribute('href', /^\/users\//);
  expect(await featured.locator('.hero-meta').evaluate(node => {
    const link = node.querySelector('[data-testid="article-author-link"]');
    const date = node.querySelector('time');
    return !!link && !!date && !!(link.compareDocumentPosition(date) & Node.DOCUMENT_POSITION_FOLLOWING);
  })).toBe(true);
});

test('series structure and chapter navigation stay distinct from prose in each theme', async ({page}) => {
  await page.emulateMedia({reducedMotion:'reduce'});
  for (const width of [320,390,650,768,1280]) {
    await page.setViewportSize({width,height:900});
    for (const route of ['/visual-book','/visual-empty-book','/visual-book-part','/visual-chapter-two','/visual-nested-chapter']) {
      await page.goto(route);
      const content=page.locator('[data-reading-content]');
      await expect(content.getByTestId('book-chapters')).toHaveCount(0);
      await expect(content.getByTestId('book-navigation')).toHaveCount(0);
      for (const list of await page.getByTestId('book-chapters').all()) {
        await expect(list).toHaveCSS('list-style-type',siteTheme==='cactus' ? 'disc' : 'none');
        expect(await list.evaluate(n=>n.tagName)).toBe(siteTheme==='cactus' ? 'UL' : 'OL');
        if (siteTheme==='folio') {
          await expect(list.locator(':scope > li').first()).toHaveCSS('counter-increment','folio-chapter 1');
          expect(await list.locator(':scope > li').first().evaluate(n=>getComputedStyle(n,'::before').content)).toContain('folio-chapter');
        }
      }
      const body=(await content.boundingBox())!;
      for (const section of await page.locator('[data-testid="book-section"], [data-testid="book-navigation"]').all()) {
        const box=(await section.boundingBox())!;
        expect(box.y-body.y-body.height,'series region has space above prose').toBeGreaterThanOrEqual(24);
        expect(Math.abs(box.x-body.x)).toBeLessThan(1);
        expect(Math.abs(box.width-body.width)).toBeLessThan(1);
        await expect(section).toHaveCSS('border-top-width','1px');
        await expect(section).toHaveCSS('border-top-style',siteTheme==='cactus' ? 'dotted' : 'solid');
        await expect(section).toHaveCSS('border-bottom-width','0px');
        await expect(section).toHaveCSS('background-color','rgba(0, 0, 0, 0)');
        await expect(section).toHaveCSS('box-shadow','none');
        await expect(section).toHaveCSS('border-radius','0px');
        await expect(section.locator(':scope > h2')).toBeVisible();
        await expect(section.locator(':scope > h2')).toHaveCSS('font-size',siteTheme==='cactus' ? '16px' : await section.getAttribute('data-testid')==='book-section' ? '28px' : '24px');
        if (siteTheme==='folio') {
          expect(await section.locator(':scope > h2').evaluate(n=>getComputedStyle(n).fontFamily)).toBe(await page.locator('.article-title').evaluate(n=>getComputedStyle(n).fontFamily));
        }
      }
      if (await page.getByTestId('book-navigation').count()) {
        await expect(page.locator('.book-context')).toHaveText('In this series');
        await expect(page.getByTestId('book-parent')).toHaveAccessibleName(/^Back to series:/);
      }
      if (process.env.SOLITUDES_SERIES_SCREENSHOTS && [390,1280].includes(width) && ['/visual-book','/visual-chapter-two'].includes(route)) {
        const section=page.getByTestId(route==='/visual-book' ? 'book-section' : 'book-navigation');
        await section.evaluate(n=>scrollTo({top:n.getBoundingClientRect().top+scrollY-140,behavior:'instant'}));
        await page.screenshot({path:path.join(process.env.SOLITUDES_SERIES_SCREENSHOTS,`${siteTheme}-${route.slice(1)}-${width}.png`),animations:'disabled'});
      }
      if (route==='/visual-chapter-two') {
        const previous=page.getByTestId('book-previous'), next=page.getByTestId('book-next');
        await expect(previous).toHaveAttribute('href','/visual-chapter');
        await expect(next).toHaveAttribute('href','/visual-book-part');
        for (const link of [previous,next]) {
          await link.locator('span').last().evaluate(n=>{n.textContent='一篇很长的章节标题，关于写作与阅读 / ALongUnbrokenChapterTitleForResponsiveNavigation'.repeat(3);});
          expect((await link.boundingBox())!.height).toBeGreaterThanOrEqual(siteTheme==='cactus' && width>500 ? 32 : 44);
        }
        const a=(await previous.boundingBox())!, b=(await next.boundingBox())!;
        if (siteTheme==='cactus') {
          expect(b.y).toBeGreaterThanOrEqual(a.y+a.height);
          await expect(previous).toHaveCSS('grid-template-columns',/px /);
          const label=(await previous.locator('.book-direction').boundingBox())!;
          const title=(await previous.locator('span').last().boundingBox())!;
          expect(title.x).toBeGreaterThan(label.x+label.width);
        } else {
          expect(a.y,'previous chapter is secondary to the continue-reading feature').toBeGreaterThanOrEqual(b.y+b.height);
          await expect(next.locator('.section-label')).toHaveText('Continue reading · Next Chapter');
          const title=next.locator('.sibling-title');
          expect(await title.evaluate(n=>parseFloat(getComputedStyle(n).fontSize))).toBeGreaterThan(22);
        }
      }
      expect(await page.evaluate(()=>document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
    }
    await page.goto('/visual-long-article');
    await expect(page.locator('[data-testid="book-section"], [data-testid="book-navigation"]')).toHaveCount(0);
  }
});

test('book reading contract covers nested chapters, roles, navigation and empty series', async ({ page }) => {
  test.skip(!adminEmail || !readerEmail || !editorEmail || !!process.env.E2E_PAGING_THREAD, 'Requires the normal isolated book fixtures.');
  test.setTimeout(90000);
  const capture = async (name: string) => {
    if (!process.env.SOLITUDES_BOOK_SCREENSHOTS) return;
    await page.screenshot({path:path.join(process.env.SOLITUDES_BOOK_SCREENSHOTS, `${siteTheme}-${name}-${page.viewportSize()!.width}.png`),fullPage:true,animations:'disabled'});
  };
  for (const role of ['guest', 'reader', 'editor', 'admin'] as const) {
    await page.context().clearCookies();
    if (role !== 'guest') await signIn(page, role === 'reader' ? readerEmail! : role === 'editor' ? editorEmail! : adminEmail!, role === 'admin' ? adminPassword! : readerPassword!);
    for (const width of [390, 1280]) {
      await page.setViewportSize({width, height: 950});
      await page.goto('/books/');
      await expect(page.locator('main h1')).toBeVisible();
      await expect(page.locator('main h1')).toHaveText('Books');
      if (role === 'guest') await capture('archive');
      await page.locator('a[data-testid="article-list-link"][href$="/visual-book"]').click();
      const links = page.getByTestId('book-chapter-link');
      await expect(links).toHaveCount(role === 'guest' ? 4 : role === 'reader' ? 5 : role === 'editor' ? 6 : 7);
      await expect(page.locator('[data-testid="book-chapters"] > ul')).toHaveCount(0);
      await expect(page.locator('li > [data-testid="book-chapters"] [href="/visual-nested-chapter"]')).toBeVisible();
      if (siteTheme === 'cactus') {
        expect(await page.getByTestId('book-chapters').first().evaluate(node => getComputedStyle(node).display)).toBe('block');
        expect(await page.getByTestId('book-chapters').first().evaluate(node => getComputedStyle(node).listStyleType)).toBe('disc');
      }
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
      await capture(role);
      await links.filter({hasText:'Chapter 1:'}).click();
      await expect(page.getByTestId('book-previous')).toHaveCount(0);
      await expect(page.getByTestId('book-next')).toHaveAttribute('href', '/visual-chapter-two');
      await page.getByTestId('book-next').click();
      await expect(page.getByTestId('book-previous')).toHaveAttribute('href', '/visual-chapter');
      await expect(page.getByTestId('book-next')).toHaveAttribute('href', '/visual-book-part');
      if (role === 'guest') await capture('chapter');
      await page.getByTestId('book-next').click();
      if (role === 'guest') await capture('nested');
      await page.getByTestId('book-chapter-link').filter({hasText:'Chapter 3:'}).click();
      await expect(page.getByTestId('book-parent')).toHaveAttribute('href', '/visual-book-part');
      await expect(page.getByTestId('book-previous')).toHaveCount(0);
      await expect(page.getByTestId('book-next')).toHaveCount(0);
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
      await page.getByTestId('book-parent').click();
      await page.getByTestId('book-parent').click();
      await expect(page).toHaveURL(/\/visual-book$/);
      await page.goto('/visual-empty-book');
      await expect(page.getByTestId('book-empty')).toBeVisible();
      await expect(page.getByTestId('book-chapter-link')).toHaveCount(0);
      if (role === 'guest') await capture('empty');
    }
    for (const [slug, allowed] of [['visual-members-chapter', role !== 'guest'], ['visual-editors-chapter', role === 'editor' || role === 'admin'], ['visual-private-part', role === 'admin']] as const) {
      expect((await page.request.get('/'+slug)).status()).toBe(allowed ? 200 : 404);
    }
  }
});

test('Cactus publication dates wrap as complete labelled facts beside sharing', async ({browser}) => {
  test.skip(siteTheme !== 'cactus');
  test.setTimeout(90000);
  for (const locale of ['zh-CN','en']) {
    const context=await browser.newContext({locale,baseURL:process.env.E2E_BASE_URL,reducedMotion:'reduce'});
    try {
      const page=await context.newPage();
      for (const width of [320,375,390,428,768,1280]) {
        await page.setViewportSize({width,height:900});
        for (const route of ['/visual-editor-post','/visual-long-article']) {
          await page.goto(route);
          const meta=page.getByTestId('article-meta');
          const times=meta.locator('time');
          await expect(times).toHaveCount(route==='/visual-long-article' ? 1 : 2);
          for (const time of await times.all()) {
            await expect(time).toHaveAttribute('datetime',/^\d{4}-\d{2}-\d{2}T/);
            const group=time.locator('..');
            await expect(group).toHaveClass('postdate-item');
            await expect(group).toHaveCSS('white-space','nowrap');
            const lines=await time.evaluate(n=>{const r=document.createRange();r.selectNodeContents(n);return Array.from(r.getClientRects()).filter(b=>b.width>0).map(b=>b.y);});
            expect(new Set(lines).size,'date never breaks between year, month and day').toBe(1);
            const rect=(await group.boundingBox())!, bounds=(await meta.boundingBox())!;
            expect(rect.x).toBeGreaterThanOrEqual(bounds.x-1);
            expect(rect.x+rect.width).toBeLessThanOrEqual(bounds.x+bounds.width+1);
            expect(rect.height,'label and date stay on one line').toBeLessThanOrEqual(await group.evaluate(n=>parseFloat(getComputedStyle(n).lineHeight))+1);
          }
          const share=page.getByTestId('article-byline').getByTestId('article-share');
          if (width<900) {
            await expect(share).toBeVisible();
            const a=(await meta.boundingBox())!, b=(await share.boundingBox())!;
            expect(a.x+a.width).toBeLessThanOrEqual(b.x);
            expect(b.height).toBeGreaterThanOrEqual(44);
          } else await expect(share).toBeHidden();
          if (process.env.SOLITUDES_DATE_SCREENSHOTS && width===390) {
            await page.getByTestId('article-author-link').evaluate((n,name)=>{n.textContent=name;},locale==='zh-CN' ? '奶爸' : 'Author');
            await page.screenshot({path:path.join(process.env.SOLITUDES_DATE_SCREENSHOTS,`${locale}-${route.slice(1)}.png`),animations:'disabled'});
          }
          await page.getByTestId('article-author-link').evaluate(n=>{n.textContent='AnUnbrokenAuthorName'.repeat(8);});
          expect(await page.evaluate(()=>document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
        }
      }
    } finally {await context.close();}
  }
});

test('sharing uses the desktop Cactus menu and inline metadata elsewhere for every role', async ({ page }) => {
  test.skip(!articleSlug || !readerEmail || !editorEmail || !adminEmail, 'Requires isolated roles.');
  test.setTimeout(90000);
  const assertInlineShare = async () => {
    const byline = page.getByTestId('article-byline');
    const share = byline.getByTestId('article-share');
    if (siteTheme === 'cactus' && page.viewportSize()!.width >= 900) {
      await expect(share).not.toBeVisible();
      await expect(articleShareTrigger(page)).toBeVisible();
      return;
    }
    await expect(share).toBeVisible();
    const button = (await share.boundingBox())!;
    const meta = (await byline.getByTestId('article-meta').boundingBox())!;
    expect(button.height).toBeGreaterThanOrEqual(44);
    expect(button.width).toBeLessThan(110);
    expect(button.x).toBeGreaterThanOrEqual(meta.x + meta.width);
    expect(button.y).toBeLessThan(meta.y + meta.height);
    expect(button.y + button.height).toBeGreaterThan(meta.y);
    expect(await share.evaluate(node => getComputedStyle(node).borderTopWidth)).toBe('0px');
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(page.viewportSize()!.width);
    await expect(page.locator('.article-actions')).toHaveCount(0);
  };
  for (const role of ['guest', 'reader', 'editor', 'admin'] as const) {
    await page.context().clearCookies();
    if (role !== 'guest') {
      await signIn(page, role === 'reader' ? readerEmail! : role === 'editor' ? editorEmail! : adminEmail!, role === 'admin' ? adminPassword! : readerPassword!);
    }
    for (const width of [360, 1280]) {
      await page.setViewportSize({width, height: 900});
      for (const slug of [role === 'editor' ? 'visual-editor-post' : articleSlug!, 'visual-book']) {
        await page.goto('/' + slug, {waitUntil: 'domcontentloaded'});
        await assertInlineShare();
        await expect(page.getByTestId('article-edit-link')).toHaveCount(role === 'admin' || (role === 'editor' && slug === 'visual-editor-post') ? 1 : 0);
        if (role === 'guest' && process.env.SOLITUDES_SHARE_SCREENSHOTS) {
          await page.screenshot({path: path.join(process.env.SOLITUDES_SHARE_SCREENSHOTS, `${siteTheme}-${slug}-${width}.png`), animations: 'disabled'});
        }
      }
    }
  }
  await page.setViewportSize({width: 320, height: 900});
  await page.goto('/' + articleSlug!);
  await page.getByTestId('article-author-link').evaluate(node => { node.textContent = 'AnUnusuallyLongAuthorNameWithoutAnySpaces'.repeat(3); });
  await assertInlineShare();
});

test('readers can share articles and books with a working copy fallback', async ({ page }) => {
  test.skip(!articleSlug, 'Sharing requires an isolated article.');
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'share', { configurable: true, value: undefined });
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: {
      writeText: async (value: string) => { (window as any).__sharedURL = value; },
    } });
  });
  for (const slug of [articleSlug!, 'visual-book']) {
    await page.goto('/' + slug, { waitUntil: 'domcontentloaded' });
    expect((await articleShareTrigger(page).boundingBox())!.height).toBeGreaterThanOrEqual(siteTheme === 'cactus' ? 32 : 44);
    await articleShareTrigger(page).click();
    await page.getByTestId('article-share-copy').click();
    await expect(page.getByTestId('article-share-status')).toHaveText('Link copied');
    expect(await page.evaluate(() => (window as any).__sharedURL)).toBe(await page.getByTestId('article-share').getAttribute('data-url'));
  }
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: {
      writeText: async () => { throw new Error('Clipboard blocked'); },
    } });
  });
  await page.goto('/' + articleSlug, { waitUntil: 'domcontentloaded' });
  await articleShareTrigger(page).click();
    await page.getByTestId('article-share-copy').click();
  await expect(page.getByTestId('article-share-status')).toHaveText('Select and copy this link');
  await expect(page.getByTestId('article-share-fallback')).toBeVisible();
  await expect(page.getByTestId('article-share-fallback')).toHaveValue((await page.getByTestId('article-share').getAttribute('data-url'))!);
});

test('reading share dialog supports native share, cancellation, focus and canonical URLs', async ({ page }) => {
  await page.addInitScript(() => {
    (window as any).__shareResult = 'cancel';
    Object.defineProperty(navigator, 'share', { configurable: true, value: async (value: unknown) => {
      (window as any).__nativePayload = value;
      const result = (window as any).__shareResult;
      if (result !== 'success') throw new DOMException(result, result === 'cancel' ? 'AbortError' : 'NotAllowedError');
    } });
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`/${articleSlug}?utm_source=private-tracking#reply`);
  const trigger = page.getByTestId('article-share');
  await trigger.click();
  const dialog = page.getByTestId('article-share-dialog');
  await expect(dialog).toBeVisible();
  await expect(page.getByTestId('article-share-copy')).toBeFocused();
  for (let i = 0; i < 8; i++) {
    await page.keyboard.press('Tab');
    expect(await dialog.evaluate(el => el.contains(document.activeElement))).toBe(true);
  }
  await page.getByTestId('article-share-native').click();
  await expect(dialog).toBeVisible();
  await expect(page.getByTestId('article-share-status')).toBeEmpty();
  const payload = await page.evaluate(() => (window as any).__nativePayload);
  expect(payload.url).toBe(await trigger.getAttribute('data-url'));
  expect(payload.url).not.toMatch(/utm_source|#reply/);
  expect(payload.title).toBe(articleTitle);
  await page.evaluate(() => { (window as any).__shareResult = 'denied'; });
  await page.getByTestId('article-share-native').click();
  await expect(page.getByTestId('article-share-status')).toHaveText('Select and copy this link');
  await expect(page.getByTestId('article-share-fallback')).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(dialog).not.toBeVisible();
  await expect(trigger).toBeFocused();
  await trigger.click();
  await page.evaluate(() => { (window as any).__shareResult = 'success'; });
  await page.getByTestId('article-share-native').click();
  await expect(dialog).not.toBeVisible();
  await expect(trigger).toBeFocused();
});

test('reading contents follow the active section and remain reachable on desktop and phone', async ({ page }) => {
  test.skip(siteTheme !== 'folio', 'Cactus uses its original menu and footer TOC.');
  await page.emulateMedia({ reducedMotion: 'reduce' });
  for (const width of [1920,1440,1439,1280,1024,768,390,320]) {
    await page.setViewportSize({ width, height: 800 });
    await page.goto('/visual-long-article');
    const toc = page.getByTestId('article-toc');
    const details = toc.locator('details');
    const reading = (await page.locator('[data-reading-content]').boundingBox())!;
    const shell = (await page.locator('#main-content').boundingBox())!;
    expect(Math.abs(reading.width-Math.min(928,shell.width)),'Folio matches the original magazine reading width').toBeLessThan(1);
    expect(Math.abs(reading.x+reading.width/2-shell.x-shell.width/2),'reading stays centered').toBeLessThan(1);
    for (const selector of ['.article-header','#comments']) {
      const box = (await page.locator(selector).boundingBox())!;
      expect(Math.abs(box.x-reading.x),selector+' left edge').toBeLessThan(1);
      expect(Math.abs(box.width-reading.width),selector+' width').toBeLessThan(1);
    }
    if (width >= 1440) {
      await expect(details).toHaveAttribute('open', '');
      const panel = (await toc.boundingBox())!;
      expect(panel.x-reading.x-reading.width,'sidebar clearance after widening').toBeGreaterThanOrEqual(31);
      expect(panel.x+panel.width).toBeLessThanOrEqual(await page.evaluate(()=>document.documentElement.clientWidth)-15);
    } else {
      await expect(details).not.toHaveAttribute('open');
      const panel = (await toc.boundingBox())!;
      expect(Math.abs(panel.x-reading.x)).toBeLessThan(1);
      expect(Math.abs(panel.width-reading.width)).toBeLessThan(1);
      await toc.locator('summary').click();
    }
    const target = toc.locator('a').nth(1);
    const hash = await target.getAttribute('href');
    await target.click();
    await expect(page).toHaveURL(new RegExp(hash!.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '$'));
    await expect(target).toHaveAttribute('aria-current', 'location');
    await expect(page.getByTestId('reading-progress')).not.toHaveText('0%');
    if (width < 1440) {
      await expect(details).not.toHaveAttribute('open');
      const box = (await toc.boundingBox())!;
      expect(box.y).toBeGreaterThanOrEqual(0);
      expect(box.y).toBeLessThan(150);
      await toc.locator('summary').click();
      await page.keyboard.press('Escape');
      await expect(toc.locator('summary')).toBeFocused();
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
  }
  await page.goto('/' + articleSlug);
  await expect(page.getByTestId('article-toc')).toHaveCount(0);
});

test('Folio magazine typography keeps centered headers and safe opening drop caps', async ({page}) => {
  test.skip(siteTheme !== 'folio');
  test.setTimeout(90000);
  await page.emulateMedia({reducedMotion:'reduce'});
  for (const width of [320,390,768,1280,1440,1920]) {
    await page.setViewportSize({width,height:900});
    for (const slug of [articleSlug!, 'visual-book']) {
      await page.goto('/'+slug);
      await expect(page.locator('.article-header')).toHaveCSS('text-align','center');
      await expect(page.locator('.article-title')).toHaveCSS('text-align','center');
      await expect(page.locator('.article-header .article-divider')).toHaveCount(1);
      await expect(page.locator('.article-header .article-divider')).toHaveAttribute('aria-hidden','true');
      const content=page.locator('[data-reading-content]');
      const paragraph=content.locator('p').first();
      const firstLetter=await paragraph.evaluate(node=>({float:getComputedStyle(node,'::first-letter').cssFloat,size:parseFloat(getComputedStyle(node,'::first-letter').fontSize),body:parseFloat(getComputedStyle(node).fontSize)}));
      expect(firstLetter.float).toBe('left');
      expect(firstLetter.size).toBeGreaterThan(firstLetter.body*2);
      expect(await page.evaluate(()=>document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
    }
    await page.locator('.article-title').evaluate(node=>{node.textContent='一篇很长的专栏标题：关于写作、读者与共同成长 / A long editorial headline across different screens';});
    const title=(await page.locator('.article-title').boundingBox())!;
    const content=(await page.locator('[data-reading-content]').boundingBox())!;
    expect(title.x).toBe(content.x);
    expect(title.width).toBe(content.width);
    expect(await page.evaluate(()=>document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
  }
  const content=page.locator('[data-reading-content]');
  for (const markup of [
    '<p><img alt="Example" src="data:image/svg+xml,%3Csvg xmlns=%22http://www.w3.org/2000/svg%22/%3E">Image caption</p>',
    '<p><code>const value = 1</code> is a code opening.</p>',
    '<div class="access-notice">Sign in to read this part.</div><p>A paragraph after a permission notice.</p>',
    '<div class="version-notice">Viewing an earlier version.</div><p>A paragraph after a version notice.</p>',
  ]) {
    await content.evaluate((node,html)=>{node.innerHTML=html;},markup);
    expect(await content.locator('p').evaluate(node=>getComputedStyle(node,'::first-letter').cssFloat)).toBe('none');
  }
});

test('Cactus links underline only on hover and preserve explicit Markdown formatting', async ({page}) => {
  test.skip(siteTheme !== 'cactus');
  for (const width of [390,1280]) {
    await page.setViewportSize({width,height:900});
    for (const route of ['/', '/posts/', '/visual-long-article', '/deep-chapter-3-1', '/visual-page']) {
      await page.goto(route);
      await page.mouse.move(0,0);
      for (const link of await page.locator('a').all()) {
        if (!(await link.isVisible())) continue;
        expect(await link.evaluate(node=>getComputedStyle(node).textDecorationLine), `${route}: ${await link.textContent()}`).toBe('none');
        // The site logo is intentionally drawn as a background image.
        if (await link.getAttribute('id') !== 'logo') await expect(link).toHaveCSS('background-image','none');
      }
      if (route === '/visual-long-article') {
        const content=page.locator('[data-reading-content]');
        const link=content.getByRole('link',{name:'Ordinary reading link',exact:true});
        await expect(link).toHaveAttribute('href','/visual-page');
        await link.hover();
        await expect(link).toHaveCSS('text-decoration-line','underline');
        await expect(link).toHaveCSS('text-underline-offset','3px');
        await page.mouse.move(0,0);
        await expect(link).toHaveCSS('text-decoration-line','none');
        await link.focus();
        await expect(link).toHaveCSS('outline-style','solid');
        await expect(link).toHaveCSS('outline-width','2px');
        for (const underline of await content.locator('u').all()) await expect(underline).toHaveCSS('text-decoration-line','underline');
        await expect(content.locator('u')).toHaveCount(3);
        await expect(content.locator('del')).toHaveCSS('text-decoration-line','line-through');
        await expect(content.locator('h3')).toHaveCSS('text-decoration-line','none');
      }
      if (route === '/deep-chapter-3-1' || route === '/visual-page') {
        const heading=page.locator('#reply-list > h2');
        await expect(heading).toHaveCSS('font-size','24px');
        await expect(heading).toHaveCSS('font-weight','700');
        await expect(heading).toHaveCSS('margin-bottom','16px');
      }
    }
  }
});

test('Cactus desktop menu opens at the viewport corner with compact links and separators', async ({page}) => {
  test.skip(siteTheme !== 'cactus');
  for (const width of [390,768,899,900,1024,1280,1440,1920]) {
    await page.setViewportSize({width,height:900});
    for (const slug of ['visual-long-article','visual-book']) {
      await page.goto('/'+slug);
      const inlineShare = page.getByTestId('article-byline').getByTestId('article-share');
      if (width < 900) {
        await expect(inlineShare).toBeVisible();
        await expect(page.locator('#header-post #nav')).not.toBeVisible();
        continue;
      }
      const toggle = page.locator('#menu-icon');
      await expect(toggle).toHaveClass(/active/);
      await expect(toggle).toHaveAttribute('aria-expanded','true');
      await expect(inlineShare).not.toBeVisible();
      await expect(page.getByTestId('article-share-menu')).toBeVisible();
      const box = (await toggle.boundingBox())!;
      const clientWidth = await page.evaluate(()=>document.documentElement.getBoundingClientRect().width);
      expect(Math.abs(clientWidth-box.x-box.width-32)).toBeLessThan(1);
      expect(box.y).toBe(32);
      const links = page.locator('#header-post #nav a');
      for (const link of await links.all()) expect((await link.boundingBox())!.height).toBe(15);
      for (const item of await page.locator('#header-post #nav li:not(:last-child)').all()) {
        expect(await item.evaluate(node=>getComputedStyle(node,'::after').height)).toBe('15px');
        expect(await item.evaluate(node=>getComputedStyle(node).borderRightWidth)).toBe('0px');
      }
      await links.first().hover();
      await expect(links.first()).toHaveCSS('text-underline-offset','3px');
      await expect(links.first()).toHaveCSS('background-image','none');
      await toggle.click();
      await expect(toggle).toHaveAttribute('aria-expanded','false');
      await expect(page.locator('#header-post #nav')).not.toBeVisible();
      await toggle.click();
      await expect(page.getByTestId('article-share-menu')).toBeVisible();
    }
  }
  await page.setViewportSize({width:899,height:900});
  await expect(page.locator('#menu-icon-tablet')).toHaveAttribute('aria-expanded','false');
  await page.setViewportSize({width:900,height:900});
  await expect(page.locator('#menu-icon')).toHaveAttribute('aria-expanded','true');
});

test('comment reply and thread links share a compact content-aligned action row', async ({page}) => {
  for (const width of [390,1280]) {
    await page.setViewportSize({width,height:900});
    await page.goto('/deep-chapter-3-1#comments');
    const entry=page.getByTestId('comment-entry').filter({has:page.getByTestId('comment-thread-link')}).first();
    const actions=entry.locator('.comment-actions').first();
    const reply=actions.getByTestId('comment-reply');
    const thread=actions.getByTestId('comment-thread-link');
    await expect(reply).toBeVisible();
    await expect(thread).toBeVisible();
    const a=(await reply.boundingBox())!, b=(await thread.boundingBox())!;
    if (siteTheme==='cactus') {
      expect(Math.abs((a.y+a.height/2)-(b.y+b.height/2))).toBeLessThan(1);
      const content=(await entry.locator('p.comment-meta').last().boundingBox())!;
      expect(Math.abs(a.x-content.x)).toBeLessThan(1);
      expect(a.y).toBeGreaterThanOrEqual(content.y+content.height);
      expect(a.height).toBe(width<500 ? 44 : 28);
    }
    await thread.click();
    await expect(page).toHaveURL(/\?thread=.+#comments$/);
    await expect(page.getByTestId('comments-back')).toBeVisible();
    await page.getByTestId('comments-back').click();
    await expect(page).toHaveURL(/\?comment_page=1#comments$/);
    await page.getByTestId('comment-reply').first().click();
    await expect(page.getByTestId('comment-content')).toBeFocused();
  }
});

test('Cactus original TOC lives in the article menu and mobile footer', async ({page}) => {
  test.skip(siteTheme !== 'cactus');
  await page.emulateMedia({reducedMotion:'reduce'});
  for (const width of [1920,1800,1799,1440,1280,1024,768,500,390,320]) {
    await page.setViewportSize({width,height:900});
    await page.goto('/visual-long-article');
    await expect(page.locator('.reader-toc')).toHaveCount(0);
    await expect(page.locator('[data-testid="site-article"] [data-testid="article-toc"]')).toHaveCount(0);
    const mobile = width <= 500;
    const toc = page.locator(mobile ? '#toc-footer' : '#toc');
    const toggle = mobile ? page.locator('#toc-footer-toggle') : page.locator('#menu-icon, #menu-icon-tablet').filter({visible:true});
    if (width < 900) {
      await expect(toc).not.toBeVisible();
      await toggle.click();
    }
    await expect(toc).toBeVisible();
    await expect(toc.locator('a')).toHaveCount(3);
    for (const link of await toc.locator('a').all()) await expect(link).toBeVisible();
    if (width >= 1800) {
      const panel = (await toc.boundingBox())!, body = (await page.locator('[data-reading-content]').boundingBox())!;
      expect(panel.x).toBeGreaterThanOrEqual(body.x+body.width);
    }
    if (process.env.SOLITUDES_LAYOUT_SCREENSHOTS && [1440,1024,390].includes(width)) await page.screenshot({path:path.join(process.env.SOLITUDES_LAYOUT_SCREENSHOTS,`cactus-toc-${width}.png`),animations:'disabled'});
    const target = toc.locator('a').nth(1);
    const hash = await target.getAttribute('href');
    await target.click();
    await expect.poll(() => new URL(page.url()).hash).toBe(hash);
    await expect(target).toHaveAttribute('aria-current', 'location');
    if (mobile) {
      await expect(toc).not.toBeVisible();
      await page.evaluate(() => window.scrollTo(0,0));
      await toggle.click();
      await expect(toc).toBeVisible();
    } else {
      await expect(toc).toBeVisible();
      await expect(toggle).toHaveAttribute('aria-expanded', 'true');
    }
    await page.keyboard.press('Escape');
    await expect(toc).not.toBeVisible();
    await expect(toggle).toBeFocused();
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
  }
});

test('standalone pages have their own layout, metadata and working comments', async ({page}) => {
  test.setTimeout(90000);
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.emulateMedia({reducedMotion:'reduce'});
  for (const width of [320,390,768,1440]) {
    await page.setViewportSize({width,height:900});
    await page.goto('/visual-page');
    const content = page.getByTestId('site-page');
    await expect(content).toBeVisible();
    await expect(content).toHaveAttribute('aria-label','About this publication');
    await expect(page).toHaveTitle(/About this publication/);
    await expect(content.locator('h1, .standalone-page-header')).toHaveCount(0);
    const dates = page.getByTestId('page-dates');
    await expect(dates.locator('time')).toHaveCount(2);
    const bodyBox = (await page.locator('[data-reading-content]').boundingBox())!;
    const datesBox = (await dates.boundingBox())!;
    expect(datesBox.y).toBeGreaterThanOrEqual(bodyBox.y+bodyBox.height);
    expect(datesBox.y+datesBox.height).toBeLessThanOrEqual((await page.locator(siteTheme === 'cactus' ? '#reply-list' : '#comments').boundingBox())!.y);
    for (const time of await dates.locator('time').all()) {
      await expect(time).toHaveAttribute('datetime',/^\d{4}-\d{2}-\d{2}T/);
      await expect(time).toContainText(/\d{2}:\d{2}$/);
      const box = (await time.locator('..').boundingBox())!;
      expect(box.x).toBeGreaterThanOrEqual(datesBox.x);
      expect(box.x+box.width).toBeLessThanOrEqual(datesBox.x+datesBox.width+1);
    }
    for (const id of ['site-article','article-byline','article-share','article-toc','article-previous','article-next','page-edit-link']) {
      await expect(page.getByTestId(id)).toHaveCount(0);
    }
    await expect(page.locator('#header-post, #footer-post, #article-share-dialog, .reading-content')).toHaveCount(0);
    await expect(page.locator('meta[property="og:type"]')).toHaveAttribute('content','website');
    await expect(page.locator('meta[property^="article:"]')).toHaveCount(0);
    const schemas = await page.locator('script[type="application/ld+json"]').evaluateAll(nodes => nodes.map(node=>JSON.parse(node.textContent!)));
    expect(schemas.some(schema=>schema['@type']==='WebPage')).toBe(true);
    expect(schemas.some(schema=>schema['@type']==='Article')).toBe(false);
    const crumbs = schemas.find(schema=>schema['@type']==='BreadcrumbList').itemListElement;
    expect(crumbs.map((item:{position:number})=>item.position)).toEqual([1,2]);
    expect(crumbs[1].name).toBe('About this publication');
    await expect(page.getByTestId('comment-sign-in')).toBeVisible();
    await expect(page.getByTestId('comment-register')).toBeVisible();
    if (siteTheme === 'folio') {
      expect(await page.locator('[data-reading-content] > p').first().evaluate(node=>getComputedStyle(node,'::first-letter').float)).toBe('none');
    } else {
      await expect(page.locator('#header')).toBeVisible();
      // Page and article templates must share Markdown typography, not browser
      // default blockquote margins (40px per side on a narrow phone screen).
      const quote = page.locator('[data-reading-content] > blockquote');
      const quoteBox = (await quote.boundingBox())!;
      expect(quoteBox.x - bodyBox.x).toBeCloseTo(10, 0);
      expect(bodyBox.width - quoteBox.width).toBeCloseTo(20, 0);
      expect(await quote.locator('p').first().evaluate(node => getComputedStyle(node).margin)).toBe('0px');
      expect(await quote.evaluate(node => getComputedStyle(node, '::before').content)).not.toBe('none');
      expect(await page.locator('[data-reading-content] h2').evaluate(node => getComputedStyle(node).fontSize)).toBe('24px');
      const image = page.locator('[data-reading-content] img');
      expect(await image.evaluate(node => getComputedStyle(node).display)).toBe('block');
      expect(await image.evaluate(node => getComputedStyle(node).maxWidth)).toBe(width <= 768 ? '95%' : '80%');
    }
    const codeBox = (await page.locator('[data-reading-content] pre').boundingBox())!;
    expect(codeBox.x).toBeGreaterThanOrEqual(bodyBox.x);
    expect(codeBox.x + codeBox.width).toBeLessThanOrEqual(bodyBox.x + bodyBox.width + 1);
    expect(await page.evaluate(()=>document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
    if (process.env.SOLITUDES_LAYOUT_SCREENSHOTS && [390,1440].includes(width)) {
      await page.screenshot({path:path.join(process.env.SOLITUDES_LAYOUT_SCREENSHOTS,`${siteTheme}-standalone-page-${width}.png`),fullPage:true,animations:'disabled'});
    }
    if (width <= 768) {
      if (siteTheme === 'cactus') {
        await page.locator('#header #nav button').click();
        await expect(page.getByTestId('site-account-nav')).toBeVisible();
        await page.locator('#header #nav button').click();
      } else {
        await page.locator('#folio-menu-toggle').click();
        await expect(page.getByTestId('site-account-nav-mobile')).toBeVisible();
        await page.keyboard.press('Escape');
        await expect(page.locator('#folio-mobile-menu')).not.toBeVisible();
      }
    }
  }
  await page.goto('/visual-closed-2');
  await expect(page.getByTestId('page-created-at')).toBeVisible();
  await expect(page.getByTestId('page-updated-at')).toHaveCount(0);
  await page.goto('/visual-page');
  const message = `Standalone guest comment ${siteTheme} ${Date.now()}`;
  await page.getByTestId('comment-nickname').fill('Page visitor');
  await page.getByTestId('comment-content').fill(message);
  await page.getByTestId('comment-submit').click();
  await expect(page.locator('[id^="comment-"]').filter({hasText:message}).first()).toBeVisible();
  for (const [role,email,password] of [['reader',readerEmail,readerPassword],['editor',editorEmail,readerPassword],['admin',adminEmail,adminPassword]]) {
    await page.context().clearCookies();
    await signIn(page,email!,password!);
    await page.goto('/visual-page');
    await expect(page.getByTestId('page-edit-link')).toHaveCount(role === 'admin' ? 1 : 0);
    if (role === 'reader') {
      await expect(page.getByTestId('comment-nickname')).toHaveCount(0);
      const memberMessage = `Standalone member comment ${siteTheme} ${Date.now()}`;
      await page.getByTestId('comment-content').fill(memberMessage);
      await page.getByTestId('comment-submit').click();
      await expect(page.locator('[id^="comment-"]').filter({hasText:memberMessage}).first().getByTestId('comment-role')).toContainText('Member');
    }
    if (role === 'admin') {
      await page.getByTestId('page-edit-link').click();
      await expect(page.getByTestId('publish-title')).toHaveValue('About this publication');
    }
  }
  expect(errors).toEqual([]);
});

test('Cactus reading pages use the full centered site width without title jumps', async ({page,browser}) => {
  test.skip(siteTheme !== 'cactus');
  test.setTimeout(90000);
  for (const width of [320,390,500,501,768,900,1024,1200,1280,1440,1799,1800,1920]) {
    await page.setViewportSize({width,height:900});
    await page.goto('/posts/');
    const site = (await page.locator('#main-content').boundingBox())!;
    const siteHeader = (await page.locator('#header').boundingBox())!;
    await page.goto('/visual-long-article');
    const title = page.locator('[data-testid="site-article"] h1').first();
    const heading = (await title.boundingBox())!;
    const body = (await page.locator('[data-reading-content]').boundingBox())!;
    expect(Math.abs(heading.x-site.x),'title follows site left edge at '+width).toBeLessThan(1);
    expect(Math.abs(body.x-site.x),'body follows site left edge at '+width).toBeLessThan(1);
    expect(Math.abs(heading.y-siteHeader.y),'same top baseline, including 500/501 breakpoint at '+width).toBeLessThan(1);
    expect(Math.abs(body.width-site.width),'reading uses full site width at '+width).toBeLessThan(1);
    for (const selector of ['[data-testid="article-byline"]','#reply-list','#reply']) {
      const box = (await page.locator(selector).boundingBox())!;
      expect(Math.abs(box.x-site.x),selector+' shares site left edge').toBeLessThan(1);
      expect(Math.abs(box.width-site.width),selector+' shares site width').toBeLessThan(1);
    }
    const fontSize = await title.evaluate(node => parseFloat(getComputedStyle(node).fontSize));
    const sectionSize = await page.locator('[data-reading-content] h2').first().evaluate(node => parseFloat(getComputedStyle(node).fontSize));
    expect(fontSize,'article title outranks section headings').toBeGreaterThan(sectionSize);
    if (width >= 1800) {
      const toggle = page.locator('#menu-icon');
      if (await toggle.getAttribute('aria-expanded') === 'false') await toggle.click();
      const toc = (await page.locator('#toc').boundingBox())!;
      expect(toc.x-body.x-body.width,'TOC has a real gutter').toBeGreaterThanOrEqual(31);
      expect(toc.x+toc.width,'TOC stays inside viewport').toBeLessThanOrEqual(width-16);
    } else if (width > 500 && width < 900) {
      await expect(page.locator('#toc')).not.toBeVisible();
    }
    if (width >= 1280) {
      const toggle = (await page.locator('#menu-icon').boundingBox())!;
      expect(toggle.x,'fixed menu toggle does not cover full-width text').toBeGreaterThanOrEqual(body.x+body.width);
    }
    // Long bilingual titles may grow naturally, but cannot shift or overflow.
    await title.evaluate(node => { node.textContent = '一篇较长的文章标题：从阅读笔记到共同写作，理解多作者博客的协作方式 / Writing together'; });
    const longTitle = (await title.boundingBox())!;
    expect(longTitle.y).toBe(heading.y);
    expect(longTitle.x).toBe(heading.x);
    expect(longTitle.width).toBe(body.width);
    expect(await page.evaluate(()=>document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
  }
  const touch = await browser.newContext({hasTouch:true});
  try {
    const touchPage = await touch.newPage();
    for (const width of [501,900,1440]) {
      await touchPage.setViewportSize({width,height:900});
      await touchPage.goto(new URL('/visual-long-article',process.env.E2E_BASE_URL!).href);
      const toggle = touchPage.locator('#menu-icon, #menu-icon-tablet').filter({visible:true});
      if (await toggle.getAttribute('aria-expanded') === 'false') await toggle.tap();
      const nav = (await touchPage.locator('#header-post #nav').boundingBox())!;
      const heading = (await touchPage.locator('[data-testid="site-article"] h1').first().boundingBox())!;
      expect(nav.y+nav.height,'44px touch targets do not overlap title at '+width).toBeLessThanOrEqual(heading.y);
      for (const link of await touchPage.locator('#header-post #nav a').all()) {
        await link.focus();
        const box = (await link.boundingBox())!;
        expect(box.x,'focused menu link can be reached').toBeGreaterThanOrEqual(nav.x-1);
        expect(box.x+box.width).toBeLessThanOrEqual(nav.x+nav.width+1);
      }
    }
  } finally { await touch.close(); }
});

test('page width and start height contract stays consistent within each theme and page family', async ({page}) => {
  test.skip(!readerEmail || !process.env.E2E_ADMIN_ID || !!process.env.E2E_PAGING_THREAD);
  test.setTimeout(150000);
  await page.emulateMedia({reducedMotion:'reduce'});
  const bounds = async (selector:string) => {
    const box = await page.locator(selector).first().boundingBox();
    expect(box).not.toBeNull();
    return box!;
  };
  const sameWidth = (actual:{x:number;width:number}, expected:{x:number;width:number}, route:string) => {
    expect(Math.abs(actual.width-expected.width), route+' width').toBeLessThan(1);
    expect(Math.abs(actual.x-expected.x), route+' left edge').toBeLessThan(1);
  };
  const capture = async (name:string, width:number) => {
    if (process.env.SOLITUDES_LAYOUT_SCREENSHOTS && [390,1440].includes(width)) await page.screenshot({path:path.join(process.env.SOLITUDES_LAYOUT_SCREENSHOTS,`${siteTheme}-${name}-${width}.png`),animations:'disabled'});
  };
  for (const width of [390,768,1024,1440]) {
    await page.context().clearCookies();
    await page.setViewportSize({width,height:900});
    await page.goto('/');
    const wide = await bounds('#main-content');
    await capture('home',width);
    const routes = [
      ['/posts/','posts','.posts-listing'], ['/books/','books','.posts-listing'],
      ['/tags/','tags','.tag-cloud'], ['/tags/Topic/','topics','.posts-listing'],
      ['/search/?w=reading','search','.search-section'], ['/readers/','readers','#main-content'],
      ['/users/'+process.env.E2E_ADMIN_ID,'profile','#main-content'],
    ] as const;
    for (const [route,name,folioSelector] of routes) {
      await page.goto(route);
      sameWidth(await bounds(siteTheme === 'folio' ? folioSelector : '#main-content'), wide, route);
      expect(Math.abs((await bounds('#main-content')).y-wide.y),route+' content start').toBeLessThan(1);
      if (siteTheme === 'folio' && await page.locator('.page-header').count()) sameWidth(await bounds('.page-header'),wide,route+' heading');
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
      await capture(name,width);
    }
    let reading: {x:number;width:number}|undefined;
    let titleY: number|undefined;
    for (const slug of [articleSlug!, 'visual-book', 'visual-long-article']) {
      await page.goto('/'+slug);
      const box = await bounds('[data-reading-content]');
      const heading = await bounds('[data-testid="site-article"] h1');
      if (titleY === undefined) titleY = heading.y;
      else expect(Math.abs(heading.y-titleY),slug+' title start').toBeLessThan(1);
      if (reading) sameWidth(box,reading,slug); else reading=box;
      sameWidth(await bounds('[data-testid="article-byline"]'),box,slug+' byline');
      expect(box.width).toBeLessThanOrEqual(wide.width);
      if (siteTheme === 'cactus' || width <= 768) sameWidth(box,wide,slug+' site width');
      await capture(slug,width);
    }
    await page.goto('/visual-page');
    for (const selector of ['#main-content','[data-reading-content]','[data-testid="page-dates"]',siteTheme === 'folio' ? '#comments' : '#reply-list']) {
      sameWidth(await bounds(selector),wide,'standalone page '+selector+' follows site width');
    }
    expect(Math.abs((await bounds('#main-content')).y-wide.y),'standalone page follows site navigation').toBeLessThan(1);
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
    await capture('visual-page',width);
    await page.goto('/login');
    const auth = await bounds('[data-testid="site-auth-page"]');
    await capture('login',width);
    await page.goto('/register');
    sameWidth(await bounds('[data-testid="site-auth-page"]'),auth,'register');
    expect(Math.abs((await bounds('[data-testid="site-auth-page"]')).y-auth.y),'auth start').toBeLessThan(1);
    await capture('register',width);
    for (const [email,password] of [[readerEmail!,readerPassword!],[editorEmail!,readerPassword!],[adminEmail!,adminPassword!]]) {
      await page.context().clearCookies();
      await signIn(page,email,password);
      for (const route of ['/account','/account/oidc/clients']) {
        await page.goto(route);
        sameWidth(await bounds('#main-content'),wide,route);
        expect(Math.abs((await bounds('#main-content')).y-wide.y),route+' start '+width+' '+email).toBeLessThan(1);
        expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
        if (email === readerEmail) await capture(route.replaceAll('/','-'),width);
      }
      await page.goto('/visual-page');
      sameWidth(await bounds('#main-content'),wide,'standalone page for '+email);
      expect(Math.abs((await bounds('#main-content')).y-wide.y),'standalone page start for '+email).toBeLessThan(1);
      sameWidth(await bounds('[data-reading-content]'),wide,'standalone page body for '+email);
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
    }
  }
});

test('three-level books and comment discussions stay navigable in both themes', async ({page}) => {
  test.skip(!readerEmail || !!process.env.E2E_PAGING_THREAD);
  test.setTimeout(120000);
  await page.emulateMedia({reducedMotion:'reduce'});
  const capture = async (name:string) => {
    if (process.env.SOLITUDES_LAYOUT_SCREENSHOTS) await page.screenshot({path:path.join(process.env.SOLITUDES_LAYOUT_SCREENSHOTS,`${siteTheme}-deep-${name}-${page.viewportSize()!.width}.png`),animations:'disabled'});
  };
  for (const width of [390,1440]) {
    await page.context().clearCookies();
    await page.setViewportSize({width,height:900});
    await page.goto('/deep-book-1');
    await expect(page.getByTestId('book-chapters')).toHaveCount(3);
    await expect(page.getByTestId('book-chapter-link')).toHaveCount(8);
    await page.getByTestId('book-chapters').first().scrollIntoViewIfNeeded();
    await capture('tree');
    for (let level=1;level<=3;level++) {
      if (level>1) await page.getByTestId('book-chapter-link').filter({hasText:`Level ${level} ·`}).click();
      for (let n=1;n<=2;n++) {
        await page.locator(`[data-testid="book-chapter-link"][href="/deep-chapter-${level}-${n}"]`).click();
        await expect(page.getByTestId('book-parent')).toHaveAttribute('href',`/deep-book-${level}`);
        const comments = page.getByTestId('comment-entry');
        await expect(comments).toHaveCount(3);
        const root = comments.filter({hasText:'Level 1:'});
        const reply = comments.filter({hasText:'Level 2:'});
        await expect(root.getByTestId('comment-role')).toContainText('Guest');
        await expect(reply.getByTestId('comment-thread-link')).toBeVisible();
        await root.getByTestId('comment-thread-link').click();
        const rootThread = new URL(page.url()).searchParams.get('thread');
        await expect(page.getByTestId('comments-parent')).toHaveCount(0);
        await page.getByTestId('comment-entry').filter({hasText:'Level 2:'}).getByTestId('comment-thread-link').click();
        await expect(page.getByTestId('comment-entry').filter({hasText:'Level 3:'})).toBeVisible();
        await expect(page.getByTestId('comments-parent')).toHaveAttribute('href',`?thread=${rootThread}#comments`);
        expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
        if (n===1) await capture('thread-level-'+level);
        await page.getByTestId('comments-parent').click();
        expect(new URL(page.url()).searchParams.get('thread')).toBe(rootThread);
        await page.getByTestId('comments-back').click();
        expect(new URL(page.url()).searchParams.has('thread')).toBe(false);
        await page.getByTestId('book-parent').click();
      }
    }
    await page.getByTestId('book-parent').click();
    await expect(page).toHaveURL(/\/deep-book-2$/);
    await page.getByTestId('book-parent').click();
    await expect(page).toHaveURL(/\/deep-book-1$/);
  }
  await signIn(page,readerEmail!,readerPassword!);
  await page.goto('/deep-chapter-3-1');
  await page.getByTestId('comment-entry').filter({hasText:'Level 2:'}).getByTestId('comment-thread-link').click();
  await page.getByTestId('comment-entry').filter({hasText:'Level 3:'}).getByTestId('comment-reply').click();
  const message = 'A new reply to the deepest discussion '+siteTheme;
  await page.getByTestId('comment-content').fill(message);
  await page.getByTestId('comment-submit').click();
  await expect(page.getByTestId('comment-entry').filter({hasText:message})).toBeVisible();
  await page.getByTestId('comments-parent').click();
  await expect(page.getByTestId('comment-entry').filter({hasText:'Level 3:'})).toBeVisible();
  await expect(page.getByTestId('comment-entry').filter({hasText:message})).toBeVisible();
});

test('custom home intro stays below the Folio masthead without suppressing its script', async ({ page }) => {
  test.skip(siteTheme !== 'folio' || process.env.SOLITUDES_HOMETOP_FIXTURE !== '1');
  for (const width of [1440, 768, 390, 320]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/');
    const masthead = (await page.getByTestId('folio-masthead').boundingBox())!;
    const custom = (await page.getByTestId('home-top-content').boundingBox())!;
    const stories = (await page.getByTestId('folio-frontpage').boundingBox())!;
    expect(custom.y).toBeGreaterThanOrEqual(masthead.y + masthead.height);
    expect(stories.y).toBeGreaterThanOrEqual(custom.y + custom.height);
    await expect(page.locator('#yearsSinceWorking')).toHaveText(/^\d+$/);
    await expect(page.locator('#yearsSinceRemoteWorking')).toHaveText(/^\d+$/);
    await expect(page.locator('.homepage-btn')).toHaveAttribute('href', 'https://nai.ba');
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
  }
});

test('server failures hide internal details in both HTML and plain-text responses', async ({ page, request }) => {
  const html = await page.goto('/__e2e/server-error', { waitUntil: 'domcontentloaded' });
  expect(html?.status()).toBe(500);
  await expect(page.locator('body')).not.toContainText('private-database-token-must-not-leak');
  await expect(page.locator('body')).toContainText('500');
  expect(html?.headers()['cache-control']).toContain('no-store');

  const plain = await request.get('/__e2e/server-error', { headers: { Accept: 'text/plain' } });
  expect(plain.status()).toBe(500);
  expect(await plain.text()).not.toContain('private-database-token-must-not-leak');
  expect(plain.headers()['cache-control']).toContain('no-store');
});

test('invalid application metadata cannot initiate authorization', async ({ page }) => {
  const clientID = process.env.E2E_INVALID_OIDC_CLIENT_ID;
  test.skip(!clientID || !oidcRedirectURI, 'Requires an isolated invalid client.');
  const query = new URLSearchParams({client_id:clientID!, redirect_uri:oidcRedirectURI!, response_type:'code', scope:'openid',
    code_challenge:createHash('sha256').update('v'.repeat(64)).digest('base64url'), code_challenge_method:'S256'});
  const response = await page.request.get('/authorize?'+query, {maxRedirects:0});
  expect(response.status()).toBe(400);
  expect(response.headers()['location']).toBeUndefined();
  expect(await response.text()).not.toContain('javascript:');
});

test('article comment submission and reply use shared site selectors', async ({ page }) => {
  test.skip(!articleSlug || !articleTitle, 'Set E2E_ARTICLE_SLUG and E2E_ARTICLE_TITLE (isolated test data).');
  const article = await page.goto('/' + articleSlug, { waitUntil: 'domcontentloaded' });
  expect(article?.status()).toBe(200);
  await expect(page.getByTestId('site-article')).toContainText(articleTitle!);
  for (const id of ['comment-form', 'comment-content', 'comment-nickname', 'comment-email', 'comment-submit']) {
    await expect(page.getByTestId(id)).toBeVisible();
  }
  const comment = `Browser comment ${Date.now()}`;
  await page.getByTestId('comment-nickname').fill('Browser Reader');
  await page.getByTestId('comment-email').fill('browser@example.com');
  await page.getByTestId('comment-content').fill(comment);
  await page.getByTestId('comment-submit').click();
  await expect(page.getByText(comment, { exact: true })).toBeVisible();
  expect(await page.evaluate(() => localStorage.getItem('solitudes_cm_nickname'))).toBe('Browser Reader');
  await page.getByTestId('comment-reply').last().click();
  await expect(page.locator('#id_reply_to')).not.toHaveValue('');
});

test('Cactus article menu stays aligned and usable through resize and scroll', async ({ page }) => {
  test.skip(siteTheme !== 'cactus', 'Cactus article navigation.');
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  for (const width of [768, 900, 1200, 1440]) {
    await page.setViewportSize({ width, height: 700 });
    await page.goto('/visual-long-article', { waitUntil: 'domcontentloaded' });
    const toggle = page.locator(width < 900 ? '#menu-icon-tablet' : '#menu-icon');
    await expect(toggle).toBeVisible();
    if (await toggle.getAttribute('aria-expanded') === 'false') await toggle.click();
    const link = page.locator('#header-post #nav a').last();
    await expect(link).toBeVisible();
    const buttonBox = (await toggle.boundingBox())!;
    const linkBox = (await link.boundingBox())!;
    expect(buttonBox.height, 'desktop menu stays compact').toBe(32);
    expect(linkBox.height, 'desktop links keep the original text-height hit area').toBe(15);
    expect(Math.abs(buttonBox.y + buttonBox.height / 2 - linkBox.y - linkBox.height / 2), `menu alignment at ${width}px`).toBeLessThan(2);
    await page.evaluate(() => window.scrollTo(0, 200));
    await expect.poll(() => page.evaluate(() => window.scrollY)).toBeGreaterThan(64);
    await expect(toggle).toBeVisible();
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');
    await expect(link).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
    await expect(toggle).toBeFocused();
    await expect(link).not.toBeVisible();
    await page.keyboard.press('Enter');
    await expect(link).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
  }
  for (const route of ['/search/', '/visual-page', '/visual-closed-1', '/visual-closed-2']) {
    await page.goto(route, { waitUntil: 'domcontentloaded' });
    await page.evaluate(() => window.scrollTo(0, 300));
  }
  await page.setViewportSize({ width: 390, height: 900 });
  await page.goto('/visual-editor-post', { waitUntil: 'domcontentloaded' });
  await page.locator('#menu-footer').click();
  expect((await page.locator('#nav-footer ul').evaluate(node => getComputedStyle(node).gridTemplateColumns)).split(' ')).toHaveLength(2);
  for (const link of await page.locator('#nav-footer a').all()) {
    const box = (await link.boundingBox())!;
    expect(box.height, 'mobile article navigation labels must not split into narrow vertical columns').toBe(44);
  }
  expect(errors, 'article, page and search scripts must not fail on scroll or missing comment forms').toEqual([]);
});

test('Cactus article menu retains previous next and back to top without panel decoration', async ({page}) => {
  test.skip(siteTheme !== 'cactus');
  test.setTimeout(90000);
  for (const width of [501,768,900,1200,1440,1920]) {
    await page.setViewportSize({width,height:700});
    for (const direction of ['previous','next']) {
      await page.goto('/visual-editor-post');
      const toggle = page.locator('#menu-icon, #menu-icon-tablet').filter({visible:true});
      if (await toggle.getAttribute('aria-expanded') === 'false') await toggle.click();
      const link = page.getByTestId('article-'+direction);
      await expect(link).toBeVisible();
      const href = (await link.getAttribute('href'))!;
      expect(href).toMatch(/^\/[^/]/);
      await link.click();
      await expect.poll(()=>new URL(page.url()).pathname).toBe(href);
      await expect(page.getByTestId('site-article')).toBeVisible();
    }
    await page.goto('/visual-long-article');
    await page.evaluate(()=>window.scrollTo(0,600));
    await expect.poll(()=>page.evaluate(()=>window.scrollY)).toBeGreaterThan(300);
    const toggle = page.locator('#menu-icon, #menu-icon-tablet').filter({visible:true});
    if (await toggle.getAttribute('aria-expanded') === 'false') await toggle.click();
    for (const selector of ['#header-post','#header-post #nav','#actions','#toc']) {
      const node = page.locator(selector);
      await expect(node).toBeVisible();
      await expect(node).toHaveCSS('background-color','rgba(0, 0, 0, 0)');
      await expect(node).toHaveCSS('box-shadow','none');
      await expect(node).toHaveCSS('border-top-width','0px');
    }
    await page.getByTestId('article-back-top').click();
    await expect.poll(()=>page.evaluate(()=>window.scrollY)).toBe(0);
    expect(await page.evaluate(()=>document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
  }
  // An end chapter must not manufacture a nonexistent neighbour.
  await page.goto('/deep-chapter-3-1');
  await expect(page.getByTestId('article-previous')).toHaveCount(0);
  await expect(page.getByTestId('article-next')).toHaveAttribute('href','/deep-chapter-3-2');
  await page.goto('/deep-chapter-3-2');
  await expect(page.getByTestId('article-previous')).toHaveAttribute('href','/deep-chapter-3-1');
  await expect(page.getByTestId('article-next')).toHaveCount(0);
});

test('Cactus action buttons do not move under the pointer and share works on the first click', async ({ page, browser }) => {
  test.skip(siteTheme !== 'cactus', 'Cactus article actions.');
  for (const width of [900, 1200, 1440, 1800, 1920]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/visual-editor-post', { waitUntil: 'load' });
    const toggle = page.locator('#menu-icon');
    if (await toggle.getAttribute('aria-expanded') === 'false') await toggle.click();
    await expect(page.locator('#actions')).toBeVisible();
    for (const action of await page.locator('#actions .icon').all()) {
      await page.mouse.move(0, 0);
      const before = (await action.boundingBox())!;
      await page.mouse.move(before.x + before.width / 2, before.y + before.height / 2, { steps: 8 });
      const after = (await action.boundingBox())!;
      expect(after, 'hover labels must never move the click target').toEqual(before);
      const hint = action.locator('..').locator('.action-hint');
      const targetTitle = await action.getAttribute('title');
      await expect(hint).toBeVisible();
      await expect(hint).toHaveText(targetTitle || (await action.getAttribute('aria-label'))!);
      await expect(page.locator('#actions .action-hint').filter({visible:true})).toHaveCount(1);
      await expect(hint).toHaveCSS('background-color','rgba(0, 0, 0, 0)');
      await expect(hint).toHaveCSS('pointer-events','none');
      const hintBox = (await hint.boundingBox())!;
      const toolbar = (await page.locator('#actions').boundingBox())!;
      expect(hintBox.x).toBeGreaterThanOrEqual(0);
      expect(hintBox.x+hintBox.width).toBeLessThan(toolbar.x);
    }
    await page.mouse.move(0,0);
    await expect(page.locator('#actions .action-hint').filter({visible:true})).toHaveCount(0);
    const share = page.locator('#actions [data-share-open]');
    const box = (await share.boundingBox())!;
    // Raw coordinates: locator.click() would retry/re-aim and hide this regression.
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2, { steps: 8 });
    await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
    await expect(page.getByTestId('article-share-dialog')).toBeVisible();
    await expect(page.getByTestId('article-share-copy')).toBeFocused();
    await page.keyboard.press('Escape');
    await expect(page.getByTestId('article-share-dialog')).not.toBeVisible();
    await expect(share).toBeFocused();
  }
  const touchContext = await browser.newContext({ hasTouch: true });
  try {
    const touchPage = await touchContext.newPage();
    for (const width of [900, 1440]) {
      await touchPage.setViewportSize({ width, height: 900 });
      await touchPage.goto(new URL('/visual-editor-post', process.env.E2E_BASE_URL!).href, { waitUntil: 'load' });
      const toggle = touchPage.locator('#menu-icon');
      if (await toggle.getAttribute('aria-expanded') === 'false') await toggle.tap();
      expect((await toggle.boundingBox())!.height).toBe(44);
      await touchPage.getByTestId('article-share-menu').tap();
      const panel = (await touchPage.getByTestId('article-share-dialog').boundingBox())!;
      expect(panel.x).toBeGreaterThanOrEqual(0);
      expect(panel.x + panel.width).toBeLessThanOrEqual(width);
      await touchPage.locator('[data-share-close]').tap();
    }
  } finally {
    await touchContext.close();
  }
});

test('Cactus action title hints support keyboard focus and long titles without moving controls', async ({page}) => {
  test.skip(siteTheme !== 'cactus');
  for (const width of [501,900,1440,1920]) {
    await page.setViewportSize({width,height:900});
    await page.goto('/visual-editor-post');
    const toggle = page.locator('#menu-icon, #menu-icon-tablet').filter({visible:true});
    if (await toggle.getAttribute('aria-expanded') === 'false') await toggle.click();
    await page.mouse.move(0,0);
    await page.keyboard.press('Tab');
    for (const action of await page.locator('#actions .icon').all()) {
      const before = (await action.boundingBox())!;
      await action.focus();
      const hint = action.locator('..').locator('.action-hint');
      await expect(hint).toBeVisible();
      await expect(page.locator('#actions .action-hint').filter({visible:true})).toHaveCount(1);
      expect(await action.boundingBox()).toEqual(before);
    }
    const action = page.getByTestId('article-previous');
    const title = '把读者的问题变成下一篇文章：多作者协作中的阅读、讨论与修订 / A very long article title '+ 'unbroken'.repeat(15);
    const hint = action.locator('..').locator('.action-hint');
    await action.evaluate((node,title)=>node.setAttribute('title',title),title);
    await hint.evaluate((node,title)=>{node.textContent=title;},title);
    const before = (await action.boundingBox())!;
    await action.focus();
    await expect(hint).toBeVisible();
    await expect(action).toHaveAttribute('title',title);
    const box = (await hint.boundingBox())!;
    const line = await hint.evaluate(node=>parseFloat(getComputedStyle(node).lineHeight));
    expect(box.height).toBeLessThanOrEqual(line*2+1);
    expect(box.x).toBeGreaterThanOrEqual(0);
    expect(await action.boundingBox()).toEqual(before);
    await page.keyboard.press('Escape');
    await expect(toggle).toBeFocused();
    await expect(hint).not.toBeVisible();
  }
});

test('closed articles and pages remain error-free for all viewer roles', async ({ page }) => {
  test.skip(!adminEmail || !readerEmail || !editorEmail, 'Requires isolated accounts.');
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  for (const role of ['guest', 'reader', 'editor', 'admin'] as const) {
    if (role !== 'guest') await signIn(page, role === 'admin' ? adminEmail! : role === 'editor' ? editorEmail! : readerEmail!, role === 'admin' ? adminPassword! : readerPassword!);
    for (const route of ['/visual-closed-1', '/visual-closed-2']) {
      expect((await page.goto(route, { waitUntil: 'load' }))?.status()).toBe(200);
      await expect(page.getByTestId('comment-form')).toHaveCount(0);
      expect(errors).toEqual([]);
    }
    if (role !== 'guest') {
      await page.goto('/account');
      await page.getByTestId('account-logout').click();
    }
  }
});

test('comment previews are unnumbered without removing Markdown list numbers or reusing stale styles', async ({ page }) => {
  // Old asset URLs can stay in a returning visitor's cache for 30 days.
  // Poison those URLs so accidentally restoring one fails this regression.
  await page.route(/\/static\/site\/(cactus\/css\/main\.css\?v2026092706|folio\/css\/style\.css\?v2026092705)$/, route =>
    route.fulfill({ contentType: 'text/css', body: '.home-comments-list, .topic-comments-preview { list-style: decimal !important; }' }));
  for (const width of [1280, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/', { waitUntil: 'load' });
    for (const selector of ['.home-comments-list', '.topic-comments-preview']) {
      const list = page.locator(selector).first();
      await expect(list).toBeVisible();
      expect(await list.evaluate(node => node.tagName)).toBe('UL');
      await expect(list).toHaveCSS('list-style-type', 'none');
      for (const item of await list.locator(':scope > li').all()) {
        await expect(item).toHaveCSS('list-style-type', 'none');
        expect(await item.evaluate(node => getComputedStyle(node, '::before').content)).toMatch(/^(none|normal|"")$/);
      }
    }
    await page.goto('/visual-page', { waitUntil: 'load' });
    const ordered = page.getByTestId('site-page').locator('ol').filter({ hasText: 'Choose a story' });
    await expect(ordered).toBeVisible();
    await expect(ordered).toHaveCSS('list-style-type', 'decimal');
  }
});

test('topics keep compact comments inside the bubble and author metadata outside', async ({ page }) => {
  for (const width of [1280, 390, 320]) {
    await page.setViewportSize({ width, height: 900 });
    for (const route of ['/', '/tags/Topic/']) {
      await page.goto(route, { waitUntil: 'domcontentloaded' });
      const quiet = page.locator('[data-topic-slug="visual-topic"]');
      const busy = page.locator('[data-topic-slug="visual-topic-discussion"]');
      for (const entry of [quiet, busy]) {
        await expect(entry).toBeVisible();
        await expect(entry.getByTestId('topic-content').getByTestId('article-author-link')).toHaveCount(0);
        const content = (await entry.getByTestId('topic-bubble').boundingBox())!;
        const byline = (await entry.getByTestId('topic-byline').boundingBox())!;
        expect(byline.y, 'author row belongs outside and below the content bubble').toBeGreaterThanOrEqual(content.y + content.height);
        await expect(entry.getByTestId('article-author-link')).toHaveAttribute('href', /^\/users\//);
        await expect(entry.getByTestId('topic-reply-link')).toBeVisible();
      }
      await expect(quiet.getByTestId('topic-comments')).toHaveCount(0);
      await expect(busy.getByTestId('topic-comment')).toHaveCount(4);
      for (const role of ['admin', 'editor', 'user', 'guest']) await expect(busy.locator('.role-' + role)).toHaveCount(1);
      await expect(busy).not.toContainText('topic-spam-must-not-render');
      await expect(busy.getByTestId('comment-author-link')).toHaveCount(3);
      const preview = (await busy.getByTestId('topic-comments').boundingBox())!;
      const bubble = (await busy.getByTestId('topic-bubble').boundingBox())!;
      await expect(busy.getByTestId('topic-bubble').getByTestId('topic-comments')).toHaveCount(1);
      expect(preview.y).toBeGreaterThan(bubble.y);
      expect(preview.y + preview.height).toBeLessThan(bubble.y + bubble.height);
      expect(preview.height, 'four comment previews remain compact at every width').toBeLessThan(130);
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
      const link = busy.getByTestId('topic-comment-link').first();
      const target = await link.getAttribute('href');
      const badgeColors = await busy.locator('.comment-role').evaluateAll(badges => badges.map(badge => ({
        role: Array.from(badge.classList).find(name => name.startsWith('role-'))!,
        color: getComputedStyle(badge).color,
      })));
      await link.click();
      await expect(page).toHaveURL(new RegExp(target!.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '$'));
      await expect(page.locator(new URL(page.url()).hash)).toBeVisible();
      // A direct comment link opens that thread, not an unrelated first page.
      // Return to the whole discussion before comparing every role badge.
      await page.getByTestId('comments-back').click();
      for (const badge of badgeColors) {
        await expect(page.getByTestId('comment-role').and(page.locator('.' + badge.role)).first(),
          'article comments preserve the readable role colors from the preview').toHaveCSS('color', badge.color);
      }
    }
  }
});

test('public profiles show content directly without redundant anchor controls', async ({ page }) => {
  test.setTimeout(90000);
  test.skip(!adminEmail || !readerEmail || !editorEmail, 'Requires isolated profile fixtures.');
  await page.emulateMedia({ reducedMotion: 'reduce' });
  const adminID = process.env.E2E_ADMIN_ID!;
  for (const role of ['guest', 'reader', 'editor', 'admin'] as const) {
    const email = role === 'admin' ? adminEmail : role === 'editor' ? editorEmail : readerEmail;
    const ownerID = role === 'guest' || role === 'admin' ? adminID :
      role === 'reader' ? process.env.E2E_READER_ID! : process.env.E2E_EDITOR_ID!;
    if (role !== 'guest') await signIn(page, email!, role === 'admin' ? adminPassword! : readerPassword!);
    for (const width of [1280, 390, 320]) {
      await page.setViewportSize({ width, height: 900 });
      await page.goto(`/users/${ownerID}`, { waitUntil: 'domcontentloaded' });
      await expect(page.locator('.public-profile-toolbar').getByTestId('profile-reader-circle')).toBeVisible();
      await expect(page.locator('.public-profile-toolbar').getByTestId('profile-edit-link')).toHaveCount(role === 'guest' ? 0 : 1);
      await expect(page.locator('.public-profile-intro a')).toHaveCount(0);
      await expect(page.locator('.public-profile-header a[href^="#"]')).toHaveCount(0);
      await expect(page.locator('#profile-articles')).toBeVisible();
      await expect(page.locator('#profile-comments')).toBeVisible();
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
      await page.getByTestId('profile-reader-circle').focus();
      await expect(page.getByTestId('profile-reader-circle')).toHaveCSS('outline-style', 'solid');
    }
    await page.getByTestId('profile-reader-circle').click();
    await expect(page).toHaveURL(/\/readers\/$/);
    if (role !== 'guest') {
      const otherID = role === 'admin' ? process.env.E2E_READER_ID! : adminID;
      await page.goto(`/users/${otherID}`, { waitUntil: 'domcontentloaded' });
      await expect(page.getByTestId('profile-edit-link')).toHaveCount(0);
      await page.goto(`/users/${ownerID}`, { waitUntil: 'domcontentloaded' });
      await page.getByTestId('profile-edit-link').click();
      await expect(page).toHaveURL(/\/account$/);
      await page.getByTestId('account-logout').click();
    }
  }
});

test('public profiles show only public activity and comments distinguish every identity', async ({ page }) => {
  test.skip(!articleSlug || !adminEmail || !readerEmail || !editorEmail, 'Requires isolated accounts and articles.');
  const adminID = process.env.E2E_ADMIN_ID!;
  const readerID = process.env.E2E_READER_ID!;
  const editorID = process.env.E2E_EDITOR_ID!;
  await page.goto('/posts/', { waitUntil: 'domcontentloaded' });
  const listedPost = page.getByTestId('article-author-link').first().locator('xpath=ancestor::li[1]');
  await expect(listedPost).toBeVisible();
  expect(await listedPost.evaluate(node => {
    const title = node.querySelector('.post-title-row a, .article-card-title');
    const author = node.querySelector('[data-testid="article-author-link"]');
    const date = node.querySelector('time');
    return !!title && !!author && !!date &&
      !!(title.compareDocumentPosition(author) & Node.DOCUMENT_POSITION_FOLLOWING) &&
      !!(author.compareDocumentPosition(date) & Node.DOCUMENT_POSITION_FOLLOWING);
  })).toBe(true);
  await expect(listedPost.getByTestId('article-author-link')).toHaveAttribute('rel', 'author');
  await page.goto('/' + articleSlug, { waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('article-author-link').first()).toHaveAttribute('href', `/users/${adminID}`);
  await expect(page.getByTestId('article-author-link').first()).toHaveAttribute('rel', 'author');
  await expect(page.locator('meta[name="author"]')).toHaveAttribute('content', /Browser Admin/);
  await expect(page.getByTestId('comment-sign-in')).toBeVisible();
  await expect(page.getByTestId('comment-register')).toHaveAttribute('href', `/register?return_to=%2F${articleSlug}%23reply`);
  await expect(page.getByTestId('comment-sign-in')).toHaveCSS('min-height', '44px');
  await page.getByTestId('article-author-link').first().click();
  await expect(page.getByTestId('public-profile')).toBeVisible();
  await expect(page.locator('meta[name="author"]')).toHaveAttribute('content', /Browser Admin/);
  await expect(page.locator('link[rel="canonical"]')).toHaveAttribute('href', new RegExp(`/users/${adminID}$`));
  await expect(page.locator('.public-profile-header a[href^="#"]')).toHaveCount(0);
  await expect(page.getByTestId('profile-reader-circle')).toHaveAttribute('href', '/readers/');
  await page.waitForLoadState('domcontentloaded');
  const profileArticle = page.getByTestId('public-profile-article').filter({ hasText: articleTitle! });
  // Earlier publishing tests can move this shared article off the first page.
  // Follow the actual pagination UI rather than assuming it is always recent.
  for (let n = 0; n < 20 && await profileArticle.count() === 0; n++) {
    await expect(page.locator('body')).not.toContainText('Private profile draft');
    const next = page.locator('#profile-articles').getByTestId('pagination-next');
    await expect(next, `Missing ${articleSlug} at ${page.url()}; articles: ${await page.getByTestId('public-profile-article').allTextContents()}`).toBeVisible();
    await next.click();
    await page.waitForLoadState('domcontentloaded');
  }
  await expect(profileArticle).toHaveAttribute('href', '/' + articleSlug);
  await expect(page.locator('body')).not.toContainText(adminEmail!);
  await expect(page.locator('body')).not.toContainText('Private profile draft');
  await expect(page.locator('body')).not.toContainText('Private comment content');
  await expect(page.locator('body')).not.toContainText('Spam comment content');
  await expect(page.locator('body')).not.toContainText('Reply to spam root');
  expect((await page.request.get('/users/not-a-uuid')).status()).toBe(404);
  expect((await page.request.get(`/users/${adminID}?comments_page=1001`)).status()).toBe(400);

  await page.goto('/' + articleSlug, { waitUntil: 'domcontentloaded' });
  const guestMessage = `Guest identity ${Date.now()}`;
  await page.getByTestId('comment-nickname').fill('Browser Admin');
  await page.getByTestId('comment-email').fill(adminEmail!);
  await page.getByTestId('comment-content').fill(guestMessage);
  await page.getByTestId('comment-submit').click();
  const guest = page.locator('[id^="comment-"]').filter({ hasText: guestMessage }).first();
  await expect(guest.getByTestId('comment-role')).toContainText('Guest');
  await expect(guest.getByTestId('comment-author-link')).toHaveCount(0);

  await signIn(page, readerEmail!, readerPassword!, /\/account/);
  await page.goto('/account', { waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('account-public-profile')).toHaveAttribute('href', `/users/${readerID}`);
  await page.getByTestId('account-profile-nickname').fill('Browser Reader');
  await page.getByTestId('account-profile-bio').fill('<img src=x onerror=alert(1)> Reader bio');
  await page.getByTestId('account-profile-save').click();
  await expect(page).toHaveURL(/profile_updated=1/);
  await page.getByTestId('account-public-profile').click();
  await expect(page.getByTestId('public-profile-bio')).toContainText('<img src=x onerror=alert(1)>');
  await expect(page.locator('.public-profile-bio img')).toHaveCount(0);
  await expect(page.getByTestId('profile-edit-link')).toHaveAttribute('href', '/account');
  await expect(page.locator('body')).not.toContainText(readerEmail!);
  await page.setViewportSize({ width: 390, height: 844 });
  const bioBox = (await page.getByTestId('public-profile-bio').boundingBox())!;
  const profileHeaderBox = (await page.locator('.public-profile-header').boundingBox())!;
  expect(bioBox.width, 'mobile biography uses the full card width').toBeGreaterThan(profileHeaderBox.width * .75);
  await page.setViewportSize({ width: 1280, height: 900 });

  await page.goto('/' + articleSlug, { waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('comment-nickname')).toHaveCount(0);
  await expect(page.getByTestId('comment-content')).toBeVisible();
  const memberUIMessage = `Signed-in comment ${Date.now()}`;
  await page.getByTestId('comment-content').fill(memberUIMessage);
  await page.getByTestId('comment-submit').click();
  const memberUI = page.locator('[id^="comment-"]').filter({ hasText: memberUIMessage }).first();
  await expect(memberUI.getByTestId('comment-role')).toContainText('Member');
  const postAs = async (message: string) => {
    const response = await page.request.post('/api/comment', {
      headers: { Origin: new URL(page.url()).origin },
      data: { nickname: 'Spoofed Administrator', email: 'not-a-valid-email', website: 'not-a-url', content: message,
        version: 1, slug: articleSlug },
    });
    expect(response.status()).toBe(200);
    const created = await response.json();
    await page.goto('/'+articleSlug+'?thread='+created.id+'#comment-'+created.id, { waitUntil: 'domcontentloaded' });
    return page.locator('[id^="comment-"]').filter({ hasText: message }).first();
  };
  const member = await postAs(`Member identity ${Date.now()}`);
  await expect(member.getByTestId('comment-role')).toContainText('Member');
  await expect(member.getByTestId('comment-author-link')).toHaveAttribute('href', `/users/${readerID}`);
  await expect(member).not.toContainText('Spoofed Administrator');
  await page.goto(`/users/${readerID}`, { waitUntil: 'domcontentloaded' });
  await expect(page.locator('meta[name="robots"]')).toHaveCount(0);
  await expect(page.locator('link[rel="canonical"]')).toHaveAttribute('href', new RegExp(`/users/${readerID}$`));
  await expect(page.getByTestId('public-profile-comment').first()).toContainText('Member identity');
  await expect(page.getByTestId('public-profile-comment-article').first()).toContainText(articleTitle!);
  await page.goto('/account', { waitUntil: 'domcontentloaded' });
  await page.getByTestId('account-logout').click();

  await signIn(page, editorEmail!, readerPassword!);
  await page.goto('/' + articleSlug, { waitUntil: 'domcontentloaded' });
  const editor = await postAs(`Editor identity ${Date.now()}`);
  await expect(editor.getByTestId('comment-role')).toContainText('Editor');
  await expect(editor.getByTestId('comment-author-link')).toHaveAttribute('href', `/users/${editorID}`);
  await page.goto('/account', { waitUntil: 'domcontentloaded' });
  await page.getByTestId('account-logout').click();

  await signIn(page);
  await page.goto('/' + articleSlug, { waitUntil: 'domcontentloaded' });
  const admin = await postAs(`Administrator identity ${Date.now()}`);
  await expect(admin.getByTestId('comment-role')).toContainText('Administrator');
  await expect(admin.getByTestId('comment-author-link')).toHaveAttribute('href', `/users/${adminID}`);
});

test('admin navigation, authoring, identity, users, and OIDC controls share selectors', async ({ page }) => {
  test.skip(!adminEmail || !adminPassword, 'Set isolated E2E_ADMIN_EMAIL / E2E_ADMIN_PASSWORD.');
  await signIn(page);
  await page.goto('/admin/publish', { waitUntil: 'domcontentloaded' });
  for (const id of ['publish-title', 'publish-slug', 'publish-tags', 'publish-template',
    'publish-content', 'publish-visibility', 'publish-submit']) {
    await expect(page.getByTestId(id)).toBeVisible();
  }
  await page.goto('/account', { waitUntil: 'domcontentloaded' });
  for (const id of ['account-passkey-add', 'account-password-form', 'account-current-password',
    'account-new-password', 'account-password-submit', 'account-logout']) {
    await expect(page.getByTestId(id)).toBeVisible();
  }
  await page.goto('/admin/users', { waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('user-role-form').first()).toBeVisible();
  await expect(page.getByTestId('user-role-select').first()).toBeVisible();
  expect((await page.goto('/admin/media', { waitUntil: 'domcontentloaded' }))?.status()).toBe(200);
  await page.goto('/admin/oidc/clients', { waitUntil: 'domcontentloaded' });
  for (const id of ['oidc-client-form', 'oidc-client-name', 'oidc-client-description', 'oidc-client-homepage', 'oidc-client-redirects',
    'oidc-client-public', 'oidc-client-submit', 'oidc-rotate-keys']) {
    await expect(page.getByTestId(id)).toBeVisible();
  }
  await expect(page.locator(`link[href*="/static/admin/${adminTheme}/"]`).first()).toHaveCount(1);
});

test('publish button creates an article from the default admin workspace', async ({ page }) => {
  test.skip(!adminEmail || !adminPassword, 'Set isolated E2E_ADMIN_EMAIL / E2E_ADMIN_PASSWORD.');
  await signIn(page);
  // The editor is CDN-hosted. Stub only its editor widget so the browser test
  // exercises Solitudes' real publish button and HTTP route while offline.
  await page.route(/https:\/\/cdn\.jsdelivr\.net\/npm\/vditor@[^/]+\/dist\/index\.min\.js/, route =>
    route.fulfill({ contentType: 'application/javascript', body: `window.Vditor = class {
      constructor(id, options) {
        this.value = options.value || '';
        window.__testEditor = this;
        document.getElementById(id).setAttribute('data-editor-ready', 'true');
        queueMicrotask(() => { if (options.after) options.after(); });
      }
      getValue() { return this.value; }
      setValue(value) { this.value = value; }
      setTheme() {}
    };` })
  );
  await page.goto('/admin/publish', { waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('publish-content')).toHaveAttribute('data-editor-ready', 'true');
  const slug = `e2e-ui-${Date.now()}`;
  await page.getByTestId('publish-title').fill('Browser UI post');
  await page.getByTestId('publish-slug').fill(slug);
  await page.evaluate(() => (window as any).__testEditor.setValue('Published using the real button'));
  await page.getByTestId('publish-submit').click();
  await expect.poll(async () => (await page.request.get('/' + slug)).status()).toBe(200);
  if (adminTheme === 'default') {
    // Default redirects to the new article; wait for its own navigation to
    // finish instead of racing it with a second page.goto().
    await expect(page).toHaveURL(new RegExp(`/${slug}$`));
  } else {
    await expect(page).toHaveURL(/\/admin\/publish\?id=/);
    await page.goto('/' + slug, { waitUntil: 'domcontentloaded' });
  }
  await expect(page.getByTestId('site-article')).toContainText('Browser UI post');
});

test('admin can change a reader role from the default admin workspace', async ({ page }) => {
  test.skip(!adminEmail || !adminPassword || !readerEmail, 'Requires isolated admin and reader fixtures.');
  await signIn(page);
  await page.goto('/admin/users', { waitUntil: 'domcontentloaded' });
  const roleForm = page.locator('tr').filter({ hasText: readerEmail! }).getByTestId('user-role-form');
  await roleForm.getByTestId('user-role-select').selectOption('editor');
  await roleForm.getByTestId('user-role-save').click();
  await expect(roleForm.getByTestId('user-role-select')).toHaveValue('editor');
  await roleForm.getByTestId('user-role-select').selectOption('user');
  await roleForm.getByTestId('user-role-save').click();
  await expect(roleForm.getByTestId('user-role-select')).toHaveValue('user');
});

test('admin creates and disables an OIDC client via the UI', async ({ page }) => {
  test.skip(!adminEmail || !adminPassword, 'Requires an isolated admin fixture.');
  await signIn(page);
  await page.goto('/admin/oidc/clients', { waitUntil: 'domcontentloaded' });
  const name = `Browser client ${Date.now()}`;
  await page.getByTestId('oidc-client-name').fill(name);
  await page.getByTestId('oidc-client-homepage').fill('https://browser.example.test/');
  await page.getByTestId('oidc-client-redirects').fill('http://localhost:9999/callback');
  await page.getByTestId('oidc-client-public').check();
  await page.getByTestId('oidc-client-submit').click();
  await expect(page.getByTestId('oidc-created-id')).not.toBeEmpty();
  await page.getByTestId('oidc-created-back').click();
  const client = page.locator('li').filter({ has: page.getByText(name, { exact: true }) });
  await client.getByTestId('oidc-client-disable').click();
  await expect(page.locator('li').filter({ has: page.getByText(name, { exact: true }) })).toContainText('(');
});

test('ordinary readers can register their own OIDC app but cannot manage another client', async ({ page }) => {
  test.skip(!readerEmail || !readerPassword || !oidcClientID, 'Requires isolated reader and another client.');
  await signIn(page, readerEmail!, readerPassword!, /\/account(?:\?|$)/);
  await page.goto('/', { waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('site-account-nav')).toHaveAttribute('href', '/account');
  await page.getByTestId('site-account-nav').click();
  await expect(page).toHaveURL(/\/account$/);
  await expect(page.locator(`link[href*="/static/site/${siteTheme}/"]`).first()).toHaveCount(1);
  await page.addStyleTag({ content: '*, *::before, *::after { animation: none !important; transition: none !important; }' });
  await page.getByTestId('account-oidc-clients').click();
  await expect(page).toHaveURL(/\/account\/oidc\/clients$/);
  expect((await page.request.get('/admin/oidc/clients', { maxRedirects: 0 })).status()).toBe(403);
  await expect(page.getByText('Browser external app', { exact: true })).toHaveCount(0);
  const name = `My app ${Date.now()}`;
  await page.getByTestId('oidc-client-name').fill(name);
  await page.getByTestId('oidc-client-homepage').fill('https://reader.example.test/');
  await page.getByTestId('oidc-client-redirects').fill('http://localhost:9998/callback');
  await page.getByTestId('oidc-client-public').check();
  await page.getByTestId('oidc-client-submit').click();
  const clientID = await page.getByTestId('oidc-created-id').textContent();
  expect(clientID).toBeTruthy();
  await page.getByTestId('oidc-created-back').click();
  await expect(page.getByText(name, { exact: true })).toBeVisible();
  const denied = await page.request.post(`/account/oidc/clients/${oidcClientID}/disable`,
    { headers: { Origin: new URL(page.url()).origin }, maxRedirects: 0 });
  expect(denied.status()).toBe(404);
  const forbiddenEdit = await page.request.post(`/account/oidc/clients/${oidcClientID}/metadata`, {
    headers: { Origin: new URL(page.url()).origin }, maxRedirects: 0,
    form: { name: 'Takeover', homepage_url: 'https://takeover.example.test/', redirect_uris: 'https://takeover.example.test/callback' },
  });
  expect(forbiddenEdit.status()).toBe(404);
  const own = page.locator('li').filter({ has: page.getByText(name, { exact: true }) });
  await own.locator('details summary').click();
  await own.getByTestId('oidc-client-edit-form').locator('[name="description"]').fill('Reader-owned application');
  await own.getByTestId('oidc-client-edit-form').locator('[name="homepage_url"]').fill('https://updated-reader.example.test/');
  await own.getByTestId('oidc-client-edit-save').click();
  await expect(page.locator('li').filter({ has: page.getByText(name, { exact: true }) })).toContainText('Application website');
  await expect(page.locator('li').filter({ has: page.getByText(name, { exact: true }) }).locator('a[href="https://updated-reader.example.test/"]')).toBeVisible();
  await page.locator('li').filter({ has: page.getByText(name, { exact: true }) }).getByTestId('oidc-client-disable').click();
  await expect(page.locator('li').filter({ has: page.getByText(name, { exact: true }) })).toContainText('(');
});

test('front account can unlink an OAuth identity and start a PKCE-protected account link', async ({ page }) => {
  test.skip(!readerEmail || !readerPassword, 'Requires an isolated reader with a linked GitHub account.');
  await signIn(page, readerEmail!, readerPassword!, /\/account(?:\?|$)/);
  await expect(page.getByTestId('account-oauth-unlink')).toBeVisible();
  await expect(page.getByTestId('account-link-github')).toHaveCount(0);
  await page.getByTestId('account-oauth-unlink').click();
  await expect(page.getByTestId('account-oauth-unlink')).toHaveCount(0);
  await expect(page.getByTestId('account-link-github')).toBeVisible();
  const redirect = await page.request.post('/auth/github/link', {
    headers: { Origin: new URL(page.url()).origin }, maxRedirects: 0,
  });
  expect(redirect.status()).toBe(302);
  const url = new URL(redirect.headers()['location']!);
  expect(url.origin).toBe('https://github.com');
  expect(url.pathname).toBe('/login/oauth/authorize');
  expect(url.searchParams.get('state')).toMatch(/^[a-f0-9]{64}$/);
  expect(url.searchParams.get('code_challenge_method')).toBe('S256');
});

test('reader can register and remove a WebAuthn passkey from the front site', async ({ page }) => {
  test.setTimeout(60000);
  test.skip(!readerEmail || !readerPassword, 'Requires isolated WebAuthn configuration and reader credentials.');
  await signIn(page, readerEmail!, readerPassword!, /\/account(?:\?|$)/);
  const session = await page.context().newCDPSession(page);
  await session.send('WebAuthn.enable');
  await session.send('WebAuthn.addVirtualAuthenticator', {
    options: { protocol: 'ctap2', transport: 'internal', hasResidentKey: true, hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true },
  });
  // Account actions must initialize without waiting for optional CDN scripts
  // in the footer, and must not accept clicks before their handlers are ready.
  let releaseAccount!: () => void;
  let releaseFooter!: () => void;
  const accountReady = new Promise<void>(resolve => { releaseAccount = resolve; });
  const footerReady = new Promise<void>(resolve => { releaseFooter = resolve; });
  await page.route('**/js/account.js?*', async route => { await accountReady; await route.continue(); });
  await page.route('https://cdn.jsdelivr.net/npm/vditor@4.0.0/dist/method.min.js', async route => { await footerReady; await route.abort(); });
  try {
    await page.goto('/account', {waitUntil:'commit'});
    await expect(page.getByTestId('account-passkey-add')).toBeDisabled();
    releaseAccount();
    await expect(page.getByTestId('account-passkey-add')).toBeEnabled();
    const registered = page.waitForResponse(response => new URL(response.url()).pathname === '/account/passkeys/finish');
    await page.getByTestId('account-passkey-add').click({noWaitAfter:true});
    expect((await registered).status()).toBe(201);
  } finally {
    releaseAccount();
    releaseFooter();
  }
  await expect(page.getByTestId('account-passkey-delete')).toBeVisible();
  await page.getByTestId('account-logout').click();
  await page.goto('/login', { waitUntil: 'domcontentloaded' });
  await page.addStyleTag({ content: '*, *::before, *::after { animation: none !important; transition: none !important; }' });
  await page.getByTestId('auth-email').fill(readerEmail!);
  await page.locator('#passkey-login').click();
  await expect(page).toHaveURL(/\/account(?:\?|$)/);
  await expect(page.getByTestId('account-passkey-delete')).toBeVisible();
  page.once('dialog', dialog => dialog.accept());
  await page.getByTestId('account-passkey-delete').click();
  await expect(page.getByTestId('account-passkey-delete')).toHaveCount(0);
  await session.detach();
});

test('reader creates a confidential OAuth client with a one-time secret in each site theme', async ({ page }) => {
  test.skip(!readerEmail || !readerPassword, 'Requires isolated reader credentials.');
  await signIn(page, readerEmail!, readerPassword!, /\/account(?:\?|$)/);
  await page.getByTestId('account-oidc-clients').click();
  await page.getByTestId('oidc-client-name').fill('Private app');
  await page.getByTestId('oidc-client-homepage').fill('https://client.example.com/');
  await page.getByTestId('oidc-client-redirects').fill('https://client.example.com/callback');
  await page.getByTestId('oidc-client-submit').click();
  await expect(page.getByTestId('oidc-created-id')).not.toBeEmpty();
  await expect(page.getByTestId('oidc-created-secret')).not.toBeEmpty();
  await page.getByTestId('oidc-created-back').click();
  await expect(page.getByTestId('oidc-created-secret')).toHaveCount(0);
});

for (const publicClient of [true, false]) {
  test(`member-owned ${publicClient ? 'public' : 'confidential'} OIDC app returns the visiting user identity, not its owner`, async ({ page }) => {
    test.skip(!readerEmail || !editorEmail || !readerPassword || !oidcRedirectURI);
    await signIn(page, readerEmail!, readerPassword!);
    await page.goto('/account/oidc/clients');
    await page.getByTestId('oidc-client-name').fill(`Cross-user ${publicClient ? 'public' : 'confidential'} app`);
    await page.getByTestId('oidc-client-homepage').fill('https://external.example.test/');
    await page.getByTestId('oidc-client-redirects').fill(oidcRedirectURI!);
    await page.getByTestId('oidc-client-public').setChecked(publicClient);
    await page.getByTestId('oidc-client-submit').click();
    const clientID = (await page.getByTestId('oidc-created-id').innerText()).trim();
    const secret = publicClient ? '' : (await page.getByTestId('oidc-created-secret').innerText()).trim();
    await page.goto('/account');
    await page.getByTestId('account-logout').click();
    const discovery = await (await page.request.get('/.well-known/openid-configuration')).json();
    for (const scope of ['openid email profile', 'openid']) {
      const verifier = 'c'.repeat(64);
      const state = 'different-owner-and-user-' + scope;
      const nonce = 'cross-user-nonce-' + scope;
      await page.goto('/authorize?' + new URLSearchParams({client_id: clientID, redirect_uri: oidcRedirectURI!,
        response_type: 'code', scope, state, nonce,
        code_challenge: createHash('sha256').update(verifier).digest('base64url'), code_challenge_method: 'S256'}));
      if (scope !== 'openid') {
        await expect(page).toHaveURL(/\/login\?return_to=/);
        await expect(page.getByTestId('oidc-application-owner')).toHaveAttribute('href', `/users/${process.env.E2E_READER_ID}`);
        await page.getByTestId('auth-email').fill(editorEmail!);
        await page.getByTestId('auth-password').fill(readerPassword!);
        await page.getByTestId('auth-captcha').fill('0');
        await page.getByTestId('auth-submit').click();
      }
      await expect(page).toHaveURL(/\/oidc\/consent\?/);
      await page.getByTestId('oidc-consent-allow').click();
      await expect(page).toHaveURL(/callback\?code=/);
      expect(new URL(page.url()).searchParams.get('state')).toBe(state);
      const form = { grant_type: 'authorization_code', client_id: clientID, redirect_uri: oidcRedirectURI!,
        code: new URL(page.url()).searchParams.get('code')!, code_verifier: verifier };
      const headers: Record<string, string> = publicClient ? {} : {Authorization: 'Basic ' + Buffer.from(clientID + ':' + secret).toString('base64')};
      const response = await page.request.post('/oauth/token', {form, headers});
      expect(response.status()).toBe(200);
      const tokens = await response.json();
      const [encodedHeader, encodedClaims, signature] = tokens.id_token.split('.');
      const jwtHeader = JSON.parse(Buffer.from(encodedHeader, 'base64url').toString());
      expect(jwtHeader.alg).toBe('RS256');
      const jwks = await (await page.request.get(discovery.jwks_uri)).json();
      const jwk = jwks.keys.find((key: any) => key.kid === jwtHeader.kid);
      expect(jwk).toBeTruthy();
      expect(verify('RSA-SHA256', Buffer.from(encodedHeader + '.' + encodedClaims),
        createPublicKey({key: jwk, format: 'jwk'}), Buffer.from(signature, 'base64url'))).toBe(true);
      const claims = JSON.parse(Buffer.from(encodedClaims, 'base64url').toString());
      expect(claims.sub).toBe(process.env.E2E_EDITOR_ID);
      expect(claims.sub).not.toBe(process.env.E2E_READER_ID);
      expect(claims.iss).toBe(discovery.issuer);
      expect([claims.aud].flat()).toContain(clientID);
      expect(claims.nonce).toBe(nonce);
      expect(claims.exp).toBeGreaterThan(Date.now() / 1000);
      const userInfo = await page.request.get('/userinfo', {headers: {Authorization: 'Bearer ' + tokens.access_token}});
      expect(userInfo.status()).toBe(200);
      const profile = await userInfo.json();
      expect(profile.sub).toBe(process.env.E2E_EDITOR_ID);
      if (scope === 'openid') {
        expect(profile).not.toHaveProperty('email');
        expect(profile).not.toHaveProperty('name');
        expect(profile).not.toHaveProperty('nickname');
      } else {
        expect(profile.email).toBe(editorEmail);
        expect(profile.email).not.toBe(readerEmail);
        expect(profile.email_verified).toBe(true);
        expect(profile.name).toBe('Browser Editor');
      }
      expect((await page.request.post('/oauth/token', {form, headers})).status()).toBe(400);
    }
    expect((await page.request.get('/userinfo')).status()).toBe(401);
    expect((await page.request.get('/userinfo', {headers: {Authorization: 'Bearer invalid-token'}})).status()).toBe(401);
  });
}

test('admin navigation groups sign-in providers separately from external OIDC applications', async ({ page }) => {
  test.skip(!adminEmail, 'Requires an isolated administrator.');
  await signIn(page);
  await page.goto('/admin/', { waitUntil: 'domcontentloaded' });
  await page.getByTestId('admin-nav-providers').click();
  await expect(page).toHaveURL(/\/admin\/auth\/providers$/);
  for (const id of ['provider-settings-form', 'provider-github-id', 'provider-github-secret',
    'provider-google-id', 'provider-google-secret', 'provider-oidc-id',
    'provider-oidc-issuer', 'provider-oidc-secret', 'provider-settings-save']) {
    await expect(page.getByTestId(id)).toBeVisible();
  }
  await page.getByTestId('provider-github-id').fill('browser-github-client');
  await page.getByTestId('provider-github-secret').fill('browser-github-secret');
  await page.getByTestId('provider-settings-save').click();
  await expect(page).toHaveURL(/\/admin\/auth\/providers\?saved=1/);
  await expect(page.getByTestId('provider-github-id')).toHaveValue('browser-github-client');
  await expect(page.getByTestId('provider-github-secret')).toHaveValue('');
  expect(await page.content()).not.toContain('browser-github-secret');
  await page.getByTestId('provider-github-disable').check();
  await page.getByTestId('provider-settings-save').click();
  await expect(page.getByTestId('provider-github-id')).toHaveValue('');
});

for (const role of ['user', 'editor', 'admin'] as const) {
  test(`${role} can consent to an external OIDC app and receive their own identity`, async ({ page }) => {
    const email = role === 'user' ? readerEmail : role === 'editor' ? editorEmail : adminEmail;
    test.skip(!email || !oidcClientID || !oidcRedirectURI || !readerPassword, 'Requires isolated users and a public OIDC client.');
    const verifier = 'v'.repeat(64);
    const challenge = createHash('sha256').update(verifier).digest('base64url');
    const query = new URLSearchParams({ client_id: oidcClientID!, redirect_uri: oidcRedirectURI!,
      response_type: 'code', scope: 'openid email profile', state: `browser-${role}`,
      code_challenge: challenge, code_challenge_method: 'S256' });
    await page.goto('/authorize?' + query.toString(), { waitUntil: 'domcontentloaded' });
    await expect(page).toHaveURL(/\/login\?return_to=/);
    await expect(page.getByTestId('oidc-application-name')).toHaveText('Browser external app');
    await expect(page.getByTestId('oidc-application-owner')).toHaveAttribute('href', `/users/${process.env.E2E_ADMIN_ID}`);
    await expect(page.getByTestId('oidc-application-homepage')).toHaveAttribute('href', 'https://client.example.test/');
    await page.addStyleTag({ content: '*, *::before, *::after { animation: none !important; transition: none !important; }' });
    await page.getByTestId('auth-email').fill(email!);
    await page.getByTestId('auth-password').fill(readerPassword!);
    await page.getByTestId('auth-captcha').fill('0');
    await expect(page.locator('input[name="captchaId"]')).not.toHaveValue('');
    await page.getByTestId('auth-submit').click();
    await expect(page).toHaveURL(/\/oidc\/consent\?authRequestID=/);
    await expect(page.getByTestId('oidc-application-owner')).toHaveAttribute('href', `/users/${process.env.E2E_ADMIN_ID}`);
    await expect(page.getByTestId('oidc-application-homepage')).toHaveAttribute('href', 'https://client.example.test/');
    await page.addStyleTag({ content: '*, *::before, *::after { animation: none !important; transition: none !important; }' });
    await expect(page.getByText('Browser external app', { exact: true })).toBeVisible();
    await page.getByTestId('oidc-consent-allow').click();
    await expect(page).toHaveURL(new RegExp(`^${oidcRedirectURI!.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}\\?`));
    const callback = new URL(page.url());
    expect(callback.searchParams.get('state')).toBe(`browser-${role}`);
    const code = callback.searchParams.get('code');
    expect(code).toBeTruthy();
    const token = await page.request.post('/oauth/token', { form: {
      grant_type: 'authorization_code', client_id: oidcClientID!,
      redirect_uri: oidcRedirectURI!, code: code!, code_verifier: verifier,
    } });
    expect(token.status()).toBe(200);
    const body = await token.json();
    expect(body.id_token).toBeTruthy();
    const userInfo = await page.request.get('/userinfo', { headers: { Authorization: `Bearer ${body.access_token}` } });
    expect(userInfo.status()).toBe(200);
    expect((await userInfo.json()).email).toBe(email);
  });
}

test('mobile admin navigation keeps provider settings reachable', async ({ page }) => {
  test.skip(!adminEmail, 'Requires an isolated administrator.');
  await page.setViewportSize({ width: 390, height: 844 });
  await signIn(page);
  await page.goto('/admin/', { waitUntil: 'domcontentloaded' });
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  const toggle = page.locator('.admin-mobile-toggle');
  await toggle.click();
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
  await page.getByTestId('admin-nav-providers').click();
  await expect(page).toHaveURL(/\/admin\/auth\/providers$/);
});

test('admin can trace a reader-owned application, its logins and security events', async ({ page }) => {
  test.skip(!readerEmail || !adminEmail || !oidcRedirectURI, 'Requires isolated identities.');
  await signIn(page, readerEmail!, readerPassword!);
  const name = `Audited reader application ${Date.now()}`;
  await page.goto('/account/oidc/clients');
  await page.getByTestId('oidc-client-name').fill(name);
  await page.getByTestId('oidc-client-homepage').fill('https://audit.example.test/');
  await page.getByTestId('oidc-client-redirects').fill(oidcRedirectURI!);
  await page.getByTestId('oidc-client-public').check();
  await page.getByTestId('oidc-client-submit').click();
  const clientID = (await page.getByTestId('oidc-created-id').textContent())!.trim();
  const verifier = 'a'.repeat(64);
  const authorize = new URLSearchParams({client_id: clientID, redirect_uri: oidcRedirectURI!, response_type: 'code',
    scope: 'openid email', state: 'audit-e2e', code_challenge: createHash('sha256').update(verifier).digest('base64url'), code_challenge_method: 'S256'});
  await page.goto('/authorize?' + authorize);
  await page.getByTestId('oidc-consent-allow').click();
  await expect(page).toHaveURL(/callback\?code=/);
  const code = new URL(page.url()).searchParams.get('code')!;
  const token = await page.request.post('/oauth/token', { form: { grant_type: 'authorization_code', client_id: clientID,
    redirect_uri: oidcRedirectURI!, code, code_verifier: verifier } });
  expect(token.status()).toBe(200);
  const issued = await token.json();
  for (const role of ['reader', 'editor'] as const) {
    if (role === 'editor') {
      await page.goto('/account'); await page.getByTestId('account-logout').click();
      await signIn(page, editorEmail!, readerPassword!);
    }
    for (const route of ['/admin/audit', `/admin/users/${process.env.E2E_READER_ID}`, `/admin/oidc/clients/${clientID}`]) {
      expect((await page.request.get(route, {maxRedirects: 0})).status()).toBe(403);
    }
  }
  await page.goto('/account'); await page.getByTestId('account-logout').click();
  await signIn(page);
  for (const width of [1280, 390]) {
    await page.setViewportSize({width, height: 900});
    await page.goto('/admin/users');
    await expect(page.getByTestId('identity-total-users')).not.toHaveText('0');
    await page.getByTestId('admin-user-search').fill(readerEmail!);
    await page.getByTestId('admin-user-filter').click();
    await expect(page.getByTestId('admin-user-row')).toHaveCount(1);
    await expect(page.getByTestId('user-app-count')).not.toHaveText('0');
    await page.getByTestId('admin-user-detail').click();
    await expect(page.getByTestId('admin-user-profile')).toBeVisible();
    await page.getByRole('link', {name, exact: true}).click();
    await expect(page.getByTestId('client-login-count')).toHaveText('1');
    await expect(page.getByTestId('client-login-users')).toHaveText('1');
    await expect(page.getByTestId('admin-client-owner')).toHaveAttribute('href', `/admin/users/${process.env.E2E_READER_ID}`);
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
    await page.getByTestId('client-audit-link').click();
    await page.getByTestId('audit-action-filter').selectOption('oidc.login');
    await page.getByTestId('audit-filter-submit').click();
    await expect(page.getByTestId('audit-event')).toHaveCount(1);
    await expect(page.getByTestId('audit-event').locator('a[href="/admin/users/' + process.env.E2E_READER_ID + '"]')).toBeVisible();
    await expect(page.locator('body')).not.toContainText(issued.access_token);
    await expect(page.locator('body')).not.toContainText(code);
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
  }
  await page.goto(`/admin/oidc/clients/${clientID}`);
  await page.getByTestId('oidc-client-disable').click();
  await page.goto(`/admin/oidc/clients/${clientID}`);
  await expect(page.getByTestId('client-login-count')).toHaveText('1');
  await page.goto(`/admin/audit?client_id=${clientID}&action=client.disable`);
  await expect(page.getByTestId('audit-event')).toHaveCount(1);
});

test('reader can change their password in both account themes', async ({ page }) => {
  test.skip(!readerEmail || !readerPassword, 'Requires an isolated reader fixture.');
  await signIn(page, readerEmail!, readerPassword!, /\/account(?:\?|$)/);
  await page.addStyleTag({ content: '*, *::before, *::after { animation: none !important; transition: none !important; }' });
  await page.getByTestId('account-current-password').fill(readerPassword!);
  await page.getByTestId('account-new-password').fill('updated-reader-password');
  await page.getByTestId('account-password-submit').click();
  await expect(page).toHaveURL(/\/account\?password_changed=1/);
  await page.addStyleTag({ content: '*, *::before, *::after { animation: none !important; transition: none !important; }' });
  await page.getByTestId('account-logout').click();
  await signIn(page, readerEmail!, 'updated-reader-password', /\/account(?:\?|$)/);
  await expect(page.getByTestId('account-logout')).toBeVisible();
});

test('folio theme preference persists through reload', async ({ page }) => {
  test.skip(siteTheme !== 'folio', 'Cactus uses the configured color scheme rather than a UI toggle.');
  await page.goto('/', { waitUntil: 'domcontentloaded' });
  await page.locator('.folio-header .theme-toggle-btn').click();
  expect(await page.evaluate(() => localStorage.getItem('solitudes_theme'))).toBe('dark');
  await page.reload({ waitUntil: 'domcontentloaded' });
  await expect(page.locator('html')).toHaveClass(/dark/);
});
