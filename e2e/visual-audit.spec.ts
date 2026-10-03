import { expect, test, type Page } from '@playwright/test';
import { createHash } from 'node:crypto';
import { mkdirSync, writeFileSync } from 'node:fs';
import path from 'node:path';

// Opt-in visual inventory. Uses the same disposable database/server as the
// browser contract test; it never writes theme screenshots or production data.
const outputRoot = process.env.SOLITUDES_VISUAL_DIR;
const site = process.env.E2E_SITE_THEME || 'cactus';
const admin = process.env.E2E_ADMIN_THEME || 'default';
const base = outputRoot ? path.join(outputRoot, `${site}-${admin}`) : '';
const articleSlug = process.env.E2E_ARTICLE_SLUG || 'e2e-theme-article';
const adminID = process.env.E2E_ADMIN_ID;
const editorID = process.env.E2E_EDITOR_ID;
const readerID = process.env.E2E_READER_ID;
const manifest: Array<{ role: string; viewport: string; page: string; status: number; width: number; viewportWidth: number; image: string }> = [];
const browserErrors = new WeakMap<Page, string[]>();

test.describe('reading tools review', () => {
  test.use({ locale: 'zh-CN' });
  test('home intro, persistent contents and share dialog', async ({ page }) => {
    test.skip(!outputRoot);
    mkdirSync(base, { recursive: true });
    for (const width of [1440, 1024, 390, 320]) {
      await page.setViewportSize({ width, height: 900 });
      await capture(page, 'guest', String(width), 'custom-home', '/');
      await capture(page, 'guest', String(width), 'reading-toc', '/visual-long-article');
      if (site === 'cactus') {
        if (width <= 500) await page.locator('#toc-footer-toggle').click();
        else if (width < 900) await page.locator('#menu-icon, #menu-icon-tablet').filter({visible:true}).click();
      } else if (width < 1440) await page.getByTestId('article-toc').locator('summary').click();
      await capture(page, 'guest', String(width), 'reading-toc-open');
      await page.keyboard.press('Escape');
      if (site === 'cactus' && width >= 900) await page.locator('#menu-icon').click();
      await page.getByTestId(site === 'cactus' && width >= 900 ? 'article-share-menu' : 'article-share').click();
      await capture(page, 'guest', String(width), 'reading-share');
      await page.keyboard.press('Escape');
    }
    await page.emulateMedia({ colorScheme: 'dark' });
    await page.setViewportSize({ width: 1440, height: 900 });
    await capture(page, 'guest', '1440-dark', 'reading-toc', '/visual-long-article');
    await page.getByTestId(site === 'cactus' ? 'article-share-menu' : 'article-share').click();
    await capture(page, 'guest', '1440-dark', 'reading-share');
  });
});

test.describe('Folio editorial visual review', () => {
  test.use({ locale: 'zh-CN' });
  test('editorial pages and role navigation', async ({ page }) => {
    test.skip(!outputRoot || site !== 'folio');
    test.setTimeout(240000);
    mkdirSync(base, { recursive: true });
    for (const width of [1280, 390]) {
      await page.setViewportSize({ width, height: 900 });
      for (const [name, route] of [['home', '/'], ['posts', '/posts/'], ['article', `/${articleSlug}`],
        ['long', '/visual-long-article'], ['topics', '/tags/Topic/'], ['readers', '/readers/'],
        ['profile', `/users/${editorID}`], ['login', '/login']] as const) {
        await capture(page, 'guest', String(width), 'editorial-' + name, route);
      }
      await page.goto('/visual-long-article');
      const toc = page.getByTestId('article-toc').locator('details');
      if (width < 1440) {
        await expect(toc).not.toHaveAttribute('open');
        await toc.locator('summary').click();
      }
      await expect(toc).toHaveAttribute('open', '');
      await capture(page, 'guest', String(width), 'editorial-toc-expanded');
    }
    for (const role of ['reader', 'editor', 'admin'] as const) {
      await login(page, role);
      await page.setViewportSize({ width: 1280, height: 900 });
      await capture(page, role, '1280', 'editorial-home', '/');
      await page.setViewportSize({ width: 390, height: 900 });
      await capture(page, role, '390', 'editorial-account', '/account');
      await page.goto('/');
      await page.locator('#folio-menu-toggle').click();
      await expect(page.getByTestId('site-manage-nav-mobile')).toHaveCount(0);
      await expect(page.getByTestId('site-account-nav-mobile')).toBeVisible();
      await capture(page, role, '390', 'editorial-menu');
      await signOut(page);
    }
    await page.emulateMedia({ colorScheme: 'dark' });
    for (const width of [1280, 320]) {
      await page.setViewportSize({ width, height: 900 });
      await capture(page, 'guest', `${width}-dark`, 'editorial-home', '/');
      await capture(page, 'guest', `${width}-dark`, 'editorial-article', `/${articleSlug}`);
    }
  });
});

