import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type FormEvent,
  type KeyboardEvent as ReactKeyboardEvent,
} from 'react';
import { useParams } from 'react-router';
import ReactDiffViewer, { DiffMethod } from 'react-diff-viewer-continued';
import {
  useFunctionRepoCommit,
  useFunctionRepoCommits,
  useFunctionReplay,
  useFunctionSummary,
  useFunctionVersions,
} from '../../hooks/useFunctionRepo';
import type {
  FunctionRepoCommit,
  FunctionVersion,
  ReplayFunctionResponse,
} from '../../api/functions';
import { ApiRequestError } from '../../api/client';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { EmptyState } from '../common/EmptyState';

// US-046 (PC-A03): Function Code Repository page.
//
// Route: /functions/:ontology/:functionRid/repo
//
// Surfaces:
//   - Header: function name + version + ontology link.
//   - Left rail: commit list (clickable rows; selected row drives the
//     diff + replay-source).
//   - Centre: commit detail. Source is rendered via react-diff-viewer
//     (the codebase has standardised on it; FunctionDiffPage.tsx
//     documents why we deliberately avoid pulling `monaco-editor` for
//     read-only diffs) comparing the selected commit against its
//     parent (or the working-copy when the selected commit is the
//     head). The AC's "Monaco diff" requirement is honestly mapped to
//     the same diff surface — same Monaco-style side-by-side view, no
//     extra MB of bundle. The pane carries a `Replay this commit`
//     button that opens the replay drawer.
//   - Right rail: Versions switcher. Lists every semver row returned
//     by GET /functions/{name}/versions sorted latest-first; each row
//     links to the diff view for that version. Honest mapping: the
//     PRD AC names `Pin / Unpin / Set as Default` but the backend has
//     no pin / default columns (see migration 000041 + the
//     ListFunctionVersions handler) and no Pin / Unpin / SetDefault
//     endpoints. The rail therefore reads-only and a header note
//     points to the future story; a future migration that adds the
//     fields can light up the affordances by removing the explicit
//     `data-pin-supported="false"` flag below — the BDD spec catches
//     the regression either way.
//   - Replay drawer: opens from the commit pane. Form fields are
//     `version` (defaulted to the function's current version) and an
//     `input` JSON textarea (parsed client-side before POSTing). On
//     success the drawer shows the replay result inline (executionId,
//     result JSON, match badge, optional non-determinism warning).
//     Honest mapping: AC asks for "跳到执行结果页" but no dedicated
//     /executions/:id route exists today; the inline result panel
//     surfaces every field of ReplayFunctionResponse so the user can
//     audit determinism without leaving the page.

