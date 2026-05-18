#!/usr/bin/env node

import { readdir, readFile } from 'node:fs/promises';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { gzipSync } from 'node:zlib';

const here = dirname(fileURLToPath(import.meta.url));
const webRoot = resolve(here, '..');

export const defaultBundleBudgets = [
  {
    label: 'main application chunk',
    fileNamePattern: '^assets/index-[^/]+\\.js$',
    maxRawBytes: 1_500_000,
    maxGzipBytes: 450_000,
  },
];

function formatBytes(bytes) {
  return `${bytes.toLocaleString('en-US')} B`;
}

function describeLimit(budget) {
  return `${formatBytes(budget.maxRawBytes)} raw / ${formatBytes(budget.maxGzipBytes)} gzip`;
}

export function evaluateBundleBudgets(assets, budgets = defaultBundleBudgets) {
  const failures = [];
  const checked = [];

  for (const budget of budgets) {
    const pattern = new RegExp(budget.fileNamePattern);
    const matches = assets.filter((asset) => pattern.test(asset.fileName));

    if (matches.length === 0) {
      failures.push({
        label: budget.label,
        fileName: budget.fileNamePattern,
        reason: 'missing',
        message: `${budget.label}: no emitted chunks matched ${budget.fileNamePattern}`,
      });
      continue;
    }

    for (const asset of matches) {
      const rawOverBudget = asset.rawBytes > budget.maxRawBytes;
      const gzipOverBudget = asset.gzipBytes > budget.maxGzipBytes;
      checked.push({ asset, budget });
      if (!rawOverBudget && !gzipOverBudget) continue;

      failures.push({
        label: budget.label,
        fileName: asset.fileName,
        rawBytes: asset.rawBytes,
        gzipBytes: asset.gzipBytes,
        maxRawBytes: budget.maxRawBytes,
        maxGzipBytes: budget.maxGzipBytes,
        reason: rawOverBudget && gzipOverBudget ? 'raw-and-gzip' : rawOverBudget ? 'raw' : 'gzip',
        message: `${budget.label}: ${asset.fileName} is ${formatBytes(asset.rawBytes)} raw / ${formatBytes(
          asset.gzipBytes,
        )} gzip; limit is ${describeLimit(budget)}`,
      });
    }
  }

  return {
    ok: failures.length === 0,
    checked,
    failures,
  };
}

export function formatBudgetReport(result) {
  if (result.ok) {
    return `Bundle budgets passed (${result.checked.length} chunk${result.checked.length === 1 ? '' : 's'} checked).`;
  }

  return ['Bundle budget check failed:', ...result.failures.map((failure) => `- ${failure.message}`)].join(
    '\n',
  );
}

export async function readBundleAssets(distDir = resolve(webRoot, 'dist')) {
  const assetsDir = join(distDir, 'assets');
  const entries = await readdir(assetsDir, { withFileTypes: true });
  const jsFiles = entries
    .filter((entry) => entry.isFile() && entry.name.endsWith('.js'))
    .map((entry) => entry.name)
    .sort();

  const assets = [];
  for (const fileName of jsFiles) {
    const fullPath = join(assetsDir, fileName);
    const content = await readFile(fullPath);
    assets.push({
      fileName: `assets/${fileName}`,
      rawBytes: content.byteLength,
      gzipBytes: gzipSync(content).byteLength,
    });
  }
  return assets;
}

export async function runBundleBudgetCheck({ distDir = resolve(webRoot, 'dist'), budgets = defaultBundleBudgets } = {}) {
  const assets = await readBundleAssets(distDir);
  return evaluateBundleBudgets(assets, budgets);
}

function parseArgs(argv) {
  const args = { distDir: resolve(webRoot, 'dist') };
  for (let i = 0; i < argv.length; i += 1) {
    if (argv[i] === '--dist') {
      const value = argv[i + 1];
      if (!value) throw new Error('--dist requires a path');
      args.distDir = resolve(value);
      i += 1;
    }
  }
  return args;
}

async function main() {
  const result = await runBundleBudgetCheck(parseArgs(process.argv.slice(2)));
  const report = formatBudgetReport(result);
  if (result.ok) {
    console.log(report);
    return;
  }

  console.error(report);
  process.exitCode = 1;
}

if (import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().catch((error) => {
    console.error(`Bundle budget check failed: ${error instanceof Error ? error.message : String(error)}`);
    process.exitCode = 1;
  });
}
