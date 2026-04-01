import { test, expect } from '@playwright/test';

// ---------------------------------------------------------------------------
// Performance — timing budgets measured via the Navigation Timing API
// ---------------------------------------------------------------------------
//
// Thresholds are intentionally generous for a local dev build served on
// loopback. The goal is to catch regressions (e.g. synchronous blocking,
// N+1 queries on startup) not to enforce production SLAs.
//
// All timings are logged to stdout so they appear in the test report.
// ---------------------------------------------------------------------------

interface NavTiming {
  fetchStart: number;
  domContentLoadedEventEnd: number;
  loadEventEnd: number;
  responseEnd: number;
  responseStart: number;
}

/** Collect Navigation Timing metrics for the current page. */
async function collectNavTiming(page: import('@playwright/test').Page): Promise<NavTiming> {
  return page.evaluate(() => {
    const [entry] = performance.getEntriesByType('navigation') as PerformanceNavigationTiming[];
    return {
      fetchStart: entry.fetchStart,
      domContentLoadedEventEnd: entry.domContentLoadedEventEnd,
      loadEventEnd: entry.loadEventEnd,
      responseEnd: entry.responseEnd,
      responseStart: entry.responseStart,
    };
  });
}

test('dashboard loads in under 500 ms (DOM Content Loaded)', async ({ page }) => {
  // Perform an initial navigation to warm up the server's DB connection and
  // the browser's TCP connection, then measure the real second navigation.
  // The first page load after server start can take 2-3 s (SQLite init,
  // TCP handshake) which would give false positives on a 500 ms budget.
  await page.goto('/dashboard', { waitUntil: 'domcontentloaded' });
  await page.goto('/dashboard', { waitUntil: 'domcontentloaded' });

  const timing = await collectNavTiming(page);
  const dclMs = timing.domContentLoadedEventEnd - timing.fetchStart;

  console.log(`[perf] dashboard DCL: ${dclMs.toFixed(1)} ms`);
  expect(dclMs).toBeLessThan(500);
});

test('dashboard First Contentful Paint is measured and logged', async ({ page }) => {
  await page.goto('/dashboard', { waitUntil: 'networkidle' });

  const fcp = await page.evaluate((): number | null => {
    const entries = performance.getEntriesByName('first-contentful-paint');
    if (entries.length === 0) return null;
    return (entries[0] as PerformancePaintTiming).startTime;
  });

  if (fcp !== null) {
    console.log(`[perf] dashboard FCP: ${fcp.toFixed(1)} ms`);
    // FCP under 1 s on loopback is a reasonable baseline
    expect(fcp).toBeLessThan(1000);
  } else {
    // FCP entry may not exist in headless Chromium for very fast loads;
    // log and skip rather than fail.
    console.log('[perf] dashboard FCP: not available (entry not emitted)');
  }
});

test('GET /health API response time is under 50 ms', async ({ request }) => {
  const start = Date.now();
  const response = await request.get('/health');
  const elapsed = Date.now() - start;

  console.log(`[perf] /health response time: ${elapsed} ms`);
  expect(response.status()).toBe(200);
  expect(elapsed).toBeLessThan(50);
});

test('GET /api/accounts response time is under 100 ms', async ({ request }) => {
  const start = Date.now();
  const response = await request.get('/api/accounts');
  const elapsed = Date.now() - start;

  console.log(`[perf] /api/accounts response time: ${elapsed} ms`);
  expect(response.status()).toBe(200);
  expect(elapsed).toBeLessThan(100);
});

test('GET /api/sync-status response time is under 100 ms', async ({ request }) => {
  const start = Date.now();
  const response = await request.get('/api/sync-status');
  const elapsed = Date.now() - start;

  console.log(`[perf] /api/sync-status response time: ${elapsed} ms`);
  expect(response.status()).toBe(200);
  expect(elapsed).toBeLessThan(100);
});

test('connect page DCL is under 500 ms', async ({ page }) => {
  await page.goto('/connect', { waitUntil: 'domcontentloaded' });

  const timing = await collectNavTiming(page);
  const dclMs = timing.domContentLoadedEventEnd - timing.fetchStart;

  console.log(`[perf] connect page DCL: ${dclMs.toFixed(1)} ms`);
  expect(dclMs).toBeLessThan(500);
});

test('sync-log page DCL is under 500 ms', async ({ page }) => {
  await page.goto('/sync-log', { waitUntil: 'domcontentloaded' });

  const timing = await collectNavTiming(page);
  const dclMs = timing.domContentLoadedEventEnd - timing.fetchStart;

  console.log(`[perf] sync-log page DCL: ${dclMs.toFixed(1)} ms`);
  expect(dclMs).toBeLessThan(500);
});

test('budget page DCL is under 500 ms', async ({ page }) => {
  await page.goto('/budget', { waitUntil: 'domcontentloaded' });

  const timing = await collectNavTiming(page);
  const dclMs = timing.domContentLoadedEventEnd - timing.fetchStart;

  console.log(`[perf] budget page DCL: ${dclMs.toFixed(1)} ms`);
  expect(dclMs).toBeLessThan(500);
});

test('insights page DCL is under 500 ms', async ({ page }) => {
  await page.goto('/insights', { waitUntil: 'domcontentloaded' });

  const timing = await collectNavTiming(page);
  const dclMs = timing.domContentLoadedEventEnd - timing.fetchStart;

  console.log(`[perf] insights page DCL: ${dclMs.toFixed(1)} ms`);
  expect(dclMs).toBeLessThan(500);
});

test('review page DCL is under 500 ms', async ({ page }) => {
  await page.goto('/review', { waitUntil: 'domcontentloaded' });

  const timing = await collectNavTiming(page);
  const dclMs = timing.domContentLoadedEventEnd - timing.fetchStart;

  console.log(`[perf] review page DCL: ${dclMs.toFixed(1)} ms`);
  expect(dclMs).toBeLessThan(500);
});

test('GET /api/categories response time is under 100 ms', async ({ request }) => {
  const start = Date.now();
  const response = await request.get('/api/categories');
  const elapsed = Date.now() - start;

  console.log(`[perf] /api/categories response time: ${elapsed} ms`);
  expect(response.status()).toBe(200);
  expect(elapsed).toBeLessThan(100);
});
