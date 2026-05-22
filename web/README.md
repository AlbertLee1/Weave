# Weave Web UI

This package contains the browser UI for Weave's OSv2 service layer. It is a
Vite, React, TypeScript, and Tailwind app that talks to the local Weave API on
`:9117` during dogfood and E2E runs.

## Local Development

Run these commands from `web/` unless noted otherwise:

```bash
npm install
npm run dev
```

The dev server defaults to `http://localhost:5173`. Authentication behavior is
controlled by the backend `AUTH_MODE`; the E2E stack seeds deterministic users
for token-mode flows and runs safely in dev-mode as well.

## Verification

Use the narrowest command that proves the change, then run the broader gate
when the change touches shared UI, routing, or browser dogfood behavior:

```bash
npm test
npm run typecheck
npm run build
```

Additional scripts:

- `npm run lint` runs ESLint across the package.
- `npm run test:watch` starts Vitest in watch mode.
- `npm run test:coverage` collects Vitest coverage.
- `npm run test:e2e` runs Playwright against a running Weave stack.

## Playwright BDD And E2E

The Playwright config discovers both BDD-style browser tests and lower-level
E2E probes:

- `web/tests/` holds BDD-flavoured specs named
  `feature.<domain>.<scenario>.spec.ts`.
- `web/tests/support/` holds Given/When/Then helpers, page objects, login
  helpers, and deterministic data factories.
- `web/e2e/` holds phase gates, dogfood probes, and US-444 core-flow specs.

Start the local browser dogfood stack from the repository root:

```bash
make e2e-up
cd web
npm run test:e2e
cd ..
make e2e-down
```

`make e2e-up` starts Docker services, `bin/weave`, Vite, and the deterministic
Northwind seed data. Logs are written under `.e2e-logs/`; process IDs are kept
under `.e2e-pids/`.

Useful scoped runs:

```bash
cd web
npx playwright test tests/
npx playwright test --grep @smoke
npx playwright test e2e/dogfood-verify.spec.ts
```

## Primary Browser Surfaces

- Dashboard: `/` lists ontologies, object type counts, and recent activity.
- Browser: `/browser/:ontology/:objectType` inspects and filters objects.
- ObjectSets: `/objectsets/:ontology` is the Query Builder workspace.
- Quiver: `/quiver/:ontology` builds and shares time-series dashboards.
- Import Data: `/import/:ontology` drives CSV upload and schema mapping.
- Admin: `/admin/:ontology/*` manages object types, link types, action types,
  interfaces, value types, schema graph, history, and security policies.
- Command Palette: available globally from the shell for keyboard navigation,
  including ontology-scoped workspace routes when an ontology is active.

Other top-level surfaces include Dashboards, Apps, AIP Threads, AIP Logic,
Pipelines, Marketplace, Notifications, Permission Requests, API Playground, API
Metrics, Settings, and Vertex workspaces.

## Related Docs

- `web/tests/README.md` documents the BDD Playwright conventions.
- `web/e2e/README.md` documents phase-gate and dogfood E2E setup.
- `web/e2e/us444/README.md` documents the 20 core-flow Playwright suite.
