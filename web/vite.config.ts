/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:9117',
        ws: true,
      },
      '/health': 'http://localhost:9117',
      '/metrics': 'http://localhost:9117',
      '/swagger': 'http://localhost:9117',
      '/mcp': 'http://localhost:9117',
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: './src/test/setup.ts',
    css: true,
    // Vitest picks up *.spec.ts by default; we must exclude Playwright
    // e2e specs (web/e2e/** and web/tests/**) so they don't get loaded
    // by Vitest.
    exclude: [
      '**/node_modules/**',
      '**/dist/**',
      '**/playwright-report/**',
      '**/test-results/**',
      '**/e2e/**',
      '**/tests/**',
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
      },
    },
  },
})