test.beforeEach(async ({ page }) => {
  const errors: string[] = [];
  browserErrors.set(page, errors);
  page.on('pageerror', error => errors.push(error.message));
  await page.route('https://cdnjs.cloudflare.com/ajax/libs/jquery/3.3.1/jquery.min.js', route =>
    route.fulfill({ path: path.resolve('node_modules/jquery/dist/jquery.min.js'), contentType: 'application/javascript' }));
});

async function capture(page: Page, role: string, viewport: string, name: string, route?: string, expectedStatus = 200) {
  let status = expectedStatus;
  if (route) {
    const response = await page.goto(route, { waitUntil: 'domcontentloaded', timeout: 20000 });
    status = response?.status() || 0;
    expect(status, `${role} ${route}`).toBe(expectedStatus);
  }
  await page.addStyleTag({ content: '*, *::before, *::after { animation: none !important; transition: none !important; caret-color: transparent !important; }' });
  await page.waitForTimeout(80);
  if (await page.locator('#editSection, #topicContent').count()) {
    await expect(page.locator('.vditor-toolbar').first()).toBeVisible({ timeout: 20000 });
    await expect(page.locator('.vditor [contenteditable="true"]:visible').first()).toBeVisible({ timeout: 20000 });
  }
  const image = path.join(base, `${role}-${viewport}-${name}.jpg`);
  await page.screenshot({ path: image, type: 'jpeg', quality: 65, fullPage: !name.startsWith('reading-'), animations: 'disabled' });
  const dimensions = await page.evaluate(() => ({ width: document.documentElement.scrollWidth, viewportWidth: window.innerWidth }));
  expect(browserErrors.get(page), `${role}/${viewport}/${name}: browser script errors`).toEqual([]);
  expect(dimensions.width, `${role}/${viewport}/${name}: horizontal overflow`).toBeLessThanOrEqual(dimensions.viewportWidth + 1);
  manifest.push({ role, viewport, page: name, status, ...dimensions, image });
}

async function login(page: Page, role: 'admin' | 'editor' | 'reader') {
  const prefix = role === 'reader' ? 'READER' : role === 'editor' ? 'EDITOR' : 'ADMIN';
  const email = process.env[`E2E_${prefix}_EMAIL`];
  const password = role === 'editor' ? process.env.E2E_READER_PASSWORD : process.env[`E2E_${prefix}_PASSWORD`];
  if (!email || !password) throw new Error(`missing ${role} browser account`);
  await page.goto('/login', { waitUntil: 'domcontentloaded' });
  await page.addStyleTag({ content: '*, *::before, *::after { animation: none !important; transition: none !important; }' });
  await page.getByTestId('auth-email').fill(email);
  await page.getByTestId('auth-password').fill(password);
  await page.getByTestId('auth-captcha').fill('0');
  await page.getByTestId('auth-submit').click();
  await expect(page).toHaveURL(/\/(?:admin|account)/);
}

