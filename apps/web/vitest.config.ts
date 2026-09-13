import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import { fileURLToPath } from 'node:url'

/**
 * Component tests. See ADR-009 for why these exist alongside Playwright.
 *
 * Scope is deliberately narrow: components whose contract cannot be
 * reached from a browser test. Anything involving layout, focus order or
 * computed styles belongs in `tests/e2e` — jsdom will happily lie about
 * all three.
 */
export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
    // Only `src`. Without this, Vitest collects tests/e2e/*.spec.ts and
    // then fails on Playwright's imports with an error that looks like a
    // broken install rather than a misconfigured glob.
    include: ['src/**/*.test.{ts,tsx}'],
  },
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
})
