import { expect, test, type Page } from '@playwright/test';
import path from 'node:path';

const siteTheme = process.env.E2E_SITE_THEME || 'cactus';
const adminTheme = process.env.E2E_ADMIN_THEME || 'default';
const adminEmail = process.env.E2E_ADMIN_EMAIL;
const adminPassword = process.env.E2E_ADMIN_PASSWORD;
const readerEmail = process.env.E2E_READER_EMAIL;
const readerPassword = process.env.E2E_READER_PASSWORD;
const articleSlug = process.env.E2E_ARTICLE_SLUG;
const articleTitle = process.env.E2E_ARTICLE_TITLE;

test.beforeEach(async ({ page }) => {
  // Keep the test independent of the jQuery CDN. Production pages still use
  // their normal URL; only browser test requests receive this local fixture.
  await page.route('https://cdnjs.cloudflare.com/ajax/libs/jquery/3.3.1/jquery.min.js', route =>
    route.fulfill({ path: path.resolve('node_modules/jquery/dist/jquery.min.js'), contentType: 'application/javascript' })
  );
});

async function signIn(page: Page, email = adminEmail!, password = adminPassword!, destination = /\/admin(?:\/|\?|$)/) {
  await page.goto('/admin/login', { waitUntil: 'domcontentloaded' });
  await page.addStyleTag({ content: '*, *::before, *::after { animation: none !important; transition: none !important; }' });
  await page.getByTestId('auth-email').fill(email);
  await page.getByTestId('auth-password').fill(password);
  await page.getByTestId('auth-captcha').fill('0');
  await expect(page.locator('input[name="captchaId"]')).not.toHaveValue('');
  // The isolated Go browser test enables SOLITUDES_E2E for the captcha.
  await page.getByTestId('auth-submit').click();
  await expect(page).toHaveURL(destination);
}

test('guest can find login, registration, and resend controls', async ({ page }) => {
  const login = await page.goto('/admin/login', { waitUntil: 'domcontentloaded' });
  await page.addStyleTag({ content: '*, *::before, *::after { animation: none !important; transition: none !important; }' });
  expect(login?.status()).toBe(200);
  for (const id of ['auth-login-form', 'auth-email', 'auth-password', 'auth-captcha', 'auth-submit']) {
    await expect(page.getByTestId(id)).toBeVisible();
  }
  await page.getByTestId('auth-register-link').click();
  await expect(page).toHaveURL(/\/admin\/register$/);
  for (const id of ['register-form', 'register-email', 'register-nickname', 'register-password',
    'register-captcha', 'register-submit', 'resend-form', 'resend-email', 'resend-captcha', 'resend-submit']) {
    await expect(page.getByTestId(id)).toBeVisible();
  }
});

test('registration delivers a verification email before a new reader can sign in', async ({ page }) => {
  test.skip(!adminEmail, 'Requires the in-process SMTP catcher and isolated browser server.');
  const email = `new-reader-${siteTheme}-${adminTheme}-${Date.now()}@example.com`;
  const password = 'browser-registration-password';
  await page.goto('/admin/register', { waitUntil: 'domcontentloaded' });
  await page.addStyleTag({ content: '*, *::before, *::after { animation: none !important; transition: none !important; }' });
  await page.getByTestId('register-email').fill(email);
  await page.getByTestId('register-nickname').fill('New Browser Reader');
  await page.getByTestId('register-password').fill(password);
  await page.getByTestId('register-captcha').fill('0');
  await expect(page.locator('input[name="captchaId"]').first()).not.toHaveValue('');
  const registered = page.waitForResponse(response => response.url().endsWith('/admin/register') && response.request().method() === 'POST');
  await page.getByTestId('register-submit').click();
  expect((await registered).status()).toBe(202);
  await page.goto('/admin/login', { waitUntil: 'domcontentloaded' });
  await page.addStyleTag({ content: '*, *::before, *::after { animation: none !important; transition: none !important; }' });
  await page.getByTestId('auth-email').fill(email);
  await page.getByTestId('auth-password').fill(password);
  await page.getByTestId('auth-captcha').fill('0');
  await expect(page.locator('input[name="captchaId"]')).not.toHaveValue('');
  const denied = page.waitForResponse(response => response.url().endsWith('/admin/login') && response.request().method() === 'POST');
  await page.getByTestId('auth-submit').click();
  const deniedResponse = await denied;
  expect(deniedResponse.status()).toBe(403);
  let link = '';
  await expect.poll(async () => {
    const response = await page.request.get('/__e2e/verification-mail');
    if (response.status() === 200) link = await response.text();
    return link;
  }).toMatch(/^http:\/\/localhost:\d+\/admin\/verify-email\?token=[a-f0-9]{64}$/);
  await page.goto(link, { waitUntil: 'domcontentloaded' });
  await expect(page).toHaveURL(/\/admin\/login\?verified=1/);
  await signIn(page, email, password, /\/account(?:\?|$)/);
  await expect(page.getByTestId('account-logout')).toBeVisible();
});

test('site navigation and search form work in both site themes', async ({ page }) => {
  const home = await page.goto('/', { waitUntil: 'domcontentloaded' });
  expect(home?.status()).toBe(200);
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

test('admin navigation, authoring, identity, users, and OIDC controls share selectors', async ({ page }) => {
  test.skip(!adminEmail || !adminPassword, 'Set isolated E2E_ADMIN_EMAIL / E2E_ADMIN_PASSWORD.');
  await signIn(page);
  await page.goto('/admin/publish', { waitUntil: 'domcontentloaded' });
  for (const id of ['publish-title', 'publish-slug', 'publish-tags', 'publish-template',
    'publish-content', 'publish-private', 'publish-submit']) {
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
  await page.goto('/admin/oidc/clients', { waitUntil: 'domcontentloaded' });
  for (const id of ['oidc-client-form', 'oidc-client-name', 'oidc-client-redirects',
    'oidc-client-public', 'oidc-client-submit', 'oidc-rotate-keys']) {
    await expect(page.getByTestId(id)).toBeVisible();
  }
  await expect(page.locator(`link[href*="/static/admin/${adminTheme}/"]`).first()).toHaveCount(1);
});

test('publish button creates an article using the same controls in both admin themes', async ({ page }) => {
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
  await page.goto('/' + slug, { waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('site-article')).toContainText('Browser UI post');
});

test('admin can change a reader role with the same form in both admin themes', async ({ page }) => {
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
  await page.getByTestId('oidc-client-redirects').fill('http://localhost:9999/callback');
  await page.getByTestId('oidc-client-public').check();
  await page.getByTestId('oidc-client-submit').click();
  await expect(page.getByTestId('oidc-created-id')).not.toBeEmpty();
  await page.getByTestId('oidc-created-back').click();
  const client = page.locator('li').filter({ has: page.getByText(name, { exact: true }) });
  await client.getByTestId('oidc-client-disable').click();
  await expect(page.locator('li').filter({ has: page.getByText(name, { exact: true }) })).toContainText('(');
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