async function signOut(page: Page) {
  await page.goto('/account', { waitUntil: 'domcontentloaded' });
  await page.getByTestId('account-logout').click();
  await expect(page).toHaveURL(/\/$/);
}

async function commentAs(page: Page, role: string, viewport: string) {
  await page.goto(`/${articleSlug}`, { waitUntil: 'domcontentloaded' });
  const message = `Role audit ${site}/${admin} ${role} ${viewport}`;
  await page.getByTestId('comment-content').fill(message);
  if (role === 'guest') {
    await page.getByTestId('comment-nickname').fill('Visual Visitor');
    await page.getByTestId('comment-email').fill('visitor@example.test');
  } else {
    await expect(page.getByTestId('comment-nickname')).toHaveCount(0);
  }
  await capture(page, role + '-site', viewport, 'comment-composing');
  await page.getByTestId('comment-submit').click();
  await expect(page.getByText(message, { exact: true })).toBeVisible();
  await capture(page, role + '-site', viewport, 'comment-published');
  if (site === 'folio') {
    await page.getByTestId('article-share').click();
    await capture(page, role + '-site', viewport, 'share-feedback');
  }
}

test('visual inventory of every live theme page and interactive state', async ({ page }) => {
  test.setTimeout(360000);
  test.skip(!outputRoot, 'Only run with SOLITUDES_VISUAL_DIR against an isolated browser fixture.');
  mkdirSync(base, { recursive: true });

  const guestPages: Array<[string, string, number?]> = [
    ['home', '/'], ['posts', '/posts/'], ['books', '/books/'], ['reader-circle', '/readers/'],
    ['search', '/search/?w=reading'], ['tags', '/tags/'], ['tag-detail', '/tags/design/'],
    ['article', `/${articleSlug}`], ['article-long', '/visual-long-article'],
    ['topic', '/visual-topic'], ['topic-discussion', '/visual-topic-discussion'], ['topic-list', '/tags/Topic/'], ['page', '/visual-page'],
    ['comments-closed-article', '/visual-closed-1'], ['comments-closed-page', '/visual-closed-2'],
    ['book-detail', '/visual-book'], ['chapter', '/visual-chapter'],
    ['profile-admin', `/users/${adminID}`],
    ['profile-editor', `/users/${editorID}`],
    ['profile-reader', `/users/${readerID}`],
    ['not-found', '/visual-missing-page', 404],
  ];
  const adminPages: Array<[string, string, number?]> = [
    ['dashboard', '/admin/'], ['articles', '/admin/articles'],
    ['publish', '/admin/publish'], ['comments', '/admin/comments'],
    ['tags', '/admin/tags'], ['media', '/admin/media'],
    ['users', '/admin/users'], ['providers', '/admin/auth/providers'],
    ['oidc-clients', '/admin/oidc/clients'], ['settings', '/admin/settings'],
    ['not-found', '/admin/visual-missing-page', 404],
  ];
  for (const [viewport, size] of [
    ['desktop', { width: 1366, height: 900 }], ['mobile', { width: 390, height: 844 }],
  ] as const) {
    await page.setViewportSize(size);
    for (const [name, route, status] of guestPages) await capture(page, 'guest-site', viewport, name, route, status);
    await page.goto('/tags/Topic/', { waitUntil: 'domcontentloaded' });
    const topicAction = page.getByTestId('topic-reply-link').first();
    await expect(topicAction).toBeVisible();
    const byline = page.locator('[data-topic-slug="visual-topic"]').getByTestId('topic-byline');
    await expect(byline).toContainText('Browser Admin');
    await expect(byline.locator('time')).toBeVisible();
    await expect(byline.getByTestId('topic-reply-link')).toBeVisible();
    if (viewport === 'mobile') {
      await page.goto('/', { waitUntil: 'domcontentloaded' });
      await page.locator(site === 'folio' ? '#folio-menu-toggle' : '#cactus-navigation .icon button').click();
      await capture(page, 'guest-site', viewport, 'navigation-expanded');
    }
    await commentAs(page, 'guest', viewport);
    await capture(page, 'guest-site', viewport, 'login', '/login');
    await capture(page, 'guest-site', viewport, 'register', '/register');
    const loginChallenge = createHash('sha256').update('v'.repeat(64)).digest('base64url');
    const loginAuthorize = new URLSearchParams({ client_id: process.env.E2E_OIDC_CLIENT_ID!,
      redirect_uri: process.env.E2E_OIDC_REDIRECT_URI!, response_type: 'code', scope: 'openid email profile',
      state: 'login-visual-audit', code_challenge: loginChallenge, code_challenge_method: 'S256' });
    await page.goto('/authorize?' + loginAuthorize, { waitUntil: 'domcontentloaded' });
    await expect(page).toHaveURL(/\/login\?return_to=/);
    await capture(page, 'guest-site', viewport, 'oidc-login');
    await login(page, 'admin');
    await commentAs(page, 'admin', viewport);
    for (const [name, route, status] of adminPages) await capture(page, 'admin', viewport, name, route, status);
    await page.goto('/admin/users', { waitUntil: 'domcontentloaded' });
    await page.locator('tr').filter({ hasText: 'Browser Reader' }).getByTestId('user-role-select').selectOption('editor');
    await capture(page, 'admin', viewport, 'user-role-selected');
    await page.goto('/admin/comments', { waitUntil: 'domcontentloaded' });
    await capture(page, 'admin', viewport, 'comments-management');
    await page.goto('/admin/articles', { waitUntil: 'domcontentloaded' });
    const editURL = await page.getByTestId('article-edit').first().getAttribute('href');
    expect(editURL).toBeTruthy();
    await capture(page, 'admin', viewport, 'publish-edit', editURL!);
    await capture(page, 'admin-site', viewport, 'account', '/account');
    await capture(page, 'admin-site', viewport, 'clients', '/account/oidc/clients');
    await capture(page, 'admin-site', viewport, 'public-profile', `/users/${adminID}`);

    // Consent and one-time client result are real, navigable pages but are not GET routes.
    const verifier = 'v'.repeat(64);
    const challenge = createHash('sha256').update(verifier).digest('base64url');
    const authorize = new URLSearchParams({ client_id: process.env.E2E_OIDC_CLIENT_ID!,
      redirect_uri: process.env.E2E_OIDC_REDIRECT_URI!, response_type: 'code', scope: 'openid email', state: 'visual-audit',
      code_challenge: challenge, code_challenge_method: 'S256' });
    await page.goto('/authorize?' + authorize, { waitUntil: 'domcontentloaded' });
    await expect(page).toHaveURL(/\/oidc\/consent\?/);
    await capture(page, 'admin-site', viewport, 'consent');
    await page.goto('/account/oidc/clients', { waitUntil: 'domcontentloaded' });
    await page.getByTestId('oidc-client-name').fill(`Visual audit ${site}/${admin} ${viewport}`);
    await page.getByTestId('oidc-client-homepage').fill('https://client.example.test/');
    await page.getByTestId('oidc-client-redirects').fill('https://client.example.test/callback');
    await page.getByTestId('oidc-client-submit').click();
    await expect(page.getByTestId('oidc-created-id')).not.toBeEmpty();
    await page.getByTestId('oidc-created-secret').evaluate(node => { node.textContent = '••••••••'; });
    await capture(page, 'admin-site', viewport, 'client-created');
    if (viewport === 'mobile') {
      await page.goto('/admin/articles', { waitUntil: 'domcontentloaded' });
      await page.locator('.admin-mobile-toggle').click();
      await capture(page, 'admin', viewport, 'navigation-expanded');
    }
    await signOut(page);
    await login(page, 'editor');
    await commentAs(page, 'editor', viewport);
    await capture(page, 'editor', viewport, 'articles', '/admin/articles');
    const editorEditURL = await page.getByTestId('article-edit').first().getAttribute('href');
    expect(editorEditURL).toBeTruthy();
    await capture(page, 'editor', viewport, 'publish-own-edit', editorEditURL!);
    await capture(page, 'editor', viewport, 'publish', '/admin/publish');
    await capture(page, 'editor-site', viewport, 'account', '/account');
    await capture(page, 'editor-site', viewport, 'clients', '/account/oidc/clients');
    await capture(page, 'editor-site', viewport, 'public-profile', `/users/${editorID}`);
    await capture(page, 'editor', viewport, 'admin-users-denied', '/admin/users', 403);
    await signOut(page);
    await login(page, 'reader');
    await commentAs(page, 'reader', viewport);
    await capture(page, 'reader-site', viewport, 'account', '/account');
    await capture(page, 'reader-site', viewport, 'clients', '/account/oidc/clients');
    await capture(page, 'reader-site', viewport, 'public-profile', `/users/${readerID}`);
    await capture(page, 'reader', viewport, 'admin-publish-denied', '/admin/publish', 403);
    await capture(page, 'reader', viewport, 'admin-users-denied', '/admin/users', 403);
    await signOut(page);
  }
  writeFileSync(path.join(base, 'manifest.json'), JSON.stringify(manifest, null, 2));
  console.log(`VISUAL_AUDIT ${site}/${admin}: ${manifest.length} screenshots in ${base}`);
});

