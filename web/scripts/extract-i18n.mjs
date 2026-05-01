#!/usr/bin/env node
// Static i18n key extractor.
//
// Scans `web/src/**/*.{ts,tsx}` for translation call sites and compares the
// extracted set against the canonical zh-CN resource tree. Reports:
//   - keys referenced in source but missing from zh-CN.ts (`MISSING:`)
//   - keys present in zh-CN.ts but missing from en.ts (`UNTRANSLATED:`)
//   - keys present in en.ts but absent from zh-CN.ts (`EXTRA-IN-EN:`)
//   - keys defined in resources but never referenced in source (`UNUSED:`)
//
// Recognised call shapes (string-literal first arg only — dynamic keys are
// ignored on purpose; flag them by adding a constant at the call site):
//   t('common.cancel')          plain-string single-arg
//   t("nav.dashboard", { ... }) plain-string with options
//   <Trans i18nKey="auth.signIn">…</Trans>
//
// Exits 0 on a clean comparison, 1 when any drift is found. Useful as a
// pre-commit / CI gate; intentionally has no third-party deps so it runs
// from the bare repo without a `node_modules` install.
//
// Usage:
//   node web/scripts/extract-i18n.mjs            # report
//   node web/scripts/extract-i18n.mjs --json     # machine-readable JSON

import { readdir, readFile } from 'node:fs/promises';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { dirname, join, resolve } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const webRoot = resolve(here, '..');
const srcRoot = join(webRoot, 'src');
const resourcesDir = join(srcRoot, 'i18n', 'resources');

const TRANSLATABLE_EXTENSIONS = new Set(['.ts', '.tsx']);
const SKIP_DIRECTORIES = new Set(['node_modules', '__tests__', 'dist', 'coverage']);

