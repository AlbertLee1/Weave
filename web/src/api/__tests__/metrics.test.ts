// US-456: parser + digest unit tests for the prometheus text-format
// scraper used by the admin Performance Dashboard.

import { describe, it, expect } from 'vitest';
import { parsePrometheusText, digestSnapshot, rateBetween, type MetricsSnapshot } from '../metrics';

describe('parsePrometheusText', () => {
  it('skips comment + blank lines', () => {
    const out = parsePrometheusText('# HELP foo doc\n# TYPE foo counter\n\n');
    expect(out).toEqual([]);
  });

  it('parses an unlabelled sample', () => {
    const out = parsePrometheusText('weave_uptime_seconds 12.5\n');
    expect(out).toEqual([{ name: 'weave_uptime_seconds', labels: {}, value: 12.5 }]);
  });

  it('parses a labelled sample', () => {
    const out = parsePrometheusText('weave_http_requests_total{method="GET",status="200"} 17\n');
    expect(out).toEqual([
      { name: 'weave_http_requests_total', labels: { method: 'GET', status: '200' }, value: 17 },
    ]);
  });

  it('parses multiple samples + ignores trailing timestamps', () => {
    const out = parsePrometheusText(
      [
        'weave_http_requests_total{method="GET",status="200"} 17',
        'weave_http_requests_total{method="POST",status="500"} 3 1700000000000',
        'weave_db_queries_total 99',
      ].join('\n'),
    );
    expect(out).toHaveLength(3);
    expect(out[1].value).toBe(3);
  });

  it('tolerates label values that contain escaped quotes', () => {
    const out = parsePrometheusText('m{path="/a\\"b"} 1');
    expect(out[0].labels.path).toBe('/a"b');
  });

  it('drops malformed lines', () => {
    const out = parsePrometheusText('not a metric\nbroken{ 1\nweave_x 5');
    expect(out.map((m) => m.name)).toEqual(['weave_x']);
  });
});

describe('digestSnapshot', () => {
  it('aggregates http counters across labels', () => {
    const snap = digestSnapshot(
      [
        { name: 'weave_http_requests_total', labels: { method: 'GET', status: '200' }, value: 10 },
        { name: 'weave_http_requests_total', labels: { method: 'POST', status: '201' }, value: 4 },
        { name: 'weave_http_requests_total', labels: { method: 'POST', status: '500' }, value: 2 },
        { name: 'weave_http_request_duration_seconds_count', labels: {}, value: 16 },
        { name: 'weave_http_request_duration_seconds_sum', labels: {}, value: 1.6 },
      ],
      1000,
    );

    expect(snap.httpRequestsTotal).toBe(16);
    expect(snap.httpRequests5xxTotal).toBe(2);
    expect(snap.httpDurationCount).toBe(16);
    expect(snap.httpDurationSum).toBeCloseTo(1.6);
    expect(snap.fetchedAt).toBe(1000);
  });

  it('counts 5xx by status-prefix not exact match', () => {
    const snap = digestSnapshot([
      { name: 'weave_http_requests_total', labels: { status: '503' }, value: 2 },
      { name: 'weave_http_requests_total', labels: { status: '599' }, value: 1 },
      { name: 'weave_http_requests_total', labels: { status: '404' }, value: 7 },
    ]);
    expect(snap.httpRequests5xxTotal).toBe(3);
  });
});

describe('rateBetween', () => {
  function snap(over: Partial<MetricsSnapshot>): MetricsSnapshot {
    return {
      fetchedAt: 0,
      httpRequestsTotal: 0,
      httpRequests5xxTotal: 0,
      httpDurationCount: 0,
      httpDurationSum: 0,
      dbQueriesTotal: 0,
      natsPublishTotal: 0,
      natsConsumeTotal: 0,
      rawSamples: [],
      ...over,
    };
  }

  it('returns null when dt is non-positive', () => {
    expect(rateBetween(snap({ fetchedAt: 1000 }), snap({ fetchedAt: 1000 }))).toBeNull();
  });

  it('computes QPS, error rate, and average latency', () => {
    const prev = snap({
      fetchedAt: 0,
      httpRequestsTotal: 100,
      httpRequests5xxTotal: 5,
      httpDurationCount: 100,
      httpDurationSum: 5,
      dbQueriesTotal: 200,
      natsPublishTotal: 10,
      natsConsumeTotal: 8,
    });
    const next = snap({
      fetchedAt: 5000,
      httpRequestsTotal: 200,
      httpRequests5xxTotal: 7,
      httpDurationCount: 200,
      httpDurationSum: 15,
      dbQueriesTotal: 300,
      natsPublishTotal: 20,
      natsConsumeTotal: 18,
    });

    const r = rateBetween(prev, next);
    expect(r).not.toBeNull();
    expect(r!.qps).toBeCloseTo(20);
    expect(r!.errorRate5xx).toBeCloseTo((7 - 5) / (200 - 100));
    expect(r!.avgLatencyMs).toBeCloseTo((10 / 100) * 1000);
    expect(r!.dbQps).toBeCloseTo(20);
    expect(r!.natsPublishQps).toBeCloseTo(2);
    expect(r!.natsConsumeQps).toBeCloseTo(2);
  });

  it('returns null when counters reset (process restart)', () => {
    const prev = snap({ fetchedAt: 0, httpRequestsTotal: 100 });
    const next = snap({ fetchedAt: 1000, httpRequestsTotal: 5 });
    expect(rateBetween(prev, next)).toBeNull();
  });

  it('reports zero error rate when no requests in the window', () => {
    const prev = snap({ fetchedAt: 0 });
    const next = snap({ fetchedAt: 1000 });
    const r = rateBetween(prev, next);
    expect(r).not.toBeNull();
    expect(r!.errorRate5xx).toBe(0);
    expect(r!.qps).toBe(0);
    expect(r!.avgLatencyMs).toBe(0);
  });
});