test('expanded article menus and topic conversations', async ({ page }) => {
  test.skip(!outputRoot, 'Opt-in expanded interaction screenshots.');
  mkdirSync(base, { recursive: true });
  for (const role of ['guest', 'reader', 'editor', 'admin'] as const) {
    if (role !== 'guest') await login(page, role);
    for (const width of [1440, 900, 390]) {
      await page.setViewportSize({ width, height: 900 });
      await page.goto('/visual-long-article', { waitUntil: 'domcontentloaded' });
      if (site === 'cactus') {
        const toggle = page.locator(width === 390 ? '#menu-footer' : '#menu-icon');
        if (await toggle.getAttribute('aria-expanded') === 'false') await toggle.click();
      } else if (width === 390) {
        await page.locator('#folio-menu-toggle').click();
      }
      await capture(page, role + '-site', String(width), 'article-menu-open');
      if (site === 'cactus' && width >= 900) {
        await page.getByTestId('article-share-menu').hover();
        await page.getByTestId('article-share-menu').click();
        await capture(page, role + '-site', String(width), 'article-share-open');
      }
      await capture(page, role + '-site', String(width), 'topic-conversations', '/tags/Topic/');
      await capture(page, role + '-site', String(width), 'home-density', '/');
    }
    if (role !== 'guest') await signOut(page);
  }
  await page.emulateMedia({ colorScheme: 'dark' });
  await capture(page, 'guest-site', '390-dark', 'topic-conversations', '/tags/Topic/');
  await capture(page, 'guest-site', '390-dark', 'home-density', '/');
});

