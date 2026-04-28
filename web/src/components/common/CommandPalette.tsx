import { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router';
import { Command } from 'cmdk';
import { useOntologies } from '../../hooks/useOntologies';
import { useObjectTypes } from '../../hooks/useObjectTypes';

interface CommandPaletteProps {
  open: boolean;
  onClose: () => void;
  activeOntology: string | null;
}

interface PageItem {
  label: string;
  to: string;
  hint?: string;
}

const STATIC_PAGES: PageItem[] = [
  { label: 'Dashboard', to: '/' },
  { label: 'AIP Threads', to: '/threads' },
  { label: 'AIP Logic', to: '/logic-flows' },
  { label: 'Pipelines', to: '/pipelines' },
  { label: 'API Playground', to: '/developer/playground' },
  { label: 'API Metrics', to: '/developer/metrics' },
  { label: 'Schema Inference', to: '/schema/infer' },
  { label: 'Markings', to: '/admin/markings' },
];

function CommandPaletteInner({ onClose, activeOntology }: Omit<CommandPaletteProps, 'open'>) {
  const navigate = useNavigate();
  const overlayRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState('');

  const { data: ontologies } = useOntologies();
  const { data: objectTypes } = useObjectTypes(activeOntology ?? '');

  const ontologyList = ontologies ?? [];
  const objectTypeList = useMemo(() => objectTypes ?? [], [objectTypes]);

  useEffect(() => {
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose();
    }
    document.addEventListener('keydown', handleKey);
    return () => document.removeEventListener('keydown', handleKey);
  }, [onClose]);

  useEffect(() => {
    const t = setTimeout(() => inputRef.current?.focus(), 0);
    return () => clearTimeout(t);
  }, []);

  const go = (to: string) => {
    onClose();
    navigate(to);
  };

  return (
    <div
      ref={overlayRef}
      data-testid="command-palette-overlay"
      className="fixed inset-0 z-50 flex items-start justify-center pt-[12vh]"
      style={{
        background: 'linear-gradient(to bottom, rgba(0,0,0,0.85), rgba(8,11,22,0.9))',
        animation: 'fadeIn 120ms ease-out both',
      }}
      onClick={(e) => {
        if (e.target === overlayRef.current) onClose();
      }}
    >
      <div
        data-testid="command-palette"
        className="w-full max-w-xl mx-4 rounded-xl border border-border/50 overflow-hidden"
        style={{
          background: 'rgba(30,36,51,0.95)',
          backdropFilter: 'blur(24px)',
          WebkitBackdropFilter: 'blur(24px)',
          boxShadow:
            '0 25px 60px rgba(0,0,0,0.6), 0 0 0 1px rgba(245,158,11,0.06)',
          animation: 'modalEnter 160ms cubic-bezier(0.34,1.56,0.64,1) both',
        }}
      >
        <Command
          label="Global Search"
          shouldFilter={true}
          loop
          className="flex flex-col"
        >
          <div className="flex items-center gap-3 px-4 py-3 border-b border-border/50">
            <svg
              className="w-4 h-4 text-text-muted flex-shrink-0"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.75"
              strokeLinecap="round"
              strokeLinejoin="round"
              aria-hidden="true"
            >
              <path d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
            </svg>
            <Command.Input
              ref={inputRef}
              value={query}
              onValueChange={setQuery}
              placeholder="Search pages, ontologies, object types..."
              className="flex-1 bg-transparent text-sm text-text-primary placeholder-text-muted outline-none"
            />
            <kbd className="text-[10px] text-text-muted border border-border/40 rounded px-1.5 py-0.5 font-sans">
              ESC
            </kbd>
          </div>
          <Command.List className="max-h-[60vh] overflow-y-auto py-2">
            <Command.Empty data-testid="command-palette-empty" className="px-4 py-6 text-center text-sm text-text-muted">
              No matches found.
            </Command.Empty>

            <Command.Group heading="Pages" className="px-2 [&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-[10px] [&_[cmdk-group-heading]]:font-semibold [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-widest [&_[cmdk-group-heading]]:text-text-muted">
              {STATIC_PAGES.map((page) => (
                <Command.Item
                  key={page.to}
                  value={`page ${page.label}`}
                  onSelect={() => go(page.to)}
                  className="flex items-center gap-3 px-3 py-2 rounded-md cursor-pointer text-sm text-text-secondary aria-selected:bg-bg-tertiary aria-selected:text-text-primary"
                >
                  <span className="text-text-muted text-xs">↗</span>
                  <span>{page.label}</span>
                </Command.Item>
              ))}
            </Command.Group>

            {ontologyList.length > 0 && (
              <Command.Group
                heading="Ontologies"
                className="px-2 [&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-[10px] [&_[cmdk-group-heading]]:font-semibold [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-widest [&_[cmdk-group-heading]]:text-text-muted"
              >
                {ontologyList.map((ont) => (
                  <Command.Item
                    key={ont.apiName}
                    value={`ontology ${ont.displayName} ${ont.apiName}`}
                    onSelect={() => go(`/explorer/${ont.apiName}`)}
                    className="flex items-center gap-3 px-3 py-2 rounded-md cursor-pointer text-sm text-text-secondary aria-selected:bg-bg-tertiary aria-selected:text-text-primary"
                  >
                    <span className="text-text-muted text-xs">◆</span>
                    <span>{ont.displayName}</span>
                    <span className="ml-auto text-[10px] text-text-muted font-mono">
                      {ont.apiName}
                    </span>
                  </Command.Item>
                ))}
              </Command.Group>
            )}

            {activeOntology && objectTypeList.length > 0 && (
              <Command.Group
                heading={`Object Types in ${activeOntology}`}
                className="px-2 [&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-[10px] [&_[cmdk-group-heading]]:font-semibold [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-widest [&_[cmdk-group-heading]]:text-text-muted"
              >
                {objectTypeList.map((ot) => (
                  <Command.Item
                    key={ot.rid}
                    value={`objectType ${ot.displayName} ${ot.apiName}`}
                    onSelect={() =>
                      go(`/browser/${activeOntology}/${ot.apiName}`)
                    }
                    className="flex items-center gap-3 px-3 py-2 rounded-md cursor-pointer text-sm text-text-secondary aria-selected:bg-bg-tertiary aria-selected:text-text-primary"
                  >
                    <span className="text-text-muted text-xs">▦</span>
                    <span>{ot.displayName}</span>
                    <span className="ml-auto text-[10px] text-text-muted font-mono">
                      {ot.apiName}
                    </span>
                  </Command.Item>
                ))}
              </Command.Group>
            )}
          </Command.List>

          <div className="flex items-center gap-3 px-4 py-2 border-t border-border/40 text-[10px] text-text-muted">
            <span>
              <kbd className="border border-border/40 rounded px-1 py-0.5 font-sans">↑↓</kbd>{' '}
              navigate
            </span>
            <span>
              <kbd className="border border-border/40 rounded px-1 py-0.5 font-sans">↵</kbd>{' '}
              select
            </span>
            <span className="ml-auto">
              <kbd className="border border-border/40 rounded px-1 py-0.5 font-sans">⌘K</kbd>{' '}
              to toggle
            </span>
          </div>
        </Command>
      </div>

      <style>{`
        @keyframes modalEnter {
          from { opacity: 0; transform: scale(0.97) translateY(-4px); }
          to   { opacity: 1; transform: scale(1)    translateY(0);    }
        }
        @keyframes fadeIn {
          from { opacity: 0; }
          to   { opacity: 1; }
        }
      `}</style>
    </div>
  );
}

export function CommandPalette({ open, onClose, activeOntology }: CommandPaletteProps) {
  if (!open) return null;
  return <CommandPaletteInner onClose={onClose} activeOntology={activeOntology} />;
}
