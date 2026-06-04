import { useMemo, useState } from 'react';

import { ApiRequestError } from '../../api/client';
import type {
  CreateAipToolRequest,
  ToolRecord,
  UpdateAipToolRequest,
} from '../../api/aipTools';
import { getAipTool, validateToolName } from '../../api/aipTools';
import {
  useAipTools,
  useCreateAipTool,
  useDeleteAipTool,
  useUpdateAipTool,
} from '../../hooks/useAipTools';
import { EmptyState } from '../common/EmptyState';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { Modal } from '../common/Modal';

function describeError(err: unknown): string {
  if (err instanceof ApiRequestError) {
    return `${err.errorName}: ${err.parameters?.reason ?? err.message}`;
  }
  if (err instanceof Error) return err.message;
  return 'Request failed.';
}

// formatTimestamp renders an ISO instant as a localized, human-readable string,
// falling back to the raw value when it is unparseable.
function formatTimestamp(raw: string): string {
  if (!raw) return '';
  const date = new Date(raw);
  if (Number.isNaN(date.getTime())) return raw;
  return date.toLocaleString();
}

interface ToolDraft {
  name: string;
  description: string;
  parametersText: string;
  handlerFunctionRid: string;
  enabled: boolean;
}

const EMPTY_DRAFT: ToolDraft = {
  name: '',
  description: '',
  parametersText: '',
  handlerFunctionRid: '',
  enabled: true,
};

function draftFromTool(tool: ToolRecord): ToolDraft {
  let parametersText = '';
  if (tool.parameters !== undefined && tool.parameters !== null) {
    try {
      parametersText = JSON.stringify(tool.parameters, null, 2);
    } catch {
      parametersText = '';
    }
  }
  return {
    name: tool.name,
    description: tool.description ?? '',
    parametersText,
    handlerFunctionRid: tool.handlerFunctionRid ?? '',
    enabled: tool.enabled,
  };
}

// parseParameters returns [value, error]. An empty editor maps to "omit
// parameters" (undefined). A non-empty editor must parse to a JSON object —
// the wire shape is a JSON-schema descriptor, never a bare array/scalar.
function parseParameters(text: string): [unknown, string | null] {
  const trimmed = text.trim();
  if (trimmed === '') return [undefined, null];
  let parsed: unknown;
  try {
    parsed = JSON.parse(trimmed);
  } catch (e) {
    return [undefined, `Invalid JSON: ${(e as Error).message}`];
  }
  if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)) {
    return [undefined, 'Parameters must be a JSON object.'];
  }
  return [parsed, null];
}

