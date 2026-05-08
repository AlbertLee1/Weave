// US-456: Prometheus text-format scraper used by the admin Performance
// Dashboard. The /metrics endpoint already lives in `cmd/server/main.go`
// (registered under promhttp), so the SPA can poll it directly without a
// new route. The shape we expose is intentionally narrow — counters and
// histogram aggregates the dashboard renders — and the parser is robust
// to unknown families so a future metric does not break the page.

const METRICS_PATH = '/metrics';

export interface ParsedMetric {
  name: string;
  labels: Record<string, string>;
  value: number;
}

// parsePrometheusText extracts every (name, labels, value) sample from the
// raw text-format payload returned by promhttp. Comment lines (`# HELP …`,
// `# TYPE …`) and blank lines are ignored. Histograms surface as their
// constituent samples (`_count`, `_sum`, `_bucket{le=…}`); the dashboard's
// derivation layer is responsible for combining those into rate/quantile
// summaries.
export function parsePrometheusText(raw: string): ParsedMetric[] {
  const out: ParsedMetric[] = [];
  for (const rawLine of raw.split('\n')) {
    const line = rawLine.trim();
    if (line.length === 0) continue;
    if (line.startsWith('#')) continue;

    const sample = parseSampleLine(line);
    if (sample) out.push(sample);
  }
  return out;
}

function parseSampleLine(line: string): ParsedMetric | null {
  const lbraceIdx = line.indexOf('{');
  let name: string;
  let labels: Record<string, string> = {};
  let rest: string;
  if (lbraceIdx === -1) {
    const space = line.indexOf(' ');
    if (space === -1) return null;
    name = line.slice(0, space);
    rest = line.slice(space + 1);
  } else {
    name = line.slice(0, lbraceIdx);
    const rbraceIdx = line.indexOf('}', lbraceIdx);
    if (rbraceIdx === -1) return null;
    labels = parseLabels(line.slice(lbraceIdx + 1, rbraceIdx));
    rest = line.slice(rbraceIdx + 1).trimStart();
  }
  const valueStr = rest.split(/\s+/, 1)[0];
  if (!valueStr) return null;
  const value = Number(valueStr);
  if (!Number.isFinite(value)) return null;
  return { name, labels, value };
}

function parseLabels(body: string): Record<string, string> {
  const labels: Record<string, string> = {};
  // simple pairwise scan: name="value",name="value"
  let i = 0;
  while (i < body.length) {
    while (i < body.length && (body[i] === ' ' || body[i] === ',')) i++;
    const eq = body.indexOf('=', i);
    if (eq === -1) break;
    const key = body.slice(i, eq).trim();
    if (body[eq + 1] !== '"') break;
    let j = eq + 2;
    let value = '';
    while (j < body.length && body[j] !== '"') {
      if (body[j] === '\\' && j + 1 < body.length) {
        value += body[j + 1];
        j += 2;
      } else {
        value += body[j];
        j += 1;
      }
    }
    labels[key] = value;
    i = j + 1;
  }
  return labels;
}

// MetricsSnapshot is the per-poll digest the dashboard renders. Counter
// rates need two snapshots taken `dtSeconds` apart, which the page does in
// its polling loop — the API surface here is intentionally point-in-time.
export interface MetricsSnapshot {
  fetchedAt: number;
  httpRequestsTotal: number;
  httpRequests5xxTotal: number;
  httpDurationCount: number;
  httpDurationSum: number;
  dbQueriesTotal: number;
  natsPublishTotal: number;
  natsConsumeTotal: number;
  rawSamples: ParsedMetric[];
}

export function digestSnapshot(samples: ParsedMetric[], fetchedAt: number = Date.now()): MetricsSnapshot {
  let httpRequestsTotal = 0;
  let httpRequests5xxTotal = 0;
  let httpDurationCount = 0;
  let httpDurationSum = 0;
  let dbQueriesTotal = 0;
  let natsPublishTotal = 0;
  let natsConsumeTotal = 0;

  for (const m of samples) {
    if (m.name === 'weave_http_requests_total') {
      httpRequestsTotal += m.value;
      const status = m.labels.status ?? '';
      if (status.startsWith('5')) httpRequests5xxTotal += m.value;
    } else if (m.name === 'weave_http_request_duration_seconds_count') {
      httpDurationCount += m.value;
    } else if (m.name === 'weave_http_request_duration_seconds_sum') {
      httpDurationSum += m.value;
    } else if (m.name === 'weave_db_queries_total') {
      dbQueriesTotal += m.value;
    } else if (m.name === 'weave_nats_publish_total') {
      natsPublishTotal += m.value;
    } else if (m.name === 'weave_nats_consume_total') {
      natsConsumeTotal += m.value;
    }
  }

  return {
    fetchedAt,
    httpRequestsTotal,
    httpRequests5xxTotal,
    httpDurationCount,
    httpDurationSum,
    dbQueriesTotal,
    natsPublishTotal,
    natsConsumeTotal,
    rawSamples: samples,
  };
}

export interface DerivedRates {
  qps: number;
  errorRate5xx: number;
  avgLatencyMs: number;
  dbQps: number;
  natsPublishQps: number;
  natsConsumeQps: number;
}

// rateBetween produces the rate-style derivations the dashboard renders.
// Returns null if dt is non-positive or if the new snapshot lost ground
// against the prior (handles process restarts that reset counters).
export function rateBetween(prev: MetricsSnapshot, next: MetricsSnapshot): DerivedRates | null {
  const dtMs = next.fetchedAt - prev.fetchedAt;
  if (dtMs <= 0) return null;
  const dt = dtMs / 1000;

  const reqDelta = next.httpRequestsTotal - prev.httpRequestsTotal;
  const err5xxDelta = next.httpRequests5xxTotal - prev.httpRequests5xxTotal;
  const durCountDelta = next.httpDurationCount - prev.httpDurationCount;
  const durSumDelta = next.httpDurationSum - prev.httpDurationSum;
  const dbDelta = next.dbQueriesTotal - prev.dbQueriesTotal;
  const natsPubDelta = next.natsPublishTotal - prev.natsPublishTotal;
  const natsConsDelta = next.natsConsumeTotal - prev.natsConsumeTotal;

  if (reqDelta < 0 || err5xxDelta < 0 || durCountDelta < 0 || durSumDelta < 0) return null;

  const qps = reqDelta / dt;
  const errorRate5xx = reqDelta > 0 ? err5xxDelta / reqDelta : 0;
  const avgLatencyMs = durCountDelta > 0 ? (durSumDelta / durCountDelta) * 1000 : 0;

  return {
    qps,
    errorRate5xx,
    avgLatencyMs,
    dbQps: Math.max(0, dbDelta / dt),
    natsPublishQps: Math.max(0, natsPubDelta / dt),
    natsConsumeQps: Math.max(0, natsConsDelta / dt),
  };
}

export async function fetchMetricsSnapshot(): Promise<MetricsSnapshot> {
  const res = await fetch(METRICS_PATH, { credentials: 'same-origin' });
  if (!res.ok) {
    throw new Error(`metrics endpoint returned ${res.status}`);
  }
  const text = await res.text();
  return digestSnapshot(parsePrometheusText(text));
}