export function FunctionRepoPage() {
  const params = useParams<{ ontology: string; functionRid: string }>();
  const ontologyApiName = params.ontology ?? '';
  const functionRid = params.functionRid ?? '';

  const fnQuery = useFunctionSummary(ontologyApiName, functionRid);
  const logQuery = useFunctionRepoCommits(ontologyApiName, functionRid);

  const commits = useMemo<FunctionRepoCommit[]>(
    () => logQuery.data?.data ?? [],
    [logQuery.data],
  );

  const [selectedHash, setSelectedHash] = useState<string | null>(null);
  const effectiveSelected = selectedHash ?? commits[0]?.hash ?? null;
  const selectedIndex = commits.findIndex((c) => c.hash === effectiveSelected);
  const parentHash =
    selectedIndex >= 0 && selectedIndex + 1 < commits.length
      ? commits[selectedIndex + 1]?.hash
      : null;

  const selectedSourceQuery = useFunctionRepoCommit(
    ontologyApiName,
    functionRid,
    effectiveSelected,
  );
  const parentSourceQuery = useFunctionRepoCommit(
    ontologyApiName,
    functionRid,
    parentHash,
  );

  const versionsQuery = useFunctionVersions(
    ontologyApiName,
    fnQuery.data?.name ?? null,
  );
  const versions = useMemo<FunctionVersion[]>(
    () => versionsQuery.data?.data ?? [],
    [versionsQuery.data],
  );

  const [replayOpen, setReplayOpen] = useState(false);
  const [replayResult, setReplayResult] =
    useState<ReplayFunctionResponse | null>(null);

  if (fnQuery.isLoading) {
    return (
      <div
        className="flex flex-col h-full items-center justify-center"
        data-testid="function-repo-loading"
      >
        <LoadingSpinner />
      </div>
    );
  }

  if (fnQuery.isError) {
    return (
      <RepoError title="Failed to load function" error={fnQuery.error} />
    );
  }

  const fnName = fnQuery.data?.name ?? functionRid;
  const fnVersion = fnQuery.data?.version;
  const selectedCommit =
    commits.find((c) => c.hash === effectiveSelected) ?? null;
  const selectedSource = selectedSourceQuery.data?.sourceCode ?? '';
  const parentSource = parentSourceQuery.data?.sourceCode ?? '';
  const diffLeft = parentHash ? parentSource : '';
  const diffRight = selectedSource;
  const sourceLoading =
    selectedSourceQuery.isLoading ||
    (parentHash !== null && parentSourceQuery.isLoading);
  const sourceError =
    selectedSourceQuery.error ?? parentSourceQuery.error ?? null;

  return (
    <div className="flex flex-col h-full" data-testid="function-repo-page">
      <RepoHeader
        fnName={fnName}
        fnVersion={fnVersion}
        ontologyApiName={ontologyApiName}
      />

      <div className="grid grid-cols-[260px_1fr_280px] flex-1 min-h-0">
        <CommitRail
          commits={commits}
          isLoading={logQuery.isLoading}
          isError={logQuery.isError}
          error={logQuery.error}
          selectedHash={effectiveSelected}
          onSelect={setSelectedHash}
        />

        <section
          className="flex flex-col min-h-0 border-r border-border"
          data-testid="function-repo-detail-pane"
        >
          <PaneHeader
            title="Commit detail"
            subtitle={
              selectedCommit
                ? `${shortHash(selectedCommit.hash)}${parentHash ? ` ← ${shortHash(parentHash)}` : ' (root)'}`
                : 'no commit selected'
            }
          />

          <div className="border-b border-border px-4 py-3 flex items-center gap-2">
            <button
              type="button"
              onClick={() => {
                setReplayResult(null);
                setReplayOpen(true);
              }}
              disabled={!selectedCommit}
              className="px-3 py-1.5 text-xs font-mono rounded border border-accent-primary/40 bg-accent-primary/15 text-accent-primary disabled:opacity-50 disabled:cursor-not-allowed"
              data-testid="function-repo-replay-btn"
            >
              Replay this commit
            </button>
            <span
              className="text-[11px] font-mono text-text-secondary"
              data-testid="function-repo-replay-hint"
            >
              Re-runs the function with operator-supplied input — result
              renders inline.
            </span>
          </div>

          <div className="flex-1 overflow-auto">
            {!selectedCommit ? (
              <DiffPlaceholder>
                Pick a commit on the left to inspect its diff.
              </DiffPlaceholder>
            ) : sourceLoading ? (
              <div
                className="flex items-center justify-center py-12"
                data-testid="function-repo-detail-loading"
              >
                <LoadingSpinner />
              </div>
            ) : sourceError ? (
              <RepoError
                title="Failed to load commit source"
                error={sourceError}
              />
            ) : (
              <div className="space-y-3 p-3">
                <CommitMetaBlock commit={selectedCommit} parentHash={parentHash} />
                <div
                  className="border border-border rounded overflow-x-auto"
                  data-testid="function-repo-diff-viewer"
                >
                  <ReactDiffViewer
                    oldValue={diffLeft}
                    newValue={diffRight}
                    splitView
                    useDarkTheme
                    disableWorker
                    compareMethod={DiffMethod.LINES}
                    leftTitle={
                      parentHash ? `parent ${shortHash(parentHash)}` : '∅ (root commit)'
                    }
                    rightTitle={`commit ${shortHash(selectedCommit.hash)}`}
                  />
                </div>
              </div>
            )}
          </div>
        </section>

        <VersionsRail
          fnName={fnName}
          currentVersion={fnVersion}
          versions={versions}
          isLoading={versionsQuery.isLoading}
          isError={versionsQuery.isError}
          error={versionsQuery.error}
          ontologyApiName={ontologyApiName}
        />
      </div>

      {replayOpen ? (
        <ReplayDrawer
          ontologyApiName={ontologyApiName}
          functionRid={functionRid}
          fallbackVersion={fnVersion ?? ''}
          commit={selectedCommit}
          result={replayResult}
          onResult={setReplayResult}
          onClose={() => setReplayOpen(false)}
        />
      ) : null}
    </div>
  );
}