export function AipToolsPage() {
  const toolsQuery = useAipTools();
  const tools = useMemo(() => toolsQuery.data ?? [], [toolsQuery.data]);

  const createMutation = useCreateAipTool();
  const updateMutation = useUpdateAipTool();
  const deleteMutation = useDeleteAipTool();

  const [modalOpen, setModalOpen] = useState(false);
  const [editingName, setEditingName] = useState<string | null>(null);
  const [draft, setDraft] = useState<ToolDraft>(EMPTY_DRAFT);
  const [formError, setFormError] = useState<string | null>(null);
  const [parametersError, setParametersError] = useState<string | null>(null);

  // Read-only "View" detail modal state. Unlike the edit modal it fetches the
  // full record via getAipTool (the read-time source of truth) rather than
  // reusing the list row, and never mutates anything.
  const [detailOpen, setDetailOpen] = useState(false);
  const [detailName, setDetailName] = useState<string | null>(null);
  const [detailTool, setDetailTool] = useState<ToolRecord | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState<string | null>(null);

  const isEditing = editingName !== null;

  const openCreate = () => {
    setEditingName(null);
    setDraft(EMPTY_DRAFT);
    setFormError(null);
    setParametersError(null);
    setModalOpen(true);
  };

  const openEdit = (tool: ToolRecord) => {
    setEditingName(tool.name);
    setDraft(draftFromTool(tool));
    setFormError(null);
    setParametersError(null);
    setModalOpen(true);
  };

  const closeModal = () => setModalOpen(false);

  const openView = (tool: ToolRecord) => {
    setDetailName(tool.name);
    // Seed the modal with the row we already have so it renders instantly,
    // then refresh from the read-only endpoint for the authoritative record.
    setDetailTool(tool);
    setDetailError(null);
    setDetailLoading(true);
    setDetailOpen(true);
    getAipTool(tool.name)
      .then((full) => setDetailTool(full))
      .catch((err) => setDetailError(describeError(err)))
      .finally(() => setDetailLoading(false));
  };

  const closeDetail = () => setDetailOpen(false);

  const onSubmit = () => {
    setFormError(null);
    setParametersError(null);

    if (!isEditing) {
      const nameErr = validateToolName(draft.name);
      if (nameErr) {
        setFormError(nameErr);
        return;
      }
    }

    const [parameters, paramErr] = parseParameters(draft.parametersText);
    if (paramErr) {
      setParametersError(paramErr);
      return;
    }

    if (isEditing && editingName) {
      const body: UpdateAipToolRequest = {
        description: draft.description,
        parameters,
        handlerFunctionRid: draft.handlerFunctionRid,
        enabled: draft.enabled,
      };
      updateMutation.mutate(
        { name: editingName, body },
        {
          onSuccess: () => setModalOpen(false),
          onError: (err) => setFormError(describeError(err)),
        },
      );
      return;
    }

    const body: CreateAipToolRequest = {
      name: draft.name.trim(),
      description: draft.description || undefined,
      parameters,
      handlerFunctionRid: draft.handlerFunctionRid || undefined,
      enabled: draft.enabled,
    };
    createMutation.mutate(body, {
      onSuccess: () => setModalOpen(false),
      onError: (err) => setFormError(describeError(err)),
    });
  };

  const onDelete = (tool: ToolRecord) => {
    if (typeof window !== 'undefined') {
      const ok = window.confirm(
        `Delete tool "${tool.name}"? This cannot be undone.`,
      );
      if (!ok) return;
    }
    deleteMutation.mutate(tool.name);
  };

  const submitting = createMutation.isPending || updateMutation.isPending;

  return (
    <div
      className="mx-auto max-w-[1100px] px-4 py-6"
      data-testid="aip-tools-page"
    >
      <header className="mb-5 flex items-start justify-between gap-4">
        <div>
          <h1 className="text-lg font-semibold tracking-tight text-text-primary">
            AIP Tool Catalog
          </h1>
          <p className="mt-1 text-xs text-text-secondary">
            Custom LLM-visible tools. Each tool is an OpenAI / Anthropic
            JSON-schema descriptor optionally backed by a Function handler.
          </p>
        </div>
        <button
          type="button"
          onClick={openCreate}
          data-testid="aip-tool-create-btn"
          className="rounded-md bg-amber-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-amber-500"
        >
          + New tool
        </button>
      </header>

      {toolsQuery.isLoading ? (
        <div
          className="flex items-center justify-center py-16"
          data-testid="aip-tools-loading"
        >
          <LoadingSpinner />
        </div>
      ) : toolsQuery.isError ? (
        <div
          className="rounded-lg border border-border/50 bg-bg-secondary/60 px-4 py-10"
          data-testid="aip-tools-unavailable"
        >
          <EmptyState
            title="Tool catalog unavailable"
            description={
              'The AIP tool catalog could not be loaded on this deployment. ' +
              describeError(toolsQuery.error)
            }
          />
        </div>
      ) : tools.length === 0 ? (
        <div
          className="rounded-lg border border-border/50 bg-bg-secondary/60 px-4 py-10"
          data-testid="aip-tools-empty"
        >
          <EmptyState
            title="No tools yet"
            description="Define a tool so the AIP runtime can expose it to the LLM."
            action={
              <button
                type="button"
                onClick={openCreate}
                data-testid="aip-tools-empty-cta"
                className="rounded-md bg-amber-600 px-3 py-1.5 text-sm font-semibold text-white hover:bg-amber-500"
              >
                + New tool
              </button>
            }
          />
        </div>
      ) : (
        <ul
          className="space-y-2"
          aria-label="AIP tool list"
          data-testid="aip-tool-list"
        >
          {tools.map((tool) => (
            <li
              key={tool.name}
              data-testid="aip-tool-row"
              data-tool-name={tool.name}
              className="rounded-lg border border-border bg-bg-secondary px-4 py-3"
            >
              <div className="flex items-start justify-between gap-4">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="font-mono text-sm font-semibold text-text-primary">
                      {tool.name}
                    </span>
                    <EnabledBadge enabled={tool.enabled} />
                  </div>
                  {tool.description && (
                    <p className="mt-1 text-xs text-text-secondary">
                      {tool.description}
                    </p>
                  )}
                  {tool.handlerFunctionRid ? (
                    <p className="mt-1 font-mono text-[11px] text-text-muted">
                      {tool.handlerFunctionRid}
                    </p>
                  ) : (
                    <p className="mt-1 text-[11px] text-text-muted">
                      definition-only (no handler)
                    </p>
                  )}
                  <ToolMetaLine tool={tool} />
                </div>
                <div className="flex shrink-0 items-center gap-2">
                  <button
                    type="button"
                    onClick={() => openView(tool)}
                    data-testid="aip-tool-view-btn"
                    className="rounded-md border border-border/60 px-2.5 py-1 text-xs text-text-secondary hover:bg-bg-tertiary"
                  >
                    View
                  </button>
                  <button
                    type="button"
                    onClick={() => openEdit(tool)}
                    data-testid="aip-tool-edit-btn"
                    className="rounded-md border border-border/60 px-2.5 py-1 text-xs text-text-secondary hover:bg-bg-tertiary"
                  >
                    Edit
                  </button>
                  <button
                    type="button"
                    onClick={() => onDelete(tool)}
                    data-testid="aip-tool-delete-btn"
                    className="rounded-md border border-rose-500/40 px-2.5 py-1 text-xs text-rose-300 hover:bg-rose-500/10"
                  >
                    Delete
                  </button>
                </div>
              </div>
            </li>
          ))}
        </ul>
      )}

      <Modal
        open={modalOpen}
        onClose={closeModal}
        title={isEditing ? `Edit tool: ${editingName}` : 'New AIP tool'}
        size="lg"
      >
        <div className="space-y-4" data-testid="aip-tool-modal">
          <label className="flex flex-col gap-1.5 text-xs text-text-secondary">
            Name
            <input
              type="text"
              value={draft.name}
              disabled={isEditing}
              onChange={(e) =>
                setDraft((d) => ({ ...d, name: e.target.value }))
              }
              data-testid="aip-tool-name-input"
              placeholder="my_tool"
              className="rounded-md border border-border/50 bg-bg-primary px-2.5 py-2 font-mono text-sm text-text-primary outline-none focus:border-amber-500/60 disabled:opacity-60"
            />
          </label>

          <label className="flex flex-col gap-1.5 text-xs text-text-secondary">
            Description (optional)
            <textarea
              value={draft.description}
              onChange={(e) =>
                setDraft((d) => ({ ...d, description: e.target.value }))
              }
              rows={2}
              data-testid="aip-tool-description-input"
              placeholder="What the tool does — surfaced to the LLM."
              className="rounded-md border border-border/50 bg-bg-primary px-2.5 py-2 text-xs text-text-primary outline-none focus:border-amber-500/60"
            />
          </label>

          <label className="flex flex-col gap-1.5 text-xs text-text-secondary">
            Parameters (JSON schema object, optional)
            <textarea
              value={draft.parametersText}
              onChange={(e) => {
                setDraft((d) => ({ ...d, parametersText: e.target.value }));
                if (parametersError) setParametersError(null);
              }}
              rows={8}
              data-testid="aip-tool-parameters-input"
              placeholder={'{\n  "type": "object",\n  "properties": {}\n}'}
              className="rounded-md border border-border/50 bg-bg-primary px-2.5 py-2 font-mono text-[11px] text-text-primary outline-none focus:border-amber-500/60"
            />
            {parametersError && (
              <span
                role="alert"
                data-testid="aip-tool-parameters-error"
                className="text-[11px] text-rose-300"
              >
                {parametersError}
              </span>
            )}
          </label>

          <label className="flex flex-col gap-1.5 text-xs text-text-secondary">
            Handler Function RID (optional)
            <input
              type="text"
              value={draft.handlerFunctionRid}
              onChange={(e) =>
                setDraft((d) => ({
                  ...d,
                  handlerFunctionRid: e.target.value,
                }))
              }
              data-testid="aip-tool-handler-input"
              placeholder="ri.function.main.function.my-handler"
              className="rounded-md border border-border/50 bg-bg-primary px-2.5 py-2 font-mono text-xs text-text-primary outline-none focus:border-amber-500/60"
            />
          </label>

          <label className="flex items-center gap-2 text-xs text-text-secondary">
            <input
              type="checkbox"
              checked={draft.enabled}
              onChange={(e) =>
                setDraft((d) => ({ ...d, enabled: e.target.checked }))
              }
              data-testid="aip-tool-enabled-toggle"
              className="h-4 w-4 rounded border-border/60 bg-bg-primary"
            />
            Enabled (visible to the LLM runtime)
          </label>

          {formError && (
            <div
              role="alert"
              data-testid="aip-tool-form-error"
              className="rounded-md border border-rose-500/40 bg-rose-500/10 px-3 py-2 text-xs text-rose-300"
            >
              {formError}
            </div>
          )}

          <div className="flex justify-end gap-2">
            <button
              type="button"
              onClick={closeModal}
              className="rounded-md border border-border/60 px-3 py-1.5 text-xs text-text-secondary hover:bg-bg-tertiary"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={onSubmit}
              disabled={submitting}
              data-testid="aip-tool-submit-btn"
              className="rounded-md bg-amber-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-amber-500 disabled:opacity-60"
            >
              {submitting
                ? 'Saving…'
                : isEditing
                  ? 'Save changes'
                  : 'Create tool'}
            </button>
          </div>
        </div>
      </Modal>

      <Modal
        open={detailOpen}
        onClose={closeDetail}
        title={detailName ? `Tool: ${detailName}` : 'Tool detail'}
        size="lg"
      >
        <ToolDetail
          tool={detailTool}
          loading={detailLoading}
          error={detailError}
        />
      </Modal>
    </div>
  );
}

