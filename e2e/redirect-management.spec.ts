import { test, expect, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
import path from 'node:path';

type RedirectRequest = {
  url: string;
  type: string;
  contentType: string;
  data: string;
  success: () => void;
};

declare global {
  interface Window {
    redirectTestRequests: RedirectRequest[];
    ajaxRequest: (request: RedirectRequest) => void;
  }
}

const template = readFileSync(path.resolve(import.meta.dirname, '../resource/themes/admin/default/templates/redirects.html'), 'utf8');
const translate = (markup: string) => markup.replace(/{{\.Tr\.T "([^"]+)"}}/g, '$1');
const script = template.match(/<script>([\s\S]*?)<\/script>/)?.[1];
const form = template.match(/<form id="redirect-form"[\s\S]*?<\/form>/)?.[0];
if (!script || !form) throw new Error('Redirect template must contain its real editor form and interaction script');
const sourcePath = '/old"><script>window.redirectInjected = true</script>';
const targetPath = '/new?q="quoted"&page=1#section';
const maximumRuleID = '18446744073709551615';

// Go's TestRedirectTemplates verifies the rendered listing, translations and
// escaping. This isolated browser fixture exercises the production form/script
// without depending on redirect routes, authentication or a running database.
async function openRedirectFixture(page: Page) {
  await page.route('https://redirect-ui.test/admin/redirects', route => route.fulfill({
    contentType: 'text/html',
    body: `<h2 id="redirect-form-heading">redirect_add</h2>${translate(form!)}
      <label><input id="redirect-select-page" type="checkbox" data-testid="redirect-select-page">Select page</label>
      <button id="redirect-delete-selected" data-testid="redirect-delete-selected" disabled>Delete selected</button>
      <table><tbody>
        <tr data-testid="redirect-rule-row" data-redirect-id="${maximumRuleID}"><td>
          <input type="checkbox" data-testid="redirect-select" value="${maximumRuleID}">
          <code data-testid="redirect-source"></code><code data-testid="redirect-target"></code>
          <button data-testid="redirect-edit">Edit</button><button data-testid="redirect-delete">Delete</button>
        </td></tr>
        <tr data-testid="redirect-rule-row" data-redirect-id="2"><td>
          <input type="checkbox" data-testid="redirect-select" value="2">
          <code data-testid="redirect-source">/previous-slug</code><code data-testid="redirect-target">/latest-slug</code>
          <button data-testid="redirect-delete">Delete</button>
        </td></tr>
      </tbody></table>`,
  }));
  await page.goto('https://redirect-ui.test/admin/redirects');
  await page.getByTestId('redirect-source').first().evaluate((node, value) => { node.textContent = value; }, sourcePath);
  await page.getByTestId('redirect-target').first().evaluate((node, value) => { node.textContent = value; }, targetPath);
  await page.evaluate(() => {
    window.redirectTestRequests = [];
    window.ajaxRequest = request => { window.redirectTestRequests.push(request); };
  });
  await page.addScriptTag({ content: translate(script!) });
}

async function requests(page: Page) {
  return page.evaluate(() => window.redirectTestRequests.map(({ success, ...request }) => request));
}

test.beforeEach(async ({ page }) => { await openRedirectFixture(page); });

test('single and batch deletion cancellation never submits a request', async ({ page }) => {
  let confirmations = 0;
  page.on('dialog', async dialog => {
    confirmations++;
    expect(dialog.type()).toBe('confirm');
    await dialog.dismiss();
  });
  await expect(page.getByTestId('redirect-delete-selected')).toBeDisabled();
  await page.getByTestId('redirect-delete').first().click();
  expect(await requests(page)).toEqual([]);
  await page.getByTestId('redirect-select-page').check();
  await page.getByTestId('redirect-delete-selected').click();
  expect(await requests(page)).toEqual([]);
  expect(confirmations).toBe(2);
});

test('batch selection is page-local and preserves exact uint64 IDs', async ({ page }) => {
  page.on('dialog', dialog => dialog.accept());
  const selectPage = page.getByTestId('redirect-select-page');
  const selections = page.getByTestId('redirect-select');
  await selections.first().check();
  await expect(selectPage).toHaveJSProperty('indeterminate', true);
  await expect(page.getByTestId('redirect-delete-selected')).toBeEnabled();
  await selectPage.check();
  await expect(selections.first()).toBeChecked();
  await expect(selections.last()).toBeChecked();
  await expect(selectPage).toHaveJSProperty('indeterminate', false);
  await page.getByTestId('redirect-delete-selected').click();
  expect(await requests(page)).toEqual([{
    url: '/admin/redirects', type: 'DELETE', contentType: 'application/json',
    data: `{"ids":[${maximumRuleID},2]}`,
  }]);
  await selectPage.uncheck();
  await expect(selections.first()).not.toBeChecked();
  await expect(selections.last()).not.toBeChecked();
  await expect(page.getByTestId('redirect-delete-selected')).toBeDisabled();
  await selections.last().check();
  await page.getByTestId('redirect-delete-selected').click();
  expect((await requests(page))[1]!.data).toBe('{"ids":[2]}');
});

test('manual editing, cancellation and creation retain safe path values', async ({ page }) => {
  await page.getByTestId('redirect-edit').click();
  await expect(page.getByTestId('redirect-source-input')).toHaveValue(sourcePath);
  await expect(page.getByTestId('redirect-target-input')).toHaveValue(targetPath);
  await expect(page.locator('#redirect-form-heading')).toHaveText('redirect_edit');
  await expect(page.getByTestId('redirect-cancel-edit')).toBeVisible();
  await expect(page.getByTestId('redirect-source-input')).toBeFocused();
  await page.getByTestId('redirect-save').click();
  expect(await requests(page)).toEqual([{
    url: '/admin/redirects', type: 'POST', contentType: 'application/json',
    data: `{"id":${maximumRuleID},"source_path":${JSON.stringify(sourcePath)},"target_path":${JSON.stringify(targetPath)}}`,
  }]);
  await page.getByTestId('redirect-cancel-edit').click();
  await expect(page.locator('#redirect-form-heading')).toHaveText('redirect_add');
  await expect(page.getByTestId('redirect-cancel-edit')).toBeHidden();
  await expect(page.getByTestId('redirect-source-input')).toHaveValue('');
  await expect(page.getByTestId('redirect-target-input')).toHaveValue('');
  expect(await requests(page)).toHaveLength(1);
  await page.getByTestId('redirect-save').click();
  expect(await requests(page)).toHaveLength(1); // Native required-field validation.
  await page.getByTestId('redirect-source-input').fill('/manual-source');
  await page.getByTestId('redirect-target-input').fill('/r/go?url=https%3A%2F%2Fexample.test#target');
  await page.getByTestId('redirect-save').click();
  expect(JSON.parse((await requests(page))[1]!.data)).toEqual({
    id: 0, source_path: '/manual-source', target_path: '/r/go?url=https%3A%2F%2Fexample.test#target',
  });
  expect(await page.evaluate(() => Object.hasOwn(window, 'redirectInjected'))).toBe(false);
});

for (const operation of ['save', 'delete'] as const) {
  test(`successful ${operation} reloads the current page`, async ({ page }) => {
    if (operation === 'save') {
      await page.getByTestId('redirect-source-input').fill('/source');
      await page.getByTestId('redirect-target-input').fill('/target');
      await page.getByTestId('redirect-save').click();
    } else {
      page.on('dialog', dialog => dialog.accept());
      await page.getByTestId('redirect-delete').last().click();
      expect((await requests(page))[0]!.data).toBe('{"ids":[2]}');
    }
    const navigation = page.waitForEvent('framenavigated', frame => frame === page.mainFrame());
    await page.evaluate(() => window.redirectTestRequests[0]!.success());
    await navigation;
    await expect(page).toHaveURL('https://redirect-ui.test/admin/redirects');
  });
}
