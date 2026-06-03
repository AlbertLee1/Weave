import { useMemo, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router';
import { useTranslation } from 'react-i18next';
import {
  useBranchReconcileDiff,
  useMergeBranch,
  useRebaseBranch,
} from '../../hooks/useBranches';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { EmptyState } from '../common/EmptyState';
import {
  MergeBranchConflictError,
  RebaseBranchConflictError,
} from '../../api/ontologies';
import { ApiRequestError } from '../../api/client';
import type {
  AnnotatedDiffEntry,
  AnnotatedMergeConflict,
  ConflictResolutionChoice,
} from '../../api/types';

const changeStyles: Record<string, { bg: string; text: string; label: string }> = {
  ADDED: { bg: 'bg-green-900/30', text: 'text-green-400', label: 'Added' },
  MODIFIED: { bg: 'bg-yellow-900/30', text: 'text-yellow-400', label: 'Modified' },
  DELETED: { bg: 'bg-red-900/30', text: 'text-red-400', label: 'Deleted' },
};

function ChangeBadge({ changeType }: { changeType: string }) {
  const style =
    changeStyles[changeType] ?? {
      bg: 'bg-bg-tertiary',
      text: 'text-text-secondary',
      label: changeType,
    };
  return (
    <span
      className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium ${style.bg} ${style.text}`}
    >
      {style.label}
    </span>
  );
}

function EntityTypeBadge({ entityType }: { entityType: string }) {
  return (
    <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-mono bg-bg-tertiary text-text-secondary border border-border">
      {entityType}
    </span>
  );
}

function SideBySideJson({
  left,
  right,
  leftLabel,
  rightLabel,
}: {
  left: Record<string, unknown> | null | undefined;
  right: Record<string, unknown> | null | undefined;
  leftLabel: string;
  rightLabel: string;
}) {
  const allKeys = new Set<string>();
  if (left) Object.keys(left).forEach((k) => allKeys.add(k));
  if (right) Object.keys(right).forEach((k) => allKeys.add(k));
  const sortedKeys = Array.from(allKeys).sort();

  return (
    <div className="grid grid-cols-2 gap-px bg-border rounded overflow-hidden text-xs font-mono">
      <div className="bg-bg-secondary px-3 py-1.5 text-text-muted font-sans font-medium text-xs">
        {leftLabel}
      </div>
      <div className="bg-bg-secondary px-3 py-1.5 text-text-muted font-sans font-medium text-xs">
        {rightLabel}
      </div>
      {sortedKeys.length === 0 && (
        <>
          <div className="px-3 py-2 bg-bg-primary text-text-muted italic">—</div>
          <div className="px-3 py-2 bg-bg-primary text-text-muted italic">—</div>
        </>
      )}
      {sortedKeys.map((key) => {
        const lVal = left?.[key];
        const rVal = right?.[key];
        const lStr = lVal !== undefined ? JSON.stringify(lVal) : '';
        const rStr = rVal !== undefined ? JSON.stringify(rVal) : '';
        const changed = lStr !== rStr;
        return (
          <div key={key} className="contents">
            <div
              className={`px-3 py-1 ${changed ? 'bg-red-950/30' : 'bg-bg-primary'}`}
            >
              <span className="text-text-muted">{key}: </span>
              <span
                className={changed ? 'text-red-400' : 'text-text-secondary'}
              >
                {lStr || <span className="text-text-muted italic">—</span>}
              </span>
            </div>
            <div
              className={`px-3 py-1 ${changed ? 'bg-green-950/30' : 'bg-bg-primary'}`}
            >
              <span className="text-text-muted">{key}: </span>
              <span
                className={changed ? 'text-green-400' : 'text-text-secondary'}
              >
                {rStr || <span className="text-text-muted italic">—</span>}
              </span>
            </div>
          </div>
        );
      })}
    </div>
  );
}

function ConflictRow({
  conflict,
  resolution,
  onChange,
  testIdPrefix,
}: {
  conflict: AnnotatedMergeConflict;
  resolution: ConflictResolutionChoice | undefined;
  onChange: (choice: ConflictResolutionChoice) => void;
  testIdPrefix: string;
}) {
  const { t } = useTranslation();
  const groupName = `resolve-${conflict.resolutionKey}`;
  return (
    <div
      className="border border-yellow-700/40 rounded bg-yellow-950/10 p-4 space-y-3"
      data-testid={`${testIdPrefix}-${conflict.resolutionKey}`}
    >
      <div className="flex items-center gap-3 flex-wrap">
        <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-yellow-900/40 text-yellow-300">
          {t('branchReconcile.conflictBadge')}
        </span>
        <ChangeBadge changeType={conflict.changeType} />
        <EntityTypeBadge entityType={conflict.entityType} />
        <span className="text-sm font-mono text-text-primary">
          {conflict.apiName || conflict.entityRid}
        </span>
        <span className="text-xs text-text-muted font-mono ml-auto truncate">
          {conflict.resolutionKey}
        </span>
      </div>

      <SideBySideJson
        left={conflict.mainState ?? null}
        right={conflict.branchState ?? null}
        leftLabel={t('branchReconcile.mainSide')}
        rightLabel={t('branchReconcile.branchSide')}
      />

      <fieldset className="flex items-center gap-4 pt-1">
        <legend className="sr-only">
          {t('branchReconcile.chooseResolution')}
        </legend>
        <label className="flex items-center gap-2 text-xs text-text-secondary cursor-pointer">
          <input
            type="radio"
            name={groupName}
            value="use-main"
            checked={resolution === 'use-main'}
            onChange={() => onChange('use-main')}
            data-testid={`${testIdPrefix}-${conflict.resolutionKey}-use-main`}
            className="accent-accent-cyan"
          />
          <span>{t('branchReconcile.useMain')}</span>
        </label>
        <label className="flex items-center gap-2 text-xs text-text-secondary cursor-pointer">
          <input
            type="radio"
            name={groupName}
            value="use-branch"
            checked={resolution === 'use-branch'}
            onChange={() => onChange('use-branch')}
            data-testid={`${testIdPrefix}-${conflict.resolutionKey}-use-branch`}
            className="accent-accent-cyan"
          />
          <span>{t('branchReconcile.useBranch')}</span>
        </label>
        {resolution === undefined && (
          <span className="text-xs text-yellow-400">
            {t('branchReconcile.unresolvedHint')}
          </span>
        )}
      </fieldset>
    </div>
  );
}

function NonConflictRow({ entry }: { entry: AnnotatedDiffEntry }) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(false);
  const hasDetail =
    entry.changeType === 'MODIFIED' || !!entry.before || !!entry.after;
  return (
    <div className="border border-border rounded bg-bg-secondary">
      <button
        type="button"
        onClick={() => hasDetail && setExpanded((v) => !v)}
        className="w-full flex items-center gap-3 px-4 py-3 text-left hover:bg-bg-tertiary transition-colors"
        data-testid={`reconcile-entry-${entry.resolutionKey}`}
      >
        <ChangeBadge changeType={entry.changeType} />
        <EntityTypeBadge entityType={entry.entityType} />
        <span className="text-sm font-mono text-text-primary truncate flex-1">
          {entry.apiName || entry.entityRid}
        </span>
        {hasDetail && (
          <svg
            className={`w-4 h-4 text-text-muted transition-transform ${expanded ? 'rotate-180' : ''}`}
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
            strokeWidth={2}
            aria-hidden
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              d="M19 9l-7 7-7-7"
            />
          </svg>
        )}
      </button>
      {expanded && hasDetail && (
        <div className="px-4 pb-4">
          <SideBySideJson
            left={entry.before ?? null}
            right={entry.after ?? null}
            leftLabel={t('branchReconcile.mainSide')}
            rightLabel={t('branchReconcile.branchSide')}
          />
        </div>
      )}
    </div>
  );
}

export function BranchReconcilePage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { ontology, branch } = useParams<{ ontology: string; branch: string }>();
  const ontologyApiName = ontology ?? '';
  const branchId = branch ?? '';

  const { data, isLoading, error } = useBranchReconcileDiff(
    ontologyApiName,
    branchId,
  );
  const mergeMutation = useMergeBranch(ontologyApiName, branchId);
  const rebaseMutation = useRebaseBranch(ontologyApiName, branchId);

  const [resolutions, setResolutions] = useState<
    Record<string, ConflictResolutionChoice>
  >({});
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [serverConflicts, setServerConflicts] = useState<
    AnnotatedMergeConflict[] | null
  >(null);
  const [rebaseError, setRebaseError] = useState<string | null>(null);
  const [rebaseSuccess, setRebaseSuccess] = useState<number | null>(null);

  const conflicts = useMemo(
    () => serverConflicts ?? data?.conflicts ?? [],
    [serverConflicts, data],
  );
  const allEntries = useMemo<AnnotatedDiffEntry[]>(() => {
    if (!data) return [];
    return [...data.added, ...data.modified, ...data.deleted];
  }, [data]);
  const conflictKeys = useMemo(
    () => new Set(conflicts.map((c) => c.resolutionKey)),
    [conflicts],
  );
  const nonConflictEntries = useMemo(
    () => allEntries.filter((e) => !conflictKeys.has(e.resolutionKey)),
    [allEntries, conflictKeys],
  );

  if (!ontologyApiName || !branchId) {
    return (
      <div className="flex items-center justify-center h-full">
        <EmptyState
          title={t('branchReconcile.missingParamsTitle')}
          description={t('branchReconcile.missingParamsDescription')}
        />
      </div>
    );
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-full">
        <LoadingSpinner size="lg" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex items-center justify-center h-full">
        <EmptyState
          title={t('branchReconcile.loadFailedTitle')}
          description={String(error)}
        />
      </div>
    );
  }

  if (!data) return null;

  const branchClosed = data.branch.status !== 'open';
  const unresolvedCount = conflicts.filter(
    (c) => resolutions[c.resolutionKey] === undefined,
  ).length;
  const canSubmit =
    !branchClosed && !mergeMutation.isPending && unresolvedCount === 0;

  const setChoice = (key: string, choice: ConflictResolutionChoice) => {
    setResolutions((prev) => ({ ...prev, [key]: choice }));
  };

  const onSubmit = () => {
    setSubmitError(null);
    setServerConflicts(null);
    mergeMutation.mutate(
      { conflictResolution: resolutions },
      {
        onSuccess: (resp) => {
          // After a successful merge, jump back to the branches surface.
          navigate(`/explorer/${ontologyApiName}`, {
            state: { mergedBranch: resp.branch.id },
          });
        },
        onError: (err) => {
          if (err instanceof MergeBranchConflictError) {
            setServerConflicts(err.conflicts);
            setSubmitError(t('branchReconcile.submitConflictHint'));
            return;
          }
          if (err instanceof ApiRequestError) {
            setSubmitError(`${err.errorCode}: ${err.errorName}`);
            return;
          }
          setSubmitError(String(err));
        },
      },
    );
  };

  const onRebase = () => {
    if (!window.confirm(t('branchReconcile.rebaseConfirm'))) return;
    setRebaseError(null);
    setRebaseSuccess(null);
    rebaseMutation.mutate(undefined, {
      onSuccess: (resp) => {
        setRebaseSuccess(resp.baseVersion);
      },
      onError: (err) => {
        if (err instanceof RebaseBranchConflictError) {
          setRebaseError(
            t('branchReconcile.rebaseConflictHint', {
              count: err.conflicts.length,
            }),
          );
          return;
        }
        if (err instanceof ApiRequestError) {
          setRebaseError(
            t('branchReconcile.rebaseError', {
              message: `${err.errorCode}: ${err.errorName}`,
            }),
          );
          return;
        }
        setRebaseError(
          t('branchReconcile.rebaseError', { message: String(err) }),
        );
      },
    });
  };

  const totalCount =
    data.added.length + data.modified.length + data.deleted.length;

  return (
    <div className="flex flex-col h-full" data-testid="branch-reconcile-page">
      {/* Header */}
      <div className="px-6 py-4 border-b border-border shrink-0">
        <div className="flex items-center gap-2 text-xs text-text-muted mb-2">
          <Link
            to={`/explorer/${ontologyApiName}`}
            className="hover:text-text-secondary transition-colors"
          >
            {ontologyApiName}
          </Link>
          <span>/</span>
          <span>{t('branchReconcile.crumbBranches')}</span>
          <span>/</span>
          <span className="text-text-primary font-medium">{branchId}</span>
          <span>/</span>
          <span className="text-text-secondary">
            {t('branchReconcile.crumbReconcile')}
          </span>
        </div>
        <div className="flex items-center justify-between gap-4">
          <div>
            <h2 className="text-lg font-semibold text-text-primary">
              {t('branchReconcile.title')}
            </h2>
            <p className="text-xs text-text-secondary mt-0.5">
              {t('branchReconcile.subtitle', {
                branch: data.branch.name,
              })}
            </p>
          </div>
          <div className="flex items-center gap-3 shrink-0">
            <Link
              to={`/explorer/${ontologyApiName}/branches/${branchId}/diff`}
              className="px-3 py-1.5 rounded text-xs text-text-secondary hover:text-text-primary border border-border hover:bg-bg-tertiary transition-colors"
            >
              {t('branchReconcile.viewLegacyDiff')}
            </Link>
            <button
              type="button"
              onClick={onRebase}
              disabled={branchClosed || rebaseMutation.isPending}
              data-testid="branch-reconcile-rebase-button"
              className={`px-3 py-1.5 rounded text-xs font-medium border transition-colors ${
                branchClosed || rebaseMutation.isPending
                  ? 'border-border text-text-muted cursor-not-allowed'
                  : 'border-accent-cyan/40 text-accent-cyan hover:bg-accent-cyan/10'
              }`}
            >
              {rebaseMutation.isPending
                ? t('branchReconcile.rebasing')
                : t('branchReconcile.rebaseButton')}
            </button>
            <button
              type="button"
              onClick={onSubmit}
              disabled={!canSubmit}
              data-testid="branch-reconcile-merge-button"
              className={`px-4 py-1.5 rounded text-xs font-medium transition-colors ${
                canSubmit
                  ? 'bg-accent-cyan text-bg-primary hover:bg-accent-cyan/90'
                  : 'bg-bg-tertiary text-text-muted cursor-not-allowed'
              }`}
            >
              {mergeMutation.isPending
                ? t('branchReconcile.merging')
                : unresolvedCount > 0
                ? t('branchReconcile.resolveBeforeMerge', {
                    count: unresolvedCount,
                  })
                : t('branchReconcile.mergeButton')}
            </button>
          </div>
        </div>

        <div className="flex items-center gap-3 mt-3 flex-wrap">
          <span
            data-testid="reconcile-status"
            className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-bg-tertiary text-text-secondary"
          >
            {t('branchReconcile.statusLabel')}: {data.branch.status}
          </span>
          {data.added.length > 0 && (
            <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs font-medium bg-green-900/30 text-green-400">
              +{data.added.length} {t('branchReconcile.addedShort')}
            </span>
          )}
          {data.modified.length > 0 && (
            <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs font-medium bg-yellow-900/30 text-yellow-400">
              ~{data.modified.length} {t('branchReconcile.modifiedShort')}
            </span>
          )}
          {data.deleted.length > 0 && (
            <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs font-medium bg-red-900/30 text-red-400">
              -{data.deleted.length} {t('branchReconcile.deletedShort')}
            </span>
          )}
          {conflicts.length > 0 && (
            <span
              data-testid="reconcile-conflict-count"
              className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs font-medium bg-yellow-900/30 text-yellow-300"
            >
              ⚠ {conflicts.length} {t('branchReconcile.conflictsShort')}
            </span>
          )}
          {totalCount === 0 && (
            <span className="text-xs text-text-muted">
              {t('branchReconcile.nothingToMerge')}
            </span>
          )}
          {branchClosed && (
            <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-red-900/40 text-red-300">
              {t('branchReconcile.branchNotOpen')}
            </span>
          )}
        </div>

        {submitError && (
          <div
            data-testid="reconcile-error"
            className="mt-3 px-3 py-2 rounded border border-red-700/40 bg-red-950/30 text-xs text-red-300"
          >
            {submitError}
          </div>
        )}

        {mergeMutation.isSuccess && (
          <div
            data-testid="reconcile-success"
            className="mt-3 px-3 py-2 rounded border border-green-700/40 bg-green-950/30 text-xs text-green-300"
          >
            {t('branchReconcile.successMessage', {
              applied: mergeMutation.data?.appliedCount ?? 0,
              skipped: mergeMutation.data?.skippedCount ?? 0,
            })}
          </div>
        )}

        {rebaseError && (
          <div
            data-testid="reconcile-rebase-error"
            className="mt-3 px-3 py-2 rounded border border-red-700/40 bg-red-950/30 text-xs text-red-300"
          >
            {rebaseError}
          </div>
        )}

        {rebaseSuccess !== null && (
          <div
            data-testid="reconcile-rebase-success"
            className="mt-3 px-3 py-2 rounded border border-green-700/40 bg-green-950/30 text-xs text-green-300"
          >
            {t('branchReconcile.rebaseSuccess', { version: rebaseSuccess })}
          </div>
        )}
      </div>

      <div className="flex-1 overflow-auto p-6 space-y-8 max-w-5xl">
        {conflicts.length > 0 && (
          <section data-testid="reconcile-conflicts-section">
            <h3 className="text-sm font-semibold text-text-primary mb-3">
              {t('branchReconcile.conflictsHeading', {
                count: conflicts.length,
              })}
            </h3>
            <div className="space-y-3">
              {conflicts.map((c) => (
                <ConflictRow
                  key={c.resolutionKey}
                  conflict={c}
                  resolution={resolutions[c.resolutionKey]}
                  onChange={(choice) => setChoice(c.resolutionKey, choice)}
                  testIdPrefix="reconcile-conflict"
                />
              ))}
            </div>
          </section>
        )}

        {nonConflictEntries.length > 0 && (
          <section data-testid="reconcile-changes-section">
            <h3 className="text-sm font-semibold text-text-primary mb-3">
              {t('branchReconcile.changesHeading', {
                count: nonConflictEntries.length,
              })}
            </h3>
            <div className="space-y-2">
              {nonConflictEntries.map((entry) => (
                <NonConflictRow
                  key={`${entry.resolutionKey}:${entry.entityRid}`}
                  entry={entry}
                />
              ))}
            </div>
          </section>
        )}

        {totalCount === 0 && (
          <EmptyState
            title={t('branchReconcile.emptyTitle')}
            description={t('branchReconcile.emptyDescription')}
          />
        )}
      </div>
    </div>
  );
}
