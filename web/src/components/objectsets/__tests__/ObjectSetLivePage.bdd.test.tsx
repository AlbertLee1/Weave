// US-501: ObjectSet 实时订阅页（Live toggle）
//
// PRD literal acceptance:
//   - "接入 US-459 SSE endpoint"
//   - "UI 显示 push 事件 + 断线指示器"
//
// BDD scenarios encoded here:
//   1. Given an ObjectSet rid, When the user toggles Live on and the
//      server pushes two canonical {seq, type, rid, properties} events,
//      Then both rows render with type badges + rid + seq, and the
//      status indicator transitions idle → connecting → connected.
//   2. Given Live is on and the connection drops, When EventSource
//      fires onerror, Then the status indicator shows "reconnecting"
//      AND already-received events stay in the list (regression guard
//      for "wipe event list on disconnect").
//
// Negative control: a malformed JSON payload must not crash the page
// and must NOT increment the event counter.

import {
  describe,
  it,
  expect,
  vi,
  beforeAll,
  afterAll,
  afterEach,
  beforeEach,
} from 'vitest';
import { render, screen, fireEvent, act } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { setupServer } from 'msw/node';
import { ObjectSetLivePage } from '../ObjectSetLivePage';

const ONTOLOGY = 'test';

type EventSourceListener = (evt: MessageEvent) => void;
type ErrorListener = (evt: Event) => void;

class MockEventSource {
  static instances: MockEventSource[] = [];
  url: string;
  readyState = 0;
  onmessage: EventSourceListener | null = null;
  onerror: ErrorListener | null = null;
  onopen: (() => void) | null = null;
  closed = false;

  constructor(url: string) {
    this.url = url;
    MockEventSource.instances.push(this);
  }

  close() {
    this.closed = true;
    this.readyState = 2;
  }

  simulateOpen() {
    this.readyState = 1;
    this.onopen?.();
  }

  simulateMessage(data: string, lastEventId?: string) {
    const evt = new MessageEvent('message', {
      data,
      lastEventId: lastEventId ?? '',
    });
    this.onmessage?.(evt);
  }

  simulateError() {
    this.readyState = 2;
    this.onerror?.(new Event('error'));
  }
}

(MockEventSource as unknown as Record<string, number>).CONNECTING = 0;
(MockEventSource as unknown as Record<string, number>).OPEN = 1;
(MockEventSource as unknown as Record<string, number>).CLOSED = 2;

const server = setupServer();
beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

beforeEach(() => {
  MockEventSource.instances = [];
  vi.stubGlobal('EventSource', MockEventSource);
  vi.useFakeTimers();
  window.localStorage.clear();
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  window.localStorage.clear();
});