test('modern admin workspace visual review', async ({ page }) => {
  test.skip(!outputRoot, 'Opt-in administration visual review.');
  test.setTimeout(180000);
  mkdirSync(base, { recursive: true });
  await login(page, 'admin');
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 900 });
    for (const [name, route] of [
      ['dashboard', '/admin/'], ['articles', '/admin/articles'], ['comments', '/admin/comments'],
      ['media', '/admin/media'], ['tags', '/admin/tags'], ['publish', '/admin/publish'], ['settings', '/admin/settings'],
      ['users', '/admin/users'], ['user', '/admin/users/' + adminID], ['clients', '/admin/oidc/clients'],
      ['client', '/admin/oidc/clients/' + process.env.E2E_OIDC_CLIENT_ID], ['providers', '/admin/auth/providers'], ['audit', '/admin/audit'],
    ]) await capture(page, 'admin', String(width), 'modern-' + name, route);
    await page.getByTestId('admin-theme-toggle').click();
    for (const [name, route] of [['users', '/admin/users'], ['publish', '/admin/publish'], ['providers', '/admin/auth/providers']]) {
      await capture(page, 'admin', width + '-dark', 'modern-' + name, route);
    }
    await page.getByTestId('admin-theme-toggle').click();
    if (width === 390) {
      await page.getByTestId('admin-nav-toggle').click();
      await capture(page, 'admin', '390', 'modern-drawer');
      await page.keyboard.press('Escape');
    }
  }
  await signOut(page);
  await login(page, 'editor');
  await capture(page, 'editor', '390', 'modern-articles', '/admin/articles');
});

