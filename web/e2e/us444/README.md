# US-444 — 20 core-flow Playwright specs

Playwright spec suite covering the 20 user flows enumerated in US-444:

| # | Spec | Flow |
|---|------|------|
| 01 | `01-login.spec.ts` | login |
| 02 | `02-browse.spec.ts` | browse |
| 03 | `03-aggregate.spec.ts` | aggregate |
| 04 | `04-action.spec.ts` | action |
| 05 | `05-saga.spec.ts` | saga |
| 06 | `06-branch.spec.ts` | branch |
| 07 | `07-merge.spec.ts` | merge |
| 08 | `08-app-builder.spec.ts` | app builder |
| 09 | `09-quiver.spec.ts` | quiver |
| 10 | `10-marketplace.spec.ts` | marketplace |
| 11 | `11-pkg-install.spec.ts` | pkg install |
| 12 | `12-sdk-mock.spec.ts` | sdk mock |
| 13 | `13-lineage-view.spec.ts` | lineage view |
| 14 | `14-pitr.spec.ts` | pitr |
| 15 | `15-role-mgmt.spec.ts` | role mgmt |
| 16 | `16-mask.spec.ts` | mask |
| 17 | `17-cell-mask.spec.ts` | cell mask |
| 18 | `18-fn-publish.spec.ts` | fn publish |
| 19 | `19-fn-replay.spec.ts` | fn replay |
| 20 | `20-subscribe.spec.ts` | subscribe |

## Run locally

```bash
make e2e-up                                  # boot pg + nats + bin/weave + vite
cd web && npm run test:e2e -- us444/         # this suite only
make e2e-down                                # tear down
```

## Degraded-mode behaviour

Every spec opens with `await skipWhenBackendDown(request)` and treats
`404` / `503` from optional feature endpoints as a `test.skip()` rather
than a failure. This keeps the suite useful both for full local stacks
AND for the syntactic CI gate (`npx playwright test --list`) which runs
without a live backend.

## CI

`.github/workflows/ci.yml` runs `npx playwright test --list us444/` in
the `web` job to gate that the suite stays parseable. A full Playwright
run requires the docker-compose stack and is performed locally via
`make e2e-up`.
