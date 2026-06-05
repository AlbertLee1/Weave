import { useCallback, useEffect, useRef, useState } from 'react';
import * as React from 'react';
import {
  useCreateReaction,
  useDeleteReaction,
  useReactionSummary,
} from '../../hooks/useReactions';
import { describeApiError } from '../../api/describeError';
import { useToastStore } from '../../stores/toastStore';

// Elements that can receive keyboard focus, used by the picker's focus trap.
const FOCUSABLE_SELECTOR = [
  'a[href]',
  'button:not([disabled])',
  'textarea:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',');

// DEFAULT_EMOJIS is the small palette offered by the picker. Keep it
// short so the bar doesn't dominate the panel — most reactions converge
// on a handful in practice. Non-default emojis stay reachable via the
// custom-input fallback inside the picker.
const DEFAULT_EMOJIS = ['👍', '❤️', '🎉', '🚀', '👀', '😄', '😢', '🙏'];

interface ReactionPickerProps {
  customEmoji: string;
  setCustomEmoji: (v: string) => void;
  onPick: (emoji: string) => void;
  onClose: () => void;
}

// ReactionPicker is the self-drawn popover (role="dialog") offered by the + button.
// It is the shared common/Modal-free surface, so it owns its own focus management
// (mirrors VertexShareLinkPanel #229): on open it moves focus inside, keeps
// Tab/Shift+Tab cycling within (focus trap, degrades safely), closes on Escape,
// and restores focus to the trigger button on close. The parent conditionally
// mounts/unmounts this component on open/close, so mount == open and
// unmount == close — which is what makes the focus-restore effect fire at the
// right moment.
function ReactionPicker({
  customEmoji,
  setCustomEmoji,
  onPick,
  onClose,
}: ReactionPickerProps) {
  const dialogRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLElement | null>(null);

  // Record the element that had focus when the picker mounted (the + button),
  // move focus into the dialog, and restore focus to that trigger on unmount so
  // keyboard users never end up stranded behind the popover.
  useEffect(() => {
    triggerRef.current = document.activeElement as HTMLElement | null;
    const dialog = dialogRef.current;
    if (dialog) {
      const first = dialog.querySelector<HTMLElement>(FOCUSABLE_SELECTOR);
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

  // Escape closes the picker.
  useEffect(() => {
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose();
    }
    document.addEventListener('keydown', handleKey);
    return () => document.removeEventListener('keydown', handleKey);
  }, [onClose]);

  // Focus trap: keep Tab / Shift+Tab cycling among the dialog's focusable
  // elements instead of escaping to the background page.
  const handleTrapKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLDivElement>) => {
      if (e.key !== 'Tab') return;
      const dialog = dialogRef.current;
      if (!dialog) return;
      const focusables = Array.from(
        dialog.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR),
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

  return (
    <div
      ref={dialogRef}
      className="absolute left-0 mt-1 z-20 p-2 bg-surface border border-border rounded shadow-lg"
      data-testid="reaction-picker"
      role="dialog"
      aria-modal="true"
      aria-label="Reaction picker"
      tabIndex={-1}
      onKeyDown={handleTrapKeyDown}
    >
      <div className="flex flex-wrap gap-1 max-w-[16rem]">
        {DEFAULT_EMOJIS.map((emoji) => (
          <button
            key={emoji}
            type="button"
            onClick={() => onPick(emoji)}
            data-testid={`reaction-picker-option-${emoji}`}
            className="px-2 py-1 text-base hover:bg-accent-cyan/10 rounded"
          >
            {emoji}
          </button>
        ))}
      </div>
      <form
        className="mt-2 flex items-center gap-1"
        onSubmit={(e) => {
          e.preventDefault();
          onPick(customEmoji);
        }}
      >
        <input
          type="text"
          value={customEmoji}
          onChange={(e) => setCustomEmoji(e.target.value)}
          placeholder="Custom..."
          aria-label="Custom emoji"
          data-testid="reaction-picker-custom-input"
          className="flex-1 px-2 py-1 text-xs bg-bg border border-border rounded text-text-primary"
          maxLength={32}
        />
        <button
          type="submit"
          data-testid="reaction-picker-custom-submit"
          disabled={!customEmoji.trim()}
          className="px-2 py-1 text-xs font-mono rounded border border-border text-text-secondary hover:text-text-primary disabled:opacity-50"
        >
          Add
        </button>
      </form>
    </div>
  );
}

interface ReactionBarProps {
  targetRid: string | null | undefined;
}

// ReactionBar (US-342) renders the aggregate (emoji → count) view for a
// target_rid plus a + button that opens the picker. Clicking an existing
// pill toggles the caller's reaction; the bar hides itself when the
// /api/v2/reactions endpoint is unmounted in degraded-mode deployments
// (same hide-on-404 contract as WatchButton).
export function ReactionBar({ targetRid }: ReactionBarProps) {
  const summary = useReactionSummary(targetRid);
  const create = useCreateReaction();
  const remove = useDeleteReaction();
  const pushToast = useToastStore((s) => s.push);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [customEmoji, setCustomEmoji] = useState('');

  if (!targetRid) return null;
  if (summary.data && summary.data.available === false) return null;

  const buckets = summary.data?.emojis ?? [];
  const pending = create.isPending || remove.isPending;

  const togglePill = (emoji: string, mine: boolean) => {
    if (!targetRid || pending) return;
    if (mine) {
      // Surface unreact failures: the hook's onSuccess invalidates the
      // summary, but without an onError a 5xx/timeout left the pill stuck
      // with zero feedback. Push a user-visible error toast instead.
      remove.mutate(
        { targetRid, emoji },
        {
          onError: (err) =>
            pushToast({
              message: `Failed to remove ${emoji} reaction: ${describeApiError(err, 'Reaction update failed.')}`,
              severity: 'error',
            }),
        },
      );
    } else {
      create.mutate(
        { targetRid, emoji },
        {
          onError: (err) =>
            pushToast({
              message: `Failed to add ${emoji} reaction: ${describeApiError(err, 'Reaction update failed.')}`,
              severity: 'error',
            }),
        },
      );
    }
  };

  const onPick = (emoji: string) => {
    if (!targetRid || pending) return;
    const trimmed = emoji.trim();
    if (!trimmed) return;
    const existing = buckets.find((b) => b.emoji === trimmed);
    if (existing && existing.mine) {
      remove.mutate(
        { targetRid, emoji: trimmed },
        {
          onError: (err) =>
            pushToast({
              message: `Failed to remove ${trimmed} reaction: ${describeApiError(err, 'Reaction update failed.')}`,
              severity: 'error',
            }),
        },
      );
    } else {
      create.mutate(
        { targetRid, emoji: trimmed },
        {
          onError: (err) =>
            pushToast({
              message: `Failed to add ${trimmed} reaction: ${describeApiError(err, 'Reaction update failed.')}`,
              severity: 'error',
            }),
        },
      );
    }
    setCustomEmoji('');
    setPickerOpen(false);
  };

  return (
    <div
      className="flex flex-wrap items-center gap-1"
      data-testid="reaction-bar"
    >
      {buckets.map((b) => (
        <button
          key={b.emoji}
          type="button"
          onClick={() => togglePill(b.emoji, b.mine)}
          aria-pressed={b.mine}
          disabled={pending}
          data-testid={`reaction-pill-${b.emoji}`}
          data-mine={b.mine ? 'true' : 'false'}
          className={[
            'px-2 py-0.5 text-xs font-mono rounded border transition-colors',
            b.mine
              ? 'border-accent-cyan/60 bg-accent-cyan/10 text-accent-cyan hover:bg-accent-cyan/20'
              : 'border-border text-text-secondary hover:text-text-primary hover:border-text-secondary',
            pending ? 'opacity-60 cursor-progress' : '',
          ].join(' ')}
          title={b.mine ? `Remove your ${b.emoji} reaction` : `React with ${b.emoji}`}
        >
          <span aria-hidden="true" className="mr-1">
            {b.emoji}
          </span>
          <span data-testid={`reaction-count-${b.emoji}`}>{b.count}</span>
        </button>
      ))}
      <div className="relative">
        <button
          type="button"
          onClick={() => setPickerOpen((v) => !v)}
          disabled={pending}
          data-testid="reaction-add-button"
          aria-label="Add reaction"
          aria-expanded={pickerOpen}
          className="px-2 py-0.5 text-xs font-mono rounded border border-dashed border-border text-text-secondary hover:text-text-primary hover:border-text-secondary"
        >
          + 😊
        </button>
        {pickerOpen && (
          <ReactionPicker
            customEmoji={customEmoji}
            setCustomEmoji={setCustomEmoji}
            onPick={onPick}
            onClose={() => setPickerOpen(false)}
          />
        )}
      </div>
    </div>
  );
}
