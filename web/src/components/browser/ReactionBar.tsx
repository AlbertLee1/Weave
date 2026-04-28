import { useState } from 'react';
import {
  useCreateReaction,
  useDeleteReaction,
  useReactionSummary,
} from '../../hooks/useReactions';

// DEFAULT_EMOJIS is the small palette offered by the picker. Keep it
// short so the bar doesn't dominate the panel — most reactions converge
// on a handful in practice. Non-default emojis stay reachable via the
// custom-input fallback inside the picker.
const DEFAULT_EMOJIS = ['👍', '❤️', '🎉', '🚀', '👀', '😄', '😢', '🙏'];

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
  const [pickerOpen, setPickerOpen] = useState(false);
  const [customEmoji, setCustomEmoji] = useState('');

  if (!targetRid) return null;
  if (summary.data && summary.data.available === false) return null;

  const buckets = summary.data?.emojis ?? [];
  const pending = create.isPending || remove.isPending;

  const togglePill = (emoji: string, mine: boolean) => {
    if (!targetRid || pending) return;
    if (mine) {
      remove.mutate({ targetRid, emoji });
    } else {
      create.mutate({ targetRid, emoji });
    }
  };

  const onPick = (emoji: string) => {
    if (!targetRid || pending) return;
    const trimmed = emoji.trim();
    if (!trimmed) return;
    const existing = buckets.find((b) => b.emoji === trimmed);
    if (existing && existing.mine) {
      remove.mutate({ targetRid, emoji: trimmed });
    } else {
      create.mutate({ targetRid, emoji: trimmed });
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
          <div
            className="absolute left-0 mt-1 z-20 p-2 bg-surface border border-border rounded shadow-lg"
            data-testid="reaction-picker"
            role="dialog"
            aria-label="Reaction picker"
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
        )}
      </div>
    </div>
  );
}
