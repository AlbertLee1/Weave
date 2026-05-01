import axe from 'axe-core';

export interface RunAxeOptions {
  /** Ignore rule ids on top of the defaults (color-contrast etc). */
  disableRules?: string[];
  /** Override the WCAG-tag set used to scope rules. */
  tags?: string[];
}

const DEFAULT_DISABLED_RULES = [
  // jsdom does not run a real layout pipeline, so getComputedStyle returns
  // inert defaults; axe's color-contrast check is unreliable here. The same
  // rule still runs in real-browser CI (Playwright) — disable for vitest only.
  'color-contrast',
];

const DEFAULT_TAGS = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'];

/**
 * Run axe-core against `container` and return any critical / serious
 * WCAG 2.1 AA violations. Returns an empty array when the subtree is clean.
 *
 * Helpers in this file deliberately scope to critical+serious — the PRD ask
 * (US-351) is "no critical issue" on Dashboard / Explorer / Browser. Minor
 * and moderate findings are surfaced via `runAxe` directly when callers
 * want a stricter assertion.
 */
export async function runAxe(
  container: Element,
  options: RunAxeOptions = {},
): Promise<axe.AxeResults> {
  const disabled = [...DEFAULT_DISABLED_RULES, ...(options.disableRules ?? [])];
  const rules = disabled.reduce<Record<string, { enabled: boolean }>>(
    (acc, id) => {
      acc[id] = { enabled: false };
      return acc;
    },
    {},
  );
  return axe.run(container, {
    runOnly: { type: 'tag', values: options.tags ?? DEFAULT_TAGS },
    rules,
    resultTypes: ['violations'],
  });
}

export async function expectNoCriticalViolations(
  container: Element,
  options?: RunAxeOptions,
): Promise<void> {
  const results = await runAxe(container, options);
  const blocking = results.violations.filter(
    (v) => v.impact === 'critical' || v.impact === 'serious',
  );
  if (blocking.length === 0) return;
  const summary = blocking
    .map(
      (v) =>
        `[${v.impact}] ${v.id} — ${v.help} (${v.nodes.length} node${v.nodes.length === 1 ? '' : 's'})\n` +
        v.nodes
          .slice(0, 3)
          .map((n) => `    target: ${n.target.join(' ')}\n    html:   ${n.html.slice(0, 200)}`)
          .join('\n'),
    )
    .join('\n');
  throw new Error(
    `axe-core found ${blocking.length} critical/serious violation(s):\n${summary}`,
  );
}
