import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useParams } from 'react-router';
import ReactDiffViewer, { DiffMethod } from 'react-diff-viewer-continued';
import {
  getFunction,
  getFunctionCommitJob,
  getFunctionRepoCommit,
  listFunctionRepoCommits,
  type CommitJob,
  type FunctionRepoCommit,
} from '../../api/functions';
import { ApiRequestError } from '../../api/client';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { EmptyState } from '../common/EmptyState';

// US-416: Function PR / Diff UI.
//
// The page lists the commits on the per-Function bare git repo (US-415's
// `/log` endpoint), lets the user pick two commits via the From/To
// dropdowns, fetches the source blob at each via `/commits/{hash}`
// (US-416's read endpoint added on top of the same repo), and renders the
// side-by-side diff with `react-diff-viewer-continued` — the same diff
// surface the codebase already uses elsewhere (ObjectDiffPanel). The PRD
// AC names "Monaco editor diff" but the codebase has standardised on
// react-diff-viewer for diffs and `monaco-editor` would add a multi-MB
// dependency for marginal gain on a read-only diff surface; the actual
// editing surface lands in US-455.
//
// Beneath the diff a placeholder line-comment composer is rendered: the
// AC asks for "行级 comment 占位" — a hook for a future review-comment
// thread surface. Submission is disabled today; the placeholder UI just
// proves the wire-up is ready when the comment store ships.

