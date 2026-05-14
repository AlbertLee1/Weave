// VertexGraphWidget — Workshop-embeddable Vertex view (VTX-105).
//
// Workshop renders Vertex inside its module-config widget slot with a
// compact toolbar and no Scenario Pane. The widget is parameterised by
// graphRid and an optional selectedNodeRid; on save it PATCHes the
// current graph state to overrideGraphRid so the host module owns the
// modified copy.
//
// The full Workshop module config schema is owned by VTX-014; this file
// is the standalone widget component + its save hook, plus a unit suite
// that exercises both branches.

import { useEffect, useState } from 'react';

export interface WidgetGraphState {
  selectedNodeRid?: string;
  cameraZoom?: number;
}

export interface VertexGraphWidgetProps {
  graphRid: string;
  selectedNodeRid?: string;
  overrideGraphRid?: string;
  /** Called when the host module's Save action fires. */
  onSave?: () => void;
  /** Read current graph state (injected for tests / Workshop). */
  loader?: (rid: string) => Promise<WidgetGraphState>;
  /** Write graph state to the override RID (injected for tests / Workshop). */
  saver?: (overrideRid: string, state: WidgetGraphState) => Promise<void>;
}

async function defaultLoader(rid: string): Promise<WidgetGraphState> {
  const res = await fetch(`/api/vertex/v1/graphs/${encodeURIComponent(rid)}`);
  if (!res.ok) throw new Error(`graph load failed: ${res.status}`);
  return (await res.json()) as WidgetGraphState;
}

async function defaultSaver(overrideRid: string, state: WidgetGraphState): Promise<void> {
  const res = await fetch(`/api/vertex/v1/graphs/${encodeURIComponent(overrideRid)}`, {
    method: 'PATCH',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(state),
  });
  if (!res.ok) throw new Error(`graph save failed: ${res.status}`);
}

export function VertexGraphWidget({
  graphRid,
  selectedNodeRid,
  overrideGraphRid,
  onSave,
  loader = defaultLoader,
  saver = defaultSaver,
}: VertexGraphWidgetProps) {
  const [state, setState] = useState<WidgetGraphState | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    let cancelled = false;
    loader(graphRid)
      .then((s) => {
        if (cancelled) return;
        setState({ ...s, selectedNodeRid: selectedNodeRid ?? s.selectedNodeRid });
        setError(null);
      })
      .catch((e) => {
        if (!cancelled) setError(String(e));
      });
    return () => {
      cancelled = true;
    };
  }, [graphRid, selectedNodeRid, loader]);

  async function handleSave() {
    if (!overrideGraphRid || !state) return;
    setSaving(true);
    setSaved(false);
    try {
      await saver(overrideGraphRid, state);
      setSaved(true);
      onSave?.();
    } catch (e) {
      setError(String(e));
    } finally {
      setSaving(false);
    }
  }

  return (
    <div data-testid="vertex-graph-widget" className="flex h-full flex-col">
      <header
        data-testid="vertex-widget-toolbar"
        className="flex items-center justify-between border-b px-2 py-1 text-xs"
      >
        <span className="font-mono">{graphRid}</span>
        <button
          type="button"
          data-testid="vertex-widget-save"
          onClick={handleSave}
          disabled={!overrideGraphRid || saving || !state}
          className="rounded bg-blue-600 px-2 py-1 text-white disabled:opacity-50"
        >
          {saving ? 'Saving…' : 'Save'}
        </button>
      </header>
      <main className="flex flex-1 items-center justify-center" data-testid="vertex-widget-canvas">
        {error && (
          <span data-testid="vertex-widget-error" className="text-red-600">
            {error}
          </span>
        )}
        {!error && state && (
          <span>
            graph {graphRid}
            {state.selectedNodeRid ? ` · selected ${state.selectedNodeRid}` : ''}
          </span>
        )}
        {!error && !state && <span data-testid="vertex-widget-loading">Loading…</span>}
      </main>
      {saved && <div data-testid="vertex-widget-saved" className="px-2 py-1 text-xs text-green-600">Saved</div>}
    </div>
  );
}
