import { defineConfig, devices } from '@playwright/test';
import path from 'node:path';

const outputDir = process.env.E2E_SITE_THEME
  ? path.join('test-results', process.env.E2E_PAGING_THREAD ? 'pagination' : 'matrix', `${process.env.E2E_SITE_THEME}-${process.env.E2E_ADMIN_THEME}`)
  : path.join('test-results', 'standalone');

export default defineConfig({
  testDir: '.',
  testMatch: '*.spec.ts',
  outputDir,
  // Screenshots are opt-in; the default suite shares selectors across themes.
  testIgnore: ['visual-audit.spec.ts'],
  fullyParallel: false,
  retries: 0,
  workers: 1,
  reporter: 'list',
  timeout: 30000,
  use: {
    baseURL: process.env.E2E_BASE_URL || 'http://localhost:8080',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [
    {
      name: 'chromium',
      use: {
        ...devices['Desktop Chrome'],
        launchOptions: process.env.E2E_CHROMIUM_EXECUTABLE
          ? { executablePath: process.env.E2E_CHROMIUM_EXECUTABLE }
          : {},
      },
    },
  ],
});
