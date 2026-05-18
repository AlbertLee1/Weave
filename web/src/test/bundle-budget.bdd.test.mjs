import { existsSync, readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { describe, expect, it } from 'vitest';

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), '../..');

async function loadBudgetGuard() {
  const guardPath = resolve(webRoot, 'scripts/check-bundle-budget.mjs');
  if (!existsSync(guardPath)) {
    expect.fail('missing bundle budget guard script at web/scripts/check-bundle-budget.mjs');
  }
  return import(pathToFileURL(guardPath).href);
}

async function loadChunkingHelper() {
  const helperPath = resolve(webRoot, 'src/build/chunking.ts');
  if (!existsSync(helperPath)) {
    expect.fail('missing Vite chunking helper at web/src/build/chunking.ts');
  }
  return import('../build/chunking.ts');
}

describe('SELF-408 bundle budget guard', () => {
  it('Given CI runs the web build When the build script is inspected Then it invokes the bundle budget guard after Vite emits assets', () => {
    const packageJson = JSON.parse(readFileSync(resolve(webRoot, 'package.json'), 'utf8'));

    expect(packageJson.scripts.build).toContain('vite build');
    expect(packageJson.scripts.build).toContain('node scripts/check-bundle-budget.mjs');
  });

  it('Given the production build emits an oversized main chunk When the bundle budget check runs Then the failure names the chunk and configured thresholds', async () => {
    const { evaluateBundleBudgets, formatBudgetReport } = await loadBudgetGuard();

    const result = evaluateBundleBudgets(
      [
        {
          fileName: 'assets/index-test.js',
          rawBytes: 1_250_000,
          gzipBytes: 390_000,
        },
      ],
      [
        {
          label: 'main application chunk',
          fileNamePattern: '^assets/index-[^/]+\\.js$',
          maxRawBytes: 1_000_000,
          maxGzipBytes: 350_000,
        },
      ],
    );

    expect(result.ok).toBe(false);
    const report = formatBudgetReport(result);
    expect(report).toContain('main application chunk');
    expect(report).toContain('assets/index-test.js');
    expect(report).toContain('1,000,000 B raw');
    expect(report).toContain('350,000 B gzip');
  });

  it('Given heavy upper-layer dependencies are present When Vite classifies modules Then graph, markdown, charting, and spreadsheet chunks have stable names', async () => {
    const { chunkNameForModule } = await loadChunkingHelper();

    expect(typeof chunkNameForModule).toBe('function');
    expect(chunkNameForModule('/repo/web/node_modules/@react-sigma/core/dist/index.js')).toBe(
      'vendor-graph',
    );
    expect(chunkNameForModule('/repo/web/node_modules/react-markdown/index.js')).toBe(
      'vendor-markdown',
    );
    expect(chunkNameForModule('/repo/web/node_modules/uplot/dist/uPlot.esm.js')).toBe(
      'vendor-charts',
    );
    expect(chunkNameForModule('/repo/web/node_modules/xlsx/xlsx.mjs')).toBe(
      'vendor-spreadsheet',
    );

    const viteConfig = readFileSync(resolve(webRoot, 'vite.config.ts'), 'utf8');
    expect(viteConfig).toContain("from './src/build/chunking'");
    expect(viteConfig).toMatch(/manualChunks\s*:\s*chunkNameForModule/);
  });
});
