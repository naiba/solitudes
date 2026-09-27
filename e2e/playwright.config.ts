import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: '.',
  testMatch: '*.spec.ts',
  // Screenshot scripts and the historical single-theme suite are opt-in; the
  // default suite uses the same selectors for every configured theme pair.
  testIgnore: ['shot_*.spec.ts', 'screenshot.spec.ts', 'theme-verify.spec.ts'],
  fullyParallel: false,
  retries: 0,
  workers: 1,
  reporter: 'list',
  timeout: 30000,
  use: {
    baseURL: process.env.E2E_BASE_URL || 'http://localhost:8080',
    trace: 'off',
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
