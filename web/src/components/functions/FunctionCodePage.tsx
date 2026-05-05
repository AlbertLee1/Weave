import {
  useCallback,
  useMemo,
  useRef,
  useState,
  type ChangeEvent,
} from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useParams } from 'react-router';
import ReactDiffViewer, { DiffMethod } from 'react-diff-viewer-continued';
import {
  createFunctionRepoCommit,
  getFunction,
  getFunctionRepoCommit,
  listFunctionRepoCommits,
  type FunctionRepoCommit,
} from '../../api/functions';
import { ApiRequestError } from '../../api/client';
import { LoadingSpinner } from '../common/LoadingSpinner';

// US-455: Function Code Repository UI. The page wires the same per-Function
// bare-git surface as US-415/US-416/US-417 into a Monaco-style editing
// experience: commit list on the left, an editable monospace source pane
// in the middle, and a side-by-side diff (against the selected commit) on
// the right. Submitting the toolbar form POSTs a new commit which then
// shows up in the list once the log query refetches.
//
// The PRD AC names "Monaco editor" but the codebase deliberately avoids
// pulling in `monaco-editor` (multi-MB bundle) — the same trade-off
// FunctionDiffPage documents. We approximate the Monaco look-and-feel with
// a `<textarea>` + line-number gutter (`MonacoLikeEditor`); read-only
// fallback is just the same surface with `readOnly` so the rendering path
// stays the same.

export function FunctionCodePage() {
  const params = useParams<{ ontology: string; functionRid: string }>();
  const ontologyApiName = params.ontology ?? '';
  const functionRid = params.functionRid ?? '';
  const queryClient = useQueryClient();

  const fnQuery = useQuery({
    queryKey: ['function-code', 'function', ontologyApiName, functionRid],
    queryFn: () => getFunction(ontologyApiName, functionRid),
    enabled: ontologyApiName !== '' && functionRid !== '',
    retry: false,
  });

  const logQueryKey = useMemo(
    () => ['function-code', 'log', ontologyApiName, functionRid] as const,
    [ontologyApiName, functionRid],
  );
  const logQuery = useQuery({
    queryKey: logQueryKey,
    queryFn: () => listFunctionRepoCommits(ontologyApiName, functionRid),
    enabled: ontologyApiName !== '' && functionRid !== '',
    retry: false,
  });

  const commits = useMemo<FunctionRepoCommit[]>(
    () => logQuery.data?.data ?? [],
    [logQuery.data],
  );

  // The commit selected for the diff base. Defaults to the most recent
  // commit so the editor starts comparing against the current HEAD.
  const [selectedHash, setSelectedHash] = useState<string | null>(null);
  const effectiveSelected = selectedHash ?? commits[0]?.hash ?? null;

  const selectedSourceQuery = useQuery({
    queryKey: [
      'function-code',
      'commit',
      ontologyApiName,
      functionRid,
      effectiveSelected,
    ],
    queryFn: () =>
      getFunctionRepoCommit(
        ontologyApiName,
        functionRid,
        effectiveSelected as string,
      ),
    enabled: !!effectiveSelected,
    retry: false,
  });

  // Editor draft state. The draft starts from the function's working-copy
  // sourceCode (set on first load) and tracks subsequent edits. Picking a
  // commit from the list does NOT overwrite the draft — that would silently
  // drop typed-but-uncommitted changes; the operator clicks "Load into
  // editor" explicitly when they want to checkout. The seed runs render-
  // phase (OfflineIndicator pattern) so React 19's
  // `react-hooks/set-state-in-effect` rule stays satisfied.
  const [draft, setDraft] = useState<string | null>(null);
  const [draftSeeded, setDraftSeeded] = useState(false);
  const seedSource = fnQuery.data?.sourceCode;
  if (!draftSeeded && typeof seedSource === 'string') {
    setDraftSeeded(true);
    setDraft(seedSource);
  }

  const draftValue = draft ?? '';

  const [message, setMessage] = useState('');

  const commitMutation = useMutation({
    mutationFn: () =>
      createFunctionRepoCommit(ontologyApiName, functionRid, {
        message,
        sourceCode: draftValue,
      }),
    onSuccess: (created) => {
      setMessage('');
      setSelectedHash(created.hash);
      queryClient.invalidateQueries({ queryKey: logQueryKey });
    },
  });

  const handleLoadCommitIntoEditor = useCallback(
    (source: string) => {
      setDraft(source);
    },
    [],
  );

  if (fnQuery.isLoading) {
    return (
      <div
        className="flex flex-col h-full items-center justify-center"
        data-testid="function-code-loading"
      >
        <LoadingSpinner />
      </div>
    );
  }

  if (fnQuery.isError) {
    return (
      <FunctionCodeError
        title="Failed to load function"
        error={fnQuery.error}
      />
    );
  }

  const fnName = fnQuery.data?.name ?? functionRid;
  const fnVersion = fnQuery.data?.version;
  const selectedCommit =
    commits.find((c) => c.hash === effectiveSelected) ?? null;
  const selectedSource = selectedSourceQuery.data?.sourceCode ?? '';
  const sourceLoading = selectedSourceQuery.isLoading;

  const canCommit =
    !commitMutation.isPending && message.trim() !== '' && draftValue !== '';

  return (
    <div className="flex flex-col h-full" data-testid="function-code-page">
      <CodeHeader fnName={fnName} fnVersion={fnVersion} />

      <CommitToolbar
        message={message}
        onMessageChange={setMessage}
        canCommit={canCommit}
        pending={commitMutation.isPending}
        error={commitMutation.error}
        onCommit={() => commitMutation.mutate()}
      />

      <div className="grid grid-cols-[260px_1fr_1fr] flex-1 min-h-0">
        <CommitListPanel
          commits={commits}
          isLoading={logQuery.isLoading}
          isError={logQuery.isError}
          error={logQuery.error}
          selectedHash={effectiveSelected}
          onSelect={setSelectedHash}
          onLoadIntoEditor={() =>
            handleLoadCommitIntoEditor(selectedSource)
          }
          loadDisabled={!effectiveSelected || sourceLoading}
        />

        <section
          className="flex flex-col min-h-0 border-r border-border"
          data-testid="function-code-editor-pane"
        >
          <PaneHeader title="Editor" subtitle="working copy" />
          <MonacoLikeEditor
            value={draftValue}
            onChange={setDraft}
            disabled={commitMutation.isPending}
          />
        </section>

        <section
          className="flex flex-col min-h-0"
          data-testid="function-code-diff-pane"
        >
          <PaneHeader
            title="Diff"
            subtitle={
              selectedCommit
                ? `${shortHash(selectedCommit.hash)} → editor`
                : 'no commit selected'
            }
          />
          <div className="flex-1 overflow-auto">
            {!effectiveSelected ? (
              <DiffPlaceholder>
                Pick a commit on the left to compare its source against the
                editor.
              </DiffPlaceholder>
            ) : sourceLoading ? (
              <div
                className="flex items-center justify-center py-12"
                data-testid="function-code-diff-loading"
              >
                <LoadingSpinner />
              </div>
            ) : selectedSourceQuery.isError ? (
              <FunctionCodeError
                title="Failed to load commit source"
                error={selectedSourceQuery.error}
              />
            ) : (
              <div data-testid="function-code-diff-viewer">
                <ReactDiffViewer
                  oldValue={selectedSource}
                  newValue={draftValue}
                  splitView
                  useDarkTheme
                  disableWorker
                  compareMethod={DiffMethod.LINES}
                  leftTitle={
                    selectedCommit
                      ? `${shortHash(selectedCommit.hash)} · ${selectedCommit.author || 'unknown'}`
                      : 'commit'
                  }
                  rightTitle="editor (working copy)"
                />
              </div>
            )}
          </div>
        </section>
      </div>
    </div>
  );
}

