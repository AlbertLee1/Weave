/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 5173,
    proxy: {
      '^/api(/|$)': {
        target: 'http://localhost:9117',
        ws: true,
      },
      '^/health(/|$)': 'http://localhost:9117',
      '^/metrics(/|$)': 'http://localhost:9117',
      '^/swagger(/|$)': 'http://localhost:9117',
      '^/mcp(/|$)': 'http://localhost:9117',
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: './src/test/setup.ts',
    css: true,
    // VTX-122 — coverage mode (v8 + many small files) pushes a few
    // import-heavy tests past the 5s default. Bump to 30s globally so
    // `vitest run --coverage` stays green; non-coverage runs are
    // unaffected because they finish well under the floor anyway.
    testTimeout: 30000,
    // Vitest picks up *.spec.ts by default; we must exclude Playwright
    // e2e specs (web/e2e/**) and BDD specs (web/tests/**, US-002) so
    // they don't get loaded by Vitest.
    exclude: [
      '**/node_modules/**',
      '**/dist/**',
      '**/playwright-report/**',
      '**/test-results/**',
      '**/e2e/**',
      'tests/**',
      '**/*.e2e.{test,spec}.{ts,tsx}',
    ],
    coverage: {
      provider: 'v8',
      reporter: ['text', 'lcov', 'html'],
      exclude: [
        '**/node_modules/**',
        '**/dist/**',
        '**/e2e/**',
        '**/playwright-report/**',
        '**/test-results/**',
        '**/*.config.{ts,js,mjs,cjs}',
        '**/__tests__/**',
        '**/test/**',
        '**/*.test.{ts,tsx}',
        '**/*.spec.{ts,tsx}',
      ],
      thresholds: {
        lines: 60,
        branches: 50,
        functions: 60,
        statements: 60,
        // VTX-122: Vertex-stream components (web/src/vertex/**) are
        // gated at ≥75% per file. The story scopes the hard floor to
        // the Vertex surface only — global thresholds above stay at
        // the project-wide 60% for the legacy Workshop / Explorer /
        // Admin pages that haven't been backfilled yet.
        'src/vertex/**/*.{ts,tsx}': {
          lines: 75,
          branches: 60,
          functions: 75,
          statements: 75,
        },
      },
    },
  },
})