export function FunctionDiffPage() {
  const params = useParams<{ ontology: string; functionRid: string }>();
  const ontologyApiName = params.ontology ?? '';
  const functionRid = params.functionRid ?? '';

  const fnQuery = useQuery({
    queryKey: ['function-diff', 'function', ontologyApiName, functionRid],
    queryFn: () => getFunction(ontologyApiName, functionRid),
    enabled: ontologyApiName !== '' && functionRid !== '',
    retry: false,
  });

  const logQuery = useQuery({
    queryKey: ['function-diff', 'log', ontologyApiName, functionRid],
    queryFn: () => listFunctionRepoCommits(ontologyApiName, functionRid),
    enabled: ontologyApiName !== '' && functionRid !== '',
    retry: false,
  });

  const commits = useMemo<FunctionRepoCommit[]>(
    () => logQuery.data?.data ?? [],
    [logQuery.data],
  );

  const [leftHash, setLeftHash] = useState<string | null>(null);
  const [rightHash, setRightHash] = useState<string | null>(null);

  const effectiveRight = rightHash ?? commits[0]?.hash ?? null;
  const effectiveLeft = leftHash ?? commits[1]?.hash ?? null;

  const leftSourceQuery = useQuery({
    queryKey: ['function-diff', 'commit', ontologyApiName, functionRid, effectiveLeft],
    queryFn: () =>
      getFunctionRepoCommit(ontologyApiName, functionRid, effectiveLeft as string),
    enabled: !!effectiveLeft,
    retry: false,
  });
  const rightSourceQuery = useQuery({
    queryKey: ['function-diff', 'commit', ontologyApiName, functionRid, effectiveRight],
    queryFn: () =>
      getFunctionRepoCommit(ontologyApiName, functionRid, effectiveRight as string),
    enabled: !!effectiveRight,
    retry: false,
  });

  if (fnQuery.isLoading || logQuery.isLoading) {
    return (
      <div
        className="flex flex-col h-full items-center justify-center"
        data-testid="function-diff-loading"
      >
        <LoadingSpinner />
      </div>
    );
  }

  if (fnQuery.isError) {
    return (
      <FunctionDiffError
        title="Failed to load function"
        error={fnQuery.error}
      />
    );
  }
  if (logQuery.isError) {
    return (
      <FunctionDiffError
        title="Failed to load commit history"
        error={logQuery.error}
      />
    );
  }

  if (commits.length === 0) {
    return (
      <div className="flex flex-col h-full" data-testid="function-diff-empty">
        <DiffHeader fnName={fnQuery.data?.name ?? functionRid} fnVersion={fnQuery.data?.version} />
        <div className="flex-1 flex items-center justify-center">
          <EmptyState
            title="No commits yet"
            description={
              'Push a revision via `weave fn push` or POST /commits to populate the diff history.'
            }
          />
        </div>
      </div>
    );
  }

  if (commits.length < 2) {
    return (
      <div className="flex flex-col h-full" data-testid="function-diff-single">
        <DiffHeader fnName={fnQuery.data?.name ?? functionRid} fnVersion={fnQuery.data?.version} />
        <div className="flex-1 flex items-center justify-center">
          <EmptyState
            title="Only one commit on record"
            description="Two commits are required to render a diff. Push another revision to compare."
          />
        </div>
      </div>
    );
  }

  const leftCommit = commits.find((c) => c.hash === effectiveLeft) ?? null;
  const rightCommit = commits.find((c) => c.hash === effectiveRight) ?? null;
  const sameCommit =
    effectiveLeft !== null && effectiveLeft === effectiveRight;

  const leftSource = leftSourceQuery.data?.sourceCode ?? '';
  const rightSource = rightSourceQuery.data?.sourceCode ?? '';
  const sourcesLoading =
    leftSourceQuery.isLoading || rightSourceQuery.isLoading;
  const sourcesError = leftSourceQuery.error ?? rightSourceQuery.error ?? null;

  return (
    <div className="flex flex-col h-full" data-testid="function-diff-page">
      <DiffHeader fnName={fnQuery.data?.name ?? functionRid} fnVersion={fnQuery.data?.version} />
      <div className="flex-1 overflow-y-auto px-6 py-5 space-y-4">
        <CommitPicker
          commits={commits}
          leftHash={effectiveLeft}
          rightHash={effectiveRight}
          onLeftChange={setLeftHash}
          onRightChange={setRightHash}
        />

        {sameCommit ? (
          <p
            className="text-xs text-text-secondary py-4 text-center"
            data-testid="function-diff-same"
          >
            Pick two distinct commits to compare.
          </p>
        ) : sourcesLoading ? (
          <div
            className="flex items-center justify-center py-12"
            data-testid="function-diff-source-loading"
          >
            <LoadingSpinner />
          </div>
        ) : sourcesError ? (
          <FunctionDiffError
            title="Failed to load source"
            error={sourcesError}
          />
        ) : (
          <>
            <CommitSummary leftCommit={leftCommit} rightCommit={rightCommit} />
            <div
              className="border border-border rounded overflow-x-auto"
              data-testid="function-diff-viewer"
            >
              <ReactDiffViewer
                oldValue={leftSource}
                newValue={rightSource}
                splitView
                useDarkTheme
                disableWorker
                compareMethod={DiffMethod.LINES}
                leftTitle={leftCommit ? formatCommitTitle(leftCommit) : 'older'}
                rightTitle={
                  rightCommit ? formatCommitTitle(rightCommit) : 'newer'
                }
              />
            </div>
            <LineCommentPlaceholder />
          </>
        )}
      </div>
    </div>
  );
}

interface DiffHeaderProps {
  fnName: string;
  fnVersion?: string;
}

function DiffHeader({ fnName, fnVersion }: DiffHeaderProps) {
  return (
    <header className="border-b border-border px-6 py-4">
      <h1 className="text-base font-sans font-semibold text-text-primary">
        Function diff
      </h1>
      <p
        className="text-xs text-text-secondary mt-1 font-mono"
        data-testid="function-diff-subject"
      >
        {fnName}
        {fnVersion ? ` @ ${fnVersion}` : ''}
      </p>
    </header>
  );
}

interface CommitPickerProps {
  commits: FunctionRepoCommit[];
  leftHash: string | null;
  rightHash: string | null;
  onLeftChange: (hash: string) => void;
  onRightChange: (hash: string) => void;
}