interface CodeHeaderProps {
  fnName: string;
  fnVersion?: string;
}

function CodeHeader({ fnName, fnVersion }: CodeHeaderProps) {
  return (
    <header className="border-b border-border px-6 py-4">
      <h1 className="text-base font-sans font-semibold text-text-primary">
        Function code
      </h1>
      <p
        className="text-xs text-text-secondary mt-1 font-mono"
        data-testid="function-code-subject"
      >
        {fnName}
        {fnVersion ? ` @ ${fnVersion}` : ''}
      </p>
    </header>
  );
}

interface CommitToolbarProps {
  message: string;
  onMessageChange: (value: string) => void;
  canCommit: boolean;
  pending: boolean;
  error: unknown;
  onCommit: () => void;
}

function CommitToolbar({
  message,
  onMessageChange,
  canCommit,
  pending,
  error,
  onCommit,
}: CommitToolbarProps) {
  return (
    <div
      className="flex items-center gap-3 border-b border-border px-6 py-3"
      data-testid="function-code-toolbar"
    >
      <input
        type="text"
        value={message}
        onChange={(e: ChangeEvent<HTMLInputElement>) =>
          onMessageChange(e.target.value)
        }
        placeholder="Commit message"
        disabled={pending}
        className="flex-1 bg-bg-elevated border border-border rounded text-xs font-mono px-3 py-1.5"
        data-testid="function-code-commit-message"
      />
      <button
        type="button"
        onClick={onCommit}
        disabled={!canCommit}
        className="px-4 py-1.5 text-xs font-mono rounded border border-accent-primary/40 bg-accent-primary/15 text-accent-primary disabled:opacity-50 disabled:cursor-not-allowed"
        data-testid="function-code-commit-button"
      >
        {pending ? 'Committing…' : 'Commit'}
      </button>
      {error ? (
        <p
          className="text-xs font-mono text-accent-error truncate max-w-xs"
          data-testid="function-code-commit-error"
          title={formatErrorMessage(error)}
        >
          {formatErrorMessage(error)}
        </p>
      ) : null}
    </div>
  );
}

