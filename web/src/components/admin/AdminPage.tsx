import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useOntologies } from '../../hooks/useOntologies';
import { useObjectTypes } from '../../hooks/useObjectTypes';
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
import { listOutgoingLinkTypes } from '../../api/ontologies';
import type { LinkType } from '../../api/types';
import { OntologyForm } from './OntologyForm';
import { ObjectTypeForm } from './ObjectTypeForm';
import { LinkTypeForm } from './LinkTypeForm';
import { ActionTypeForm } from './ActionTypeForm';
import { Modal } from '../common/Modal';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { EmptyState } from '../common/EmptyState';
import { Badge } from '../common/Badge';

type TabKey = 'objectTypes' | 'linkTypes' | 'actionTypes';

export function AdminPage() {
  const queryClient = useQueryClient();
  const { data: ontologies, isLoading: ontologiesLoading } = useOntologies();

  const [selectedOntology, setSelectedOntology] = useState<string>('');
  const [activeTab, setActiveTab] = useState<TabKey>('objectTypes');
  const [showCreateOntology, setShowCreateOntology] = useState(false);
  const [showCreateObjectType, setShowCreateObjectType] = useState(false);
  const [showCreateLinkType, setShowCreateLinkType] = useState(false);
  const [showCreateActionType, setShowCreateActionType] = useState(false);
  const [linkTypes, setLinkTypes] = useState<LinkType[]>([]);

  const { data: objectTypes, isLoading: objectTypesLoading } = useObjectTypes(selectedOntology);
  const { data: actionTypes, isLoading: actionTypesLoading } = useActionTypes(selectedOntology);

  // Fetch link types when ontology and first object type are available
  useState(() => {
    if (selectedOntology && objectTypes && objectTypes.length > 0) {
      Promise.all(
        objectTypes.map((ot) => listOutgoingLinkTypes(selectedOntology, ot.apiName)),
      ).then((results) => {
        const allLinks = results.flat();
        const unique = allLinks.filter(
          (lt, i, arr) => arr.findIndex((x) => x.rid === lt.rid) === i,
        );
        setLinkTypes(unique);
      });
    }
  });

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
      queryClient.invalidateQueries({ queryKey: ['linkTypes'] });
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

  const tabs: { key: TabKey; label: string }[] = [
    { key: 'objectTypes', label: 'Object Types' },
    { key: 'linkTypes', label: 'Link Types' },
    { key: 'actionTypes', label: 'Action Types' },
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
            {/* Tabs */}
            <div className="flex border-b border-border bg-bg-primary">
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
                </button>
              ))}
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
                  ) : !objectTypes || objectTypes.length === 0 ? (
                    <EmptyState
                      title="No Object Types"
                      description="Create your first object type to get started."
                    />
                  ) : (
                    <div className="flex flex-col gap-2">
                      {objectTypes.map((ot) => (
                        <div
                          key={ot.rid}
                          className="flex items-center justify-between p-3 bg-bg-tertiary border border-border rounded"
                        >
                          <div>
                            <div className="text-sm font-mono text-text-primary">{ot.apiName}</div>
                            <div className="text-xs text-text-secondary mt-0.5">
                              {ot.displayName}
                              {ot.description && ` - ${ot.description}`}
                            </div>
                          </div>
                          <div className="flex items-center gap-2">
                            <Badge
                              variant={
                                ot.status === 'ACTIVE'
                                  ? 'success'
                                  : ot.status === 'EXPERIMENTAL'
                                    ? 'warning'
                                    : 'error'
                              }
                            >
                              {ot.status}
                            </Badge>
                            <Badge>{ot.visibility}</Badge>
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
                  {linkTypes.length === 0 ? (
                    <EmptyState
                      title="No Link Types"
                      description="Create a link type to connect object types."
                    />
                  ) : (
                    <div className="flex flex-col gap-2">
                      {linkTypes.map((lt) => (
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
                          <Badge>{lt.cardinality}</Badge>
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
                  ) : !actionTypes || actionTypes.length === 0 ? (
                    <EmptyState
                      title="No Action Types"
                      description="Create an action type to define operations."
                    />
                  ) : (
                    <div className="flex flex-col gap-2">
                      {actionTypes.map((at) => (
                        <div
                          key={at.rid}
                          className="flex items-center justify-between p-3 bg-bg-tertiary border border-border rounded"
                        >
                          <div>
                            <div className="text-sm font-mono text-text-primary">{at.apiName}</div>
                            <div className="text-xs text-text-secondary mt-0.5">
                              {at.displayName}
                              {at.description && ` - ${at.description}`}
                            </div>
                          </div>
                          <Badge
                            variant={
                              at.status === 'ACTIVE'
                                ? 'success'
                                : at.status === 'EXPERIMENTAL'
                                  ? 'warning'
                                  : 'error'
                            }
                          >
                            {at.status}
                          </Badge>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              )}
            </div>
          </>
        )}
      </div>

      {/* Modals */}
      <Modal
        open={showCreateOntology}
        onClose={() => setShowCreateOntology(false)}
        title="Create Ontology"
      >
        <OntologyForm
          onSubmit={(values) => createOntologyMutation.mutate(values)}
          isLoading={createOntologyMutation.isPending}
        />
      </Modal>

      <Modal
        open={showCreateObjectType}
        onClose={() => setShowCreateObjectType(false)}
        title="Create Object Type"
      >
        <ObjectTypeForm
          onSubmit={(values) => createObjectTypeMutation.mutate(values)}
          isLoading={createObjectTypeMutation.isPending}
        />
      </Modal>

      <Modal
        open={showCreateLinkType}
        onClose={() => setShowCreateLinkType(false)}
        title="Create Link Type"
      >
        <LinkTypeForm
          onSubmit={(values) => createLinkTypeMutation.mutate(values)}
          objectTypes={objectTypes ?? []}
          isLoading={createLinkTypeMutation.isPending}
        />
      </Modal>

      <Modal
        open={showCreateActionType}
        onClose={() => setShowCreateActionType(false)}
        title="Create Action Type"
      >
        <ActionTypeForm
          onSubmit={(values) => createActionTypeMutation.mutate(values)}
          isLoading={createActionTypeMutation.isPending}
        />
      </Modal>
    </div>
  );
}