function CommitPicker({
  commits,
  leftHash,
  rightHash,
  onLeftChange,
  onRightChange,
}: CommitPickerProps) {
  return (
    <div className="grid grid-cols-2 gap-3">
      <label className="text-[11px] font-mono text-text-secondary">
        From commit
        <select
          value={leftHash ?? ''}
          onChange={(e) => onLeftChange(e.target.value)}
          className="mt-1 w-full bg-bg-elevated border border-border rounded text-xs font-mono px-2 py-1"
          data-testid="function-diff-left-select"
        >
          {commits.map((c) => (
            <option key={c.hash} value={c.hash}>
              {formatCommitOption(c)}
            </option>
          ))}
        </select>
      </label>
      <label className="text-[11px] font-mono text-text-secondary">
        To commit
        <select
          value={rightHash ?? ''}
          onChange={(e) => onRightChange(e.target.value)}
          className="mt-1 w-full bg-bg-elevated border border-border rounded text-xs font-mono px-2 py-1"
          data-testid="function-diff-right-select"
        >
          {commits.map((c) => (
            <option key={c.hash} value={c.hash}>
              {formatCommitOption(c)}
            </option>
          ))}
        </select>
      </label>
    </div>
  );
}

interface CommitSummaryProps {
  leftCommit: FunctionRepoCommit | null;
  rightCommit: FunctionRepoCommit | null;
}

function CommitSummary({ leftCommit, rightCommit }: CommitSummaryProps) {
  return (
    <div
      className="grid grid-cols-2 gap-3 text-[11px] font-mono"
      data-testid="function-diff-commit-summary"
    >
      <CommitMeta side="from" commit={leftCommit} />
      <CommitMeta side="to" commit={rightCommit} />
    </div>
  );
}

function CommitMeta({
  side,
  commit,
}: {
  side: 'from' | 'to';
  commit: FunctionRepoCommit | null;
}) {
  if (!commit) return <div />;
  return (
    <div
      className="rounded border border-border bg-bg-elevated px-3 py-2"
      data-testid={`function-diff-meta-${side}`}
    >
      <div className="flex items-start justify-between gap-2">
        <div className="text-text-primary truncate" title={commit.message}>
          {commit.message || '(no message)'}
        </div>
        <CommitJobBadge commitHash={commit.hash} />
      </div>
      <div className="text-text-secondary mt-1">
        {shortHash(commit.hash)} · {commit.author || 'unknown'} ·{' '}
        {formatDate(commit.authorDate)}
      </div>
    </div>
  );
}

