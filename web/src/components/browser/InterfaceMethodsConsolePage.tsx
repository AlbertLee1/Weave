import { useMemo, useState, type FormEvent } from 'react';
import { useNavigate, useParams } from 'react-router';
import { useObjectType } from '../../hooks/useObjectTypes';
import {
  useInterfacesAdmin,
  useObjectTypeInterfaces,
} from '../../hooks/useInterfaces';
import {
  useInterfaceMethods,
  useInvokeInterfaceMethod,
} from '../../hooks/useInterfaceMethods';
import type {
  InterfaceMethod,
  InterfaceMethodParam,
  InvokeInterfaceMethodResponse,
} from '../../api/interfaceMethods';
import type { OntologyInterface } from '../../api/types';
import { ApiRequestError } from '../../api/client';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { EmptyState } from '../common/EmptyState';

// US-047 (PC-A04): Interface Methods Console.
//
// Route: /methods/:ontology/:objectType/:primaryKey
//
// Honest mapping notes:
//   - The PRD AC speaks of an "Interface Methods 抽屉" inside the
//     Browser detail panel; the entry point lives there
//     (ObjectDetail.tsx surfaces a "Interface Methods" button that
//     navigates here), and the console itself is a focused page so the
//     param form + result panel have room to render without crowding
//     the slide-in detail panel.
//   - Backend wire path is `/interfaces/methods/{methodRid}/invoke` (not
//     `/execute` as the AC phrased it). See pkg/oms/admin_handlers_interface_method.go.
//   - "返回值/错误/审计日志展示" maps to: (a) the Result panel renders
//     the response payload as JSON, (b) errors raise an inline banner
//     plus per-form negative state, (c) a deep link to
//     `/actions/{ontology}/history?actionType=<resolved>` so operators
//     can audit the underlying ActionType run that dispatched the
//     method call.

export function InterfaceMethodsConsolePage() {
  const params = useParams<{
    ontology: string;
    objectType: string;
    primaryKey: string;
  }>();
  const ontologyApiName = params.ontology ?? '';
  const objectTypeApiName = params.objectType ?? '';
  const primaryKey = params.primaryKey ?? '';

  const objectTypeQuery = useObjectType(ontologyApiName, objectTypeApiName);
  const objectTypeRid = objectTypeQuery.data?.rid ?? '';

  const attachmentsQuery = useObjectTypeInterfaces(
    ontologyApiName,
    objectTypeRid || undefined,
  );
  const interfacesQuery = useInterfacesAdmin(ontologyApiName);

  // Build a lookup so each attachment can resolve its interface displayName
  // + apiName cleanly. Falls back to the bare rid suffix when the admin
  // list has not yet loaded — keeps the layout stable across query stages.
  const interfacesByRid = useMemo(() => {
    const out = new Map<string, OntologyInterface>();
    for (const iface of interfacesQuery.data ?? []) {
      out.set(iface.rid, iface);
    }
    return out;
  }, [interfacesQuery.data]);

  const attachedInterfaces = useMemo(() => {
    const ridSet = new Set<string>();
    for (const att of attachmentsQuery.data ?? []) {
      ridSet.add(att.interfaceRid);
    }
    const out: OntologyInterface[] = [];
    for (const rid of ridSet) {
      const iface = interfacesByRid.get(rid);
      if (iface) out.push(iface);
      else out.push({ rid, apiName: rid, displayName: rid });
    }
    return out.sort((a, b) =>
      (a.displayName ?? a.apiName).localeCompare(b.displayName ?? b.apiName),
    );
  }, [attachmentsQuery.data, interfacesByRid]);

  const [selectedInterfaceRid, setSelectedInterfaceRid] = useState<
    string | null
  >(null);
  const effectiveInterfaceRid =
    selectedInterfaceRid ?? attachedInterfaces[0]?.rid ?? null;
  const selectedInterface =
    effectiveInterfaceRid !== null
      ? attachedInterfaces.find((i) => i.rid === effectiveInterfaceRid) ?? null
      : null;

  const methodsQuery = useInterfaceMethods(
    ontologyApiName,
    effectiveInterfaceRid,
  );
  const methods = useMemo<InterfaceMethod[]>(
    () => methodsQuery.data?.data ?? [],
    [methodsQuery.data],
  );

  const [selectedMethodRid, setSelectedMethodRid] = useState<string | null>(
    null,
  );
  const effectiveMethodRid = selectedMethodRid ?? methods[0]?.rid ?? null;
  const selectedMethod =
    effectiveMethodRid !== null
      ? methods.find((m) => m.rid === effectiveMethodRid) ?? null
      : null;

  if (objectTypeQuery.isLoading) {
    return (
      <div
        className="flex flex-col h-full items-center justify-center"
        data-testid="interface-methods-loading"
      >
        <LoadingSpinner />
      </div>
    );
  }

  if (objectTypeQuery.isError || !objectTypeQuery.data) {
    return (
      <ConsoleError
        title="Failed to load object type"
        error={objectTypeQuery.error}
      />
    );
  }

  const objectType = objectTypeQuery.data;

  return (
    <div className="flex flex-col h-full" data-testid="interface-methods-page">
      <ConsoleHeader
        ontologyApiName={ontologyApiName}
        objectTypeApiName={objectType.apiName}
        objectTypeDisplay={objectType.displayName}
        primaryKey={primaryKey}
      />

      <div className="grid grid-cols-[260px_1fr] flex-1 min-h-0">
        <InterfacesRail
          attached={attachedInterfaces}
          isLoading={attachmentsQuery.isLoading || interfacesQuery.isLoading}
          isError={attachmentsQuery.isError || interfacesQuery.isError}
          selectedRid={effectiveInterfaceRid}
          onSelect={(rid) => {
            setSelectedInterfaceRid(rid);
            setSelectedMethodRid(null);
          }}
        />

        <MethodPane
          ontologyApiName={ontologyApiName}
          objectTypeApiName={objectType.apiName}
          primaryKey={primaryKey}
          selectedInterface={selectedInterface}
          methods={methods}
          isLoading={methodsQuery.isLoading}
          isError={methodsQuery.isError}
          selectedMethodRid={effectiveMethodRid}
          onSelectMethod={setSelectedMethodRid}
          selectedMethod={selectedMethod}
        />
      </div>
    </div>
  );
}

