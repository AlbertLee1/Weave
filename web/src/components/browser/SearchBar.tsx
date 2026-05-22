import { useState, useCallback, useEffect } from 'react';

interface SearchBarProps {
  value: string;
  onSearch: (searchText: string) => void;
  onToggleFilters: () => void;
}

export function SearchBar({ value, onSearch, onToggleFilters }: SearchBarProps) {
  const [draftText, setDraftText] = useState(value);

  useEffect(() => {
    setDraftText(value);
  }, [value]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLInputElement>) => {
      if (e.key === 'Enter') {
        onSearch(draftText);
      }
    },
    [draftText, onSearch],
  );

  const handleChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      setDraftText(e.target.value);
      if (e.target.value === '') {
        onSearch('');
      }
    },
    [onSearch],
  );

  return (
    <div className="flex items-center gap-2">
      <div className="relative flex-1">
        <svg
          className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-text-muted pointer-events-none"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
        >
          <circle cx="11" cy="11" r="8" />
          <path d="M21 21l-4.35-4.35" />
        </svg>
        <input
          type="search"
          value={draftText}
          onChange={handleChange}
          onKeyDown={handleKeyDown}
          placeholder="Search objects..."
          aria-label="Search objects"
          className="w-full pl-10 pr-3 py-2 bg-bg-primary border border-border rounded text-sm font-mono text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent-cyan focus-visible:ring-2 focus-visible:ring-accent-cyan/50 transition-colors"
          data-testid="search-input"
        />
      </div>
      <button
        type="button"
        onClick={onToggleFilters}
        className="flex items-center gap-1.5 px-3 py-2 bg-bg-primary border border-border rounded text-xs font-sans text-text-secondary hover:text-text-primary hover:border-accent-cyan focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-cyan/50 transition-colors"
        data-testid="toggle-filters"
      >
        <svg
          className="w-4 h-4"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          aria-hidden="true"
          focusable="false"
        >
          <path d="M22 3H2l8 9.46V19l4 2v-8.54L22 3z" />
        </svg>
        Filters
      </button>
    </div>
  );
}