const T_CALL_RE = /\bt\(\s*(['"`])((?:\\.|(?!\1).)+)\1/g;
const TRANS_RE = /<Trans[^>]*\bi18nKey\s*=\s*["']([^"']+)["']/g;

async function* walk(dir) {
  const entries = await readdir(dir, { withFileTypes: true });
  for (const entry of entries) {
    if (entry.isDirectory()) {
      if (SKIP_DIRECTORIES.has(entry.name)) continue;
      yield* walk(join(dir, entry.name));
    } else if (entry.isFile()) {
      const ext = entry.name.slice(entry.name.lastIndexOf('.'));
      if (TRANSLATABLE_EXTENSIONS.has(ext)) {
        yield join(dir, entry.name);
      }
    }
  }
}

function looksLikeKey(value) {
  // Heuristic: real i18n keys are dot-separated identifiers. Reject sentences
  // and anything that contains whitespace, slashes, etc. — those are usually
  // user-visible strings handed to t() as a fallback or interpolation source.
  return /^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)+$/.test(value);
}

async function extractKeysFromSource() {
  const found = new Map(); // key -> [{file, line}]
  for await (const file of walk(srcRoot)) {
    if (file.startsWith(resourcesDir)) continue;
    const text = await readFile(file, 'utf8');
    const visit = (regex) => {
      regex.lastIndex = 0;
      let match;
      while ((match = regex.exec(text)) !== null) {
        const key = regex === T_CALL_RE ? match[2] : match[1];
        if (!looksLikeKey(key)) continue;
        const lineNumber = text.slice(0, match.index).split('\n').length;
        if (!found.has(key)) found.set(key, []);
        found.get(key).push({ file: file.slice(webRoot.length + 1), line: lineNumber });
      }
    };
    visit(T_CALL_RE);
    visit(TRANS_RE);
  }
  return found;
}

// Strip the CLDR plural suffix so zh-CN (only `_other`) and en (`_one` +
// `_other`) compare equal at the conceptual key level.
const PLURAL_SUFFIX = /_(zero|one|two|few|many|other)$/;

function flatten(obj, prefix = '', acc = new Set()) {
  for (const [k, v] of Object.entries(obj)) {
    const next = prefix ? `${prefix}.${k}` : k;
    if (v && typeof v === 'object' && !Array.isArray(v)) {
      flatten(v, next, acc);
    } else {
      acc.add(next.replace(PLURAL_SUFFIX, ''));
    }
  }
  return acc;
}

async function loadResource(name) {
  const path = join(resourcesDir, `${name}.ts`);
  const source = await readFile(path, 'utf8');
  // The resource files are static `as const` literals — strip the wrapper
  // and parse via dynamic import after rewriting to a JSON-like shape would
  // be brittle. Instead spawn a one-shot import via data:URL so we exercise
  // the actual TypeScript module — but TS can't be evaluated by node directly
  // without a loader. Rather than add a tsx/ts-node dep, parse the literal
  // ourselves: the files are intentionally a single `const NAME = { ... } as
  // const; export default NAME;` shape so we can extract the object literal
  // between the first `{` after `=` and the matching last `}`.
  const start = source.indexOf('= {');
  const end = source.lastIndexOf('} as const');
  if (start < 0 || end < 0 || end <= start) {
    throw new Error(`Cannot parse resource literal: ${path}`);
  }
  const literal = source.slice(start + 2, end + 1);
  // The literal uses single quotes for strings — convert to double quotes
  // safely while ignoring keys like `count_one` etc. (no apostrophes in keys
  // by convention). Use Function to evaluate the JS object safely without
  // pulling in a parser; the resource files are author-controlled.
  // eslint-disable-next-line no-new-func
  const obj = new Function(`return (${literal});`)();
  return flatten(obj);
}

function diff(a, b) {
  const out = [];
  for (const k of a) if (!b.has(k)) out.push(k);
  return out.sort();
}

async function main() {
  const json = process.argv.includes('--json');
  const sourceKeys = await extractKeysFromSource();
  const sourceKeySet = new Set(sourceKeys.keys());

  const zhKeys = await loadResource('zh-CN');
  const enKeys = await loadResource('en');

  const missing = diff(sourceKeySet, zhKeys);
  const unused = diff(zhKeys, sourceKeySet);
  const untranslated = diff(zhKeys, enKeys);
  const extraInEn = diff(enKeys, zhKeys);

  if (json) {
    process.stdout.write(
      JSON.stringify(
        {
          referenced: sourceKeys.size,
          zhCount: zhKeys.size,
          enCount: enKeys.size,
          missing,
          unused,
          untranslated,
          extraInEn,
        },
        null,
        2,
      ) + '\n',
    );
  } else {
    const lines = [];
    lines.push(`# i18n key audit`);
    lines.push(`referenced in source: ${sourceKeys.size}`);
    lines.push(`zh-CN keys: ${zhKeys.size}`);
    lines.push(`en keys: ${enKeys.size}`);
    lines.push('');
    if (missing.length) {
      lines.push(`MISSING (${missing.length}) — referenced in source but absent from zh-CN.ts:`);
      for (const k of missing) {
        const where = sourceKeys.get(k)[0];
        lines.push(`  ${k}  ${where ? `(${where.file}:${where.line})` : ''}`);
      }
      lines.push('');
    }
    if (untranslated.length) {
      lines.push(`UNTRANSLATED (${untranslated.length}) — present in zh-CN but absent from en:`);
      for (const k of untranslated) lines.push(`  ${k}`);
      lines.push('');
    }
    if (extraInEn.length) {
      lines.push(`EXTRA-IN-EN (${extraInEn.length}) — present in en but absent from zh-CN:`);
      for (const k of extraInEn) lines.push(`  ${k}`);
      lines.push('');
    }
    if (unused.length) {
      lines.push(`UNUSED (${unused.length}) — defined in zh-CN but no source reference (likely OK):`);
      for (const k of unused) lines.push(`  ${k}`);
      lines.push('');
    }
    if (!missing.length && !untranslated.length && !extraInEn.length) {
      lines.push('OK: zh-CN and en trees are consistent and every referenced key is defined.');
    }
    process.stdout.write(lines.join('\n') + '\n');
  }

  if (missing.length || untranslated.length || extraInEn.length) {
    process.exit(1);
  }
}

if (import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().catch((err) => {
    console.error(err);
    process.exit(2);
  });
}