test.describe('Chinese administration', () => {
test.use({ locale: 'zh-CN' });
test('modern admin workspace Chinese visual review', async ({ page }) => {
  test.skip(!outputRoot, 'Opt-in Chinese administration review.');
  mkdirSync(base, { recursive: true });
  await login(page, 'admin');
  await page.goto('/admin');
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh');
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 900 });
    for (const [name, route] of [['dashboard', '/admin/'], ['articles', '/admin/articles'],
      ['comments', '/admin/comments'], ['settings', '/admin/settings']]) {
      await capture(page, 'admin-zh', String(width), 'modern-' + name, route);
    }
  }
  await page.goto('/admin/oidc/clients');
  await page.getByTestId('oidc-client-name').fill('Visual administration app');
  await page.getByTestId('oidc-client-homepage').fill('https://example.test');
  await page.getByTestId('oidc-client-redirects').fill('https://example.test/callback');
  await page.getByTestId('oidc-client-public').check();
  await page.getByTestId('oidc-client-submit').click();
  await expect(page.getByTestId('oidc-created-id')).not.toBeEmpty();
  await capture(page, 'admin-zh', '390', 'modern-client-created');
});
});

test('identity administration statistics and audit visual review', async ({ page }) => {
  test.skip(!outputRoot, 'Opt-in identity administration review.');
  mkdirSync(base, { recursive: true });
  await login(page, 'admin');
  const verifier = 's'.repeat(64);
  await page.goto('/authorize?' + new URLSearchParams({client_id:process.env.E2E_OIDC_CLIENT_ID!, redirect_uri:process.env.E2E_OIDC_REDIRECT_URI!,
    response_type:'code', scope:'openid', state:'visual-audit-login', code_challenge:createHash('sha256').update(verifier).digest('base64url'), code_challenge_method:'S256'}));
  await page.getByTestId('oidc-consent-allow').click();
  await expect(page).toHaveURL(/callback\?code=/);
  const token = await page.request.post('/oauth/token', {form:{grant_type:'authorization_code', client_id:process.env.E2E_OIDC_CLIENT_ID!,
    redirect_uri:process.env.E2E_OIDC_REDIRECT_URI!, code:new URL(page.url()).searchParams.get('code')!, code_verifier:verifier}});
  expect(token.status()).toBe(200);
  for (const width of [1366, 390]) {
    await page.setViewportSize({width, height: 900});
    for (const [name, route] of [
      ['identity-dashboard', '/admin/'], ['identity-users', '/admin/users'],
      ['identity-user', `/admin/users/${adminID}`], ['identity-clients', '/admin/oidc/clients'],
      ['identity-client', `/admin/oidc/clients/${process.env.E2E_OIDC_CLIENT_ID}`],
      ['identity-audit', '/admin/audit'],
    ] as const) await capture(page, 'admin', String(width), name, route);
  }
  await page.emulateMedia({colorScheme:'dark'});
  await capture(page, 'admin', '390-dark', 'identity-audit', '/admin/audit');
});
