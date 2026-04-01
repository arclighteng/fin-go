import { defineConfig, devices } from '@playwright/test';
import * as os from 'os';
import * as path from 'path';

const testDBPath = path.join(__dirname, '.test.db');

export default defineConfig({
  testDir: './tests',
  timeout: 30_000,
  retries: 0,
  reporter: [['list'], ['html', { open: 'never' }]],

  use: {
    baseURL: 'http://127.0.0.1:8000',
    screenshot: 'only-on-failure',
    trace: 'off',
  },

  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],

  webServer: {
    // Build the binary first, then start it with a fresh temp DB.
    // Relies on `go` being available on PATH (no hardcoded absolute path).
    command: [
      'go build -o e2e/fin-test.exe ./cmd/fin',
      '&&',
      'e2e\\fin-test.exe web --host 127.0.0.1 --port 8000',
    ].join(' '),
    cwd: path.join(__dirname, '..'),
    url: 'http://127.0.0.1:8000/health',
    reuseExistingServer: false,
    timeout: 60_000,
    env: {
      FIN_DB_PATH: testDBPath,
    },
  },
});
