import { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import { useNavigate } from 'react-router';
import { useQueryClient } from '@tanstack/react-query';
import type { Ontology, ObjectType, LinkType, ActionType } from '../../api/types';

interface CommandPaletteProps {
  open: boolean;
  onClose: () => void;
}

type ResultKind = 'ontology' | 'objectType' | 'linkType' | 'actionType';

interface SearchResult {
  kind: ResultKind;
  label: string;
  sublabel: string;
  path: string;
  key: string;
}

const KIND_LABELS: Record<ResultKind, string> = {
  ontology: 'Ontologies',
  objectType: 'Object Types',
  linkType: 'Link Types',
  actionType: 'Action Types',
};

function KindIcon({ kind }: { kind: ResultKind }) {
  const cls = 'w-4 h-4 shrink-0';
  switch (kind) {
    case 'ontology':
      return (
        <svg className={cls} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <circle cx="12" cy="12" r="10" />
          <path d="M2 12h20M12 2a15.3 15.3 0 014 10 15.3 15.3 0 01-4 10 15.3 15.3 0 01-4-10 15.3 15.3 0 014-10" />
        </svg>
      );
    case 'objectType':
      return (
        <svg className={cls} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <rect x="3" y="3" width="18" height="18" rx="2" />
          <path d="M9 3v18M3 9h18" />
        </svg>
      );
    case 'linkType':
      return (
        <svg className={cls} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="M10 13a5 5 0 007.54.54l3-3a5 5 0 00-7.07-7.07l-1.72 1.71" />
          <path d="M14 11a5 5 0 00-7.54-.54l-3 3a5 5 0 007.07 7.07l1.71-1.71" />
        </svg>
      );
    case 'actionType':
      return (
        <svg className={cls} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2" />
        </svg>
      );
  }
}

export function CommandPalette({ open, onClose }: CommandPaletteProps) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [query, setQuery] = useState('');
  const [activeIndex, setActiveIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const overlayRef = useRef<HTMLDivElement>(null);

  // Gather all cached data from React Query
  const results = useMemo(() => {
    const term = query.toLowerCase().trim();
    const items: SearchResult[] = [];

    // Ontologies
    const ontologies = queryClient.getQueryData<Ontology[]>(['ontologies']);
    if (ontologies) {
      for (const ont of ontologies) {
        if (
          !term ||
          ont.apiName.toLowerCase().includes(term) ||
          ont.displayName.toLowerCase().includes(term)
        ) {
          items.push({
            kind: 'ontology',
            label: ont.apiName,
            sublabel: ont.displayName,
            path: `/admin/${ont.apiName}`,
            key: `ont-${ont.rid}`,
          });
        }
      }
    }

    // Scan all cached query data for object types, link types, action types
    const cache = queryClient.getQueryCache().getAll();
    const seenObjectTypes = new Set<string>();
    const seenLinkTypes = new Set<string>();
    const seenActionTypes = new Set<string>();

    for (const entry of cache) {
      const key = entry.queryKey;
      const data = entry.state.data;
      if (!data) continue;

      // Object types: key = ['objectTypes', ontologyApiName]
      if (key[0] === 'objectTypes' && typeof key[1] === 'string' && Array.isArray(data)) {
        const ontologyApiName = key[1] as string;
        for (const ot of data as ObjectType[]) {
          if (seenObjectTypes.has(ot.rid)) continue;
          seenObjectTypes.add(ot.rid);
          if (
            !term ||
            ot.apiName.toLowerCase().includes(term) ||
            ot.displayName.toLowerCase().includes(term)
          ) {
            items.push({
              kind: 'objectType',
              label: ot.apiName,
              sublabel: ot.displayName + (ot.description ? ` — ${ot.description}` : ''),
              path: `/admin/${ontologyApiName}/object-types/${ot.apiName}`,
              key: `ot-${ot.rid}`,
            });
          }
        }
      }

      // Link types: key = ['linkTypes', ontologyApiName]
      if (key[0] === 'linkTypes' && typeof key[1] === 'string' && key.length === 2 && Array.isArray(data)) {
        const ontologyApiName = key[1] as string;
        for (const lt of data as LinkType[]) {
          if (seenLinkTypes.has(lt.rid)) continue;
          seenLinkTypes.add(lt.rid);
          if (
            !term ||
            lt.apiName.toLowerCase().includes(term) ||
            lt.displayName.toLowerCase().includes(term)
          ) {
            items.push({
              kind: 'linkType',
              label: lt.apiName,
              sublabel: `${lt.objectTypeApiName} → ${lt.linkedObjectTypeApiName}`,
              path: `/admin/${ontologyApiName}`,
              key: `lt-${lt.rid}`,
            });
          }
        }
      }

      // Action types: key = ['actionTypes', ontologyApiName]
      if (key[0] === 'actionTypes' && typeof key[1] === 'string' && Array.isArray(data)) {
        const ontologyApiName = key[1] as string;
        for (const at of data as ActionType[]) {
          if (seenActionTypes.has(at.rid)) continue;
          seenActionTypes.add(at.rid);
          if (
            !term ||
            at.apiName.toLowerCase().includes(term) ||
            at.displayName.toLowerCase().includes(term)
          ) {
            items.push({
              kind: 'actionType',
              label: at.apiName,
              sublabel: at.displayName + (at.description ? ` — ${at.description}` : ''),
              path: `/actions/${ontologyApiName}`,
              key: `at-${at.rid}`,
            });
          }
        }
      }
    }

    return items.slice(0, 10);
  }, [query, queryClient]);

  // Group results by kind for rendering
  const grouped = useMemo(() => {
    const groups: { kind: ResultKind; items: (SearchResult & { flatIndex: number })[] }[] = [];
    const kindOrder: ResultKind[] = ['ontology', 'objectType', 'linkType', 'actionType'];
    let flatIndex = 0;

    for (const kind of kindOrder) {
      const items = results
        .filter((r) => r.kind === kind)
        .map((r) => ({ ...r, flatIndex: flatIndex++ }));
      if (items.length > 0) {
        groups.push({ kind, items });
      }
    }
    return groups;
  }, [results]);

  // Reset state when opened/closed
  useEffect(() => {
    if (open) {
      setQuery('');
      setActiveIndex(0);
      // Focus input on next tick after render
      requestAnimationFrame(() => inputRef.current?.focus());
    }
  }, [open]);

  // Scroll active item into view
  useEffect(() => {
    if (!listRef.current) return;
    const active = listRef.current.querySelector('[data-active="true"]');
    active?.scrollIntoView({ block: 'nearest' });
  }, [activeIndex]);

  const handleSelect = useCallback(
    (result: SearchResult) => {
      onClose();
      navigate(result.path);
    },
    [navigate, onClose],
  );

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === 'ArrowDown') {
        e.preventDefault();
        setActiveIndex((prev) => (prev + 1) % Math.max(results.length, 1));
      } else if (e.key === 'ArrowUp') {
        e.preventDefault();
        setActiveIndex((prev) => (prev - 1 + results.length) % Math.max(results.length, 1));
      } else if (e.key === 'Enter') {
        e.preventDefault();
        const selected = results[activeIndex];
        if (selected) handleSelect(selected);
      } else if (e.key === 'Escape') {
        e.preventDefault();
        onClose();
      }
    },
    [results, activeIndex, handleSelect, onClose],
  );

  // Reset active index when results change
  useEffect(() => {
    setActiveIndex(0);
  }, [query]);

  if (!open) return null;

  return (
    <div
      ref={overlayRef}
      className="fixed inset-0 z-50 bg-black/60"
      onClick={(e) => {
        if (e.target === overlayRef.current) onClose();
      }}
    >
      <div className="bg-bg-elevated border border-border rounded-lg shadow-xl w-full max-w-lg mx-auto mt-[20vh]">
        {/* Search input */}
        <div className="flex items-center border-b border-border">
          <svg
            className="w-4 h-4 ml-4 text-text-muted shrink-0"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
          >
            <circle cx="11" cy="11" r="8" />
            <path d="M21 21l-4.35-4.35" />
          </svg>
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="Search ontologies, object types, links, actions..."
            className="bg-bg-tertiary border-0 px-4 py-3 text-sm text-text-primary font-mono w-full focus:outline-none bg-transparent"
          />
          <kbd className="mr-4 text-xs text-text-muted border border-border rounded px-1.5 py-0.5 font-mono shrink-0">
            ESC
          </kbd>
        </div>

        {/* Results */}
        <div ref={listRef} className="max-h-[300px] overflow-y-auto py-2">
          {results.length === 0 ? (
            <div className="px-4 py-8 text-center text-sm text-text-muted">
              {query ? 'No results found.' : 'Start typing to search...'}
            </div>
          ) : (
            grouped.map((group) => (
              <div key={group.kind}>
                <div className="text-xs text-text-muted uppercase font-mono px-4 pt-2 pb-1">
                  {KIND_LABELS[group.kind]}
                </div>
                {group.items.map((item) => (
                  <button
                    key={item.key}
                    data-active={item.flatIndex === activeIndex}
                    onClick={() => handleSelect(item)}
                    onMouseEnter={() => setActiveIndex(item.flatIndex)}
                    className={`w-full flex items-center gap-3 px-4 py-2 text-sm cursor-pointer text-left transition-colors ${
                      item.flatIndex === activeIndex
                        ? 'bg-bg-tertiary text-accent-cyan'
                        : 'text-text-primary hover:bg-bg-tertiary'
                    }`}
                  >
                    <KindIcon kind={item.kind} />
                    <div className="min-w-0 flex-1">
                      <div className="font-mono text-xs truncate">{item.label}</div>
                      <div className="text-xs text-text-secondary truncate">{item.sublabel}</div>
                    </div>
                  </button>
                ))}
              </div>
            ))
          )}
        </div>

        {/* Footer hint */}
        <div className="flex items-center gap-4 px-4 py-2 border-t border-border text-xs text-text-muted">
          <span className="flex items-center gap-1">
            <kbd className="border border-border rounded px-1 py-0.5 font-mono">↑↓</kbd>
            navigate
          </span>
          <span className="flex items-center gap-1">
            <kbd className="border border-border rounded px-1 py-0.5 font-mono">↵</kbd>
            select
          </span>
          <span className="flex items-center gap-1">
            <kbd className="border border-border rounded px-1 py-0.5 font-mono">esc</kbd>
            close
          </span>
        </div>
      </div>
    </div>
  );
}
