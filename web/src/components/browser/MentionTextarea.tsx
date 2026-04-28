import {
  forwardRef,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import { searchMentionUsers, type MentionUser } from '../../api/mentions';

interface MentionTextareaProps
  extends Omit<
    React.TextareaHTMLAttributes<HTMLTextAreaElement>,
    'onChange' | 'value' | 'ref'
  > {
  value: string;
  onChange: (value: string) => void;
  /**
   * Test hook: when supplied, used in place of the real
   * /api/v2/mentions/search call. Lets unit tests drive the dropdown
   * without an MSW handler dance.
   */
  searchUsers?: (q: string) => Promise<MentionUser[]>;
}

interface MentionContext {
  query: string;
  // Caret position immediately after the `@` character.
  startIndex: number;
  endIndex: number;
}

// MENTION_TRIGGER captures `@<query>` immediately preceding the caret.
// Query is everything from `@` up to whitespace; an empty query (the
// instant after `@`) still triggers the dropdown so the user sees the
// list of nearby colleagues without having to type anything.
const MENTION_TRIGGER = /(?:^|\s)@([\S]*)$/;

function detectMention(value: string, caret: number): MentionContext | null {
  if (caret <= 0) return null;
  const slice = value.slice(0, caret);
  const m = MENTION_TRIGGER.exec(slice);
  if (!m) return null;
  // m.index is the index of the `\s` (or 0 for ^). Account for the
  // leading whitespace so the `@` itself is the start of the trigger.
  const matchStart = m.index + (m[0].startsWith('@') ? 0 : 1);
  return {
    query: m[1],
    startIndex: matchStart,
    endIndex: caret,
  };
}

const SEARCH_DEBOUNCE_MS = 150;
const MAX_SUGGESTIONS = 8;

export const MentionTextarea = forwardRef<HTMLTextAreaElement, MentionTextareaProps>(
  function MentionTextarea(
    { value, onChange, searchUsers, onKeyDown, onBlur, ...rest },
    ref,
  ) {
    const innerRef = useRef<HTMLTextAreaElement | null>(null);
    const setRef = useCallback(
      (node: HTMLTextAreaElement | null) => {
        innerRef.current = node;
        if (typeof ref === 'function') ref(node);
        else if (ref) (ref as React.MutableRefObject<HTMLTextAreaElement | null>).current = node;
      },
      [ref],
    );

    const [mention, setMention] = useState<MentionContext | null>(null);
    const [suggestions, setSuggestions] = useState<MentionUser[]>([]);
    const [activeIndex, setActiveIndex] = useState(0);
    const [open, setOpen] = useState(false);

    // Debounced fetch keyed on the live mention query. Aborts when the
    // mention context disappears (caret moved off the trigger word).
    useEffect(() => {
      if (!mention) {
        setSuggestions([]);
        setOpen(false);
        return;
      }
      let cancelled = false;
      const handle = window.setTimeout(async () => {
        try {
          const search = searchUsers
            ? searchUsers
            : async (q: string) => {
                const resp = await searchMentionUsers(q, MAX_SUGGESTIONS);
                return resp.users ?? [];
              };
          const users = mention.query === ''
            ? []
            : await search(mention.query);
          if (cancelled) return;
          setSuggestions(users);
          setActiveIndex(0);
          setOpen(true);
        } catch {
          if (cancelled) return;
          setSuggestions([]);
          setOpen(false);
        }
      }, SEARCH_DEBOUNCE_MS);
      return () => {
        cancelled = true;
        window.clearTimeout(handle);
      };
    }, [mention, searchUsers]);

    const insertMention = useCallback(
      (user: MentionUser) => {
        if (!mention) return;
        const before = value.slice(0, mention.startIndex);
        const after = value.slice(mention.endIndex);
        // Always append a trailing space so the next keystroke is not
        // captured into another mention by accident.
        const replaced = `${before}@${user.email} ${after}`;
        onChange(replaced);
        setMention(null);
        setSuggestions([]);
        setOpen(false);
        // Restore caret to just after the inserted mention.
        const newCaret = before.length + user.email.length + 2; // @ + email + space
        const node = innerRef.current;
        if (node) {
          window.requestAnimationFrame(() => {
            node.focus();
            node.setSelectionRange(newCaret, newCaret);
          });
        }
      },
      [mention, value, onChange],
    );

    const handleChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
      const next = e.target.value;
      onChange(next);
      const caret = e.target.selectionStart ?? next.length;
      setMention(detectMention(next, caret));
    };

    const handleSelect = (e: React.SyntheticEvent<HTMLTextAreaElement>) => {
      const node = e.currentTarget;
      const caret = node.selectionStart ?? node.value.length;
      setMention(detectMention(node.value, caret));
    };

    const visibleSuggestions = useMemo(
      () => (open && mention && suggestions.length > 0 ? suggestions : []),
      [open, mention, suggestions],
    );

    const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if (visibleSuggestions.length > 0) {
        if (e.key === 'ArrowDown') {
          e.preventDefault();
          setActiveIndex((i) => (i + 1) % visibleSuggestions.length);
          return;
        }
        if (e.key === 'ArrowUp') {
          e.preventDefault();
          setActiveIndex(
            (i) => (i - 1 + visibleSuggestions.length) % visibleSuggestions.length,
          );
          return;
        }
        if (e.key === 'Enter' || e.key === 'Tab') {
          e.preventDefault();
          insertMention(visibleSuggestions[activeIndex]);
          return;
        }
        if (e.key === 'Escape') {
          e.preventDefault();
          setOpen(false);
          setMention(null);
          return;
        }
      }
      onKeyDown?.(e);
    };

    const handleBlur = (e: React.FocusEvent<HTMLTextAreaElement>) => {
      // Defer so a click on a suggestion lands before the dropdown
      // unmounts (mousedown fires before blur, so onMouseDown on the
      // suggestion handles the insert directly).
      window.setTimeout(() => {
        setOpen(false);
        setMention(null);
      }, 100);
      onBlur?.(e);
    };

    return (
      <div className="relative" data-testid="mention-textarea-wrapper">
        <textarea
          ref={setRef}
          value={value}
          onChange={handleChange}
          onSelect={handleSelect}
          onKeyDown={handleKeyDown}
          onBlur={handleBlur}
          {...rest}
        />
        {visibleSuggestions.length > 0 && (
          <ul
            className="absolute z-20 mt-1 w-72 max-h-56 overflow-y-auto rounded border border-border bg-bg-elevated shadow-lg"
            data-testid="mention-suggestions"
            role="listbox"
          >
            {visibleSuggestions.map((user, i) => (
              <li
                key={user.id}
                role="option"
                aria-selected={i === activeIndex}
                data-testid={`mention-suggestion-${user.email}`}
                className={`px-3 py-1.5 cursor-pointer text-xs font-mono ${
                  i === activeIndex
                    ? 'bg-accent-cyan/20 text-accent-cyan'
                    : 'text-text-primary hover:bg-bg-base'
                }`}
                onMouseDown={(e) => {
                  e.preventDefault();
                  insertMention(user);
                }}
              >
                <span className="block">{user.name || user.email}</span>
                {user.name && (
                  <span className="block text-[10px] text-text-secondary">
                    {user.email}
                  </span>
                )}
              </li>
            ))}
          </ul>
        )}
      </div>
    );
  },
);
