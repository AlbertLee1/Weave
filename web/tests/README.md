# Weave Playwright BDD Suite

This directory hosts the BDD-flavoured Playwright suite for the Weave
frontend. Specs live under `web/tests/` and share helpers exported from
`web/tests/support/`; the existing imperative end-to-end tests under
`web/e2e/` continue to run alongside.

Introduced in **US-002** (PRD `frontend-backend-gap-coverage`) as the
infrastructure layer for the Phase 4 UI BDD push (US-021 … US-038).

## Layout

```
web/tests/
├── README.md                                   This file
├── support/
│   ├── bdd.ts                                  Given/When/Then + describeFeature
│   ├── dataFactory.ts                          uniqueName + payload builders
│   ├── seedOntology.ts                         seed-or-reuse ontology helper
│   ├── signIn.ts                               AUTH_MODE-aware login helper
│   ├── pageObjects/
│   │   ├── BrowserPage.ts                      /browser/:ontology/:objectType
│   │   ├── DashboardPage.ts                    /
│   │   └── index.ts                            barrel re-export
│   └── index.ts                                barrel re-export of everything
└── feature.<domain>.<scenario>.spec.ts         BDD specs (see naming below)
```

## File naming

```
feature.<domain>.<scenario>.spec.ts
```

- `<domain>` is the user-facing surface area (e.g. `browser`, `dashboard`,
  `admin`, `threads`, `objectSet`).
- `<scenario>` is the short kebab name of the Gherkin Feature (e.g.
  `search-and-filter`, `link-traversal`, `pitr-rollback`).

Examples:

- `feature.browser.search-and-filter.spec.ts`
- `feature.dashboard.recent-activity.spec.ts`
- `feature.admin.role-management.spec.ts`

Specs that don't fit the BDD style (raw imperative checks, low-level
network assertions, etc.) belong under `web/e2e/` instead — keep this
directory focused on Gherkin-shaped scenarios.

## Authoring guide

A scenario typically reads like:

```ts
import { test, expect } from '@playwright/test';
import { BrowserPage, Given, Then, When, describeFeature } from './support';

describeFeature('Browser search and filter', () => {
  test('Scenario: typing in the search box updates the input @smoke', async ({ page }) => {
    const browser = new BrowserPage(page);

    await Given('the user is on the employee Browser page', async () => {
      await browser.goto('northwind', 'employee');
    });
    await When('they type "alice" into the search input', async () => {
      await browser.typeSearch('alice');
    });
    await Then('the search input reflects the value', async () => {
      await expect(browser.searchInput).toHaveValue('alice');
    });
  });
});
```

### Helpers

- `Given` / `When` / `Then` / `And` wrap `test.step()` so the Playwright
  HTML report and trace viewer name the failing Gherkin clause directly.
- `describeFeature(name, body)` prefixes the suite title with
  "Feature: " — keeps the tree readable in `--reporter=list` and the HTML
  report.
- `BrowserPage` (and future page objects) own selectors; specs should
  never reach into `data-testid` strings inline, so renames stay
  one-PR-one-edit.
- `signIn(request)` returns an `Authorization` header bag under
  `AUTH_MODE=token` or `{}` under `AUTH_MODE=dev` — always safe to pass
  through to `request.get/post`.
- `seedOntology(request, { apiName: 'northwind' })` will reuse the
  pre-seeded baseline rather than re-creating it. Pass `reuseExisting:
  false` to force a fresh row, or `apiName: uniqueName('bdd_ont')` for
  isolation.
- `uniqueName(prefix)` / `ontologyPayload()` / `objectTypePayload()` in
  `dataFactory.ts` for ad-hoc identifiers and create-payload defaults.

### Tags

We use Playwright title-grep tags (the cheapest and most portable
shape):

- `@smoke` — short, deterministic scenarios safe for every CI lane.
- `@regression` — broader coverage, can require seeded baseline.

```bash
cd web
npx playwright test --grep @smoke
```

## Running

```bash
cd web

# Spin up backend + frontend + seed data (idempotent)
make -C .. e2e-up

# Run only the BDD suite under web/tests/
npx playwright test tests/

# Run only the smoke subset
npx playwright test --grep @smoke
```

The Playwright config (`web/playwright.config.ts`) discovers both
`web/e2e/**/*.spec.ts` and `web/tests/**/*.spec.ts` so a plain
`npm run test:e2e` runs everything; use the explicit `tests/` path or a
`--grep` tag to scope to BDD specs.

## Adding a page object

1. Add `web/tests/support/pageObjects/<Name>Page.ts` with a class that
   exposes:
   - `readonly page: Page` plus `Locator`s for each interactive element.
   - A `goto(...)` method that drives navigation deterministically.
   - Action methods named after user intent (`typeSearch`, `openFilters`),
     not implementation (`fillInput`, `clickButton`).
2. Re-export it from `web/tests/support/pageObjects/index.ts` and the
   top-level `web/tests/support/index.ts` barrel.
3. Reach for `data-testid` selectors and update `web/CLAUDE.md` if you
   introduce a new convention worth pinning.
