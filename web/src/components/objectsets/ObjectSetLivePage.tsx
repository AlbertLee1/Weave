import { useCallback, useMemo, useState } from 'react';
import { Link, useParams, useSearchParams } from 'react-router';
import { useSavedObjectSets, useCreateTemporaryObjectSet } from '../../hooks/useObjectSets';
import {
  useObjectSetSubscription,
  type ObjectSetEvent,
  type ObjectSetSubscriptionStatus,
} from '../../hooks/useObjectSetSubscription';
import { EmptyState } from '../common/EmptyState';

// US-501: ObjectSet 实时订阅页（Live toggle）
//
// The page is intentionally lightweight: pick or paste an ObjectSet rid,
// toggle Live to subscribe via SSE, watch push events stream into the
// list. Status indicator surfaces connection state so a dropped link is
// visible at-a-glance.
//
// Wire format follows pkg/oss/subscribe_sse.go::sseEventPayload — every
// frame carries both the legacy {eventType, object} view and the US-459
// canonical {seq, type, rid, properties} view. The list renders the
// canonical view because PRD US-501 inherits from US-459's contract.

const MAX_EVENTS = 200;

interface LiveEventRow {
  /** seq is the stable key; missing on hand-rolled payloads so we
   * fall back to the local capture index for keying. */
  seq?: number;
  /** Local monotonically-increasing capture id. Used as React key when
   * seq is missing or zero. */
  captureId: number;
  type: string;
  rid: string;
  receivedAt: number;
  properties?: Record<string, unknown>;
}

function statusLabel(s: ObjectSetSubscriptionStatus): string {
  switch (s) {
    case 'idle':
      return 'Idle';
    case 'connecting':
      return 'Connecting…';
    case 'connected':
      return 'Connected';
    case 'reconnecting':
      return 'Reconnecting…';
  }
}

function statusColor(s: ObjectSetSubscriptionStatus): string {
  switch (s) {
    case 'idle':
      return 'bg-text-muted';
    case 'connecting':
      return 'bg-accent-cyan animate-pulse';
    case 'connected':
      return 'bg-accent-green';
    case 'reconnecting':
      return 'bg-accent-error animate-pulse';
  }
}

function typeBadgeClass(t: string): string {
  switch (t) {
    case 'created':
      return 'bg-accent-green/20 text-accent-green';
    case 'modified':
      return 'bg-accent-cyan/20 text-accent-cyan';
    case 'deleted':
      return 'bg-accent-error/20 text-accent-error';
    default:
      return 'bg-bg-elevated text-text-secondary';
  }
}

