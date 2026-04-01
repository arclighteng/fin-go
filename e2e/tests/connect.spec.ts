import { test, expect } from '@playwright/test';

// ---------------------------------------------------------------------------
// Connect page — onboarding UI elements and interactivity
// ---------------------------------------------------------------------------

test.beforeEach(async ({ page }) => {
  await page.goto('/connect');
});

test('connect page loads without server errors', async ({ page }) => {
  await expect(page.locator('body')).not.toContainText('Internal Server Error');
  await expect(page.locator('body')).not.toContainText('template:');
});

test('page title is "Connect Your Bank - Fin"', async ({ page }) => {
  await expect(page).toHaveTitle('Connect Your Bank - Fin');
});

test('page header h1 reads "Connect Your Bank"', async ({ page }) => {
  await expect(page.locator('.connect-page-header h1')).toHaveText('Connect Your Bank');
});

test('page sub-heading describes what you can do', async ({ page }) => {
  const subhead = page.locator('.connect-page-header p');
  await expect(subhead).toContainText('CSV');
  await expect(subhead).toContainText('SimpleFIN');
});

test('CSV drop zone is present', async ({ page }) => {
  await expect(page.locator('#dropZone')).toBeVisible();
});

test('CSV file input is present', async ({ page }) => {
  const fileInput = page.locator('#csvFileInput');
  // The input is inside the drop zone and may be transparent; check it exists
  await expect(fileInput).toHaveCount(1);
  expect(await fileInput.getAttribute('type')).toBe('file');
  expect(await fileInput.getAttribute('accept')).toContain('.csv');
});

test('SimpleFIN section is visible', async ({ page }) => {
  await expect(page.locator('#simplefinSection')).toBeVisible();
});

test('SimpleFIN section heading reads "Connect via SimpleFIN"', async ({ page }) => {
  await expect(page.locator('#simplefinSection h2')).toHaveText('Connect via SimpleFIN');
});

test('SimpleFIN token textarea is present with placeholder', async ({ page }) => {
  const textarea = page.locator('#simplefinTokenInput');
  await expect(textarea).toBeVisible();
  expect(await textarea.getAttribute('placeholder')).toContain('https://');
});

test('SimpleFIN token textarea is editable', async ({ page }) => {
  const textarea = page.locator('#simplefinTokenInput');
  await textarea.fill('https://example.simplefin.org/test-token');
  expect(await textarea.inputValue()).toBe('https://example.simplefin.org/test-token');
});

test('"Save Token" button is present and enabled', async ({ page }) => {
  const saveBtn = page.locator('#simplefinSaveBtn');
  await expect(saveBtn).toBeVisible();
  await expect(saveBtn).toBeEnabled();
  await expect(saveBtn).toContainText('Save Token');
});

test('"Save Token" with empty input shows validation message', async ({ page }) => {
  // Click without filling the textarea
  await page.locator('#simplefinSaveBtn').click();
  const statusMsg = page.locator('#simplefinStatusMsg');
  await expect(statusMsg).toBeVisible();
  await expect(statusMsg).toContainText('token');
});

test('SimpleFIN step numbers 1, 2, 3 are visible', async ({ page }) => {
  const steps = page.locator('.simplefin-step-number');
  await expect(steps).toHaveCount(3);
  await expect(steps.nth(0)).toHaveText('1');
  await expect(steps.nth(1)).toHaveText('2');
  await expect(steps.nth(2)).toHaveText('3');
});

test('external SimpleFIN link points to simplefin.org', async ({ page }) => {
  const link = page.locator('a[href*="simplefin.org"]').first();
  await expect(link).toBeVisible();
  const href = await link.getAttribute('href');
  expect(href).toContain('simplefin');
});

test('bank accordion items are present', async ({ page }) => {
  // At least the Chase, BofA, Amex items should exist
  await expect(page.locator('.bank-accordion-item[data-bank="chase"]')).toBeVisible();
  await expect(page.locator('.bank-accordion-item[data-bank="bofa"]')).toBeVisible();
  await expect(page.locator('.bank-accordion-item[data-bank="amex"]')).toBeVisible();
});

test('clicking a bank accordion item expands it', async ({ page }) => {
  const chaseItem = page.locator('.bank-accordion-item[data-bank="chase"]');
  const chaseTrigger = chaseItem.locator('.bank-accordion-trigger');
  const chaseBody = chaseItem.locator('.bank-accordion-body');

  // Body hidden initially
  await expect(chaseBody).toBeHidden();

  await chaseTrigger.click();

  // Body visible after click
  await expect(chaseBody).toBeVisible();
  await expect(chaseBody).toContainText('chase.com');
});

test('nav bar is visible on connect page', async ({ page }) => {
  await expect(page.locator('nav.nav')).toBeVisible();
});

test('Connect nav link is marked active', async ({ page }) => {
  const connectNavLink = page.locator('nav.nav a[href="/connect"].active');
  await expect(connectNavLink).toBeVisible();
});
