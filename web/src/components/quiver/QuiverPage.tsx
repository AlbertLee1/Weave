import { useEffect, useState } from 'react';
import { useNavigate, useParams } from 'react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { EmptyState } from '../common/EmptyState';
import { pickColor } from '../../utils/quiverAggregation';
import {
  QuiverWorkbenchView,
  type SeriesSpec,
} from './QuiverWorkbenchView';
import {
  getQuiverDashboard,
  listQuiverDashboards,
  saveQuiverDashboard,
  deleteQuiverDashboard,
  type QuiverDashboard,
  type QuiverDashboardConfig,
} from '../../api/quiver';
import { ApiRequestError } from '../../api/client';
import { useBranchStore } from '../../stores/branchStore';

// US-404: pair series sharing the same (objectType, primaryKey, property)
// across branches by reusing the slot's color so dashed/solid line styles
// in the chart visually mark the branch difference.
function colorForSlot(
  series: SeriesSpec[],
  objectType: string,
  primaryKey: string,
  property: string,
): string {
  const slotKey = `${objectType}|${primaryKey}|${property}`;
  for (const s of series) {
    if (`${s.objectType}|${s.primaryKey}|${s.property}` === slotKey) {
      return s.color;
    }
  }
  const distinctSlots = new Set(
    series.map((s) => `${s.objectType}|${s.primaryKey}|${s.property}`),
  );
  return pickColor(distinctSlots.size);
}

const QUIVER_DASHBOARDS_KEY = ['quiver', 'dashboards'] as const;

function dashboardKey(rid: string) {
  return ['quiver', 'dashboards', rid] as const;
}

