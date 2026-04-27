import { useEffect, useMemo, useRef, useState } from 'react';
import { ApiRequestError } from '../../api/client';
import type { AIPMessage, AIPThread } from '../../api/aip';
import {
  useAIPMessages,
  useAIPThreads,
  useCreateAIPThread,
  useDeleteAIPThread,
  useSendAIPMessage,
} from '../../hooks/useAIPThreads';
import { EmptyState } from '../common/EmptyState';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { Modal } from '../common/Modal';

const KNOWN_PROVIDERS = ['mock', 'openai', 'anthropic'] as const;
type KnownProvider = (typeof KNOWN_PROVIDERS)[number];

// Per-character delay for the typewriter "streaming" display effect on
// freshly-received assistant replies. Real provider streaming is a future
// enhancement; this gives the UI the streaming feel without a protocol
// change.
const STREAM_TICK_MS = 18;

function describeError(err: unknown): string {
  if (err instanceof ApiRequestError) {
    return `${err.errorName}: ${err.parameters?.reason ?? err.message}`;
  }
  if (err instanceof Error) return err.message;
  return 'Request failed.';
}

function formatTimestamp(value: string): string {
  try {
    return new Date(value).toLocaleString();
  } catch {
    return value;
  }
}

interface NewThreadDraft {
  title: string;
  provider: KnownProvider | string;
  model: string;
  systemPrompt: string;
}

const EMPTY_DRAFT: NewThreadDraft = {
  title: '',
  provider: 'mock',
  model: '',
  systemPrompt: '',
};

