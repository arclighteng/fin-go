import { test, expect } from '@playwright/test';

// ---------------------------------------------------------------------------
// API endpoints — contract checks against a fresh (empty) database
// ---------------------------------------------------------------------------

test('GET /health returns 200 with status ok', async ({ request }) => {
  const response = await request.get('/health');
  expect(response.status()).toBe(200);

  const contentType = response.headers()['content-type'];
  expect(contentType).toContain('application/json');

  const body = await response.json();
  expect(body).toEqual({ status: 'ok' });
});

test('GET /api/version returns 200 with version string', async ({ request }) => {
  const response = await request.get('/api/version');
  expect(response.status()).toBe(200);

  const body = await response.json();
  expect(body).toHaveProperty('version');
  expect(typeof body.version).toBe('string');
  expect(body.version.length).toBeGreaterThan(0);
});

test('GET /api/accounts returns 200 with a JSON array or null', async ({ request }) => {
  const response = await request.get('/api/accounts');
  expect(response.status()).toBe(200);

  const body = await response.json();
  // Fresh DB — no accounts; Go returns nil slice which encodes as null
  // Accept either null or an empty array as valid empty responses
  expect(body === null || Array.isArray(body)).toBe(true);
});

test('GET /api/sync-status returns required fields', async ({ request }) => {
  const response = await request.get('/api/sync-status');
  expect(response.status()).toBe(200);

  const body = await response.json();
  expect(body).toHaveProperty('syncs_today');
  expect(body).toHaveProperty('limit');
  expect(body).toHaveProperty('can_sync');

  expect(typeof body.syncs_today).toBe('number');
  expect(body.limit).toBe(20);
  expect(typeof body.can_sync).toBe('boolean');
});

test('GET /api/sync-status reflects fresh DB (0 syncs today)', async ({ request }) => {
  const response = await request.get('/api/sync-status');
  const body = await response.json();
  // Fresh test DB has no prior runs
  expect(body.syncs_today).toBe(0);
  expect(body.can_sync).toBe(true);
});

test('POST /api/sync returns JSON (error or success — no server crash)', async ({ request }) => {
  // Without a configured SimpleFIN URL this should return a structured error,
  // not a 500 or an empty body.
  const response = await request.post('/api/sync', { data: {} });
  // Must be either success (200) or a handled client/server error with JSON body
  const contentType = response.headers()['content-type'];
  expect(contentType).toContain('application/json');

  const body = await response.json();
  // The body must be a JSON object (success payload or error object)
  expect(typeof body).toBe('object');
  expect(body).not.toBeNull();
});

test('GET /api/categories returns a non-empty JSON array', async ({ request }) => {
  const response = await request.get('/api/categories');
  expect(response.status()).toBe(200);

  const body = await response.json();
  expect(Array.isArray(body)).toBe(true);
  // Categories are compiled into the binary — there must be at least one
  expect(body.length).toBeGreaterThan(0);

  // Each category has the expected shape
  const first = body[0];
  expect(first).toHaveProperty('id');
  expect(first).toHaveProperty('name');
  expect(first).toHaveProperty('icon');
  expect(first).toHaveProperty('color');
});

test('GET /api/budget/targets returns 200 with a JSON array or null', async ({ request }) => {
  const response = await request.get('/api/budget/targets');
  expect(response.status()).toBe(200);

  const body = await response.json();
  // Fresh DB — no targets; Go returns nil slice which encodes as null
  // Accept either null or an array as valid
  expect(body === null || Array.isArray(body)).toBe(true);
});

test('GET /api/tags returns 200 with a JSON array or null', async ({ request }) => {
  const response = await request.get('/api/tags');
  expect(response.status()).toBe(200);

  const body = await response.json();
  // Fresh DB — no tags; Go returns nil slice which encodes as null
  expect(body === null || Array.isArray(body)).toBe(true);
});

test('GET /api/sync-history returns 200 with a JSON array or null', async ({ request }) => {
  const response = await request.get('/api/sync-history');
  expect(response.status()).toBe(200);

  const body = await response.json();
  // Fresh DB — no runs; Go returns nil slice which encodes as null
  expect(body === null || Array.isArray(body)).toBe(true);
});

test('GET /api/search without q param returns 400', async ({ request }) => {
  const response = await request.get('/api/search');
  expect(response.status()).toBe(400);

  const body = await response.json();
  expect(body).toHaveProperty('error');
});

test('GET /api/search with q param returns 200 with JSON array or null', async ({ request }) => {
  const response = await request.get('/api/search?q=test');
  expect(response.status()).toBe(200);

  const body = await response.json();
  // SearchTransactions returns []models.Transaction which can be nil (null) when empty
  expect(body === null || Array.isArray(body)).toBe(true);
});

test('Content-Type is application/json for all API responses', async ({ request }) => {
  const endpoints = [
    '/health',
    '/api/version',
    '/api/accounts',
    '/api/sync-status',
    '/api/categories',
    '/api/budget/targets',
    '/api/tags',
    '/api/sync-history',
  ];

  for (const endpoint of endpoints) {
    const response = await request.get(endpoint);
    const ct = response.headers()['content-type'];
    expect(ct, `${endpoint} should return application/json`).toContain('application/json');
  }
});