// ToolMetaLine renders the audit trail (createdBy / createdAt / updatedAt) as a
// single muted line. Each segment is guarded independently so a record missing
// any field never renders a dangling "Created by  ·" fragment.
function ToolMetaLine({ tool }: { tool: ToolRecord }) {
  const segments: string[] = [];
  if (tool.createdBy) segments.push(`Created by ${tool.createdBy}`);
  if (tool.createdAt) segments.push(formatTimestamp(tool.createdAt));
  if (tool.updatedAt) segments.push(`Updated ${formatTimestamp(tool.updatedAt)}`);
  if (segments.length === 0) return null;
  return (
    <p
      data-testid="aip-tool-row-meta"
      className="mt-1 text-[11px] text-text-muted"
    >
      {segments.join(' · ')}
    </p>
  );
}

// ToolDetail is the read-only body of the View modal: it never mutates the
// catalog, just surfaces the full record (including the pretty-printed
// parameters JSON schema) fetched via getAipTool.
function ToolDetail({
  tool,
  loading,
  error,
}: {
  tool: ToolRecord | null;
  loading: boolean;
  error: string | null;
}) {
  if (error) {
    return (
      <div
        role="alert"
        data-testid="aip-tool-detail-error"
        className="rounded-md border border-rose-500/40 bg-rose-500/10 px-3 py-2 text-xs text-rose-300"
      >
        {error}
      </div>
    );
  }
  if (!tool) {
    return (
      <div
        className="flex items-center justify-center py-10"
        data-testid="aip-tool-detail-loading"
      >
        <LoadingSpinner />
      </div>
    );
  }

  let parametersText = '';
  if (tool.parameters !== undefined && tool.parameters !== null) {
    try {
      parametersText = JSON.stringify(tool.parameters, null, 2);
    } catch {
      parametersText = '';
    }
  }

  return (
    <div className="space-y-4 text-xs" data-testid="aip-tool-detail">
      <div className="flex items-center gap-2">
        <span className="font-mono text-sm font-semibold text-text-primary">
          {tool.name}
        </span>
        <EnabledBadge enabled={tool.enabled} />
        {loading && (
          <span className="text-[11px] text-text-muted">refreshing…</span>
        )}
      </div>

      <DetailField label="Description">
        {tool.description ? (
          <p className="text-xs text-text-secondary">{tool.description}</p>
        ) : (
          <p className="text-[11px] italic text-text-muted">No description.</p>
        )}
      </DetailField>

      <DetailField label="Handler Function RID">
        {tool.handlerFunctionRid ? (
          <p className="font-mono text-[11px] text-text-secondary">
            {tool.handlerFunctionRid}
          </p>
        ) : (
          <p className="text-[11px] italic text-text-muted">
            definition-only (no handler)
          </p>
        )}
      </DetailField>

      <DetailField label="Audit">
        <ToolMetaLine tool={tool} />
        {!tool.createdBy && !tool.createdAt && !tool.updatedAt && (
          <p className="text-[11px] italic text-text-muted">
            No audit metadata.
          </p>
        )}
      </DetailField>

      <DetailField label="Parameters (JSON schema)">
        {parametersText ? (
          <pre
            data-testid="aip-tool-detail-parameters"
            className="max-h-72 overflow-auto rounded-md border border-border/50 bg-bg-primary px-2.5 py-2 font-mono text-[11px] text-text-primary"
          >
            {parametersText}
          </pre>
        ) : (
          <p className="text-[11px] italic text-text-muted">
            No parameters defined.
          </p>
        )}
      </DetailField>
    </div>
  );
}

function DetailField({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-[11px] font-semibold uppercase tracking-wide text-text-muted">
        {label}
      </span>
      {children}
    </div>
  );
}

function EnabledBadge({ enabled }: { enabled: boolean }) {
  return (
    <span
      data-testid="aip-tool-enabled-badge"
      data-enabled={enabled}
      className={`rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide ${
        enabled
          ? 'bg-emerald-500/15 text-emerald-300'
          : 'bg-bg-tertiary text-text-muted'
      }`}
    >
      {enabled ? 'Enabled' : 'Disabled'}
    </span>
  );
}
