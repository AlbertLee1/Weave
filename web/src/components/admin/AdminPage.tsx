import { useState, useEffect } from 'react';
import { useNavigate, useParams } from 'react-router';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useOntologies } from '../../hooks/useOntologies';
import { useObjectTypes, useLinkTypes } from '../../hooks/useObjectTypes';
import { useActionTypes } from '../../hooks/useActions';
import {
  createOntology,
  createObjectType,
  createLinkType,
  createActionType,
  type CreateOntologyInput,
  type CreateObjectTypeInput,
  type CreateLinkTypeInput,
  type CreateActionTypeInput,
} from '../../api/admin';
import {
  useDeleteObjectType,
  useDeleteLinkType,
  useDeleteActionType,
} from '../../hooks/useAdminMutations';
import { OntologyForm } from './OntologyForm';
import { ObjectTypeForm } from './ObjectTypeForm';
import { LinkTypeForm } from './LinkTypeForm';
import { ActionTypeForm } from './ActionTypeForm';
import { CommandPalette } from './CommandPalette';
import { InterfaceListPage } from './InterfaceListPage';
import { Modal } from '../common/Modal';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { EmptyState } from '../common/EmptyState';
import { Badge, statusVariant } from '../common/Badge';

type TabKey = 'objectTypes' | 'linkTypes' | 'actionTypes' | 'interfaces';

