import { useState } from 'react';
import type { ObjectType } from '../../api/types';
import { useOutgoingLinkTypes } from '../../hooks/useObjectTypes';
import { PropertiesTable } from './PropertiesTable';
import { LinkTypesPanel } from './LinkTypesPanel';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { EmptyState } from '../common/EmptyState';

interface ObjectTypeDetailProps {
  ontologyApiName: string;
  objectType: ObjectType;
}

type Tab = 'properties' | 'links' | 'actions';

const tabs: { key: Tab; label: string }[] = [
  { key: 'properties', label: 'Properties' },
  { key: 'links', label: 'Links' },
  { key: 'actions', label: 'Actions' },
];

export function ObjectTypeDetail({ ontologyApiName, objectType }: ObjectTypeDetailProps) {
  const [activeTab, setActiveTab] = useState<Tab>('properties');

  const {
    data: linkTypes,
    isLoading: linksLoading,
  } = useOutgoingLinkTypes(ontologyApiName, objectType.apiName);

  return (
    <div className="flex flex-col h-full" data-testid="object-type-detail">
      {/* Header */}
      <div className="px-6 py-4 border-b border-border">
        <h2 className="text-lg font-semibold text-text-primary">
          {objectType.displayName}
        </h2>
        <p className="text-xs font-mono text-text-secondary mt-0.5">
          {objectType.apiName}
        </p>
        {objectType.description && (
          <p className="text-sm text-text-secondary mt-1">
            {objectType.description}
          </p>
        )}
      </div>

      {/* Tabs */}
      <div className="flex border-b border-border">
        {tabs.map((tab) => (
          <button
            key={tab.key}
            onClick={() => setActiveTab(tab.key)}
            className={`px-4 py-2 text-sm font-mono transition-colors border-b-2 ${
              activeTab === tab.key
                ? 'border-accent-cyan text-accent-cyan'
                : 'border-transparent text-text-secondary hover:text-text-primary'
            }`}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {/* Content */}
      <div className="flex-1 overflow-auto p-4">
        {activeTab === 'properties' && (
          objectType.properties
            ? <PropertiesTable properties={objectType.properties} />
            : <EmptyState title="No properties" description="This object type has no properties defined." />
        )}

        {activeTab === 'links' && (
          linksLoading
            ? <div className="flex items-center justify-center py-12"><LoadingSpinner /></div>
            : <LinkTypesPanel linkTypes={linkTypes ?? []} />
        )}

        {activeTab === 'actions' && (
          <EmptyState
            title="Actions"
            description="Action types associated with this object type will appear here."
          />
        )}
      </div>
    </div>
  );
}
