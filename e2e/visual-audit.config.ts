import { defineConfig } from '@playwright/test';
import path from 'node:path';
import standard from './playwright.config';

// Explicit, opt-in screenshots. The normal CI suite never writes an image set.
export default defineConfig({
  ...standard,
  testMatch: 'visual-audit.spec.ts',
  testIgnore: [],
  // Keep opt-in visual artifacts away from the regular suite's failure output.
  outputDir: process.env.SOLITUDES_VISUAL_DIR
    ? path.join(process.env.SOLITUDES_VISUAL_DIR, 'test-results')
    : 'test-results',
  timeout: 120000,
});
