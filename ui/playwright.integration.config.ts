import { defineConfig, devices } from '@playwright/test';

/**
 * Playwright configuration for Activity UI Integration Tests
 *
 * These tests run against a REAL API backend and are NOT mocked.
 *
 * Prerequisites:
 * - Dev environment must be running (`task dev:setup`)
 * - Example app must be running (`cd ui/example && pnpm run dev`)
 * - API server must be accessible
 *
 * Usage:
 *   pnpm run test:e2e:integration
 *   pnpm run test:e2e:integration:headed
 */
export default defineConfig({
  testDir: './e2e/integration',
  testMatch: '**/*.integration.ts',

  // Run tests serially to avoid conflicts with real data
  fullyParallel: false,
  workers: 1,

  // Fail the build on CI if you accidentally left test.only in the source code
  forbidOnly: !!process.env.CI,

  // Retry on CI only (real API tests can be flaky)
  retries: process.env.CI ? 2 : 0,

  // Reporter to use
  reporter: 'html',

  // Timeout settings - real API is slower than mocks
  timeout: 60000, // 60 seconds per test
  expect: {
    timeout: 10000, // 10 seconds for assertions
  },

  // Shared settings for all the projects below
  use: {
    // Base URL to use in actions like `await page.goto('/')`
    baseURL: 'http://localhost:3000',

    // Collect trace when retrying the failed test
    trace: 'on-first-retry',

    // Screenshot on failure
    screenshot: 'only-on-failure',

    // Video on failure for debugging
    video: 'retain-on-failure',
  },

  // Configure projects for major browsers
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],

  // Don't auto-start webserver - assume dev environment is already running
  // This is intentional - integration tests require a real backend that needs
  // to be started manually with `task dev:setup`
});