export function QuiverPage() {
  const navigate = useNavigate();
  const { ontology, rid } = useParams<{ ontology: string; rid?: string }>();
  const ontologyApiName = ontology ?? '';
  const queryClient = useQueryClient();

  const [seriesList, setSeriesList] = useState<SeriesSpec[]>([]);
  const [dashboardName, setDashboardName] = useState('');
  const [dashboardRID, setDashboardRID] = useState<string | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);

  // Picker form state
  const [draftObjectType, setDraftObjectType] = useState('');
  const [draftPrimaryKey, setDraftPrimaryKey] = useState('');
  const [draftProperty, setDraftProperty] = useState('');
  const [draftLabel, setDraftLabel] = useState('');
  const [draftBranch, setDraftBranch] = useState('');

  // US-404: page-level active branch from the topbar picker is the implicit
  // default for new series. The user can override per-series via the form.
  const activeBranch = useBranchStore((s) => s.getBranch(ontologyApiName));

  const dashboardsQuery = useQuery({
    queryKey: QUIVER_DASHBOARDS_KEY,
    queryFn: listQuiverDashboards,
    // The list endpoint 404s in degraded-mode (no PG) deployments;
    // hide the panel rather than render an error toast.
    retry: false,
  });

  const loadedDashboardQuery = useQuery({
    queryKey: rid ? dashboardKey(rid) : ['quiver', 'dashboards', '__none__'],
    queryFn: () => getQuiverDashboard(rid!),
    enabled: !!rid,
    retry: false,
  });

  // When the URL carries an :rid, hydrate the editor state from the
  // persisted config the first time the dashboard loads. We use rid as
  // the effect's identity so re-renders inside the dashboard don't
  // overwrite local edits.
  useEffect(() => {
    const dashboard = loadedDashboardQuery.data;
    if (!dashboard || !rid) return;
    setDashboardRID(dashboard.rid);
    setDashboardName(dashboard.name);
    const cfg = dashboard.config ?? { ontologyApiName: '', series: [] };
    if (Array.isArray(cfg.series)) {
      setSeriesList(
        cfg.series.map((s) => ({
          id: s.id,
          ontologyApiName: cfg.ontologyApiName || ontologyApiName,
          objectType: s.objectType,
          primaryKey: s.primaryKey,
          property: s.property,
          label: s.label,
          color: s.color,
          ...(s.branch ? { branch: s.branch } : {}),
        })),
      );
    }
  }, [loadedDashboardQuery.data, rid, ontologyApiName]);

  const saveMutation = useMutation({
    mutationFn: saveQuiverDashboard,
    onSuccess: (saved: QuiverDashboard) => {
      setDashboardRID(saved.rid);
      setSaveError(null);
      queryClient.invalidateQueries({ queryKey: QUIVER_DASHBOARDS_KEY });
      queryClient.setQueryData(dashboardKey(saved.rid), saved);
    },
    onError: (err: unknown) => {
      if (err instanceof ApiRequestError) {
        setSaveError(`${err.errorName}: ${JSON.stringify(err.parameters ?? {})}`);
      } else {
        setSaveError(String(err));
      }
    },
  });

  const deleteMutation = useMutation({
    mutationFn: deleteQuiverDashboard,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: QUIVER_DASHBOARDS_KEY });
    },
  });

  function handleAdd(e: React.FormEvent) {
    e.preventDefault();
    const objectType = draftObjectType.trim();
    const primaryKey = draftPrimaryKey.trim();
    const property = draftProperty.trim();
    if (!objectType || !primaryKey || !property) return;
    const branchValue = draftBranch.trim() || activeBranch;
    const id = `${objectType}|${primaryKey}|${property}|${branchValue}|${Date.now()}`;
    const label = draftLabel.trim() || `${objectType}/${primaryKey}.${property}`;
    setSeriesList((prev) => {
      const color = colorForSlot(prev, objectType, primaryKey, property);
      return [
        ...prev,
        {
          id,
          ontologyApiName,
          objectType,
          primaryKey,
          property,
          label,
          color,
          ...(branchValue ? { branch: branchValue } : {}),
        },
      ];
    });
    setDraftObjectType('');
    setDraftPrimaryKey('');
    setDraftProperty('');
    setDraftLabel('');
    setDraftBranch('');
  }

  function handleRemove(id: string) {
    setSeriesList((prev) => prev.filter((s) => s.id !== id));
  }

  function handleSave() {
    const trimmedName = dashboardName.trim();
    if (!trimmedName) {
      setSaveError('Dashboard name is required.');
      return;
    }
    const config: QuiverDashboardConfig = {
      ontologyApiName,
      series: seriesList.map((s) => ({
        id: s.id,
        objectType: s.objectType,
        primaryKey: s.primaryKey,
        property: s.property,
        label: s.label,
        color: s.color,
        ...(s.branch ? { branch: s.branch } : {}),
      })),
    };
    saveMutation.mutate({
      ...(dashboardRID ? { rid: dashboardRID } : {}),
      name: trimmedName,
      config,
    });
  }

  function handleNew() {
    setDashboardRID(null);
    setDashboardName('');
    setSeriesList([]);
    setSaveError(null);
    if (rid) {
      navigate(`/quiver/${ontologyApiName}`);
    }
  }

  if (!ontologyApiName) {
    return (
      <div className="flex items-center justify-center h-full">
        <EmptyState
          title="Missing Ontology"
          description="Pick an ontology from the sidebar to use Quiver."
        />
      </div>
    );
  }

  const dashboards = dashboardsQuery.data?.dashboards ?? [];
  const dashboardsAvailable = !dashboardsQuery.isError;

  return (
    <div className="flex flex-col h-full overflow-hidden" data-testid="quiver-page">
      <div className="border-b border-border bg-bg-primary p-4 flex flex-col gap-4">
        <div className="flex items-start justify-between gap-4">
          <div>
            <h2 className="text-sm font-medium text-text-primary">Quiver Workbench</h2>
            <div className="text-xs font-mono text-text-secondary mt-0.5">
              {ontologyApiName}
              {dashboardRID && (
                <span className="ml-2 text-text-muted">· {dashboardRID}</span>
              )}
            </div>
          </div>
          {dashboardsAvailable && (
            <div
              className="flex items-center gap-2"
              data-testid="quiver-save-controls"
            >
              <input
                type="text"
                placeholder="Dashboard name"
                value={dashboardName}
                onChange={(e) => setDashboardName(e.target.value)}
                data-testid="quiver-dashboard-name"
                className="px-2 py-1.5 text-sm bg-bg-tertiary border border-border rounded text-text-primary w-56"
              />
              <button
                type="button"
                onClick={handleSave}
                disabled={
                  saveMutation.isPending ||
                  dashboardName.trim() === '' ||
                  seriesList.length === 0
                }
                data-testid="quiver-save-button"
                className="bg-accent-emerald text-bg-primary px-3 py-1.5 rounded text-sm font-medium hover:opacity-80 disabled:opacity-40 disabled:cursor-not-allowed"
              >
                {dashboardRID ? 'Update' : 'Save'}
              </button>
              {dashboardRID && (
                <button
                  type="button"
                  onClick={handleNew}
                  data-testid="quiver-new-button"
                  className="px-3 py-1.5 text-sm border border-border rounded text-text-secondary hover:text-text-primary"
                >
                  New
                </button>
              )}
            </div>
          )}
        </div>

        {saveError && (
          <div
            className="text-xs text-accent-error"
            data-testid="quiver-save-error"
          >
            {saveError}
          </div>
        )}

        {dashboardsAvailable && dashboards.length > 0 && (
          <div
            className="flex flex-wrap items-center gap-2 text-xs"
            data-testid="quiver-saved-list"
          >
            <span className="text-text-secondary">Saved:</span>
            {dashboards.map((d) => (
              <span
                key={d.rid}
                className={`flex items-center gap-1 border rounded px-2 py-1 ${
                  d.rid === dashboardRID
                    ? 'border-accent-cyan text-text-primary'
                    : 'border-border text-text-secondary'
                }`}
                data-testid={`quiver-saved-${d.rid}`}
              >
                <button
                  type="button"
                  onClick={() => navigate(`/quiver/${ontologyApiName}/${d.rid}`)}
                  className="hover:text-text-primary"
                  data-testid={`quiver-load-${d.rid}`}
                >
                  {d.name}
                </button>
                <button
                  type="button"
                  onClick={() => navigate(`/quiver/${ontologyApiName}/${d.rid}/view`)}
                  className="text-text-muted hover:text-accent-cyan"
                  title="Open read-only share view"
                  data-testid={`quiver-share-${d.rid}`}
                >
                  share
                </button>
                <button
                  type="button"
                  onClick={() => deleteMutation.mutate(d.rid)}
                  className="text-text-muted hover:text-accent-error"
                  aria-label={`Delete ${d.name}`}
                  data-testid={`quiver-delete-${d.rid}`}
                >
                  ×
                </button>
              </span>
            ))}
          </div>
        )}

        <form
          onSubmit={handleAdd}
          className="grid grid-cols-1 md:grid-cols-6 gap-2"
          data-testid="quiver-add-form"
        >
          <input
            type="text"
            placeholder="Object type"
            value={draftObjectType}
            onChange={(e) => setDraftObjectType(e.target.value)}
            data-testid="quiver-input-objectType"
            className="px-2 py-1.5 text-sm bg-bg-tertiary border border-border rounded text-text-primary"
          />
          <input
            type="text"
            placeholder="Primary key"
            value={draftPrimaryKey}
            onChange={(e) => setDraftPrimaryKey(e.target.value)}
            data-testid="quiver-input-primaryKey"
            className="px-2 py-1.5 text-sm bg-bg-tertiary border border-border rounded text-text-primary"
          />
          <input
            type="text"
            placeholder="Property"
            value={draftProperty}
            onChange={(e) => setDraftProperty(e.target.value)}
            data-testid="quiver-input-property"
            className="px-2 py-1.5 text-sm bg-bg-tertiary border border-border rounded text-text-primary"
          />
          <input
            type="text"
            placeholder="Label (optional)"
            value={draftLabel}
            onChange={(e) => setDraftLabel(e.target.value)}
            data-testid="quiver-input-label"
            className="px-2 py-1.5 text-sm bg-bg-tertiary border border-border rounded text-text-primary"
          />
          <input
            type="text"
            placeholder={`Branch (${activeBranch})`}
            value={draftBranch}
            onChange={(e) => setDraftBranch(e.target.value)}
            data-testid="quiver-input-branch"
            title="Override the ontology branch this series resolves on. Leave blank to track the page's active branch."
            className="px-2 py-1.5 text-sm bg-bg-tertiary border border-border rounded text-text-primary font-mono"
          />
          <button
            type="submit"
            disabled={
              !draftObjectType.trim() ||
              !draftPrimaryKey.trim() ||
              !draftProperty.trim()
            }
            data-testid="quiver-add-button"
            className="bg-accent-cyan text-bg-primary px-4 py-1.5 rounded text-sm font-medium hover:bg-accent-cyan/80 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            Add series
          </button>
        </form>
      </div>

      <div className="flex-1 overflow-y-auto p-4 space-y-4">
        {seriesList.length === 0 ? (
          <EmptyState
            title="No series yet"
            description="Add a series above to plot a time-series overlay."
          />
        ) : (
          <QuiverWorkbenchView
            seriesList={seriesList}
            onRemove={handleRemove}
          />
        )}
      </div>
    </div>
  );
}
