// ScenarioCopilotButtons — Suggest Override / Explain Result (VTX-113).
//
// Suggest Override POSTs to /api/vertex/v1/scenarios/{rid}/copilot/suggest-overrides
// and shows the LLM's recommended override range for the focus object.
// Explain Result POSTs to .../copilot/explain-result and shows a
// natural-language read-out of the baseline vs simulated diff.
//
// The handlers themselves (and the Anthropic API hop) live in another
// stream (VTX-046 + VTX-056). This file is the view + fetcher pair so
// the unit suite can validate the wire contract.

import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import '../i18n';

export interface OverrideSuggestion {
  parameter: string;
  recommendedRange: [number, number];
  rationale: string;
}

export interface ExplainResult {
  summary: string;
  bullets: string[];
}

export interface ScenarioCopilotProps {
  scenarioRid: string;
  /** Inject for tests; defaults to fetch-backed POST. */
  suggester?: (rid: string) => Promise<OverrideSuggestion[]>;
  explainer?: (rid: string) => Promise<ExplainResult>;
  /** When false, Explain Result is disabled (scenario not yet run). */
  hasResult: boolean;
}

async function defaultSuggester(rid: string): Promise<OverrideSuggestion[]> {
  const res = await fetch(
    `/api/vertex/v1/scenarios/${encodeURIComponent(rid)}/copilot/suggest-overrides`,
    { method: 'POST', headers: { 'content-type': 'application/json' }, body: '{}' },
  );
  if (!res.ok) throw new Error(`suggest-overrides: ${res.status}`);
  return (await res.json()) as OverrideSuggestion[];
}

async function defaultExplainer(rid: string): Promise<ExplainResult> {
  const res = await fetch(
    `/api/vertex/v1/scenarios/${encodeURIComponent(rid)}/copilot/explain-result`,
    { method: 'POST', headers: { 'content-type': 'application/json' }, body: '{}' },
  );
  if (!res.ok) throw new Error(`explain-result: ${res.status}`);
  return (await res.json()) as ExplainResult;
}

export function ScenarioCopilotButtons({
  scenarioRid,
  hasResult,
  suggester = defaultSuggester,
  explainer = defaultExplainer,
}: ScenarioCopilotProps) {
  const { t } = useTranslation();
  const [suggestions, setSuggestions] = useState<OverrideSuggestion[] | null>(null);
  const [explanation, setExplanation] = useState<ExplainResult | null>(null);
  const [loading, setLoading] = useState<'suggest' | 'explain' | null>(null);
  const [error, setError] = useState<string | null>(null);

  async function onSuggest() {
    setLoading('suggest');
    setError(null);
    try {
      setSuggestions(await suggester(scenarioRid));
    } catch (e) {
      setError(String(e));
    } finally {
      setLoading(null);
    }
  }

  async function onExplain() {
    setLoading('explain');
    setError(null);
    try {
      setExplanation(await explainer(scenarioRid));
    } catch (e) {
      setError(String(e));
    } finally {
      setLoading(null);
    }
  }

  return (
    <div data-testid="scenario-copilot" className="space-y-3">
      <div className="flex gap-2">
        <button
          type="button"
          data-testid="copilot-suggest"
          onClick={onSuggest}
          disabled={loading !== null}
          className="rounded bg-violet-600 px-3 py-1 text-xs text-white disabled:opacity-40"
        >
          {loading === 'suggest' ? t('vertex.copilot.thinking') : t('vertex.copilot.suggest')}
        </button>
        <button
          type="button"
          data-testid="copilot-explain"
          onClick={onExplain}
          disabled={!hasResult || loading !== null}
          className="rounded bg-violet-600 px-3 py-1 text-xs text-white disabled:opacity-40"
        >
          {loading === 'explain' ? t('vertex.copilot.reading') : t('vertex.copilot.explain')}
        </button>
      </div>
      {error && (
        <div data-testid="copilot-error" className="text-xs text-red-600">
          {error}
        </div>
      )}
      {suggestions && (
        <ul data-testid="copilot-suggestions" className="text-xs">
          {suggestions.map((s) => (
            <li key={s.parameter}>
              <span className="font-mono">{s.parameter}</span>: {s.recommendedRange[0]}…{s.recommendedRange[1]} — {s.rationale}
            </li>
          ))}
        </ul>
      )}
      {explanation && (
        <div data-testid="copilot-explanation" className="text-xs">
          <p>{explanation.summary}</p>
          <ul className="list-disc pl-5">
            {explanation.bullets.map((b, i) => (
              <li key={i}>{b}</li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}
