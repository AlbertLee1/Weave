import {
  describe,
  it,
  expect,
  beforeEach,
  beforeAll,
  afterAll,
  afterEach,
} from 'vitest';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { request, withActiveAsOf, withActiveBranch } from '../client';
import { useTimeTravelStore } from '../../stores/timeTravelStore';
import { useBranchStore } from '../../stores/branchStore';

const server = setupServer();

beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

beforeEach(() => {
  useTimeTravelStore.setState({ selections: {} });
  useBranchStore.setState({ selections: {} });
});

describe('withActiveAsOf', () => {
  it('leaves non-ontology paths untouched', () => {
    useTimeTravelStore.getState().setAsOf('foundry', 'tx-123');
    expect(withActiveAsOf('/api/v2/notifications')).toBe(
      '/api/v2/notifications',
    );
  });

  it('does not inject asOf when no entry is set', () => {
    expect(
      withActiveAsOf('/api/v2/ontologies/foundry/objects/widget'),
    ).toBe('/api/v2/ontologies/foundry/objects/widget');
  });

  it('injects ?asOf=<tx> when the ontology has a pinned transaction', () => {
    useTimeTravelStore.getState().setAsOf('foundry', 'tx-abc');
    expect(
      withActiveAsOf('/api/v2/ontologies/foundry/objects/widget'),
    ).toBe('/api/v2/ontologies/foundry/objects/widget?asOf=tx-abc');
  });

  it('preserves existing query parameters when injecting asOf', () => {
    useTimeTravelStore.getState().setAsOf('foundry', 'tx-abc');
    const out = withActiveAsOf(
      '/api/v2/ontologies/foundry/objects/widget?pageSize=50',
    );
    expect(out).toContain('pageSize=50');
    expect(out).toContain('asOf=tx-abc');
  });

  it('honours an explicit ?asOf= override on the path', () => {
    useTimeTravelStore.getState().setAsOf('foundry', 'tx-abc');
    expect(
      withActiveAsOf(
        '/api/v2/ontologies/foundry/objects/widget?asOf=tx-other',
      ),
    ).toBe('/api/v2/ontologies/foundry/objects/widget?asOf=tx-other');
  });

  it('isolates per-ontology selections', () => {
    useTimeTravelStore.getState().setAsOf('foundry', 'tx-abc');
    expect(
      withActiveAsOf('/api/v2/ontologies/other/objects/widget'),
    ).toBe('/api/v2/ontologies/other/objects/widget');
  });

  it('also injects asOf for admin ontology paths', () => {
    useTimeTravelStore.getState().setAsOf('foundry', 'tx-abc');
    expect(
      withActiveAsOf('/api/admin/ontologies/foundry/objectTypes'),
    ).toBe('/api/admin/ontologies/foundry/objectTypes?asOf=tx-abc');
  });

  it('stacks asOf on top of an injected branch', () => {
    useBranchStore.getState().setBranch('foundry', 'feature-x');
    useTimeTravelStore.getState().setAsOf('foundry', 'tx-abc');
    const out = withActiveAsOf(
      withActiveBranch('/api/v2/ontologies/foundry/objects/widget'),
    );
    expect(out).toContain('branch=feature-x');
    expect(out).toContain('asOf=tx-abc');
  });
});

describe('request() asOf injection (e2e through msw)', () => {
  it('GET to ontology surface includes ?asOf= when store has a selection', async () => {
    let observedUrl = '';
    server.use(
      http.get('/api/v2/ontologies/foundry/objects/widget', ({ request: req }) => {
        observedUrl = req.url;
        return HttpResponse.json({ data: [] });
      }),
    );

    useTimeTravelStore.getState().setAsOf('foundry', 'tx-abc');
    await request<{ data: unknown[] }>(
      'GET',
      '/api/v2/ontologies/foundry/objects/widget',
    );
    expect(observedUrl).toContain('asOf=tx-abc');
  });

  it('non-ontology surface is not asOf-decorated', async () => {
    let observedUrl = '';
    server.use(
      http.get('/api/v2/notifications', ({ request: req }) => {
        observedUrl = req.url;
        return HttpResponse.json({ data: [] });
      }),
    );

    useTimeTravelStore.getState().setAsOf('foundry', 'tx-abc');
    await request<{ data: unknown[] }>('GET', '/api/v2/notifications');
    expect(observedUrl).not.toContain('asOf=');
  });
});
