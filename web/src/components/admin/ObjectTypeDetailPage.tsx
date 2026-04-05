import { useState, useEffect } from 'react';
import { useParams, useNavigate } from 'react-router';
import { useObjectType, useOutgoingLinkTypes } from '../../hooks/useObjectTypes';
import { useActionTypes } from '../../hooks/useActions';
import {
  useUpdateObjectType,
  useDeleteObjectType,
  useCreateProperty,
  useDeleteProperty,
} from '../../hooks/useAdminMutations';
import { Badge, statusVariant } from '../common/Badge';
import { Modal } from '../common/Modal';
import { SlidePanel } from '../common/SlidePanel';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { EmptyState } from '../common/EmptyState';
import { PropertyForm } from './PropertyForm';
import type { CreatePropertyInput } from '../../api/admin';

type TabKey = 'overview' | 'properties' | 'links' | 'actions' | 'settings';

const tabs: { key: TabKey; label: string }[] = [
  { key: 'overview', label: 'Overview' },
  { key: 'properties', label: 'Properties' },
  { key: 'links', label: 'Links' },
  { key: 'actions', label: 'Actions' },
  { key: 'settings', label: 'Settings' },
];

export function ObjectTypeDetailPage() {
  const { ontology: ontologyApiName = '', objectType: objectTypeApiName = '' } =
    useParams<{ ontology: string; objectType: string }>();
  const navigate = useNavigate();

  const { data: objectType, isLoading, error } = useObjectType(ontologyApiName, objectTypeApiName);
  const { data: linkTypes, isLoading: linksLoading } = useOutgoingLinkTypes(ontologyApiName, objectTypeApiName);
  const { data: actionTypes, isLoading: actionsLoading } = useActionTypes(ontologyApiName);

  const [activeTab, setActiveTab] = useState<TabKey>('overview');

  // --- Overview state ---
  const [displayName, setDisplayName] = useState('');
  const [pluralDisplayName, setPluralDisplayName] = useState('');
  const [description, setDescription] = useState('');
  const [titleProperty, setTitleProperty] = useState('');

  // --- Settings state ---
  const [status, setStatus] = useState<string>('ACTIVE');
  const [visibility, setVisibility] = useState<string>('NORMAL');
  const [icon, setIcon] = useState('');
  const [color, setColor] = useState('');

  useEffect(() => {
    if (objectType) {
      setDisplayName(objectType.displayName);
      setPluralDisplayName(objectType.pluralDisplayName ?? '');
      setDescription(objectType.description ?? '');
      setTitleProperty(objectType.titleProperty ?? '');
      setStatus(objectType.status);
      setVisibility(objectType.visibility);
      setIcon(objectType.icon ?? '');
      setColor(objectType.color ?? '');
    }
  }, [objectType]);

  // --- Modals / panels ---
  const [showAddProperty, setShowAddProperty] = useState(false);
  const [deletePropertyRid, setDeletePropertyRid] = useState<string | null>(null);
  const [showDeleteObjectType, setShowDeleteObjectType] = useState(false);

  // --- Mutations ---
  const updateMutation = useUpdateObjectType(objectType?.rid ?? '', ontologyApiName);
  const deleteMutation = useDeleteObjectType(ontologyApiName);
  const createPropertyMutation = useCreateProperty(objectType?.rid ?? '', ontologyApiName);
  const deletePropertyMutation = useDeleteProperty(ontologyApiName);

  // --- Derived data ---
  const properties = objectType?.properties
    ? Object.entries(objectType.properties).map(([apiName, val]) => ({
        apiName,
        rid: val.rid,
        baseType: val.dataType.type,
        isArray: val.dataType.itemType !== undefined,
      }))
    : [];

  const propertyCount = properties.length;
  const linkCount = linkTypes?.length ?? 0;

  // --- Handlers ---
  function handleSaveOverview() {
    updateMutation.mutate({
      displayName,
      pluralDisplayName: pluralDisplayName || undefined,
      description: description || undefined,
      titleProperty: titleProperty || undefined,
    });
  }

  function handleSaveSettings() {
    updateMutation.mutate({
      status,
      visibility,
      icon: icon || undefined,
      color: color || undefined,
    });
  }

  function handleCreateProperty(values: CreatePropertyInput) {
    createPropertyMutation.mutate(values, {
      onSuccess: () => setShowAddProperty(false),
    });
  }

  function handleDeleteProperty() {
    if (!deletePropertyRid) return;
    deletePropertyMutation.mutate(deletePropertyRid, {
      onSuccess: () => setDeletePropertyRid(null),
    });
  }

  function handleDeleteObjectType() {
    if (!objectType) return;
    deleteMutation.mutate(objectType.rid, {
      onSuccess: () => navigate(`/admin/${ontologyApiName}`),
    });
  }

  // --- Styling constants ---
  const inputClass =
    'bg-bg-tertiary border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-cyan focus:outline-none';
  const labelClass = 'text-xs text-text-secondary font-sans mb-1';

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-full">
        <LoadingSpinner size="lg" />
      </div>
    );
  }

  if (error || !objectType) {
    return (
      <div className="flex items-center justify-center h-full">
        <EmptyState
          title="Object Type Not Found"
          description={`Could not load object type "${objectTypeApiName}".`}
          action={
            <button
              onClick={() => navigate(`/admin/${ontologyApiName}`)}
              className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80"
            >
              Back to Admin
            </button>
          }
        />
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="px-6 py-4 border-b border-border bg-bg-primary">
        <div className="flex items-center gap-3">
          <button
            onClick={() => navigate(`/admin/${ontologyApiName}`)}
            className="text-text-secondary hover:text-text-primary transition-colors"
            aria-label="Back"
          >
            <svg className="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <path d="M15 18l-6-6 6-6" />
            </svg>
          </button>
          <div className="flex-1">
            <h1 className="text-lg font-semibold text-text-primary">
              {objectType.displayName}
            </h1>
            <p className="text-xs font-mono text-text-secondary mt-0.5">
              {objectType.apiName}
            </p>
          </div>
          <Badge variant={statusVariant(objectType.status)}>
            {objectType.status}
          </Badge>
          <Badge>{objectType.visibility}</Badge>
        </div>
      </div>

      {/* Tabs */}
      <div className="flex border-b border-border bg-bg-primary px-6">
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

      {/* Tab Content */}
      <div className="flex-1 overflow-y-auto p-6">
        {/* ==================== OVERVIEW TAB ==================== */}
        {activeTab === 'overview' && (
          <div className="max-w-2xl">
            <div className="grid grid-cols-2 gap-4 mb-6">
              <div className="p-4 bg-bg-tertiary border border-border rounded">
                <div className="text-2xl font-semibold text-text-primary">{propertyCount}</div>
                <div className="text-xs text-text-secondary mt-1">Properties</div>
              </div>
              <div className="p-4 bg-bg-tertiary border border-border rounded">
                <div className="text-2xl font-semibold text-text-primary">{linkCount}</div>
                <div className="text-xs text-text-secondary mt-1">Outgoing Links</div>
              </div>
            </div>

            <div className="flex flex-col gap-4">
              <div className="flex flex-col">
                <label className={labelClass}>Display Name</label>
                <input
                  type="text"
                  value={displayName}
                  onChange={(e) => setDisplayName(e.target.value)}
                  className={inputClass}
                />
              </div>

              <div className="flex flex-col">
                <label className={labelClass}>Plural Display Name</label>
                <input
                  type="text"
                  value={pluralDisplayName}
                  onChange={(e) => setPluralDisplayName(e.target.value)}
                  className={inputClass}
                />
              </div>

              <div className="flex flex-col">
                <label className={labelClass}>Description</label>
                <textarea
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                  rows={3}
                  className={inputClass}
                />
              </div>

              <div className="flex flex-col">
                <label className={labelClass}>Primary Key (read-only)</label>
                <input
                  type="text"
                  value={objectType.primaryKey}
                  readOnly
                  className={`${inputClass} opacity-60 cursor-not-allowed`}
                />
              </div>

              <div className="flex flex-col">
                <label className={labelClass}>Title Property</label>
                <select
                  value={titleProperty}
                  onChange={(e) => setTitleProperty(e.target.value)}
                  className={inputClass}
                >
                  <option value="">-- none --</option>
                  {properties.map((p) => (
                    <option key={p.apiName} value={p.apiName}>
                      {p.apiName}
                    </option>
                  ))}
                </select>
              </div>

              <div>
                <button
                  onClick={handleSaveOverview}
                  disabled={updateMutation.isPending}
                  className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80 disabled:opacity-50"
                >
                  {updateMutation.isPending ? 'Saving...' : 'Save'}
                </button>
              </div>
            </div>
          </div>
        )}

        {/* ==================== PROPERTIES TAB ==================== */}
        {activeTab === 'properties' && (
          <div>
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-sm font-medium text-text-primary">
                Properties ({propertyCount})
              </h3>
              <button
                onClick={() => setShowAddProperty(true)}
                className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80"
              >
                + Add Property
              </button>
            </div>

            {propertyCount === 0 ? (
              <EmptyState
                title="No Properties"
                description="Add your first property to define the schema."
                action={
                  <button
                    onClick={() => setShowAddProperty(true)}
                    className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80"
                  >
                    + Add Property
                  </button>
                }
              />
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-border text-left">
                      <th className="px-3 py-2 text-xs font-medium text-text-secondary">API Name</th>
                      <th className="px-3 py-2 text-xs font-medium text-text-secondary">Base Type</th>
                      <th className="px-3 py-2 text-xs font-medium text-text-secondary">Array</th>
                      <th className="px-3 py-2 text-xs font-medium text-text-secondary text-right">Actions</th>
                    </tr>
                  </thead>
                  <tbody>
                    {properties.map((prop) => (
                      <tr
                        key={prop.apiName}
                        className="border-b border-border hover:bg-bg-tertiary/50 transition-colors"
                      >
                        <td className="px-3 py-2">
                          <span className="font-mono text-text-primary">{prop.apiName}</span>
                          {prop.apiName === objectType.primaryKey && (
                            <Badge variant="info" className="ml-2">PK</Badge>
                          )}
                          {prop.apiName === objectType.titleProperty && (
                            <Badge variant="warning" className="ml-2">Title</Badge>
                          )}
                        </td>
                        <td className="px-3 py-2">
                          <Badge>{prop.baseType}</Badge>
                        </td>
                        <td className="px-3 py-2 text-text-secondary">
                          {prop.isArray ? 'Yes' : 'No'}
                        </td>
                        <td className="px-3 py-2 text-right">
                          <div className="flex items-center justify-end gap-2">
                            {/* Delete button */}
                            <button
                              onClick={() => setDeletePropertyRid(prop.rid)}
                              className="text-text-secondary hover:text-accent-error transition-colors"
                              aria-label={`Delete ${prop.apiName}`}
                              title="Delete property"
                            >
                              <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                                <path d="M3 6h18M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2" />
                              </svg>
                            </button>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        )}

        {/* ==================== LINKS TAB ==================== */}
        {activeTab === 'links' && (
          <div>
            <h3 className="text-sm font-medium text-text-primary mb-4">
              Outgoing Links ({linkCount})
            </h3>

            {linksLoading ? (
              <div className="flex items-center justify-center py-12">
                <LoadingSpinner />
              </div>
            ) : !linkTypes || linkTypes.length === 0 ? (
              <EmptyState
                title="No Outgoing Links"
                description="Link types originating from this object type will appear here."
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
                        {lt.displayName}
                        <span className="mx-1.5">&rarr;</span>
                        <span className="font-mono">{lt.linkedObjectTypeApiName}</span>
                      </div>
                    </div>
                    <Badge variant={lt.cardinality === 'MANY_TO_MANY' ? 'warning' : 'info'}>
                      {lt.cardinality}
                    </Badge>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}

        {/* ==================== ACTIONS TAB ==================== */}
        {activeTab === 'actions' && (
          <div>
            <h3 className="text-sm font-medium text-text-primary mb-4">
              Action Types
            </h3>

            {actionsLoading ? (
              <div className="flex items-center justify-center py-12">
                <LoadingSpinner />
              </div>
            ) : !actionTypes || actionTypes.length === 0 ? (
              <EmptyState
                title="No Action Types"
                description="Action types associated with this ontology will appear here."
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
                    <Badge variant={statusVariant(at.status)}>
                      {at.status}
                    </Badge>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}

        {/* ==================== SETTINGS TAB ==================== */}
        {activeTab === 'settings' && (
          <div className="max-w-2xl">
            <div className="flex flex-col gap-4 mb-8">
              <div className="flex flex-col">
                <label className={labelClass}>Status</label>
                <select
                  value={status}
                  onChange={(e) => setStatus(e.target.value)}
                  className={inputClass}
                >
                  <option value="PROMOTED">PROMOTED</option>
                  <option value="ACTIVE">ACTIVE</option>
                  <option value="EXPERIMENTAL">EXPERIMENTAL</option>
                  <option value="DEPRECATED">DEPRECATED</option>
                  <option value="EXAMPLE">EXAMPLE</option>
                </select>
              </div>

              <div className="flex flex-col">
                <label className={labelClass}>Visibility</label>
                <select
                  value={visibility}
                  onChange={(e) => setVisibility(e.target.value)}
                  className={inputClass}
                >
                  <option value="PROMINENT">PROMINENT</option>
                  <option value="NORMAL">NORMAL</option>
                  <option value="HIDDEN">HIDDEN</option>
                </select>
              </div>

              <div className="flex flex-col">
                <label className={labelClass}>Icon</label>
                <input
                  type="text"
                  value={icon}
                  onChange={(e) => setIcon(e.target.value)}
                  className={inputClass}
                  placeholder="e.g. cube"
                />
              </div>

              <div className="flex flex-col">
                <label className={labelClass}>Color</label>
                <input
                  type="text"
                  value={color}
                  onChange={(e) => setColor(e.target.value)}
                  className={inputClass}
                  placeholder="e.g. #3b82f6"
                />
              </div>

              <div>
                <button
                  onClick={handleSaveSettings}
                  disabled={updateMutation.isPending}
                  className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80 disabled:opacity-50"
                >
                  {updateMutation.isPending ? 'Saving...' : 'Save Settings'}
                </button>
              </div>
            </div>

            {/* Danger zone */}
            <div className="border border-red-600/30 rounded p-4">
              <h4 className="text-sm font-medium text-red-400 mb-2">Danger Zone</h4>
              <p className="text-xs text-text-secondary mb-3">
                Permanently delete this object type and all its properties. This action cannot be undone.
              </p>
              <button
                onClick={() => setShowDeleteObjectType(true)}
                className="bg-red-600 text-white px-4 py-2 rounded text-sm font-medium hover:bg-red-700"
              >
                Delete Object Type
              </button>
            </div>
          </div>
        )}
      </div>

      {/* ==================== SLIDE PANEL: Add Property ==================== */}
      <SlidePanel
        open={showAddProperty}
        onClose={() => setShowAddProperty(false)}
        title="Add Property"
      >
        <PropertyForm
          onSubmit={handleCreateProperty}
          isLoading={createPropertyMutation.isPending}
        />
      </SlidePanel>

      {/* ==================== MODAL: Delete Property ==================== */}
      <Modal
        open={deletePropertyRid !== null}
        onClose={() => setDeletePropertyRid(null)}
        title="Delete Property"
      >
        <p className="text-sm text-text-secondary mb-4">
          Are you sure you want to delete this property? This action cannot be undone.
        </p>
        <div className="flex items-center justify-end gap-3">
          <button
            onClick={() => setDeletePropertyRid(null)}
            className="px-4 py-2 rounded text-sm font-medium text-text-secondary hover:text-text-primary transition-colors"
          >
            Cancel
          </button>
          <button
            onClick={handleDeleteProperty}
            disabled={deletePropertyMutation.isPending}
            className="bg-red-600 text-white px-4 py-2 rounded text-sm font-medium hover:bg-red-700 disabled:opacity-50"
          >
            {deletePropertyMutation.isPending ? 'Deleting...' : 'Delete'}
          </button>
        </div>
      </Modal>

      {/* ==================== MODAL: Delete Object Type ==================== */}
      <Modal
        open={showDeleteObjectType}
        onClose={() => setShowDeleteObjectType(false)}
        title="Delete Object Type"
      >
        <p className="text-sm text-text-secondary mb-4">
          Are you sure you want to delete <strong className="text-text-primary">{objectType.displayName}</strong>?
          This will permanently remove the object type and all its properties.
        </p>
        <div className="flex items-center justify-end gap-3">
          <button
            onClick={() => setShowDeleteObjectType(false)}
            className="px-4 py-2 rounded text-sm font-medium text-text-secondary hover:text-text-primary transition-colors"
          >
            Cancel
          </button>
          <button
            onClick={handleDeleteObjectType}
            disabled={deleteMutation.isPending}
            className="bg-red-600 text-white px-4 py-2 rounded text-sm font-medium hover:bg-red-700 disabled:opacity-50"
          >
            {deleteMutation.isPending ? 'Deleting...' : 'Delete Object Type'}
          </button>
        </div>
      </Modal>
    </div>
  );
}