interface ConsoleHeaderProps {
  ontologyApiName: string;
  objectTypeApiName: string;
  objectTypeDisplay: string;
  primaryKey: string;
}

function ConsoleHeader({
  ontologyApiName,
  objectTypeApiName,
  objectTypeDisplay,
  primaryKey,
}: ConsoleHeaderProps) {
  const navigate = useNavigate();
  return (
    <header
      className="px-6 py-4 border-b border-border flex items-center justify-between gap-4"
      data-testid="interface-methods-header"
      data-ontology-api-name={ontologyApiName}
      data-object-type-api-name={objectTypeApiName}
      data-primary-key={primaryKey}
    >
      <div>
        <h1 className="text-base font-mono text-text-primary">
          Interface Methods
        </h1>
        <p className="text-xs font-mono text-text-secondary mt-1">
          {objectTypeDisplay}{' '}
          <span className="text-accent-cyan">· {primaryKey}</span>
        </p>
      </div>
      <button
        type="button"
        className="px-3 py-1.5 text-xs font-mono border border-border rounded-sm text-text-secondary hover:text-text-primary hover:border-text-secondary"
        data-testid="interface-methods-back-btn"
        onClick={() =>
          navigate(`/browser/${ontologyApiName}/${objectTypeApiName}`)
        }
      >
        Back to Browser
      </button>
    </header>
  );
}

interface InterfacesRailProps {
  attached: OntologyInterface[];
  isLoading: boolean;
  isError: boolean;
  selectedRid: string | null;
  onSelect: (rid: string) => void;
}

function InterfacesRail({
  attached,
  isLoading,
  isError,
  selectedRid,
  onSelect,
}: InterfacesRailProps) {
  if (isLoading) {
    return (
      <aside
        className="border-r border-border p-3"
        data-testid="interface-methods-rail-loading"
      >
        <LoadingSpinner />
      </aside>
    );
  }
  if (isError) {
    return (
      <aside
        className="border-r border-border p-3"
        data-testid="interface-methods-rail-error"
      >
        <p className="text-xs font-mono text-accent-magenta">
          Failed to load interfaces.
        </p>
      </aside>
    );
  }
  if (attached.length === 0) {
    return (
      <aside
        className="border-r border-border p-3"
        data-testid="interface-methods-rail-empty"
      >
        <EmptyState
          title="No interfaces attached"
          description="Attach an interface from the admin console to enable polymorphic dispatch."
        />
      </aside>
    );
  }
  return (
    <aside
      className="border-r border-border overflow-y-auto"
      data-testid="interface-methods-rail"
    >
      <ul role="list">
        {attached.map((iface) => {
          const selected = iface.rid === selectedRid;
          return (
            <li
              key={iface.rid}
              data-testid="interface-methods-rail-row"
              data-interface-rid={iface.rid}
              data-interface-api-name={iface.apiName}
              data-interface-selected={selected ? 'true' : 'false'}
            >
              <button
                type="button"
                className={`block w-full text-left px-3 py-2 text-xs font-mono border-b border-border ${
                  selected
                    ? 'bg-bg-secondary text-accent-cyan'
                    : 'text-text-primary hover:bg-bg-secondary'
                }`}
                data-testid="interface-methods-rail-btn"
                data-interface-api-name={iface.apiName}
                onClick={() => onSelect(iface.rid)}
              >
                <span className="block">{iface.displayName}</span>
                <span className="block text-[10px] text-text-secondary mt-0.5">
                  {iface.apiName}
                </span>
              </button>
            </li>
          );
        })}
      </ul>
    </aside>
  );
}