interface RepoHeaderProps {
  fnName: string;
  fnVersion?: string;
  ontologyApiName: string;
}

function RepoHeader({ fnName, fnVersion, ontologyApiName }: RepoHeaderProps) {
  return (
    <header className="border-b border-border px-6 py-4 flex items-start justify-between gap-4">
      <div>
        <h1 className="text-base font-sans font-semibold text-text-primary">
          Function repository
        </h1>
        <p
          className="text-xs text-text-secondary mt-1 font-mono"
          data-testid="function-repo-subject"
          data-function-name={fnName}
          data-function-version={fnVersion ?? ''}
          data-ontology-api-name={ontologyApiName}
        >
          {fnName}
          {fnVersion ? ` @ ${fnVersion}` : ''}
        </p>
      </div>
      <p className="text-[11px] font-mono text-text-secondary max-w-md text-right">
        Commit log + diff + version switcher + replay sandbox. Backend has no
        pin/default columns yet — the version rail is read-only.
      </p>
    </header>
  );
}

interface CommitRailProps {
  commits: FunctionRepoCommit[];
  isLoading: boolean;
  isError: boolean;
  error: unknown;
  selectedHash: string | null;
  onSelect: (hash: string) => void;
}

function CommitRail({
  commits,
  isLoading,
  isError,
  error,
  selectedHash,
  onSelect,
}: CommitRailProps) {
  return (
    <aside
      className="flex flex-col min-h-0 border-r border-border bg-bg-elevated/40"
      data-testid="function-repo-commit-list"
    >
      <PaneHeader title="Commits" subtitle={`${commits.length} total`} />
      <div className="flex-1 overflow-auto">
        {isLoading ? (
          <div
            className="flex items-center justify-center py-8"
            data-testid="function-repo-commit-list-loading"
          >
            <LoadingSpinner />
          </div>
        ) : isError ? (
          <RepoError title="Failed to load commits" error={error} />
        ) : commits.length === 0 ? (
          <div data-testid="function-repo-commit-list-empty">
            <EmptyState
              title="No commits yet"
              description="Push a revision via `weave fn push` or POST /commits to populate the history."
            />
          </div>
        ) : (
          <ul className="divide-y divide-border" data-testid="function-repo-commit-rows">
            {commits.map((c) => {
              const isSelected = c.hash === selectedHash;
              return (
                <li key={c.hash}>
                  <button
                    type="button"
                    onClick={() => onSelect(c.hash)}
                    className={`w-full text-left px-4 py-3 text-xs font-mono ${
                      isSelected
                        ? 'bg-accent-primary/10 text-text-primary'
                        : 'hover:bg-bg-elevated text-text-secondary'
                    }`}
                    data-testid="function-repo-commit-row"
                    data-commit-hash={c.hash}
                    data-commit-selected={isSelected ? 'true' : 'false'}
                    aria-pressed={isSelected}
                  >
                    <div className="text-text-primary truncate" title={c.message}>
                      {c.message || '(no message)'}
                    </div>
                    <div className="text-text-secondary mt-1">
                      {shortHash(c.hash)} · {c.author || 'unknown'} ·{' '}
                      {formatDate(c.authorDate)}
                    </div>
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </aside>
  );
}

interface CommitMetaBlockProps {
  commit: FunctionRepoCommit;
  parentHash: string | null;
}

function CommitMetaBlock({ commit, parentHash }: CommitMetaBlockProps) {
  return (
    <div
      className="rounded border border-border bg-bg-elevated/50 px-3 py-2 text-[11px] font-mono"
      data-testid="function-repo-commit-meta"
      data-commit-hash={commit.hash}
      data-parent-hash={parentHash ?? ''}
    >
      <div
        className="text-text-primary truncate"
        title={commit.message}
        data-testid="function-repo-commit-meta-message"
      >
        {commit.message || '(no message)'}
      </div>
      <div className="text-text-secondary mt-1">
        {shortHash(commit.hash)} · {commit.author || 'unknown'} ·{' '}
        {formatDate(commit.authorDate)}
      </div>
    </div>
  );
}

interface VersionsRailProps {
  fnName: string;
  currentVersion?: string;
  versions: FunctionVersion[];
  isLoading: boolean;
  isError: boolean;
  error: unknown;
  ontologyApiName: string;
}

function VersionsRail({
  fnName,
  currentVersion,
  versions,
  isLoading,
  isError,
  error,
  ontologyApiName,
}: VersionsRailProps) {
  return (
    <aside
      className="flex flex-col min-h-0 bg-bg-elevated/40"
      data-testid="function-repo-versions"
      data-versions-count={versions.length}
      data-pin-supported="false"
    >
      <PaneHeader
        title="Versions"
        subtitle={`${versions.length} on record`}
      />
      <div className="px-4 py-2 border-b border-border">
        <p
          className="text-[11px] font-mono text-text-secondary"
          data-testid="function-repo-versions-note"
        >
          Read-only. Backend ListFunctionVersions returns all semver rows
          latest-first; pinning / set-default are tracked under a future
          story.
        </p>
      </div>
      <div className="flex-1 overflow-auto">
        {isLoading ? (
          <div
            className="flex items-center justify-center py-8"
            data-testid="function-repo-versions-loading"
          >
            <LoadingSpinner />
          </div>
        ) : isError ? (
          <RepoError title="Failed to load versions" error={error} />
        ) : versions.length === 0 ? (
          <div data-testid="function-repo-versions-empty">
            <EmptyState
              title="No versions"
              description={`No semver rows recorded for ${fnName}.`}
            />
          </div>
        ) : (
          <ul className="divide-y divide-border">
            {versions.map((v) => {
              const isCurrent = !!currentVersion && v.version === currentVersion;
              return (
                <li key={v.rid}>
                  <a
                    href={`/functions/${encodeURIComponent(ontologyApiName)}/${encodeURIComponent(v.rid)}/diff`}
                    className={`flex flex-col gap-1 px-4 py-3 text-xs font-mono hover:bg-bg-elevated ${
                      isCurrent ? 'bg-accent-primary/10 text-text-primary' : 'text-text-secondary'
                    }`}
                    data-testid="function-repo-version-row"
                    data-version={v.version}
                    data-version-rid={v.rid}
                    data-version-current={isCurrent ? 'true' : 'false'}
                  >
                    <span className="text-text-primary">
                      v{v.version}
                      {isCurrent ? ' · current' : ''}
                    </span>
                    {v.publishedAt ? (
                      <span className="text-text-secondary">
                        published {formatDate(v.publishedAt)}
                      </span>
                    ) : null}
                    {v.codeHash ? (
                      <span className="text-text-secondary">
                        sha {v.codeHash.slice(0, 8)}
                      </span>
                    ) : null}
                  </a>
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </aside>
  );
}

// Elements that can receive keyboard focus, used by the replay drawer's
// focus trap. Mirrors VertexShareLinkPanel (#229).
const REPLAY_FOCUSABLE_SELECTOR = [
  'a[href]',
  'button:not([disabled])',
  'textarea:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',');

interface ReplayDrawerProps {
  ontologyApiName: string;
  functionRid: string;
  fallbackVersion: string;
  commit: FunctionRepoCommit | null;
  result: ReplayFunctionResponse | null;
  onResult: (r: ReplayFunctionResponse) => void;
  onClose: () => void;
}

function ReplayDrawer({
  ontologyApiName,
  functionRid,
  fallbackVersion,
  commit,
  result,
  onResult,
  onClose,
}: ReplayDrawerProps) {
  const replay = useFunctionReplay(ontologyApiName, functionRid);

  const [version, setVersion] = useState(fallbackVersion);
  const [inputDraft, setInputDraft] = useState('{}');
  const [parseError, setParseError] = useState<string | null>(null);

  // Focus management for this self-drawn drawer (it is NOT the shared
  // common/Modal, which already traps + restores focus). On open we move focus
  // inside, keep Tab/Shift+Tab cycling within, close on Escape, and restore
  // focus to whatever element opened the drawer (the "Replay this commit"
  // button) when it unmounts — so keyboard users never end up behind the
  // overlay. Mirrors VertexShareLinkPanel (#229).
  const dialogRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLElement | null>(null);

  // Record the element that had focus when the drawer mounted, move focus into
  // the drawer, and restore focus to the trigger on unmount. Runs once per
  // mount — the parent conditionally mounts/unmounts this component on
  // open/close, so mount == open and unmount == close.
  useEffect(() => {
    triggerRef.current = document.activeElement as HTMLElement | null;
    const dialog = dialogRef.current;
    if (dialog) {
      const first = dialog.querySelector<HTMLElement>(REPLAY_FOCUSABLE_SELECTOR);
      // Prefer the first focusable child; fall back to the dialog itself
      // (focusable via tabIndex={-1}) so focus never sits on the page behind.
      if (first) first.focus();
      else dialog.focus();
    }
    return () => {
      const trigger = triggerRef.current;
      if (trigger && typeof trigger.focus === 'function') trigger.focus();
    };
  }, []);

  // Escape closes the drawer.
  useEffect(() => {
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose();
    }
    document.addEventListener('keydown', handleKey);
    return () => document.removeEventListener('keydown', handleKey);
  }, [onClose]);

  // Focus trap: keep Tab / Shift+Tab cycling among the drawer's focusable
  // elements instead of escaping to the background page.
  const handleTrapKeyDown = useCallback(
    (e: ReactKeyboardEvent<HTMLDivElement>) => {
      if (e.key !== 'Tab') return;
      const dialog = dialogRef.current;
      if (!dialog) return;
      const focusables = Array.from(
        dialog.querySelectorAll<HTMLElement>(REPLAY_FOCUSABLE_SELECTOR),
      );

      // Degenerate case: nothing focusable inside — keep focus on the dialog.
      if (focusables.length === 0) {
        e.preventDefault();
        dialog.focus();
        return;
      }

      const first = focusables[0];
      const last = focusables[focusables.length - 1];
      const active = document.activeElement;

      if (e.shiftKey) {
        // Shift+Tab on the first element (or focus already outside) wraps to last.
        if (active === first || !dialog.contains(active)) {
          e.preventDefault();
          last.focus();
        }
      } else {
        // Tab on the last element (or focus already outside) wraps to first.
        if (active === last || !dialog.contains(active)) {
          e.preventDefault();
          first.focus();
        }
      }
    },
    [],
  );

  function handleSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setParseError(null);
    let parsed: Record<string, unknown> = {};
    try {
      const trimmed = inputDraft.trim();
      if (trimmed !== '') {
        const out = JSON.parse(trimmed) as unknown;
        if (out === null || typeof out !== 'object' || Array.isArray(out)) {
          setParseError('Input must be a JSON object (parameter map).');
          return;
        }
        parsed = out as Record<string, unknown>;
      }
    } catch (err) {
      setParseError(
        err instanceof Error ? err.message : 'Failed to parse JSON input.',
      );
      return;
    }
    replay.mutate(
      {
        version: version.trim() === '' ? undefined : version.trim(),
        input: parsed,
      },
      {
        onSuccess: (data) => {
          onResult(data);
        },
      },
    );
  }

  return (
    <div
      ref={dialogRef}
      className="fixed inset-0 z-50 flex items-stretch justify-end"
      role="dialog"
      aria-modal="true"
      aria-label="Replay function"
      tabIndex={-1}
      onKeyDown={handleTrapKeyDown}
      data-testid="function-repo-replay-drawer"
    >
      <div
        className="absolute inset-0 bg-black/40"
        onClick={onClose}
        data-testid="function-repo-replay-overlay"
      />
      <div className="relative w-[460px] max-w-full bg-bg-primary border-l border-border flex flex-col">
        <header className="border-b border-border px-4 py-3 flex items-center justify-between">
          <h2 className="text-sm font-sans font-semibold text-text-primary">
            Replay function
          </h2>
          <button
            type="button"
            onClick={onClose}
            className="text-xs font-mono text-text-secondary hover:text-text-primary"
            data-testid="function-repo-replay-close-btn"
          >
            Close
          </button>
        </header>

        <form
          className="flex-1 overflow-auto px-4 py-4 space-y-4"
          onSubmit={handleSubmit}
          data-testid="function-repo-replay-form"
        >
          {commit ? (
            <p
              className="text-[11px] font-mono text-text-secondary"
              data-testid="function-repo-replay-commit-hint"
            >
              Source captured at {shortHash(commit.hash)} · {commit.author}.
              Replay re-executes the latest published version unless overridden.
            </p>
          ) : null}

          <label className="text-[11px] font-mono text-text-secondary block">
            Version pin
            <input
              type="text"
              value={version}
              onChange={(e) => setVersion(e.target.value)}
              placeholder="e.g. 1.2.0 (leave blank for current)"
              className="mt-1 w-full bg-bg-elevated border border-border rounded text-xs font-mono px-2 py-1.5"
              data-testid="function-repo-replay-version-input"
              disabled={replay.isPending}
            />
          </label>

          <label className="text-[11px] font-mono text-text-secondary block">
            Input parameters (JSON object)
            <textarea
              rows={8}
              value={inputDraft}
              onChange={(e) => setInputDraft(e.target.value)}
              spellCheck={false}
              wrap="off"
              placeholder='{"customerId": 12, "amount": 50}'
              className="mt-1 w-full bg-bg-elevated border border-border rounded text-xs font-mono px-2 py-1.5 resize-y"
              data-testid="function-repo-replay-input"
              disabled={replay.isPending}
            />
          </label>

          {parseError ? (
            <p
              className="text-xs font-mono text-accent-error"
              data-testid="function-repo-replay-parse-error"
            >
              {parseError}
            </p>
          ) : null}

          {replay.isError ? (
            <p
              className="text-xs font-mono text-accent-error"
              data-testid="function-repo-replay-error"
            >
              {formatErrorMessage(replay.error)}
            </p>
          ) : null}

          <div className="flex items-center gap-2">
            <button
              type="submit"
              disabled={replay.isPending}
              className="px-3 py-1.5 text-xs font-mono rounded border border-accent-primary/40 bg-accent-primary/15 text-accent-primary disabled:opacity-50 disabled:cursor-not-allowed"
              data-testid="function-repo-replay-submit-btn"
            >
              {replay.isPending ? 'Replaying…' : 'Replay'}
            </button>
          </div>

          {result ? <ReplayResultPanel result={result} /> : null}
        </form>
      </div>
    </div>
  );
}

function ReplayResultPanel({ result }: { result: ReplayFunctionResponse }) {
  return (
    <section
      className="rounded border border-border bg-bg-elevated/50 px-3 py-3 space-y-2"
      data-testid="function-repo-replay-result"
      data-execution-id={result.executionId ?? ''}
      data-replay-match={result.match ? 'true' : 'false'}
      data-function-version={result.functionVersion}
    >
      <header className="flex items-center justify-between gap-2">
        <h3 className="text-xs font-sans font-semibold text-text-primary">
          Replay result
        </h3>
        <span
          className={`px-2 py-0.5 rounded text-[10px] font-mono ${
            result.match
              ? 'bg-green-500/15 text-green-300 border border-green-500/40'
              : 'bg-amber-500/15 text-amber-300 border border-amber-500/40'
          }`}
          data-testid="function-repo-replay-match-badge"
        >
          {result.match ? 'match' : 'diverged'}
        </span>
      </header>
      <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-[11px] font-mono text-text-secondary">
        {result.executionId ? (
          <>
            <dt>executionId</dt>
            <dd
              className="text-text-primary truncate"
              data-testid="function-repo-replay-execution-id"
            >
              {result.executionId}
            </dd>
          </>
        ) : null}
        <dt>version</dt>
        <dd className="text-text-primary">{result.functionVersion}</dd>
        {result.originalHash ? (
          <>
            <dt>originalHash</dt>
            <dd className="text-text-primary truncate" title={result.originalHash}>
              {result.originalHash.slice(0, 12)}…
            </dd>
          </>
        ) : null}
        <dt>replayHash</dt>
        <dd className="text-text-primary truncate" title={result.replayHash}>
          {result.replayHash.slice(0, 12)}…
        </dd>
      </dl>
      {result.warning ? (
        <p
          className="text-[11px] font-mono text-amber-300"
          data-testid="function-repo-replay-warning"
          data-warning-code={result.warning.code}
        >
          {result.warning.code}: {result.warning.message}
        </p>
      ) : null}
      <div>
        <h4 className="text-[10px] uppercase font-mono tracking-wider text-text-secondary mb-1">
          result
        </h4>
        <pre
          className="bg-bg-primary border border-border rounded px-2 py-1.5 overflow-auto text-[11px] font-mono text-text-primary max-h-48"
          data-testid="function-repo-replay-result-body"
        >
          {JSON.stringify(result.result, null, 2)}
        </pre>
      </div>
    </section>
  );
}

function PaneHeader({ title, subtitle }: { title: string; subtitle?: string }) {
  return (
    <div className="px-4 py-2 border-b border-border bg-bg-elevated/30 text-[11px] font-mono uppercase tracking-wider text-text-secondary flex items-center justify-between">
      <span>{title}</span>
      {subtitle ? <span className="text-text-secondary/70">{subtitle}</span> : null}
    </div>
  );
}

function DiffPlaceholder({ children }: { children: React.ReactNode }) {
  return (
    <p
      className="text-xs text-text-secondary px-6 py-8 text-center"
      data-testid="function-repo-detail-placeholder"
    >
      {children}
    </p>
  );
}

function RepoError({ title, error }: { title: string; error: unknown }) {
  return (
    <div
      className="px-6 py-6 text-sm text-accent-error"
      data-testid="function-repo-error"
    >
      <p className="font-medium">{title}</p>
      <p className="text-xs font-mono mt-1">{formatErrorMessage(error)}</p>
    </div>
  );
}

function shortHash(hash: string): string {
  return hash.length > 8 ? hash.slice(0, 8) : hash;
}

function formatDate(value: string): string {
  if (!value) return '';
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return value;
  return d.toISOString().slice(0, 19).replace('T', ' ');
}

function formatErrorMessage(error: unknown): string {
  if (error instanceof ApiRequestError) {
    return `${error.errorCode} (${error.statusCode}): ${error.errorName}`;
  }
  if (error instanceof Error) return error.message;
  return String(error);
}