export function ThreadsPage() {
  const threadsQuery = useAIPThreads();
  const threads = useMemo<AIPThread[]>(
    () => threadsQuery.data?.threads ?? [],
    [threadsQuery.data],
  );

  const [activeThreadId, setActiveThreadId] = useState<string | null>(null);

  // Auto-select the first thread once the list loads. Guard so manual
  // selections aren't overwritten on refetch.
  useEffect(() => {
    if (activeThreadId !== null) return;
    if (threads.length === 0) return;
    setActiveThreadId(threads[0].id);
  }, [threads, activeThreadId]);

  // Drop the selection when the active thread vanishes (deleted from
  // another tab, etc.).
  useEffect(() => {
    if (activeThreadId === null) return;
    if (!threads.some((t) => t.id === activeThreadId)) {
      setActiveThreadId(threads[0]?.id ?? null);
    }
  }, [threads, activeThreadId]);

  const activeThread = useMemo(
    () => threads.find((t) => t.id === activeThreadId) ?? null,
    [threads, activeThreadId],
  );

  const [newThreadOpen, setNewThreadOpen] = useState(false);
  const [draft, setDraft] = useState<NewThreadDraft>(EMPTY_DRAFT);
  const [draftError, setDraftError] = useState<string | null>(null);

  const createMutation = useCreateAIPThread();
  const deleteMutation = useDeleteAIPThread();

  const openNewThread = () => {
    setDraft(EMPTY_DRAFT);
    setDraftError(null);
    setNewThreadOpen(true);
  };

  const closeNewThread = () => {
    setNewThreadOpen(false);
    setDraftError(null);
  };

  const submitDraft = () => {
    const provider = draft.provider.trim();
    if (!provider) {
      setDraftError('Provider is required.');
      return;
    }
    setDraftError(null);
    createMutation.mutate(
      {
        title: draft.title.trim() || undefined,
        provider,
        model: draft.model.trim() || undefined,
        systemPrompt: draft.systemPrompt.trim() || undefined,
      },
      {
        onSuccess: (created) => {
          setActiveThreadId(created.id);
          closeNewThread();
        },
        onError: (err) => setDraftError(describeError(err)),
      },
    );
  };

  const onDelete = (threadId: string) => {
    if (typeof window !== 'undefined') {
      // eslint-disable-next-line no-alert
      const ok = window.confirm('Delete this thread? This cannot be undone.');
      if (!ok) return;
    }
    deleteMutation.mutate(threadId, {
      onSuccess: () => {
        if (activeThreadId === threadId) {
          setActiveThreadId(null);
        }
      },
    });
  };

  return (
    <div className="mx-auto flex h-[calc(100vh-9rem)] max-w-7xl gap-4">
      <ThreadList
        threads={threads}
        loading={threadsQuery.isLoading}
        error={threadsQuery.error}
        activeThreadId={activeThreadId}
        onSelect={setActiveThreadId}
        onNew={openNewThread}
        onDelete={onDelete}
      />
      <ThreadConversation thread={activeThread} />

      <Modal
        open={newThreadOpen}
        onClose={closeNewThread}
        title="New Thread"
        size="lg"
      >
        <div className="space-y-4">
          <label className="flex flex-col gap-1.5 text-xs text-text-secondary">
            Title (optional)
            <input
              type="text"
              value={draft.title}
              onChange={(e) =>
                setDraft((d) => ({ ...d, title: e.target.value }))
              }
              placeholder="Untitled conversation"
              data-testid="new-thread-title"
              className="rounded-md border border-border/50 bg-bg-primary px-2.5 py-2 text-sm text-text-primary outline-none focus:border-amber-500/60"
            />
          </label>
          <label className="flex flex-col gap-1.5 text-xs text-text-secondary">
            Provider
            <select
              value={draft.provider}
              onChange={(e) =>
                setDraft((d) => ({ ...d, provider: e.target.value }))
              }
              data-testid="new-thread-provider"
              className="rounded-md border border-border/50 bg-bg-primary px-2.5 py-2 text-sm text-text-primary outline-none focus:border-amber-500/60"
            >
              {KNOWN_PROVIDERS.map((p) => (
                <option key={p} value={p}>
                  {p}
                </option>
              ))}
            </select>
          </label>
          <label className="flex flex-col gap-1.5 text-xs text-text-secondary">
            Model (optional)
            <input
              type="text"
              value={draft.model}
              onChange={(e) =>
                setDraft((d) => ({ ...d, model: e.target.value }))
              }
              placeholder="leave blank to use the provider default"
              data-testid="new-thread-model"
              className="rounded-md border border-border/50 bg-bg-primary px-2.5 py-2 font-mono text-xs text-text-primary outline-none focus:border-amber-500/60"
            />
          </label>
          <label className="flex flex-col gap-1.5 text-xs text-text-secondary">
            System Prompt (optional)
            <textarea
              value={draft.systemPrompt}
              onChange={(e) =>
                setDraft((d) => ({ ...d, systemPrompt: e.target.value }))
              }
              rows={4}
              placeholder="Optional system instruction prepended to every completion."
              data-testid="new-thread-system"
              className="rounded-md border border-border/50 bg-bg-primary px-2.5 py-2 font-mono text-xs text-text-primary outline-none focus:border-amber-500/60"
            />
          </label>
          {draftError && (
            <div
              role="alert"
              className="rounded-md border border-rose-500/40 bg-rose-500/10 px-3 py-2 text-xs text-rose-300"
            >
              {draftError}
            </div>
          )}
          <div className="flex justify-end gap-2">
            <button
              type="button"
              onClick={closeNewThread}
              className="rounded-md border border-border/60 px-3 py-1.5 text-xs text-text-secondary hover:bg-bg-tertiary"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={submitDraft}
              disabled={createMutation.isPending}
              data-testid="new-thread-submit"
              className="rounded-md bg-amber-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-amber-500 disabled:opacity-60"
            >
              {createMutation.isPending ? 'Creating…' : 'Create'}
            </button>
          </div>
        </div>
      </Modal>
    </div>
  );
}

interface ThreadListProps {
  threads: AIPThread[];
  loading: boolean;
  error: unknown;
  activeThreadId: string | null;
  onSelect: (id: string) => void;
  onNew: () => void;
  onDelete: (id: string) => void;
}