interface MethodPaneProps {
  ontologyApiName: string;
  objectTypeApiName: string;
  primaryKey: string;
  selectedInterface: OntologyInterface | null;
  methods: InterfaceMethod[];
  isLoading: boolean;
  isError: boolean;
  selectedMethodRid: string | null;
  onSelectMethod: (rid: string) => void;
  selectedMethod: InterfaceMethod | null;
}

function MethodPane({
  ontologyApiName,
  objectTypeApiName,
  primaryKey,
  selectedInterface,
  methods,
  isLoading,
  isError,
  selectedMethodRid,
  onSelectMethod,
  selectedMethod,
}: MethodPaneProps) {
  if (!selectedInterface) {
    return (
      <section
        className="p-6 flex items-center justify-center"
        data-testid="interface-methods-pane-empty"
      >
        <EmptyState title="Select an interface" description="Pick an interface from the rail to list its methods." />
      </section>
    );
  }
  return (
    <section
      className="flex flex-col min-h-0"
      data-testid="interface-methods-pane"
      data-interface-rid={selectedInterface.rid}
      data-interface-api-name={selectedInterface.apiName}
    >
      <header className="px-6 py-3 border-b border-border">
        <h2 className="text-sm font-mono text-text-primary">
          {selectedInterface.displayName}
        </h2>
        <p className="text-xs font-mono text-text-secondary mt-0.5">
          Methods declared on this interface.
        </p>
      </header>
      <div className="grid grid-cols-[280px_1fr] flex-1 min-h-0">
        <MethodList
          methods={methods}
          isLoading={isLoading}
          isError={isError}
          selectedRid={selectedMethodRid}
          onSelect={onSelectMethod}
        />
        <InvokePanel
          ontologyApiName={ontologyApiName}
          objectTypeApiName={objectTypeApiName}
          primaryKey={primaryKey}
          method={selectedMethod}
        />
      </div>
    </section>
  );
}

interface MethodListProps {
  methods: InterfaceMethod[];
  isLoading: boolean;
  isError: boolean;
  selectedRid: string | null;
  onSelect: (rid: string) => void;
}

function MethodList({
  methods,
  isLoading,
  isError,
  selectedRid,
  onSelect,
}: MethodListProps) {
  if (isLoading) {
    return (
      <div
        className="border-r border-border p-3"
        data-testid="interface-methods-list-loading"
      >
        <LoadingSpinner />
      </div>
    );
  }
  if (isError) {
    return (
      <div
        className="border-r border-border p-3"
        data-testid="interface-methods-list-error"
      >
        <p className="text-xs font-mono text-accent-magenta">
          Failed to load methods.
        </p>
      </div>
    );
  }
  if (methods.length === 0) {
    return (
      <div
        className="border-r border-border p-3"
        data-testid="interface-methods-list-empty"
      >
        <EmptyState title="No methods declared" description="This interface has no methods declared yet." />
      </div>
    );
  }
  return (
    <ul
      role="list"
      className="border-r border-border overflow-y-auto"
      data-testid="interface-methods-list"
    >
      {methods.map((m) => {
        const selected = m.rid === selectedRid;
        return (
          <li
            key={m.rid}
            data-testid="interface-methods-list-row"
            data-method-rid={m.rid}
            data-method-name={m.name}
            data-method-selected={selected ? 'true' : 'false'}
            data-method-param-count={String(m.params.length)}
          >
            <button
              type="button"
              className={`block w-full text-left px-3 py-2 text-xs font-mono border-b border-border ${
                selected
                  ? 'bg-bg-secondary text-accent-cyan'
                  : 'text-text-primary hover:bg-bg-secondary'
              }`}
              data-testid="interface-methods-list-btn"
              data-method-name={m.name}
              onClick={() => onSelect(m.rid)}
            >
              <span className="block">{m.name}</span>
              <span className="block text-[10px] text-text-secondary mt-0.5">
                {m.params.length === 0
                  ? 'no params'
                  : `${m.params.length} param${
                      m.params.length === 1 ? '' : 's'
                    }`}{' '}
                · returns {m.returns.type || '—'}
              </span>
            </button>
          </li>
        );
      })}
    </ul>
  );
}