export function ObjectSetLivePage() {
  const { ontology } = useParams<{ ontology: string }>();
  const ontologyApiName = ontology ?? '';
  const [searchParams] = useSearchParams();

  const initialRid = searchParams.get('rid') ?? '';
  const [rid, setRid] = useState<string>(initialRid);
  const [live, setLive] = useState<boolean>(false);
  const [status, setStatus] = useState<ObjectSetSubscriptionStatus>('idle');
  const [events, setEvents] = useState<LiveEventRow[]>([]);
  // Local counter so events without seq still get a stable React key.
  const [, setCaptureCounter] = useState(0);

  const { items: savedSets } = useSavedObjectSets(ontologyApiName);
  const createTempMut = useCreateTemporaryObjectSet(ontologyApiName);
  const [pickerError, setPickerError] = useState<string | null>(null);

  const handleEvent = useCallback((evt: ObjectSetEvent) => {
    setCaptureCounter((c) => {
      const next = c + 1;
      const row: LiveEventRow = {
        seq: typeof evt.seq === 'number' ? evt.seq : undefined,
        captureId: next,
        type: evt.type ?? (evt.eventType === 'DELETED' ? 'deleted' : 'modified'),
        rid: evt.rid ?? '',
        receivedAt: Date.now(),
        properties: evt.properties,
      };
      setEvents((prev) => {
        if (
          row.seq !== undefined &&
          prev.some((existing) => existing.seq === row.seq)
        ) {
          return prev;
        }
        const updated = [row, ...prev];
        return updated.length > MAX_EVENTS ? updated.slice(0, MAX_EVENTS) : updated;
      });
      return next;
    });
  }, []);

  const handleStatusChange = useCallback((s: ObjectSetSubscriptionStatus) => {
    setStatus(s);
  }, []);

  useObjectSetSubscription(ontologyApiName, rid, {
    enabled: live && !!rid,
    onEvent: handleEvent,
    onStatusChange: handleStatusChange,
  });

  const handleToggleLive = useCallback(() => {
    if (live) setStatus('idle');
    setLive((v) => !v);
  }, [live]);

  const sortedSaved = useMemo(
    () => savedSets.slice().sort((a, b) => a.name.localeCompare(b.name)),
    [savedSets],
  );

  const handlePickSaved = useCallback(
    async (savedId: string) => {
      setPickerError(null);
      if (!savedId) return;
      const target = savedSets.find((s) => s.id === savedId);
      if (!target) return;
      try {
        const resp = await createTempMut.mutateAsync(target.def);
        setRid(resp.objectSetRid);
      } catch (err) {
        setPickerError(
          err instanceof Error ? err.message : 'Failed to create temporary set',
        );
      }
    },
    [savedSets, createTempMut],
  );

  const handleClear = useCallback(() => {
    setEvents([]);
    setCaptureCounter(0);
  }, []);

  if (!ontologyApiName) {
    return (
      <div className="flex items-center justify-center h-full">
        <EmptyState
          title="No ontology selected"
          description="Pick an ontology from the dashboard to subscribe to ObjectSet events."
        />
      </div>
    );
  }

  const canToggle = rid.trim().length > 0;

  return (
    <div
      className="flex flex-col h-full overflow-hidden"
      data-testid="objectset-live-page"
    >
      <div className="flex items-center justify-between px-4 py-3 border-b border-border bg-bg-primary">
        <div>
          <h1 className="text-base font-sans font-semibold text-text-primary">
            Object Set Live
          </h1>
          <p className="text-xs font-mono text-text-secondary mt-0.5">
            {ontologyApiName}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Link
            to={`/objectsets/${ontologyApiName}`}
            className="bg-bg-tertiary border border-border text-text-primary px-3 py-1.5 rounded text-xs font-mono hover:bg-bg-elevated"
          >
            Composer
          </Link>
          <Link
            to={`/objectsets/${ontologyApiName}/saved`}
            className="bg-bg-tertiary border border-border text-text-primary px-3 py-1.5 rounded text-xs font-mono hover:bg-bg-elevated"
          >
            Saved
          </Link>
        </div>
      </div>

      <div className="px-4 py-3 border-b border-border bg-bg-secondary flex flex-col gap-3">
        <div className="flex flex-wrap items-end gap-3">
          <div className="flex flex-col gap-1">
            <label
              htmlFor="objectset-live-saved"
              className="text-xs font-sans text-text-secondary"
            >
              Saved Object Set
            </label>
            <select
              id="objectset-live-saved"
              data-testid="objectset-live-saved-picker"
              className="bg-bg-tertiary border border-border rounded px-2 py-1.5 text-xs font-mono text-text-primary min-w-[200px]"
              onChange={(e) => handlePickSaved(e.target.value)}
              defaultValue=""
              disabled={live}
            >
              <option value="">— pick a saved set —</option>
              {sortedSaved.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
            </select>
          </div>
          <div className="flex flex-col gap-1 flex-1 min-w-[280px]">
            <label
              htmlFor="objectset-live-rid"
              className="text-xs font-sans text-text-secondary"
            >
              ObjectSet rid
            </label>
            <input
              id="objectset-live-rid"
              data-testid="objectset-live-rid-input"
              type="text"
              value={rid}
              onChange={(e) => setRid(e.target.value)}
              placeholder="ri.objectset.weave.main.tempObjectSet.…"
              className="bg-bg-tertiary border border-border rounded px-2 py-1.5 text-xs font-mono text-text-primary"
              disabled={live}
            />
          </div>
          <button
            type="button"
            data-testid="objectset-live-toggle"
            disabled={!canToggle}
            onClick={handleToggleLive}
            className={
              live
                ? 'bg-accent-error text-bg-primary px-4 py-1.5 rounded text-xs font-mono font-medium'
                : 'bg-accent-cyan text-bg-primary px-4 py-1.5 rounded text-xs font-mono font-medium disabled:opacity-40'
            }
          >
            {live ? 'Stop' : 'Go Live'}
          </button>
          <button
            type="button"
            data-testid="objectset-live-clear"
            onClick={handleClear}
            disabled={events.length === 0}
            className="bg-bg-tertiary border border-border text-text-secondary px-3 py-1.5 rounded text-xs font-mono hover:text-text-primary disabled:opacity-40"
          >
            Clear
          </button>
        </div>

        <div className="flex items-center gap-3" data-testid="objectset-live-status-bar">
          <div
            data-testid="objectset-live-status"
            data-state={status}
            className="flex items-center gap-2"
          >
            <span
              className={`inline-block w-2 h-2 rounded-full ${statusColor(status)}`}
              aria-hidden
            />
            <span className="text-xs font-mono text-text-secondary">
              {statusLabel(status)}
            </span>
          </div>
          <span
            data-testid="objectset-live-event-count"
            className="text-xs font-mono text-text-secondary"
          >
            {events.length} event{events.length === 1 ? '' : 's'}
          </span>
          {pickerError && (
            <span
              className="text-xs font-mono text-accent-error"
              data-testid="objectset-live-picker-error"
            >
              {pickerError}
            </span>
          )}
        </div>
      </div>

      <div className="flex-1 overflow-y-auto p-4">
        {events.length === 0 ? (
          <div data-testid="objectset-live-events-empty">
            <EmptyState
              title="No events yet"
              description={
                live
                  ? 'Waiting for the server to push the first event…'
                  : 'Pick an ObjectSet rid above and click Go Live to start streaming.'
              }
            />
          </div>
        ) : (
          <ul
            className="flex flex-col gap-2"
            data-testid="objectset-live-events"
          >
            {events.map((row) => {
              const key = row.seq !== undefined ? `seq-${row.seq}` : `cap-${row.captureId}`;
              const testId =
                row.seq !== undefined
                  ? `objectset-live-event-${row.seq}`
                  : `objectset-live-event-cap-${row.captureId}`;
              return (
                <li
                  key={key}
                  data-testid={testId}
                  className="border border-border bg-bg-tertiary rounded px-3 py-2 flex items-start gap-3"
                >
                  <span
                    className={`text-xs font-mono px-2 py-0.5 rounded uppercase ${typeBadgeClass(row.type)}`}
                  >
                    {row.type}
                  </span>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="text-xs font-mono text-text-primary truncate">
                        {row.rid || '(no rid)'}
                      </span>
                      {row.seq !== undefined && (
                        <span className="text-xs font-mono text-text-muted">
                          seq #{row.seq}
                        </span>
                      )}
                    </div>
                    {row.properties && Object.keys(row.properties).length > 0 && (
                      <pre className="mt-1 text-xs font-mono text-text-secondary whitespace-pre-wrap break-words">
                        {JSON.stringify(row.properties, null, 0)}
                      </pre>
                    )}
                  </div>
                  <span className="text-xs font-mono text-text-muted">
                    {new Date(row.receivedAt).toLocaleTimeString()}
                  </span>
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </div>
  );
}