function ThreadList({
  threads,
  loading,
  error,
  activeThreadId,
  onSelect,
  onNew,
  onDelete,
}: ThreadListProps) {
  return (
    <aside
      className="flex w-72 shrink-0 flex-col rounded-lg border border-border/50 bg-bg-secondary/60"
      aria-label="Thread list"
      data-testid="thread-list"
    >
      <div className="flex items-center justify-between border-b border-border/50 px-3 py-3">
        <div>
          <h2 className="text-sm font-semibold text-text-primary tracking-tight">
            AIP Threads
          </h2>
          <p className="text-[11px] text-text-secondary">
            Conversations with configured LLM providers.
          </p>
        </div>
        <button
          type="button"
          onClick={onNew}
          data-testid="new-thread-btn"
          className="rounded-md bg-amber-600 px-2.5 py-1 text-xs font-semibold text-white hover:bg-amber-500"
        >
          New
        </button>
      </div>
      <div className="flex-1 overflow-y-auto">
        {loading ? (
          <div className="flex items-center justify-center py-10">
            <LoadingSpinner />
          </div>
        ) : error ? (
          <div
            role="alert"
            className="m-3 rounded-md border border-rose-500/40 bg-rose-500/10 px-3 py-2 text-xs text-rose-300"
          >
            {describeError(error)}
          </div>
        ) : threads.length === 0 ? (
          <div className="px-3 py-10 text-center text-xs text-text-secondary">
            No threads yet. Click <span className="font-mono">New</span> to start.
          </div>
        ) : (
          <ul className="divide-y divide-border/40">
            {threads.map((thread) => (
              <li key={thread.id}>
                <button
                  type="button"
                  onClick={() => onSelect(thread.id)}
                  data-testid="thread-list-item"
                  data-thread-id={thread.id}
                  aria-current={activeThreadId === thread.id ? 'true' : undefined}
                  className={`flex w-full flex-col items-start gap-0.5 px-3 py-2.5 text-left transition-colors ${
                    activeThreadId === thread.id
                      ? 'bg-bg-tertiary'
                      : 'hover:bg-bg-tertiary/60'
                  }`}
                >
                  <div className="flex w-full items-center justify-between gap-2">
                    <span className="truncate text-sm text-text-primary">
                      {thread.title?.trim() || 'Untitled conversation'}
                    </span>
                    <span
                      className="shrink-0 rounded-full bg-amber-500/10 px-1.5 py-0.5 font-mono text-[10px] uppercase tracking-wider text-amber-400"
                    >
                      {thread.provider}
                    </span>
                  </div>
                  <div className="flex w-full items-center justify-between gap-2 text-[10px] text-text-secondary">
                    <span className="font-mono truncate" title={thread.model || ''}>
                      {thread.model || '—'}
                    </span>
                    <button
                      type="button"
                      onClick={(e) => {
                        e.stopPropagation();
                        onDelete(thread.id);
                      }}
                      data-testid="thread-delete-btn"
                      className="text-rose-400 hover:text-rose-300"
                      aria-label={`Delete thread ${thread.id}`}
                    >
                      Delete
                    </button>
                  </div>
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </aside>
  );
}

interface ThreadConversationProps {
  thread: AIPThread | null;
}

function ThreadConversation({ thread }: ThreadConversationProps) {
  if (!thread) {
    return (
      <section className="flex flex-1 items-center justify-center rounded-lg border border-border/50 bg-bg-secondary/40">
        <EmptyState
          title="No thread selected"
          description="Pick a conversation on the left, or create a new one to start chatting."
        />
      </section>
    );
  }
  return <ThreadConversationActive thread={thread} />;
}

function ThreadConversationActive({ thread }: { thread: AIPThread }) {
  const messagesQuery = useAIPMessages(thread.id);
  const messages = useMemo<AIPMessage[]>(
    () => messagesQuery.data?.messages ?? [],
    [messagesQuery.data],
  );

  const [composerValue, setComposerValue] = useState('');
  const [composerError, setComposerError] = useState<string | null>(null);

  // Track which assistant message id is currently "streaming" via the
  // typewriter effect, plus how many characters have been revealed.
  // streamingMessageId is set when SendMessage returns; the effect below
  // ticks streamingChars upward.
  const [streamingMessageId, setStreamingMessageId] = useState<number | null>(null);
  const [streamingChars, setStreamingChars] = useState(0);

  const sendMutation = useSendAIPMessage(thread.id);

  const scrollAnchorRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    // jsdom (vitest) lacks scrollIntoView — guard so unit tests don't crash.
    const fn = scrollAnchorRef.current?.scrollIntoView;
    if (typeof fn === 'function') {
      scrollAnchorRef.current!.scrollIntoView({ behavior: 'smooth' });
    }
  }, [messages, streamingChars]);

  // Streaming display tick. Reveals one character per STREAM_TICK_MS until
  // the assistant message is fully visible. Cleanup on dependency change
  // prevents leaks when the user navigates away mid-animation.
  useEffect(() => {
    if (streamingMessageId === null) return;
    const target = messages.find((m) => m.id === streamingMessageId);
    if (!target) {
      setStreamingMessageId(null);
      return;
    }
    if (streamingChars >= target.content.length) return;
    const handle = setTimeout(() => {
      setStreamingChars((n) => Math.min(n + 1, target.content.length));
    }, STREAM_TICK_MS);
    return () => clearTimeout(handle);
  }, [streamingMessageId, streamingChars, messages]);

  const submit = () => {
    const trimmed = composerValue.trim();
    if (!trimmed) {
      setComposerError('Please enter a message.');
      return;
    }
    setComposerError(null);
    sendMutation.mutate(
      { content: trimmed },
      {
        onSuccess: (resp) => {
          setComposerValue('');
          setStreamingMessageId(resp.assistantMessage.id);
          setStreamingChars(0);
        },
        onError: (err) => setComposerError(describeError(err)),
      },
    );
  };

  const onKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      submit();
    }
  };

  return (
    <section
      className="flex flex-1 flex-col rounded-lg border border-border/50 bg-bg-secondary/60"
      data-testid="thread-conversation"
      aria-label={`Conversation: ${thread.title || thread.id}`}
    >
      <header className="flex items-center justify-between border-b border-border/50 px-4 py-3">
        <div className="space-y-0.5">
          <h2 className="text-sm font-semibold text-text-primary tracking-tight">
            {thread.title?.trim() || 'Untitled conversation'}
          </h2>
          <div className="flex flex-wrap gap-3 text-[11px] text-text-secondary">
            <span>
              Provider
              <span className="ml-1 font-mono text-text-primary">
                {thread.provider}
              </span>
            </span>
            <span>
              Model
              <span className="ml-1 font-mono text-text-primary">
                {thread.model || '—'}
              </span>
            </span>
            <span>
              Created
              <span className="ml-1 text-text-primary">
                {formatTimestamp(thread.createdAt)}
              </span>
            </span>
          </div>
        </div>
      </header>

      <div
        className="flex-1 overflow-y-auto px-4 py-4"
        data-testid="thread-messages"
      >
        {messagesQuery.isLoading ? (
          <div className="flex items-center justify-center py-12">
            <LoadingSpinner />
          </div>
        ) : messagesQuery.isError ? (
          <div
            role="alert"
            className="rounded-md border border-rose-500/40 bg-rose-500/10 px-3 py-2 text-xs text-rose-300"
          >
            {describeError(messagesQuery.error)}
          </div>
        ) : messages.length === 0 ? (
          <div className="py-10 text-center text-xs text-text-secondary">
            No messages yet. Send the first message to get started.
          </div>
        ) : (
          <ul className="space-y-4">
            {messages.map((msg) => {
              const isStreaming = msg.id === streamingMessageId;
              const renderedContent = isStreaming
                ? msg.content.slice(0, streamingChars)
                : msg.content;
              return (
                <MessageBubble
                  key={msg.id}
                  message={msg}
                  rendered={renderedContent}
                  streaming={
                    isStreaming && streamingChars < msg.content.length
                  }
                />
              );
            })}
            {sendMutation.isPending && <PendingAssistantPlaceholder />}
          </ul>
        )}
        <div ref={scrollAnchorRef} aria-hidden />
      </div>

      <footer className="border-t border-border/50 p-3">
        {composerError && (
          <div
            role="alert"
            data-testid="composer-error"
            className="mb-2 rounded-md border border-rose-500/40 bg-rose-500/10 px-3 py-2 text-xs text-rose-300"
          >
            {composerError}
          </div>
        )}
        <div className="flex items-end gap-2">
          <textarea
            value={composerValue}
            onChange={(e) => setComposerValue(e.target.value)}
            onKeyDown={onKeyDown}
            rows={2}
            placeholder="Type a message and press Enter."
            data-testid="composer-input"
            disabled={sendMutation.isPending}
            className="flex-1 resize-y rounded-md border border-border/50 bg-bg-primary px-3 py-2 text-sm text-text-primary outline-none focus:border-amber-500/60 disabled:opacity-60"
          />
          <button
            type="button"
            onClick={submit}
            disabled={sendMutation.isPending}
            data-testid="composer-send"
            className="h-10 rounded-md bg-amber-600 px-4 text-xs font-semibold text-white hover:bg-amber-500 disabled:opacity-60"
          >
            {sendMutation.isPending ? 'Sending…' : 'Send'}
          </button>
        </div>
      </footer>
    </section>
  );
}

interface MessageBubbleProps {
  message: AIPMessage;
  rendered: string;
  streaming: boolean;
}

function MessageBubble({ message, rendered, streaming }: MessageBubbleProps) {
  const isUser = message.role === 'user';
  const isAssistant = message.role === 'assistant';
  return (
    <li
      data-testid="thread-message"
      data-role={message.role}
      data-message-id={message.id}
      className={`flex ${isUser ? 'justify-end' : 'justify-start'}`}
    >
      <div
        className={`max-w-[80%] rounded-lg border px-3 py-2 text-sm ${
          isUser
            ? 'border-amber-500/30 bg-amber-500/10 text-text-primary'
            : isAssistant
            ? 'border-teal-500/30 bg-teal-500/10 text-text-primary'
            : 'border-border/50 bg-bg-tertiary/60 text-text-secondary'
        }`}
      >
        <div className="mb-1 flex items-center gap-2 text-[10px] uppercase tracking-wider text-text-secondary">
          <span className="font-mono">{message.role}</span>
          <span>·</span>
          <span>{formatTimestamp(message.createdAt)}</span>
        </div>
        <div className="whitespace-pre-wrap break-words" data-testid="message-content">
          {rendered}
          {streaming && (
            <span
              className="ml-0.5 inline-block w-2 animate-pulse text-amber-400"
              data-testid="streaming-cursor"
              aria-hidden
            >
              ▌
            </span>
          )}
        </div>
      </div>
    </li>
  );
}

function PendingAssistantPlaceholder() {
  return (
    <li
      data-testid="pending-assistant"
      className="flex justify-start"
    >
      <div className="rounded-lg border border-teal-500/30 bg-teal-500/10 px-3 py-2 text-sm">
        <div className="mb-1 text-[10px] uppercase tracking-wider text-text-secondary">
          assistant
        </div>
        <div className="flex items-center gap-1.5 text-text-secondary">
          <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-teal-400" />
          <span
            className="h-1.5 w-1.5 animate-pulse rounded-full bg-teal-400"
            style={{ animationDelay: '120ms' }}
          />
          <span
            className="h-1.5 w-1.5 animate-pulse rounded-full bg-teal-400"
            style={{ animationDelay: '240ms' }}
          />
        </div>
      </div>
    </li>
  );
}