interface InvokePanelProps {
  ontologyApiName: string;
  objectTypeApiName: string;
  primaryKey: string;
  method: InterfaceMethod | null;
}

function InvokePanel({
  ontologyApiName,
  objectTypeApiName,
  primaryKey,
  method,
}: InvokePanelProps) {
  const navigate = useNavigate();
  const invoke = useInvokeInterfaceMethod(ontologyApiName);
  const [paramValues, setParamValues] = useState<Record<string, string>>({});
  const [paramError, setParamError] = useState<string | null>(null);
  const [serverError, setServerError] = useState<string | null>(null);
  const [result, setResult] = useState<InvokeInterfaceMethodResponse | null>(
    null,
  );

  // Reset form + result state when the user picks a different method.
  // Cheap to re-derive at render — sidesteps the "setState inside useEffect"
  // lint trap covered by US-039 patterns.
  const methodKey = method?.rid ?? '';
  const [lastMethodKey, setLastMethodKey] = useState<string>('');
  if (methodKey !== lastMethodKey) {
    setLastMethodKey(methodKey);
    setParamValues({});
    setParamError(null);
    setServerError(null);
    setResult(null);
  }

  if (!method) {
    return (
      <div
        className="p-6 flex items-center justify-center"
        data-testid="interface-methods-invoke-empty"
      >
        <EmptyState title="Select a method" description="Pick a method from the list to open its parameter form." />
      </div>
    );
  }

  const onChangeParam = (name: string, value: string) => {
    setParamValues((prev) => ({ ...prev, [name]: value }));
  };

  const parseParamValue = (
    param: InterfaceMethodParam,
    raw: string,
  ): { ok: true; value: unknown } | { ok: false; error: string } => {
    const trimmed = raw.trim();
    if (trimmed === '') {
      if (param.required) {
        return { ok: false, error: `${param.name}: required` };
      }
      return { ok: true, value: null };
    }
    const lower = param.type.toLowerCase();
    if (lower === 'string') return { ok: true, value: raw };
    if (lower === 'boolean') {
      if (trimmed === 'true') return { ok: true, value: true };
      if (trimmed === 'false') return { ok: true, value: false };
      return {
        ok: false,
        error: `${param.name}: boolean must be 'true' or 'false'`,
      };
    }
    if (
      lower === 'integer' ||
      lower === 'long' ||
      lower === 'short' ||
      lower === 'byte'
    ) {
      if (!/^-?\d+$/.test(trimmed)) {
        return {
          ok: false,
          error: `${param.name}: ${param.type} must be an integer`,
        };
      }
      return { ok: true, value: Number(trimmed) };
    }
    if (lower === 'double' || lower === 'float' || lower === 'decimal') {
      const num = Number(trimmed);
      if (Number.isNaN(num)) {
        return {
          ok: false,
          error: `${param.name}: ${param.type} must be numeric`,
        };
      }
      return { ok: true, value: num };
    }
    // Default path: try JSON parse for richer types, fall back to raw
    // string so the user can still feed a value through.
    try {
      return { ok: true, value: JSON.parse(trimmed) };
    } catch {
      return { ok: true, value: raw };
    }
  };

  const onSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    setParamError(null);
    setServerError(null);
    setResult(null);

    const parameters: Record<string, unknown> = {};
    for (const param of method.params) {
      const raw = paramValues[param.name] ?? '';
      const parsed = parseParamValue(param, raw);
      if (!parsed.ok) {
        setParamError(parsed.error);
        return;
      }
      if (parsed.value !== null) {
        parameters[param.name] = parsed.value;
      }
    }

    try {
      const resp = await invoke.mutateAsync({
        methodRid: method.rid,
        body: { objectType: objectTypeApiName, parameters },
      });
      setResult(resp);
    } catch (err) {
      if (err instanceof ApiRequestError) {
        const detail = err.parameters?.reason ?? err.parameters?.error ?? '';
        setServerError(
          detail ? `${err.errorName}: ${detail}` : err.errorName,
        );
      } else if (err instanceof Error) {
        setServerError(err.message);
      } else {
        setServerError('Invocation failed');
      }
    }
  };

  return (
    <div
      className="flex flex-col min-h-0 overflow-y-auto"
      data-testid="interface-methods-invoke"
      data-method-rid={method.rid}
      data-method-name={method.name}
    >
      <form
        onSubmit={onSubmit}
        className="p-6 space-y-3 border-b border-border"
        data-testid="interface-methods-invoke-form"
      >
        <div>
          <h3 className="text-sm font-mono text-text-primary">{method.name}</h3>
          <p className="text-xs font-mono text-text-secondary mt-0.5">
            {method.description ?? 'Polymorphic dispatch via implementing ActionType.'}
          </p>
        </div>
        {method.params.length === 0 ? (
          <p
            className="text-xs font-mono text-text-secondary"
            data-testid="interface-methods-no-params"
          >
            This method takes no parameters.
          </p>
        ) : (
          method.params.map((param) => (
            <label
              key={param.name}
              className="block"
              data-testid="interface-methods-param-row"
              data-param-name={param.name}
              data-param-type={param.type}
              data-param-required={param.required ? 'true' : 'false'}
            >
              <span className="block text-[11px] font-mono text-text-secondary mb-1">
                {param.name}
                <span className="text-text-secondary ml-1">
                  ({param.type})
                </span>
                {param.required && (
                  <span className="text-accent-magenta ml-1">*</span>
                )}
              </span>
              <input
                type="text"
                value={paramValues[param.name] ?? ''}
                onChange={(e) => onChangeParam(param.name, e.target.value)}
                className="w-full px-2 py-1 text-xs font-mono border border-border rounded-sm bg-bg-primary text-text-primary"
                data-testid="interface-methods-param-input"
                data-param-name={param.name}
                aria-label={`Parameter ${param.name}`}
              />
            </label>
          ))
        )}
        {paramError && (
          <p
            className="text-xs font-mono text-accent-magenta"
            data-testid="interface-methods-param-error"
          >
            {paramError}
          </p>
        )}
        <div className="flex items-center gap-2 pt-2">
          <button
            type="submit"
            disabled={invoke.isPending}
            className="px-3 py-1.5 text-xs font-mono border border-accent-cyan text-accent-cyan rounded-sm disabled:opacity-50"
            data-testid="interface-methods-invoke-submit"
          >
            {invoke.isPending ? 'Invoking…' : 'Invoke method'}
          </button>
          <span className="text-[11px] font-mono text-text-secondary">
            ObjectType <code>{objectTypeApiName}</code> · PK{' '}
            <code>{primaryKey}</code>
          </span>
        </div>
      </form>

      {serverError && (
        <div
          className="px-6 py-3 border-b border-border bg-bg-secondary"
          data-testid="interface-methods-server-error"
          role="alert"
        >
          <p className="text-xs font-mono text-accent-magenta">{serverError}</p>
        </div>
      )}

      {result && (
        <div
          className="px-6 py-4 space-y-3"
          data-testid="interface-methods-result"
          data-action-type-api-name={result.actionTypeApiName}
          data-action-type-rid={result.actionTypeRid}
          data-method-rid={result.methodRid}
        >
          <div>
            <p className="text-[11px] font-mono uppercase text-text-secondary">
              Dispatched ActionType
            </p>
            <p
              className="text-xs font-mono text-accent-cyan mt-0.5"
              data-testid="interface-methods-result-action"
            >
              {result.actionTypeApiName}
            </p>
          </div>
          <div>
            <p className="text-[11px] font-mono uppercase text-text-secondary">
              Result
            </p>
            <pre
              className="text-[11px] font-mono whitespace-pre-wrap break-words text-text-primary mt-0.5 p-2 border border-border bg-bg-primary rounded-sm max-h-64 overflow-auto"
              data-testid="interface-methods-result-body"
            >
              {result.result === undefined
                ? '— no payload returned —'
                : JSON.stringify(result.result, null, 2)}
            </pre>
          </div>
          <div>
            <button
              type="button"
              className="text-[11px] font-mono text-accent-cyan underline"
              data-testid="interface-methods-audit-link"
              data-action-type-api-name={result.actionTypeApiName}
              onClick={() =>
                navigate(
                  `/actions/${ontologyApiName}/history?actionType=${encodeURIComponent(result.actionTypeApiName)}`,
                )
              }
            >
              View in action history
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

interface ConsoleErrorProps {
  title: string;
  error: unknown;
}

function ConsoleError({ title, error }: ConsoleErrorProps) {
  const msg =
    error instanceof ApiRequestError
      ? error.errorName
      : error instanceof Error
        ? error.message
        : 'Unknown error';
  return (
    <div
      className="p-6 flex flex-col items-center justify-center h-full"
      data-testid="interface-methods-error"
    >
      <p className="text-xs font-mono text-accent-magenta">{title}</p>
      <p className="text-[11px] font-mono text-text-secondary mt-1">{msg}</p>
    </div>
  );
}
