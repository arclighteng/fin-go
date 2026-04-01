import { test, expect } from '@playwright/test';

// ---------------------------------------------------------------------------
// Navigation — verify every page route responds and lands on the right content
// ---------------------------------------------------------------------------

test('GET / redirects to /dashboard', async ({ page }) => {
  const response = await page.goto('/');
  // After redirect chain, final URL must be /dashboard
  expect(page.url()).toContain('/dashboard');
  expect(response?.status()).toBeLessThan(400);
});

test('dashboard page loads with correct title', async ({ page }) => {
  await page.goto('/dashboard');
  await expect(page).toHaveTitle(/Dashboard.*Fin/i);
});

test('dashboard page contains nav bar', async ({ page }) => {
  await page.goto('/dashboard');
  const nav = page.locator('nav.nav');
  await expect(nav).toBeVisible();
});

test('connect page loads', async ({ page }) => {
  await page.goto('/connect');
  await expect(page).toHaveTitle(/Connect.*Fin/i);
  // The h1 on the connect page
  await expect(page.locator('h1')).toContainText('Connect Your Bank');
});

test('sync-log page loads', async ({ page }) => {
  await page.goto('/sync-log');
  await expect(page).toHaveTitle(/Fin/i);
  // The page renders without a 500 error overlay
  await expect(page.locator('body')).not.toContainText('Internal Server Error');
});

test('budget page loads', async ({ page }) => {
  await page.goto('/budget');
  await expect(page).toHaveTitle(/Fin/i);
  await expect(page.locator('body')).not.toContainText('Internal Server Error');
});

test('commitments page loads', async ({ page }) => {
  await page.goto('/commitments');
  await expect(page).toHaveTitle(/Fin/i);
  await expect(page.locator('body')).not.toContainText('Internal Server Error');
});

test('insights page loads', async ({ page }) => {
  await page.goto('/insights');
  await expect(page).toHaveTitle(/Fin/i);
  await expect(page.locator('body')).not.toContainText('Internal Server Error');
});

test('review page loads', async ({ page }) => {
  await page.goto('/review');
  await expect(page).toHaveTitle(/Fin/i);
  await expect(page.locator('body')).not.toContainText('Internal Server Error');
});

test('GET /health returns {"status":"ok"}', async ({ request }) => {
  const response = await request.get('/health');
  expect(response.status()).toBe(200);
  const body = await response.json();
  expect(body).toEqual({ status: 'ok' });
});

test('GET /api/version returns JSON with version field', async ({ request }) => {
  const response = await request.get('/api/version');
  expect(response.status()).toBe(200);
  const body = await response.json();
  expect(body).toHaveProperty('version');
  expect(typeof body.version).toBe('string');
});