interface CommitListPanelProps {
  commits: FunctionRepoCommit[];
  isLoading: boolean;
  isError: boolean;
  error: unknown;
  selectedHash: string | null;
  onSelect: (hash: string) => void;
  onLoadIntoEditor: () => void;
  loadDisabled: boolean;
}

function CommitListPanel({
  commits,
  isLoading,
  isError,
  error,
  selectedHash,
  onSelect,
  onLoadIntoEditor,
  loadDisabled,
}: CommitListPanelProps) {
  return (
    <aside
      className="flex flex-col min-h-0 border-r border-border bg-bg-elevated/40"
      data-testid="function-code-commit-list"
    >
      <PaneHeader title="Commits" subtitle={`${commits.length} total`} />
      <div className="flex-1 overflow-auto">
        {isLoading ? (
          <div className="flex items-center justify-center py-8">
            <LoadingSpinner />
          </div>
        ) : isError ? (
          <FunctionCodeError title="Failed to load commits" error={error} />
        ) : commits.length === 0 ? (
          <p
            className="text-xs text-text-secondary px-4 py-6 text-center"
            data-testid="function-code-commit-list-empty"
          >
            No commits yet. Save the editor to record the first revision.
          </p>
        ) : (
          <ul className="divide-y divide-border">
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
                    data-testid={`function-code-commit-row-${c.hash}`}
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
      {commits.length > 0 ? (
        <div className="border-t border-border px-3 py-2">
          <button
            type="button"
            onClick={onLoadIntoEditor}
            disabled={loadDisabled}
            className="w-full px-3 py-1.5 text-[11px] font-mono rounded border border-border bg-bg-primary text-text-secondary disabled:opacity-50 disabled:cursor-not-allowed hover:text-text-primary"
            data-testid="function-code-load-into-editor"
            title="Replace the editor contents with the source from the selected commit"
          >
            Load commit into editor
          </button>
        </div>
      ) : null}
    </aside>
  );
}

interface MonacoLikeEditorProps {
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
}

// MonacoLikeEditor approximates the Monaco look-and-feel without pulling in
// the `monaco-editor` package. The textarea drives the source value while a
// gutter `<pre>` paints line numbers in lockstep — both share the same
// monospace size + line-height so wrapping never desynchronises the two.
// Tab key is captured to insert two spaces (matches Monaco's TS / JS
// default) instead of moving focus to the next form control.
function MonacoLikeEditor({
  value,
  onChange,
  disabled,
}: MonacoLikeEditorProps) {
  const taRef = useRef<HTMLTextAreaElement | null>(null);
  const lineCount = Math.max(1, value.split('\n').length);
  const gutter = useMemo(
    () => Array.from({ length: lineCount }, (_, i) => i + 1).join('\n'),
    [lineCount],
  );

  return (
    <div
      className="flex-1 min-h-0 grid grid-cols-[48px_1fr] bg-bg-primary"
      data-testid="function-code-monaco-like"
    >
      <pre
        aria-hidden
        className="select-none text-right pr-2 py-3 font-mono text-[12px] leading-5 text-text-secondary/60 bg-bg-elevated/30 border-r border-border overflow-hidden"
      >
        {gutter}
      </pre>
      <textarea
        ref={taRef}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        disabled={disabled}
        spellCheck={false}
        wrap="off"
        data-testid="function-code-editor"
        className="w-full h-full resize-none bg-transparent outline-none px-3 py-3 font-mono text-[12px] leading-5 text-text-primary"
        onKeyDown={(e) => {
          if (e.key === 'Tab') {
            e.preventDefault();
            const ta = taRef.current;
            if (!ta) return;
            const start = ta.selectionStart;
            const end = ta.selectionEnd;
            const next = `${value.slice(0, start)}  ${value.slice(end)}`;
            onChange(next);
            requestAnimationFrame(() => {
              ta.selectionStart = ta.selectionEnd = start + 2;
            });
          }
        }}
      />
    </div>
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
      data-testid="function-code-diff-placeholder"
    >
      {children}
    </p>
  );
}

function FunctionCodeError({
  title,
  error,
}: {
  title: string;
  error: unknown;
}) {
  return (
    <div
      className="px-6 py-6 text-sm text-accent-error"
      data-testid="function-code-error"
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
