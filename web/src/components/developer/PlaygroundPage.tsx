import { useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import {
  extractEndpoints,
  groupByTag,
  parseOpenApiYaml,
  buildRequestUrl,
  exampleForSchema,
  type Endpoint,
  type OpenApiSpec,
} from './openapiParser';
import { buildSnippets } from './snippets';
import {
  clearHistory,
  loadHistory,
  pushHistory,
  type HistoryEntry,
} from './history';
import { useAuthStore } from '../../auth/authStore';
import { LoadingSpinner } from '../common/LoadingSpinner';

const METHOD_COLORS: Record<string, string> = {
  GET: '#14B8A6',
  POST: '#F59E0B',
  PUT: '#3B82F6',
  PATCH: '#8B5CF6',
  DELETE: '#EF4444',
};

type SnippetTab = 'curl' | 'typescript' | 'python' | 'go';

interface ResponseState {
  status: number;
  durationMs: number;
  body: string;
  contentType: string;
}

async function fetchSpec(): Promise<OpenApiSpec> {
  const res = await fetch('/api/openapi.yaml');
  if (!res.ok) throw new Error(`spec fetch failed: ${res.status}`);
  const text = await res.text();
  return parseOpenApiYaml(text);
}

export function PlaygroundPage() {
  const { data: spec, isLoading, error } = useQuery({
    queryKey: ['playground-spec'],
    queryFn: fetchSpec,
    staleTime: 5 * 60_000,
  });

  const endpoints = useMemo(() => (spec ? extractEndpoints(spec) : []), [spec]);
  const groups = useMemo(() => groupByTag(endpoints), [endpoints]);

  const [filter, setFilter] = useState('');
  const [selected, setSelected] = useState<Endpoint | null>(null);
  const effectiveSelected = selected ?? endpoints[0] ?? null;

  return (
    <div className="flex h-[calc(100vh-3rem)] bg-bg-primary">
      <EndpointList
        groups={groups}
        filter={filter}
        onFilterChange={setFilter}
        selected={effectiveSelected}
        onSelect={setSelected}
        isLoading={isLoading}
        error={error as Error | null}
      />
      <div className="flex-1 overflow-y-auto">
        {effectiveSelected && spec ? (
          <EndpointRunner key={`${effectiveSelected.method} ${effectiveSelected.path}`} endpoint={effectiveSelected} spec={spec} />
        ) : (
          <div className="flex items-center justify-center h-full text-text-secondary text-sm">
            {isLoading ? <LoadingSpinner size="md" /> : 'Select an endpoint on the left.'}
          </div>
        )}
      </div>
    </div>
  );
}

function matchesFilter(ep: Endpoint, q: string): boolean {
  if (!q) return true;
  const lower = q.toLowerCase();
  return (
    ep.path.toLowerCase().includes(lower) ||
    ep.operationId.toLowerCase().includes(lower) ||
    ep.summary.toLowerCase().includes(lower) ||
    ep.method.toLowerCase().includes(lower)
  );
}

interface EndpointListProps {
  groups: Record<string, Endpoint[]>;
  filter: string;
  onFilterChange: (v: string) => void;
  selected: Endpoint | null;
  onSelect: (ep: Endpoint) => void;
  isLoading: boolean;
  error: Error | null;
}

function EndpointList(props: EndpointListProps) {
  const { groups, filter, onFilterChange, selected, onSelect, isLoading, error } =
    props;
  const filteredGroups = Object.entries(groups)
    .map(
      ([tag, eps]) =>
        [tag, eps.filter((e) => matchesFilter(e, filter))] as const,
    )
    .filter(([, eps]) => eps.length > 0)
    .sort((a, b) => a[0].localeCompare(b[0]));

  return (
    <aside
      className="flex flex-col w-80 border-r overflow-hidden"
      style={{ borderColor: 'rgba(31,41,55,0.5)', background: 'rgba(13,17,23,0.6)' }}
    >
      <div className="p-3 border-b" style={{ borderColor: 'rgba(31,41,55,0.5)' }}>
        <h2
          className="text-xs font-semibold uppercase tracking-widest text-text-secondary mb-2"
          style={{ fontFamily: 'var(--font-sans)' }}
        >
          API Playground
        </h2>
        <input
          type="text"
          value={filter}
          onChange={(e) => onFilterChange(e.target.value)}
          placeholder="Search endpoints..."
          className="w-full px-3 py-1.5 text-sm rounded bg-bg-tertiary text-text-primary outline-none border border-transparent focus:border-accent-cyan/40"
        />
      </div>
      <div className="flex-1 overflow-y-auto py-2">
        {isLoading && (
          <div className="flex items-center justify-center py-10">
            <LoadingSpinner size="sm" />
          </div>
        )}
        {error && (
          <p className="px-3 py-2 text-xs text-accent-error">
            Failed to load OpenAPI spec: {error.message}
          </p>
        )}
        {!isLoading && !error && filteredGroups.length === 0 && (
          <p className="px-3 py-2 text-xs text-text-secondary">No matching endpoints.</p>
        )}
        {filteredGroups.map(([tag, eps]) => (
          <div key={tag} className="mb-2">
            <h3 className="px-3 py-1 text-[10px] uppercase tracking-widest text-text-secondary">
              {tag}
            </h3>
            {eps.map((ep) => {
              const isSel =
                selected?.method === ep.method && selected?.path === ep.path;
              return (
                <button
                  key={`${ep.method} ${ep.path}`}
                  onClick={() => onSelect(ep)}
                  className={`w-full text-left px-3 py-1.5 flex flex-col gap-0.5 text-xs transition-colors ${
                    isSel
                      ? 'bg-bg-tertiary text-text-primary'
                      : 'text-text-secondary hover:bg-bg-tertiary/60 hover:text-text-primary'
                  }`}
                  title={ep.summary || ep.operationId}
                >
                  <span className="flex items-center gap-2">
                    <MethodPill method={ep.method} />
                    <span className="truncate">{ep.operationId}</span>
                  </span>
                  <span
                    className="truncate pl-[3.3rem] font-mono text-[10px] text-text-muted"
                    style={{ fontFamily: 'var(--font-mono)' }}
                  >
                    {ep.path}
                  </span>
                </button>
              );
            })}
          </div>
        ))}
      </div>
    </aside>
  );
}

function MethodPill({ method }: { method: string }) {
  const color = METHOD_COLORS[method] ?? '#6B7280';
  return (
    <span
      className="inline-block px-1.5 py-0.5 rounded text-[9px] font-semibold uppercase tracking-wider"
      style={{
        background: `${color}22`,
        color,
        border: `1px solid ${color}44`,
        minWidth: '3rem',
        textAlign: 'center',
      }}
    >
      {method}
    </span>
  );
}

interface EndpointRunnerProps {
  endpoint: Endpoint;
  spec: OpenApiSpec;
}

function EndpointRunner({ endpoint, spec }: EndpointRunnerProps) {
  const pathParams = endpoint.parameters.filter((p) => p.in === 'path');
  const queryParams = endpoint.parameters.filter((p) => p.in === 'query');
  const headerParams = endpoint.parameters.filter((p) => p.in === 'header');
  const bodySchema =
    endpoint.requestBody?.content?.['application/json']?.schema;

  const [pathValues, setPathValues] = useState<Record<string, string>>({});
  const [queryValues, setQueryValues] = useState<Record<string, string>>({});
  const [headerValues, setHeaderValues] = useState<Record<string, string>>({});
  const [body, setBody] = useState<string>(() => {
    if (!bodySchema) return '';
    try {
      return JSON.stringify(exampleForSchema(bodySchema, spec), null, 2);
    } catch {
      return '';
    }
  });
  const [response, setResponse] = useState<ResponseState | null>(null);
  const [sending, setSending] = useState(false);
  const [history, setHistory] = useState<HistoryEntry[]>(() => loadHistory());
  const [snippetTab, setSnippetTab] = useState<SnippetTab>('curl');

  useEffect(() => {
    setPathValues({});
    setQueryValues({});
    setHeaderValues({});
    setResponse(null);
    if (bodySchema) {
      try {
        setBody(JSON.stringify(exampleForSchema(bodySchema, spec), null, 2));
      } catch {
        setBody('');
      }
    } else {
      setBody('');
    }
  }, [endpoint, bodySchema, spec]);

  const url = buildRequestUrl(endpoint.path, pathValues, queryValues);
  const token = useAuthStore((s) => s.accessToken) ?? '';
  const snippets = useMemo(
    () => buildSnippets({ method: endpoint.method, url, body, token }),
    [endpoint.method, url, body, token],
  );

  async function onSend() {
    setSending(true);
    const started = performance.now();
    try {
      const headers = new Headers();
      if (token) headers.set('Authorization', `Bearer ${token}`);
      const hasBody = body.trim().length > 0 && endpoint.method !== 'GET';
      if (hasBody) headers.set('Content-Type', 'application/json');
      for (const [k, v] of Object.entries(headerValues)) {
        if (v) headers.set(k, v);
      }
      const res = await fetch(url, {
        method: endpoint.method,
        headers,
        body: hasBody ? body : undefined,
      });
      const text = await res.text();
      const durationMs = Math.round(performance.now() - started);
      const state: ResponseState = {
        status: res.status,
        durationMs,
        body: text,
        contentType: res.headers.get('Content-Type') ?? '',
      };
      setResponse(state);
      setHistory(
        pushHistory({
          id: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
          method: endpoint.method,
          url,
          status: res.status,
          durationMs,
          timestamp: Date.now(),
          operationId: endpoint.operationId,
        }),
      );
    } catch (err) {
      setResponse({
        status: 0,
        durationMs: Math.round(performance.now() - started),
        body: err instanceof Error ? err.message : String(err),
        contentType: 'text/plain',
      });
    } finally {
      setSending(false);
    }
  }

  return (
    <div className="p-5 max-w-5xl mx-auto">
      <div className="mb-4">
        <div className="flex items-center gap-3 mb-2">
          <MethodPill method={endpoint.method} />
          <code
            className="text-sm text-text-primary"
            style={{ fontFamily: 'var(--font-mono)' }}
          >
            {endpoint.path}
          </code>
        </div>
        <h1 className="text-base font-semibold text-text-primary mb-1">
          {endpoint.summary || endpoint.operationId}
        </h1>
        {endpoint.description && (
          <p className="text-xs text-text-secondary whitespace-pre-line">
            {endpoint.description}
          </p>
        )}
      </div>

      <ParamSection title="Path Parameters" params={pathParams} values={pathValues} onChange={setPathValues} />
      <ParamSection title="Query Parameters" params={queryParams} values={queryValues} onChange={setQueryValues} />
      <ParamSection title="Header Parameters" params={headerParams} values={headerValues} onChange={setHeaderValues} />

      {bodySchema && (
        <Section title="Request Body (application/json)">
          <textarea
            value={body}
            onChange={(e) => setBody(e.target.value)}
            spellCheck={false}
            rows={10}
            className="w-full p-3 rounded bg-bg-tertiary text-text-primary text-xs font-mono outline-none border border-transparent focus:border-accent-cyan/40"
            style={{ fontFamily: 'var(--font-mono)' }}
          />
        </Section>
      )}

      <div className="flex items-center gap-3 mb-5">
        <button
          onClick={onSend}
          disabled={sending}
          className="px-4 py-1.5 rounded text-sm font-semibold text-bg-primary disabled:opacity-60"
          style={{
            background: 'linear-gradient(135deg, #F59E0B 0%, #14B8A6 100%)',
          }}
        >
          {sending ? 'Sending…' : 'Send'}
        </button>
        <span className="text-xs font-mono text-text-secondary truncate" style={{ fontFamily: 'var(--font-mono)' }}>
          {endpoint.method} {url}
        </span>
      </div>

      {response && (
        <Section
          title={`Response (${response.status || 'error'} — ${response.durationMs}ms)`}
        >
          <pre
            className="p-3 rounded bg-bg-tertiary text-xs text-text-primary overflow-x-auto"
            style={{ fontFamily: 'var(--font-mono)' }}
          >
            {prettyPrint(response.body, response.contentType)}
          </pre>
        </Section>
      )}

      <Section title="Code Snippets">
        <div className="flex gap-1 mb-2">
          {(['curl', 'typescript', 'python', 'go'] as const).map((tab) => (
            <button
              key={tab}
              onClick={() => setSnippetTab(tab)}
              className={`px-3 py-1 rounded text-xs ${
                snippetTab === tab
                  ? 'bg-bg-tertiary text-text-primary'
                  : 'text-text-secondary hover:bg-bg-tertiary/60'
              }`}
            >
              {tab}
            </button>
          ))}
        </div>
        <pre
          className="p-3 rounded bg-bg-tertiary text-xs text-text-primary overflow-x-auto whitespace-pre"
          style={{ fontFamily: 'var(--font-mono)' }}
        >
          {snippets[snippetTab]}
        </pre>
      </Section>

      {history.length > 0 && (
        <Section title={`History (${history.length})`}>
          <div className="flex justify-end mb-2">
            <button
              onClick={() => {
                clearHistory();
                setHistory([]);
              }}
              className="text-xs text-text-secondary hover:text-text-primary"
            >
              Clear
            </button>
          </div>
          <ul className="text-xs space-y-1 max-h-64 overflow-y-auto">
            {history.map((h) => (
              <li
                key={h.id}
                className="flex items-center gap-2 px-2 py-1 rounded hover:bg-bg-tertiary/60"
              >
                <MethodPill method={h.method} />
                <span
                  className={`w-10 text-right font-mono ${
                    h.status >= 400 ? 'text-accent-error' : 'text-accent-success'
                  }`}
                  style={{ fontFamily: 'var(--font-mono)' }}
                >
                  {h.status || 'err'}
                </span>
                <span className="font-mono truncate flex-1" style={{ fontFamily: 'var(--font-mono)' }}>
                  {h.url}
                </span>
                <span className="text-text-secondary">{h.durationMs}ms</span>
              </li>
            ))}
          </ul>
        </Section>
      )}
    </div>
  );
}

function prettyPrint(text: string, contentType: string): string {
  if (contentType.includes('json')) {
    try {
      return JSON.stringify(JSON.parse(text), null, 2);
    } catch {
      return text;
    }
  }
  return text;
}

interface ParamSectionProps {
  title: string;
  params: { name: string; required?: boolean; description?: string }[];
  values: Record<string, string>;
  onChange: (v: Record<string, string>) => void;
}

function ParamSection({ title, params, values, onChange }: ParamSectionProps) {
  if (params.length === 0) return null;
  return (
    <Section title={title}>
      <div className="space-y-2">
        {params.map((p) => (
          <div key={p.name} className="flex items-center gap-3">
            <label
              className="w-48 text-xs text-text-secondary font-mono"
              style={{ fontFamily: 'var(--font-mono)' }}
              title={p.description ?? ''}
            >
              {p.name}
              {p.required && <span className="text-accent-error"> *</span>}
            </label>
            <input
              type="text"
              value={values[p.name] ?? ''}
              onChange={(e) => onChange({ ...values, [p.name]: e.target.value })}
              className="flex-1 px-3 py-1 text-xs rounded bg-bg-tertiary text-text-primary outline-none border border-transparent focus:border-accent-cyan/40"
              placeholder={p.description}
            />
          </div>
        ))}
      </div>
    </Section>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="mb-4">
      <h3 className="text-[11px] uppercase tracking-widest text-text-secondary mb-2">
        {title}
      </h3>
      {children}
    </div>
  );
}
