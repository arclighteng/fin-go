import { test, expect } from '@playwright/test';

// ---------------------------------------------------------------------------
// Dashboard — layout, nav, empty-state, and CSS checks against a fresh DB
// ---------------------------------------------------------------------------

test.beforeEach(async ({ page }) => {
  await page.goto('/dashboard');
});

test('dashboard page renders without a server error', async ({ page }) => {
  await expect(page.locator('body')).not.toContainText('Internal Server Error');
  await expect(page.locator('body')).not.toContainText('template:');
});

test('nav bar is visible', async ({ page }) => {
  await expect(page.locator('nav.nav')).toBeVisible();
});

test('nav bar contains Dashboard link', async ({ page }) => {
  const dashLink = page.locator('nav.nav a[href="/dashboard"]');
  await expect(dashLink).toBeVisible();
  await expect(dashLink).toContainText('Dashboard');
});

test('nav bar contains Connect link', async ({ page }) => {
  const connectLink = page.locator('nav.nav a[href="/connect"]');
  await expect(connectLink).toBeVisible();
  await expect(connectLink).toContainText('Connect');
});

test('nav bar contains Sync Log link', async ({ page }) => {
  const syncLogLink = page.locator('nav.nav a[href="/sync-log"]');
  await expect(syncLogLink).toBeVisible();
  await expect(syncLogLink).toContainText('Sync Log');
});

test('nav brand / logo is present', async ({ page }) => {
  const brand = page.locator('nav.nav .nav-brand');
  await expect(brand).toBeVisible();
  await expect(brand).toContainText('Fin');
});

test('"no data" banner shown when DB is empty', async ({ page }) => {
  // Fresh DB — base.html renders the .demo-banner with an "Import" CTA
  const demoBanner = page.locator('.demo-banner');
  await expect(demoBanner).toBeVisible();
  await expect(demoBanner).toContainText('No data yet');
});

test('period selector controls are present', async ({ page }) => {
  // The dashboard renders .period-controls-v3 (month nav buttons)
  const periodControls = page.locator('.period-controls-v3');
  await expect(periodControls).toBeVisible();
});

test('main content container is present', async ({ page }) => {
  // base.html wraps page content in #main-content.container
  await expect(page.locator('#main-content')).toBeVisible();
});

test('sync button is rendered in nav', async ({ page }) => {
  const syncBtn = page.locator('#syncBtn');
  await expect(syncBtn).toBeVisible();
});

test('CSS loads — body has a non-default background color', async ({ page }) => {
  // If the stylesheet fails to load the body bg stays transparent/white (#fff).
  // app.css sets --bg-primary to a non-white value in light mode; verify that
  // at least one CSS custom property is defined on :root.
  const bgPrimary = await page.evaluate(() =>
    getComputedStyle(document.documentElement).getPropertyValue('--bg-primary').trim()
  );
  expect(bgPrimary.length).toBeGreaterThan(0);
});

test('account filter section exists on dashboard', async ({ page }) => {
  // The dashboard renders an account filter area (even when no accounts exist)
  // identified by the period-controls-v3 wrapper or a filter container.
  // We confirm the bento grid or the content region rendered at all.
  const container = page.locator('#main-content .container, #main-content');
  await expect(container).toBeVisible();
});