export function AdminPage() {
  const navigate = useNavigate();
  const { ontology: urlOntology } = useParams<{ ontology?: string }>();
  const queryClient = useQueryClient();
  const { data: ontologies, isLoading: ontologiesLoading } = useOntologies();

  const [selectedOntology, setSelectedOntology] = useState<string>(urlOntology ?? '');
  const [activeTab, setActiveTab] = useState<TabKey>('objectTypes');

  useEffect(() => {
    if (urlOntology && urlOntology !== selectedOntology) {
      setSelectedOntology(urlOntology);
    }
  }, [urlOntology]);
  const [showCreateOntology, setShowCreateOntology] = useState(false);
  const [showCreateObjectType, setShowCreateObjectType] = useState(false);
  const [showCreateLinkType, setShowCreateLinkType] = useState(false);
  const [showCreateActionType, setShowCreateActionType] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<{ type: string; rid: string; name: string } | null>(null);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [searchFilter, setSearchFilter] = useState('');

  useEffect(() => {
    function handleKey(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault();
        setPaletteOpen((prev) => !prev);
      }
    }
    document.addEventListener('keydown', handleKey);
    return () => document.removeEventListener('keydown', handleKey);
  }, []);

  const { data: objectTypes, isLoading: objectTypesLoading } = useObjectTypes(selectedOntology);
  const { data: linkTypes, isLoading: linkTypesLoading } = useLinkTypes(selectedOntology);
  const { data: actionTypes, isLoading: actionTypesLoading } = useActionTypes(selectedOntology);

  const deleteObjectTypeMutation = useDeleteObjectType(selectedOntology);
  const deleteLinkTypeMutation = useDeleteLinkType(selectedOntology);
  const deleteActionTypeMutation = useDeleteActionType(selectedOntology);

  const createOntologyMutation = useMutation({
    mutationFn: (input: CreateOntologyInput) => createOntology(input),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['ontologies'] });
      setShowCreateOntology(false);
    },
  });

  const createObjectTypeMutation = useMutation({
    mutationFn: (input: CreateObjectTypeInput) => createObjectType(selectedOntology, input),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['objectTypes', selectedOntology] });
      setShowCreateObjectType(false);
    },
  });

  const createLinkTypeMutation = useMutation({
    mutationFn: (input: CreateLinkTypeInput) => createLinkType(selectedOntology, input),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['linkTypes', selectedOntology] });
      setShowCreateLinkType(false);
    },
  });

  const createActionTypeMutation = useMutation({
    mutationFn: (input: CreateActionTypeInput) => createActionType(selectedOntology, input),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['actionTypes', selectedOntology] });
      setShowCreateActionType(false);
    },
  });

  function handleConfirmDelete() {
    if (!deleteTarget) return;
    const { type, rid } = deleteTarget;
    if (type === 'objectType') deleteObjectTypeMutation.mutate(rid, { onSuccess: () => setDeleteTarget(null) });
    if (type === 'linkType') deleteLinkTypeMutation.mutate(rid, { onSuccess: () => setDeleteTarget(null) });
    if (type === 'actionType') deleteActionTypeMutation.mutate(rid, { onSuccess: () => setDeleteTarget(null) });
  }

  const lowerFilter = searchFilter.toLowerCase();
  const filteredObjectTypes = objectTypes?.filter((ot) =>
    !searchFilter || ot.apiName.toLowerCase().includes(lowerFilter) || ot.displayName.toLowerCase().includes(lowerFilter)
  );
  const filteredLinkTypes = linkTypes?.filter((lt) =>
    !searchFilter || lt.apiName.toLowerCase().includes(lowerFilter) || lt.displayName.toLowerCase().includes(lowerFilter)
  );
  const filteredActionTypes = actionTypes?.filter((at) =>
    !searchFilter || at.apiName.toLowerCase().includes(lowerFilter) || at.displayName.toLowerCase().includes(lowerFilter)
  );

  const tabs: { key: TabKey; label: string; count?: number }[] = [
    { key: 'objectTypes', label: 'Object Types', count: objectTypes?.length },
    { key: 'linkTypes', label: 'Link Types', count: linkTypes?.length },
    { key: 'actionTypes', label: 'Action Types', count: actionTypes?.length },
    { key: 'interfaces', label: 'Interfaces' },
  ];

  return (
    <div className="flex h-full">
      {/* Left sidebar: ontologies list */}
      <div className="w-64 border-r border-border flex flex-col bg-bg-primary">
        <div className="flex items-center justify-between px-4 py-3 border-b border-border">
          <h2 className="text-sm font-medium text-text-primary">Ontologies</h2>
          <button
            onClick={() => setShowCreateOntology(true)}
            className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80"
          >
            + New
          </button>
        </div>
        <div className="flex-1 overflow-y-auto">
          {ontologiesLoading ? (
            <div className="py-8">
              <LoadingSpinner size="sm" />
            </div>
          ) : !ontologies || ontologies.length === 0 ? (
            <div className="p-4 text-xs text-text-secondary">No ontologies yet.</div>
          ) : (
            ontologies.map((ont) => (
              <button
                key={ont.rid}
                onClick={() => {
                  setSelectedOntology(ont.apiName);
                  setActiveTab('objectTypes');
                }}
                className={`w-full text-left px-4 py-3 text-sm border-b border-border transition-colors ${
                  selectedOntology === ont.apiName
                    ? 'bg-bg-tertiary text-accent-cyan'
                    : 'text-text-primary hover:bg-bg-tertiary'
                }`}
              >
                <div className="font-mono text-xs">{ont.apiName}</div>
                <div className="text-text-secondary text-xs mt-0.5">{ont.displayName}</div>
              </button>
            ))
          )}
        </div>
      </div>

      {/* Main content */}
      <div className="flex-1 flex flex-col overflow-hidden">
        {!selectedOntology ? (
          <div className="flex-1 flex items-center justify-center">
            <EmptyState
              title="Select an Ontology"
              description="Choose an ontology from the sidebar to manage its types."
            />
          </div>
        ) : (
          <>
            {/* Tabs + Search */}
            <div className="flex items-center border-b border-border bg-bg-primary">
              <div className="flex flex-1">
                {tabs.map((tab) => (
                  <button
                    key={tab.key}
                    onClick={() => setActiveTab(tab.key)}
                    className={`px-4 py-2.5 text-sm font-medium transition-colors border-b-2 ${
                      activeTab === tab.key
                        ? 'border-accent-cyan text-accent-cyan'
                        : 'border-transparent text-text-secondary hover:text-text-primary'
                    }`}
                  >
                    {tab.label}
                    {tab.count !== undefined && (
                      <span className="ml-1.5 text-xs text-text-muted">({tab.count})</span>
                    )}
                  </button>
                ))}
              </div>
              <div className="flex items-center gap-2 px-3">
                <input
                  type="text"
                  value={searchFilter}
                  onChange={(e) => setSearchFilter(e.target.value)}
                  placeholder="Filter..."
                  className="bg-bg-tertiary border border-border rounded px-2 py-1 text-xs text-text-primary font-mono w-40 focus:border-accent-cyan focus:outline-none"
                />
                <button
                  onClick={() => setPaletteOpen(true)}
                  className="text-xs text-text-muted hover:text-text-primary border border-border rounded px-2 py-1 font-mono"
                  title="Search (Cmd+K)"
                >
                  &#8984;K
                </button>
              </div>
            </div>

            {/* Tab content */}
            <div className="flex-1 overflow-y-auto p-4">
              {activeTab === 'objectTypes' && (
                <div>
                  <div className="flex items-center justify-between mb-4">
                    <h3 className="text-sm font-medium text-text-primary">Object Types</h3>
                    <button
                      onClick={() => setShowCreateObjectType(true)}
                      className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80"
                    >
                      + Create
                    </button>
                  </div>
                  {objectTypesLoading ? (
                    <LoadingSpinner />
                  ) : !filteredObjectTypes || filteredObjectTypes.length === 0 ? (
                    <EmptyState
                      title="No Object Types"
                      description={searchFilter ? 'No matches found.' : 'Create your first object type to get started.'}
                    />
                  ) : (
                    <div className="flex flex-col gap-2">
                      {filteredObjectTypes.map((ot) => (
                        <div
                          key={ot.rid}
                          className="flex items-center justify-between p-3 bg-bg-tertiary border border-border rounded cursor-pointer hover:border-accent-cyan/50 transition-colors"
                          onClick={() => navigate(`/admin/${selectedOntology}/object-types/${ot.apiName}`)}
                        >
                          <div>
                            <div className="text-sm font-mono text-text-primary">{ot.apiName}</div>
                            <div className="text-xs text-text-secondary mt-0.5">
                              {ot.displayName}
                              {ot.description && ` — ${ot.description}`}
                            </div>
                          </div>
                          <div className="flex items-center gap-2">
                            <Badge variant={statusVariant(ot.status)}>{ot.status}</Badge>
                            <Badge>{ot.visibility}</Badge>
                            <button
                              onClick={(e) => {
                                e.stopPropagation();
                                setDeleteTarget({ type: 'objectType', rid: ot.rid, name: ot.apiName });
                              }}
                              className="p-1 text-text-muted hover:text-red-400 transition-colors"
                              title="Delete"
                            >
                              <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                                <path d="M3 6h18M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2" />
                              </svg>
                            </button>
                          </div>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              )}

              {activeTab === 'linkTypes' && (
                <div>
                  <div className="flex items-center justify-between mb-4">
                    <h3 className="text-sm font-medium text-text-primary">Link Types</h3>
                    <button
                      onClick={() => setShowCreateLinkType(true)}
                      className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80"
                    >
                      + Create
                    </button>
                  </div>
                  {linkTypesLoading ? (
                    <LoadingSpinner />
                  ) : !filteredLinkTypes || filteredLinkTypes.length === 0 ? (
                    <EmptyState
                      title="No Link Types"
                      description={searchFilter ? 'No matches found.' : 'Create a link type to connect object types.'}
                    />
                  ) : (
                    <div className="flex flex-col gap-2">
                      {filteredLinkTypes.map((lt) => (
                        <div
                          key={lt.rid}
                          className="flex items-center justify-between p-3 bg-bg-tertiary border border-border rounded"
                        >
                          <div>
                            <div className="text-sm font-mono text-text-primary">{lt.apiName}</div>
                            <div className="text-xs text-text-secondary mt-0.5">
                              {lt.objectTypeApiName} &rarr; {lt.linkedObjectTypeApiName}
                            </div>
                          </div>
                          <div className="flex items-center gap-2">
                            <Badge>{lt.cardinality}</Badge>
                            <button
                              onClick={() => setDeleteTarget({ type: 'linkType', rid: lt.rid, name: lt.apiName })}
                              className="p-1 text-text-muted hover:text-red-400 transition-colors"
                              title="Delete"
                            >
                              <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                                <path d="M3 6h18M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2" />
                              </svg>
                            </button>
                          </div>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              )}

              {activeTab === 'actionTypes' && (
                <div>
                  <div className="flex items-center justify-between mb-4">
                    <h3 className="text-sm font-medium text-text-primary">Action Types</h3>
                    <button
                      onClick={() => setShowCreateActionType(true)}
                      className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80"
                    >
                      + Create
                    </button>
                  </div>
                  {actionTypesLoading ? (
                    <LoadingSpinner />
                  ) : !filteredActionTypes || filteredActionTypes.length === 0 ? (
                    <EmptyState
                      title="No Action Types"
                      description={searchFilter ? 'No matches found.' : 'Create an action type to define operations.'}
                    />
                  ) : (
                    <div className="flex flex-col gap-2">
                      {filteredActionTypes.map((at) => (
                        <div
                          key={at.rid}
                          className="flex items-center justify-between p-3 bg-bg-tertiary border border-border rounded cursor-pointer hover:border-accent-cyan/50 transition-colors"
                          onClick={() => navigate(`/admin/${selectedOntology}/action-types/${at.apiName}`)}
                        >
                          <div>
                            <div className="text-sm font-mono text-text-primary">{at.apiName}</div>
                            <div className="text-xs text-text-secondary mt-0.5">
                              {at.displayName}
                              {at.description && ` — ${at.description}`}
                            </div>
                          </div>
                          <div className="flex items-center gap-2">
                            <Badge variant={statusVariant(at.status)}>{at.status}</Badge>
                            <button
                              onClick={(e) => {
                                e.stopPropagation();
                                setDeleteTarget({ type: 'actionType', rid: at.rid, name: at.apiName });
                              }}
                              className="p-1 text-text-muted hover:text-red-400 transition-colors"
                              title="Delete"
                            >
                              <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                                <path d="M3 6h18M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2" />
                              </svg>
                            </button>
                          </div>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              )}

              {activeTab === 'interfaces' && (
                <InterfaceListPage ontologyApiName={selectedOntology} />
              )}
            </div>
          </>
        )}
      </div>

      {/* Create Modals */}
      <Modal open={showCreateOntology} onClose={() => setShowCreateOntology(false)} title="Create Ontology">
        <OntologyForm
          onSubmit={(values) => createOntologyMutation.mutate(values)}
          isLoading={createOntologyMutation.isPending}
        />
      </Modal>

      <Modal open={showCreateObjectType} onClose={() => setShowCreateObjectType(false)} title="Create Object Type">
        <ObjectTypeForm
          onSubmit={(values) => createObjectTypeMutation.mutate(values)}
          isLoading={createObjectTypeMutation.isPending}
        />
      </Modal>

      <Modal open={showCreateLinkType} onClose={() => setShowCreateLinkType(false)} title="Create Link Type">
        <LinkTypeForm
          onSubmit={(values) => createLinkTypeMutation.mutate(values)}
          objectTypes={objectTypes ?? []}
          isLoading={createLinkTypeMutation.isPending}
        />
      </Modal>

      <Modal open={showCreateActionType} onClose={() => setShowCreateActionType(false)} title="Create Action Type">
        <ActionTypeForm
          onSubmit={(values) => createActionTypeMutation.mutate(values)}
          isLoading={createActionTypeMutation.isPending}
        />
      </Modal>

      {/* Delete Confirmation Modal */}
      <Modal
        open={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        title="Confirm Delete"
      >
        <div className="flex flex-col gap-4">
          <p className="text-sm text-text-secondary">
            Are you sure you want to delete <span className="font-mono text-text-primary">{deleteTarget?.name}</span>?
            This action cannot be undone.
          </p>
          <div className="flex justify-end gap-3">
            <button
              onClick={() => setDeleteTarget(null)}
              className="px-4 py-2 rounded text-sm text-text-secondary hover:text-text-primary border border-border"
            >
              Cancel
            </button>
            <button
              onClick={handleConfirmDelete}
              className="bg-red-600 text-white px-4 py-2 rounded text-sm font-medium hover:bg-red-700"
            >
              Delete
            </button>
          </div>
        </div>
      </Modal>

      {/* Command Palette */}
      <CommandPalette open={paletteOpen} onClose={() => setPaletteOpen(false)} />
    </div>
  );
}
