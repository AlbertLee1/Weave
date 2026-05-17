# Weave WebUI Dogfood Report — 2026-05-17

## Scope
- Started local Weave stack via `scripts/e2e-setup.sh` (Docker + Go API :9117 + Vite :5173).
- Browser-tested global navigation, ontology-scoped navigation, objectsets pages, admin pages, Northwind explorer, and Vertex `/vertex/new`.
- Checked console/runtime errors in batches.

## Findings

### DOG-001 — Northwind E2E seed ontology is empty because `scripts/e2e-setup.sh` seed fails
- Severity: High
- Evidence: `scripts/e2e-setup.sh` fails during seed wipe with `functions_ontology_rid_fkey` foreign-key violation.
- Browser evidence: `http://127.0.0.1:5173/explorer/northwind` renders `No object types`.
- Screenshot: MEDIA:/Users/liyang/.hermes/cache/screenshots/browser_screenshot_123ae2f8533245039e617c16990a8c1d.png
- Expected: Northwind seed ontology should contain E2E object types such as Customers/Orders/Products and support Playwright baseline data.
- Actual: Northwind ontology exists but `/api/v2/ontologies/northwind/objectTypes` returns `data: []`; Explorer is empty.

### DOG-002 — Dashboard global Object Types metric is hardcoded to 0
- Severity: Medium
- Evidence: Dashboard shows `0 对象类型`, while IoT Demo card shows `4 types`; source `web/src/components/dashboard/DashboardPage.tsx` sets `const totalObjectTypes = 0;`.
- Screenshot: MEDIA:/Users/liyang/.hermes/cache/screenshots/browser_screenshot_27ed0d20c07a413a95a00455f0ff0041.png
- Expected: Dashboard total object type count should equal the sum of per-ontology object type counts (at least 4 with current IoT Demo data, more after Northwind seed succeeds).
- Actual: Top summary remains 0, undermining dashboard trust.

## Routes checked with no blocking runtime errors
- Global: `/`, `/dashboards`, `/apps`, `/threads`, `/logic-flows`, `/pipelines`, `/developer/playground`, `/developer/metrics`, `/schema/infer`, `/permission-requests`, `/notifications`, `/mentions`, `/marketplace`, `/settings`, `/admin/markings`, `/admin/compliance`, `/audit`.
- Ontology-scoped iotDemo: `/explorer/iotDemo`, `/objectsets/iotDemo`, `/objectsets/iotDemo/diff`, `/objectsets/iotDemo/snapshots`, `/objectsets/iotDemo/lineage`, `/objectsets/iotDemo/live`, `/quiver/iotDemo`, `/import/iotDemo`, `/approvals/iotDemo`, `/actions/iotDemo/history`, `/actions/iotDemo/jobs`, `/queries/iotDemo`, `/automation/iotDemo`, `/proposals/iotDemo`, `/admin/iotDemo/*`, `/browser/iotDemo/Device`, `/vertex/new`.

## Console
No persistent browser JS errors were observed during the sampled route sweep.