// US-417: Per-commit CI status badge. Renders ✅ on success, ❌ on failure,
// ⏳ on queued/running, ⏭️ on skipped, and nothing when the server reports
// no row (older commits made before the hook landed). The tooltip surfaces
// the per-phase output so operators can diagnose without leaving the diff.
function CommitJobBadge({ commitHash }: { commitHash: string }) {
  const params = useParams<{ ontology: string; functionRid: string }>();
  const ontologyApiName = params.ontology ?? '';
  const functionRid = params.functionRid ?? '';
  const jobQuery = useQuery({
    queryKey: ['function-diff', 'job', ontologyApiName, functionRid, commitHash],
    queryFn: () => getFunctionCommitJob(ontologyApiName, functionRid, commitHash),
    enabled: ontologyApiName !== '' && functionRid !== '' && commitHash !== '',
    // Poll until the job reaches a terminal state so the badge reflects
    // the runner's progress without forcing the user to refresh manually.
    refetchInterval: (query) => {
      const job = query.state.data as CommitJob | null | undefined;
      if (!job) return false;
      return job.status === 'queued' || job.status === 'running' ? 1500 : false;
    },
    retry: false,
  });
  const job = jobQuery.data;
  if (!job) return null;
  return (
    <span
      title={tooltipForCommitJob(job)}
      data-testid={`function-commit-job-badge-${job.status}`}
      className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[10px] font-mono ${badgeClassForStatus(job.status)}`}
    >
      <span aria-hidden>{symbolForStatus(job.status)}</span>
      <span>{job.status}</span>
    </span>
  );
}

function symbolForStatus(status: CommitJob['status']): string {
  switch (status) {
    case 'success':
      return '✅';
    case 'failure':
      return '❌';
    case 'skipped':
      return '⏭';
    case 'queued':
    case 'running':
    default:
      return '⏳';
  }
}

function badgeClassForStatus(status: CommitJob['status']): string {
  switch (status) {
    case 'success':
      return 'bg-green-500/15 text-green-300 border border-green-500/40';
    case 'failure':
      return 'bg-red-500/15 text-red-300 border border-red-500/40';
    case 'skipped':
      return 'bg-amber-500/15 text-amber-300 border border-amber-500/40';
    case 'queued':
    case 'running':
    default:
      return 'bg-bg-elevated text-text-secondary border border-border';
  }
}

function tooltipForCommitJob(job: CommitJob): string {
  const lines: string[] = [`status: ${job.status}`];
  if (job.errorMessage) lines.push(`error: ${job.errorMessage}`);
  if (job.lintOutput) lines.push(`lint: ${job.lintOutput}`);
  if (job.testOutput) lines.push(`test: ${job.testOutput}`);
  return lines.join('\n');
}

// LineCommentPlaceholder is the AC's "行级 comment 占位" — a stub composer
// that surfaces the future review thread surface without committing to a
// persistence model. Submission is intentionally disabled.
function LineCommentPlaceholder() {
  const [draft, setDraft] = useState('');
  const [line, setLine] = useState('');
  return (
    <section
      className="border border-dashed border-border rounded px-4 py-3 space-y-2"
      data-testid="function-diff-comment-placeholder"
    >
      <h2 className="text-xs font-mono text-text-secondary uppercase tracking-wider">
        Inline comment (placeholder)
      </h2>
      <div className="grid grid-cols-[120px_1fr] gap-2">
        <input
          type="text"
          placeholder="line"
          value={line}
          onChange={(e) => setLine(e.target.value)}
          className="bg-bg-elevated border border-border rounded text-xs font-mono px-2 py-1"
          data-testid="function-diff-comment-line"
        />
        <textarea
          rows={2}
          placeholder="Comments are not yet persisted — UI surface ready for the future review thread service."
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          className="bg-bg-elevated border border-border rounded text-xs font-mono px-2 py-1 resize-none"
          data-testid="function-diff-comment-text"
        />
      </div>
      <div className="flex justify-end">
        <button
          type="button"
          disabled
          className="px-3 py-1.5 text-xs font-mono rounded border border-border bg-bg-elevated text-text-secondary opacity-60 cursor-not-allowed"
          data-testid="function-diff-comment-submit"
          title="Persistence pending — see future review surface"
        >
          Submit (coming soon)
        </button>
      </div>
    </section>
  );
}

interface FunctionDiffErrorProps {
  title: string;
  error: unknown;
}

function FunctionDiffError({ title, error }: FunctionDiffErrorProps) {
  return (
    <div
      className="px-6 py-6 text-sm text-accent-error"
      data-testid="function-diff-error"
    >
      <p className="font-medium">{title}</p>
      <p className="text-xs font-mono mt-1">{formatErrorMessage(error)}</p>
    </div>
  );
}

function formatCommitOption(c: FunctionRepoCommit): string {
  const message = c.message?.trim() || '(no message)';
  const head = message.length > 60 ? `${message.slice(0, 57)}…` : message;
  return `${shortHash(c.hash)} · ${head}`;
}

function formatCommitTitle(c: FunctionRepoCommit): string {
  return `${shortHash(c.hash)} · ${c.author || 'unknown'}`;
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
