// useRouteTitle — WCAG 2.4.2 (Page Titled) support.
//
// index.html ships a single static <title>Weave</title>, so every SPA
// route historically shared the same browser-tab / screen-reader page
// title. That makes route changes inaudible to screen-reader users and
// makes browser history / tabs hard to tell apart. This hook derives a
// human-readable page title from the current pathname so the consuming
// <RouteTitle /> component can keep `document.title` in sync per route.
//
// Pure & router-agnostic: it takes a pathname string and returns the
// resolved title, which keeps it trivially unit-testable without a
// Router wrapper. The first path segment is matched against the table
// below; unmatched routes fall back to the bare brand name.

const BRAND = 'Weave';

// First-path-segment → human-readable page title. Keep these aligned
// with the <Route> paths declared in App.tsx. The empty key ('') maps
// the index route ("/") to the Dashboard.
const SEGMENT_TITLES: Record<string, string> = {
  '': 'Dashboard',
  explorer: 'Explorer',
  browser: 'Browser',
  methods: 'Interface Methods',
  actions: 'Actions',
  threads: 'Threads',
  'aip-threads': 'Threads',
  'logic-flows': 'Logic Flows',
  'aip-logic': 'Logic Flows',
  'aip-tools': 'AIP Tools',
  pipelines: 'Pipelines',
  lineage: 'Lineage',
  dashboards: 'Dashboards',
  apps: 'Apps',
  approvals: 'Approvals',
  'permission-requests': 'Permission Requests',
  mentions: 'Mentions',
  notifications: 'Notifications',
  marketplace: 'Marketplace',
  functions: 'Functions',
  settings: 'Settings',
  automation: 'Automation',
  proposals: 'Proposals',
  aggregation: 'Aggregation',
  queries: 'Queries',
  quiver: 'Quiver',
  objectsets: 'Object Sets',
  import: 'Import',
  schema: 'Schema',
  'schema-inference': 'Schema Inference',
  developer: 'Developer',
  'api-playground': 'Developer',
  'api-metrics': 'Developer',
  'sql-sandbox': 'Developer',
  admin: 'Admin',
  audit: 'Audit',
  vertex: 'Vertex',
  login: 'Sign in',
};

/**
 * Resolve the human-readable page title for a given pathname.
 *
 * Returns the full `document.title` string, e.g. `"Explorer · Weave"`
 * for `/explorer/iotDemo`, or the bare brand `"Weave"` for routes that
 * are not covered by the segment table (e.g. the NotFound fallback).
 */
export function resolveRouteTitle(pathname: string): string {
  // Strip the leading slash, then take the first segment. For "/" this
  // yields an empty string, which maps to the Dashboard above.
  const firstSegment = pathname.replace(/^\/+/, '').split('/')[0] ?? '';
  const pageTitle = SEGMENT_TITLES[firstSegment];
  return pageTitle ? `${pageTitle} · ${BRAND}` : BRAND;
}
