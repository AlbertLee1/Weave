import { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router';
import { Command } from 'cmdk';
import { useQuery } from '@tanstack/react-query';
import { useOntologies } from '../../hooks/useOntologies';
import { useObjectTypes } from '../../hooks/useObjectTypes';
import { useActionTypes } from '../../hooks/useActions';
import { listBranches } from '../../api/ontologies';
import { listApps } from '../../api/apps';
import {
  useRecentCommandsStore,
  type RecentCommand,
  type RecentCommandKind,
} from '../../stores/recentCommandsStore';

interface CommandPaletteProps {
  open: boolean;
  onClose: () => void;
  activeOntology: string | null;
}

interface PageItem {
  id: string;
  label: string;
  to: string;
  hint?: string;
}

const STATIC_PAGES: PageItem[] = [
  { id: 'page:dashboard', label: 'Dashboard', to: '/' },
  { id: 'page:threads', label: 'AIP Threads', to: '/threads' },
  { id: 'page:logic-flows', label: 'AIP Logic', to: '/logic-flows' },
  { id: 'page:aip-tools', label: 'AIP Tools', to: '/aip-tools' },
  { id: 'page:pipelines', label: 'Pipelines', to: '/pipelines' },
  { id: 'page:marketplace', label: 'Marketplace', to: '/marketplace' },
  { id: 'page:playground', label: 'API Playground', to: '/developer/playground' },
  { id: 'page:metrics', label: 'API Metrics', to: '/developer/metrics' },
  { id: 'page:schema-infer', label: 'Schema Inference', to: '/schema/infer' },
  { id: 'page:markings', label: 'Markings', to: '/admin/markings' },
];

function ontologyWorkspacePages(ontology: string | null): PageItem[] {
  if (!ontology) return [];
  const scoped = (id: string, label: string, to: string): PageItem => ({
    id: `page:ontology:${ontology}:${id}`,
    label,
    to,
    hint: ontology,
  });

  return [
    scoped('query-builder', 'Query Builder', `/objectsets/${ontology}`),
    scoped('quiver-ts', 'Quiver TS', `/quiver/${ontology}`),
    scoped('import-data', 'Import Data', `/import/${ontology}`),
    scoped('approvals', 'Approvals', `/approvals/${ontology}`),
    scoped('action-history', 'Action History', `/actions/${ontology}/history`),
    scoped('saga-jobs', 'Saga Jobs', `/actions/${ontology}/jobs`),
    scoped('querytypes', 'QueryTypes', `/queries/${ontology}`),
    scoped('automation-rules', 'Automation Rules', `/automation/${ontology}`),
    scoped('proposals', 'Proposals', `/proposals/${ontology}`),
    scoped('object-types', 'Object Types', `/admin/${ontology}/objectTypes`),
    scoped('link-types', 'Link Types', `/admin/${ontology}/linkTypes`),
    scoped('action-types', 'Action Types', `/admin/${ontology}/actionTypes`),
    scoped('interfaces', 'Interfaces', `/admin/${ontology}/interfaces`),
    scoped('value-types', 'Value Types', `/admin/${ontology}/valueTypes`),
    scoped('schema-graph', 'Schema Graph', `/admin/${ontology}/graph`),
    scoped('history', 'History', `/admin/${ontology}/history`),
    scoped('saga-dlq', 'Saga DLQ', `/admin/${ontology}/saga-dlq`),
    scoped('dataset-rollback', 'Dataset Rollback', `/admin/datasets/${ontology}/rollback`),
    scoped('security-policies', 'Security Policies', `/admin/${ontology}/security`),
  ];
}

const KIND_GLYPH: Record<RecentCommandKind, string> = {
  page: '↗',
  action: '⚡',
  object: '▦',
  branch: '⌥',
  app: '◧',
  ontology: '◆',
};

function CommandPaletteInner({ onClose, activeOntology }: Omit<CommandPaletteProps, 'open'>) {
  const navigate = useNavigate();
  const overlayRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState('');

  const { data: ontologies } = useOntologies();
  const { data: objectTypes } = useObjectTypes(activeOntology ?? '');
  const { data: actionTypes } = useActionTypes(activeOntology ?? '');
  const { data: branches } = useQuery({
    queryKey: ['branches', activeOntology],
    queryFn: () => listBranches(activeOntology as string),
    enabled: !!activeOntology,
  });
  const { data: appsResponse } = useQuery({
    queryKey: ['apps'],
    queryFn: () => listApps(),
  });

  const ontologyList = ontologies ?? [];
  const objectTypeList = useMemo(() => objectTypes ?? [], [objectTypes]);
  const actionTypeList = useMemo(() => actionTypes ?? [], [actionTypes]);
  const branchList = useMemo(() => branches ?? [], [branches]);
  const appList = useMemo(() => appsResponse?.apps ?? [], [appsResponse]);
  const pageList = useMemo(
    () => [...STATIC_PAGES, ...ontologyWorkspacePages(activeOntology)],
    [activeOntology],
  );

  const recentEntries = useRecentCommandsStore((s) => s.entries);
  const recordRecent = useRecentCommandsStore((s) => s.record);

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

  const go = (entry: Omit<RecentCommand, 'pickedAt'>) => {
    recordRecent(entry);
    onClose();
    navigate(entry.to);
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
              placeholder="Search actions, objects, branches, apps, pages…"
              className="flex-1 bg-transparent text-sm text-text-primary placeholder-text-muted outline-none"
            />
            <kbd className="text-[10px] text-text-muted border border-border/40 rounded px-1.5 py-0.5 font-sans">
              ESC
            </kbd>
          </div>
          <Command.List className="max-h-[60vh] overflow-y-auto py-2">
            <Command.Empty
              data-testid="command-palette-empty"
              className="px-4 py-6 text-center text-sm text-text-muted"
            >
              No matches found.
            </Command.Empty>

            {recentEntries.length > 0 && (
              <Command.Group
                heading="Recent"
                className="px-2 [&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-[10px] [&_[cmdk-group-heading]]:font-semibold [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-widest [&_[cmdk-group-heading]]:text-text-muted"
              >
                {recentEntries.map((entry) => (
                  <Command.Item
                    key={`recent:${entry.kind}:${entry.id}`}
                    value={`recent ${entry.kind} ${entry.label} ${entry.hint ?? ''}`}
                    onSelect={() => go(entry)}
                    className="flex items-center gap-3 px-3 py-2 rounded-md cursor-pointer text-sm text-text-secondary aria-selected:bg-bg-tertiary aria-selected:text-text-primary"
                  >
                    <span className="text-text-muted text-xs">
                      {KIND_GLYPH[entry.kind]}
                    </span>
                    <span>{entry.label}</span>
                    {entry.hint && (
                      <span className="ml-auto text-[10px] text-text-muted font-mono">
                        {entry.hint}
                      </span>
                    )}
                  </Command.Item>
                ))}
              </Command.Group>
            )}

            {activeOntology && actionTypeList.length > 0 && (
              <Command.Group
                heading="Actions"
                className="px-2 [&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-[10px] [&_[cmdk-group-heading]]:font-semibold [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-widest [&_[cmdk-group-heading]]:text-text-muted"
              >
                {actionTypeList.map((at) => (
                  <Command.Item
                    key={at.rid}
                    value={`action ${at.displayName} ${at.apiName}`}
                    onSelect={() =>
                      go({
                        id: at.rid,
                        kind: 'action',
                        label: at.displayName,
                        to: `/actions/${activeOntology}`,
                        hint: at.apiName,
                      })
                    }
                    className="flex items-center gap-3 px-3 py-2 rounded-md cursor-pointer text-sm text-text-secondary aria-selected:bg-bg-tertiary aria-selected:text-text-primary"
                  >
                    <span className="text-text-muted text-xs">{KIND_GLYPH.action}</span>
                    <span>{at.displayName}</span>
                    <span className="ml-auto text-[10px] text-text-muted font-mono">
                      {at.apiName}
                    </span>
                  </Command.Item>
                ))}
              </Command.Group>
            )}

            {activeOntology && objectTypeList.length > 0 && (
              <Command.Group
                heading="Objects"
                className="px-2 [&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-[10px] [&_[cmdk-group-heading]]:font-semibold [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-widest [&_[cmdk-group-heading]]:text-text-muted"
              >
                {objectTypeList.map((ot) => (
                  <Command.Item
                    key={ot.rid}
                    value={`object ${ot.displayName} ${ot.apiName}`}
                    onSelect={() =>
                      go({
                        id: ot.rid,
                        kind: 'object',
                        label: ot.displayName,
                        to: `/browser/${activeOntology}/${ot.apiName}`,
                        hint: ot.apiName,
                      })
                    }
                    className="flex items-center gap-3 px-3 py-2 rounded-md cursor-pointer text-sm text-text-secondary aria-selected:bg-bg-tertiary aria-selected:text-text-primary"
                  >
                    <span className="text-text-muted text-xs">{KIND_GLYPH.object}</span>
                    <span>{ot.displayName}</span>
                    <span className="ml-auto text-[10px] text-text-muted font-mono">
                      {ot.apiName}
                    </span>
                  </Command.Item>
                ))}
              </Command.Group>
            )}

            {activeOntology && branchList.length > 0 && (
              <Command.Group
                heading="Branches"
                className="px-2 [&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-[10px] [&_[cmdk-group-heading]]:font-semibold [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-widest [&_[cmdk-group-heading]]:text-text-muted"
              >
                {branchList.map((br) => (
                  <Command.Item
                    key={br.id}
                    value={`branch ${br.name} ${br.id}`}
                    onSelect={() =>
                      go({
                        id: br.id,
                        kind: 'branch',
                        label: br.name,
                        to: `/explorer/${activeOntology}/branches/${br.id}/reconcile`,
                        hint: br.status,
                      })
                    }
                    className="flex items-center gap-3 px-3 py-2 rounded-md cursor-pointer text-sm text-text-secondary aria-selected:bg-bg-tertiary aria-selected:text-text-primary"
                  >
                    <span className="text-text-muted text-xs">{KIND_GLYPH.branch}</span>
                    <span>{br.name}</span>
                    <span className="ml-auto text-[10px] text-text-muted font-mono">
                      {br.status}
                    </span>
                  </Command.Item>
                ))}
              </Command.Group>
            )}

            {appList.length > 0 && (
              <Command.Group
                heading="Apps"
                className="px-2 [&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-[10px] [&_[cmdk-group-heading]]:font-semibold [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-widest [&_[cmdk-group-heading]]:text-text-muted"
              >
                {appList.map((app) => (
                  <Command.Item
                    key={app.rid}
                    value={`app ${app.name}`}
                    onSelect={() =>
                      go({
                        id: app.rid,
                        kind: 'app',
                        label: app.name,
                        to: `/apps/${app.rid}`,
                      })
                    }
                    className="flex items-center gap-3 px-3 py-2 rounded-md cursor-pointer text-sm text-text-secondary aria-selected:bg-bg-tertiary aria-selected:text-text-primary"
                  >
                    <span className="text-text-muted text-xs">{KIND_GLYPH.app}</span>
                    <span>{app.name}</span>
                  </Command.Item>
                ))}
              </Command.Group>
            )}

            <Command.Group
              heading="Pages"
              className="px-2 [&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-[10px] [&_[cmdk-group-heading]]:font-semibold [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-widest [&_[cmdk-group-heading]]:text-text-muted"
            >
              {pageList.map((page) => (
                <Command.Item
                  key={page.id}
                  value={`page ${page.label} ${page.hint ?? ''}`}
                  onSelect={() =>
                    go({
                      id: page.id,
                      kind: 'page',
                      label: page.label,
                      to: page.to,
                      hint: page.hint,
                    })
                  }
                  className="flex items-center gap-3 px-3 py-2 rounded-md cursor-pointer text-sm text-text-secondary aria-selected:bg-bg-tertiary aria-selected:text-text-primary"
                >
                  <span className="text-text-muted text-xs">{KIND_GLYPH.page}</span>
                  <span>{page.label}</span>
                  {page.hint && (
                    <span className="ml-auto text-[10px] text-text-muted font-mono">
                      {page.hint}
                    </span>
                  )}
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
                    onSelect={() =>
                      go({
                        id: ont.apiName,
                        kind: 'ontology',
                        label: ont.displayName,
                        to: `/explorer/${ont.apiName}`,
                        hint: ont.apiName,
                      })
                    }
                    className="flex items-center gap-3 px-3 py-2 rounded-md cursor-pointer text-sm text-text-secondary aria-selected:bg-bg-tertiary aria-selected:text-text-primary"
                  >
                    <span className="text-text-muted text-xs">{KIND_GLYPH.ontology}</span>
                    <span>{ont.displayName}</span>
                    <span className="ml-auto text-[10px] text-text-muted font-mono">
                      {ont.apiName}
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
