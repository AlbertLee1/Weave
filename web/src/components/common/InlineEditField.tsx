import { useEffect, useRef, useState } from 'react';
import { useCollabCursorPublisher } from '../../lib/collabPresenceContext';

export interface InlineEditFieldProps {
  value: string;
  onSave: (next: string) => Promise<void>;
  disabled?: boolean;
  placeholder?: string;
  ariaLabel?: string;
  testId?: string;
  /**
   * When set, the input element is tagged with `data-collab-field` and the
   * field's caret/selection is published to the surrounding
   * CollabPresenceProvider. Multiple users editing the same object see one
   * another's cursors via `CollabCursorOverlay`.
   */
  collabFieldKey?: string;
}

// Click to edit, Enter to save, Esc to cancel. The display value flips to the
// caller-supplied draft optimistically as soon as Enter is pressed; if the
// onSave promise rejects the field rolls back to the previous value and
// surfaces the error message inline. Blur with no change cancels silently.
export function InlineEditField({
  value,
  onSave,
  disabled,
  placeholder,
  ariaLabel,
  testId = 'inline-edit',
  collabFieldKey,
}: InlineEditFieldProps) {
  const publishCursor = useCollabCursorPublisher();
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(value);
  // Optimistic value applied while a save is in flight. Cleared on resolve
  // (the parent's prop update overrides) or on rejection (rollback to value).
  const [optimistic, setOptimistic] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  // Track the prop value across renders so a parent refresh drops stale
  // optimistic state without an effect (avoids react-hooks/set-state-in-effect).
  const [prevValue, setPrevValue] = useState(value);
  if (value !== prevValue) {
    setPrevValue(value);
    setOptimistic(null);
  }
  const inputRef = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    if (editing) {
      inputRef.current?.focus();
      inputRef.current?.select();
    }
  }, [editing]);

  const displayValue = optimistic ?? value;

  const reportCursor = () => {
    if (!collabFieldKey) return;
    const el = inputRef.current;
    if (!el) return;
    publishCursor({
      field: collabFieldKey,
      selectionStart: el.selectionStart ?? 0,
      selectionEnd: el.selectionEnd ?? 0,
    });
  };

  if (!editing) {
    return (
      <div className="inline-edit-field">
        <button
          type="button"
          data-testid={`${testId}-display`}
          onClick={() => {
            if (disabled) return;
            setDraft(value);
            setError(null);
            setEditing(true);
          }}
          disabled={disabled}
          className={`block w-full text-left text-xs font-mono break-all whitespace-pre-wrap rounded px-1 py-0.5 -mx-1 -my-0.5 ${
            disabled
              ? 'cursor-default text-text-primary'
              : 'cursor-text text-text-primary hover:bg-bg-tertiary/40'
          }`}
          aria-label={ariaLabel ?? 'Edit value'}
        >
          {displayValue === '' ? (
            <span className="text-text-secondary italic">
              {placeholder ?? '-'}
            </span>
          ) : (
            displayValue
          )}
        </button>
        {error && (
          <span
            data-testid={`${testId}-error`}
            role="alert"
            className="block text-[10px] font-mono text-accent-magenta mt-1"
          >
            {error}
          </span>
        )}
      </div>
    );
  }

  const commit = async () => {
    if (draft === value) {
      setEditing(false);
      setError(null);
      publishCursor(null);
      return;
    }
    const next = draft;
    setOptimistic(next);
    setEditing(false);
    setError(null);
    publishCursor(null);
    try {
      await onSave(next);
      // Successful save — leave optimistic in place; the next prop update
      // will replace it with the canonical value.
    } catch (err) {
      setOptimistic(null);
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const cancel = () => {
    setEditing(false);
    setDraft(value);
    setError(null);
    publishCursor(null);
  };

  return (
    <div className="inline-edit-field">
      <input
        ref={inputRef}
        type="text"
        data-testid={`${testId}-input`}
        data-collab-field={collabFieldKey}
        value={draft}
        placeholder={placeholder}
        aria-label={ariaLabel ?? 'Edit value'}
        onChange={(e) => {
          setDraft(e.target.value);
          reportCursor();
        }}
        onSelect={() => {
          reportCursor();
        }}
        onKeyUp={() => {
          reportCursor();
        }}
        onClick={() => {
          reportCursor();
        }}
        onFocus={() => {
          reportCursor();
        }}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault();
            void commit();
          } else if (e.key === 'Escape') {
            e.preventDefault();
            cancel();
          }
        }}
        onBlur={() => {
          // Blur with no change reverts; with a change, save like Enter.
          if (draft === value) {
            cancel();
          } else {
            void commit();
          }
        }}
        className="block w-full text-xs font-mono px-1 py-0.5 -mx-1 -my-0.5 rounded bg-bg-tertiary/40 border border-accent-cyan/40 outline-none focus:border-accent-cyan"
      />
    </div>
  );
}
