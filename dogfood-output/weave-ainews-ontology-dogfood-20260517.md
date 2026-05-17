# Weave AI News Ontology Dogfood Report — 2026-05-17

## Scope
Use Weave WebUI/API as an end-to-end dogfood scenario:
1. Create a new `ainews` ontology.
2. Model an `AI_News` object type.
3. Load the AI news items sent to the user during the last week.
4. Browse, search, sort/filter, inspect metadata, and use the result as a repeatable E2E scenario.

## Test data
- Source: Hermes cron/session transcripts for AI news deliveries in the last week.
- Extracted records: **103**
- Files:
  - `dogfood-output/ainews-records-20260517.json`
  - `dogfood-output/ainews-records-20260517.csv`
- Dates: 2026-05-12: 24, 2026-05-13: 24, 2026-05-14: 16, 2026-05-15: 7, 2026-05-16: 24, 2026-05-17: 8
- Categories: 产品/模型: 25, 企业应用: 16, 安全/网络安全: 27, 监管/政策: 8, 算力/基础设施: 11, 融资/资本市场: 16
- Sessions sampled: 13

## Ontology created
- Ontology API name: `ainews`
- Display name: `AI News`
- Object type: `AI_News` / `AI News Item`
- Primary key/title: `newsId` / `title`
- Properties: `newsId`, `deliveredDate`, `sessionId`, `rank`, `title`, `source`, `category`, `summary`, `rawItem`

## What worked
- Dashboard now shows `AI News` alongside existing ontologies.
- `/explorer/ainews` shows the `AI News Item` object type.
- `/browser/ainews/AI_News` lists **103** rows after the successful ingest path.
- API list/search/aggregate work after the index exists and data is re-ingested:
  - `GET /api/v2/ontologies/ainews/objects/AI_News?pageSize=5` → `totalCount: "103"`
  - `POST /api/v2/ontologies/ainews/objects/AI_News/search` with `query: "OpenAI"` → matching rows
  - aggregate by `category` succeeds after the successful ingest path.

## Issues found

### DOG-003 — Imported ontology accepts stream ingest before index exists, causing silent non-visibility/data-loss behavior
**Severity:** high

Repro:
1. `POST /api/admin/ontologies/import?mode=replace` for ontology `ainews` + object type `AI_News` succeeds.
2. Immediately `POST /api/v2/ontologies/ainews/streams/AI_News/ingest` in 3 batches returns `200` and edit counts `(50, 50, 3)`.
3. List/search/aggregate then fail or show no data:
   - List/search initially return `index not found for object type "AI_News"`.
   - `POST /api/admin/indexes/rebuild` returns `{"scopedKey":"ainews__AI_News","indexedCount":0}`.
   - After waiting for redelivery, list/search remain `totalCount: "0"`.
4. Re-ingesting the same 103 records **after** the index exists makes the rows visible.

Expected:
- Import/create object type should bootstrap the required object index, or stream ingest should fail fast and not claim success.
- An accepted stream ingest should become visible without requiring manual index rebuild + re-ingest.

Evidence:
- First ingest response: 3 successful batches / 103 accepted edits.
- Follow-up: `ListObjectsFailed` / `SearchObjectsFailed` with `index not found for object type "AI_News"`.
- Rebuild evidence: `indexedCount: 0`.
- Re-ingest evidence: browser/API total count becomes 103.

### DOG-004 — Browser search sends `containsAnyTerm.value` as an array, backend expects string
**Severity:** high for browser usability

Repro:
1. Open `/browser/ainews/AI_News`.
2. Type `OpenAI` in “Search objects” and press Enter.
3. UI displays `INVALID_ARGUMENT: SearchObjectsFailed` and no rows.

Exact failing request shape produced by BrowserPage:
```json
{
  "where": { "type": "containsAnyTerm", "field": "title", "value": ["OpenAI"] },
  "pageSize": 25,
  "select": ["category", "deliveredDate", "newsId", "rank", "rawItem", "sessionId", "source", "summary", "title"],
  "facets": ["category", "deliveredDate", "rawItem", "source", "summary"]
}
```

Backend error:
```json
{
  "errorName": "SearchObjectsFailed",
  "parameters": {
    "reason": "containsAnyTerm value must be a string: json: cannot unmarshal array into Go value of type string"
  }
}
```

Expected:
- Browser search should return matching AI News rows and no error.
- Frontend/backend `containsAnyTerm` contract should be aligned and covered by regression tests.

Evidence screenshot:
- `/Users/liyang/.hermes/cache/screenshots/browser_screenshot_d35533c3ab0a4ca8abe04a7e129be2f4.png`

### DOG-005 — Explorer properties table renders Searchable/Sortable flags as all false despite API metadata showing true
**Severity:** medium

Repro:
1. Open `/explorer/ainews/AI_News`.
2. Properties table shows every property as `Searchable = ✕` and `Sortable = ✕`.
3. Direct API metadata contradicts the UI:
   - `/api/v2/ontologies/ainews/objectTypes/byRid/<rid>/properties` reports e.g. `title.isSearchable=true`, `title.isSortable=true`, `category.isSearchable=true`, `category.isSortable=true`, `deliveredDate.isSearchable=true`, `deliveredDate.isSortable=true`, etc.

Expected:
- Explorer should render the same `isNullable`, `isSearchable`, and `isSortable` flags returned by the detailed properties endpoint or enrich the object type response before rendering.

Evidence screenshot:
- `/Users/liyang/.hermes/cache/screenshots/browser_screenshot_99ec29c831ed4e99be0f8c06f3ae3351.png`

## Suggested E2E acceptance scenario
Given a clean or existing dev environment, when the AI News fixture is imported as `ainews` and 103 rows are ingested, then:
1. Dashboard shows the AI News ontology.
2. Explorer shows `AI_News` and accurate property flags.
3. Browser table shows 103 rows.
4. Searching `OpenAI` in the WebUI returns matching rows without `INVALID_ARGUMENT`.
5. The scenario works without manual index rebuild or second ingest.