function renderPage(initialPath = `/objectsets/${ONTOLOGY}/live`) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initialPath]}>
        <Routes>
          <Route
            path="/objectsets/:ontology/live"
            element={<ObjectSetLivePage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('US-501 BDD: ObjectSet Live toggle', () => {
  it(
    'Scenario 1: Given an rid, When user toggles Live ON and two events stream in, ' +
      'Then both render with canonical type/rid/seq AND status indicator follows idle → connecting → connected',
    () => {
      renderPage(`/objectsets/${ONTOLOGY}/live?rid=ri.set.scn1`);

      // Given the page is loaded with rid preset and Live is OFF (idle).
      expect(screen.getByTestId('objectset-live-status')).toHaveAttribute(
        'data-state',
        'idle',
      );

      // When the user clicks "Go Live".
      fireEvent.click(screen.getByTestId('objectset-live-toggle'));

      // Then exactly one EventSource is open pointing at the SSE endpoint.
      expect(MockEventSource.instances).toHaveLength(1);
      expect(MockEventSource.instances[0].url).toBe(
        `/api/v2/ontologies/${ONTOLOGY}/objectSets/ri.set.scn1/subscribe`,
      );
      // And status flips to "connecting" until the server confirms the stream.
      expect(screen.getByTestId('objectset-live-status')).toHaveAttribute(
        'data-state',
        'connecting',
      );

      // When server confirms the stream.
      act(() => {
        MockEventSource.instances[0].simulateOpen();
      });
      // Then status indicator shows "connected".
      expect(screen.getByTestId('objectset-live-status')).toHaveAttribute(
        'data-state',
        'connected',
      );

      // When two canonical events arrive (US-459 payload).
      act(() => {
        MockEventSource.instances[0].simulateMessage(
          JSON.stringify({
            seq: 7,
            type: 'created',
            rid: 'Order:o7',
            properties: { amount: 99 },
            eventType: 'ADDED_OR_UPDATED',
            object: { __apiName: 'Order', __primaryKey: 'o7', amount: 99 },
          }),
          '7',
        );
        MockEventSource.instances[0].simulateMessage(
          JSON.stringify({
            seq: 8,
            type: 'modified',
            rid: 'Order:o7',
            properties: { amount: 110 },
            eventType: 'ADDED_OR_UPDATED',
            object: { __apiName: 'Order', __primaryKey: 'o7', amount: 110 },
          }),
          '8',
        );
      });

      // Then both rows render — keyed by seq, with type badge + rid visible.
      const row7 = screen.getByTestId('objectset-live-event-7');
      expect(row7.textContent).toMatch(/created/i);
      expect(row7.textContent).toMatch(/Order:o7/);

      const row8 = screen.getByTestId('objectset-live-event-8');
      expect(row8.textContent).toMatch(/modified/i);
      expect(row8.textContent).toMatch(/Order:o7/);

      // Event counter exposes the count for at-a-glance verification.
      expect(
        screen.getByTestId('objectset-live-event-count').textContent,
      ).toMatch(/2/);
    },
  );

  it(
    'Scenario 2: Given Live is on and events are received, When the SSE connection drops, ' +
      'Then status indicator shows "reconnecting" AND prior events stay in the list',
    () => {
      renderPage(`/objectsets/${ONTOLOGY}/live?rid=ri.set.scn2`);
      fireEvent.click(screen.getByTestId('objectset-live-toggle'));

      const es = MockEventSource.instances[0];
      act(() => {
        es.simulateOpen();
        es.simulateMessage(
          JSON.stringify({
            seq: 1,
            type: 'created',
            rid: 'Order:keep-me',
            properties: { amount: 1 },
          }),
          '1',
        );
      });
      expect(screen.getByTestId('objectset-live-event-1')).toBeInTheDocument();

      // When the connection drops.
      act(() => {
        es.simulateError();
      });

      // Then status indicator surfaces "reconnecting".
      expect(screen.getByTestId('objectset-live-status')).toHaveAttribute(
        'data-state',
        'reconnecting',
      );

      // AND the previously received event remains in the list (regression
      // guard — wiping the event log on disconnect would erase forensic
      // context the user needs while debugging).
      expect(screen.getByTestId('objectset-live-event-1')).toBeInTheDocument();
      expect(
        screen.getByTestId('objectset-live-event-count').textContent,
      ).toMatch(/1/);
    },
  );

  it('Negative control: malformed event payload does NOT increment count or crash page', () => {
    renderPage(`/objectsets/${ONTOLOGY}/live?rid=ri.set.bad`);
    fireEvent.click(screen.getByTestId('objectset-live-toggle'));

    const es = MockEventSource.instances[0];
    act(() => {
      es.simulateOpen();
      es.simulateMessage('not-valid-json{', '1');
    });

    // Counter stays at zero.
    expect(
      screen.getByTestId('objectset-live-event-count').textContent,
    ).toMatch(/0/);
    // Empty placeholder still visible (proves the row container did not
    // accidentally render a row for the malformed payload).
    expect(
      screen.getByTestId('objectset-live-events-empty'),
    ).toBeInTheDocument();
  });
});
