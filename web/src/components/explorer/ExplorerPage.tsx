import { useParams, useNavigate } from 'react-router';
import { useObjectTypes, useObjectType, useOutgoingLinkTypes } from '../../hooks/useObjectTypes';
import { TypeTree } from './TypeTree';
import { ObjectTypeDetail } from './ObjectTypeDetail';
import { SchemaGraph } from './SchemaGraph';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { EmptyState } from '../common/EmptyState';

export function ExplorerPage() {
  const { ontology, objectType: objectTypeParam } = useParams<{
    ontology: string;
    objectType?: string;
  }>();
  const navigate = useNavigate();

  const ontologyApiName = ontology ?? '';

  const {
    data: objectTypes,
    isLoading: typesLoading,
    error: typesError,
  } = useObjectTypes(ontologyApiName);

  const {
    data: selectedObjectType,
    isLoading: typeLoading,
  } = useObjectType(ontologyApiName, objectTypeParam ?? '');

  // Fetch all link types for the graph view (reuse the first object type query
  // pattern -- in practice the schema graph will need all link types; we gather
  // them from each object type lazily). For the MVP, when no object type is
  // selected we show the graph with whatever link types are available from the
  // selected object type's outgoing links. A full implementation would aggregate
  // across all types.
  const {
    data: allLinkTypes,
  } = useOutgoingLinkTypes(ontologyApiName, objectTypeParam ?? '');

  function handleTypeSelect(apiName: string) {
    navigate(`/explorer/${ontologyApiName}/${apiName}`);
  }

  if (!ontologyApiName) {
    return (
      <div className="flex items-center justify-center h-full">
        <EmptyState
          title="No ontology selected"
          description="Select an ontology to explore its schema."
        />
      </div>
    );
  }

  if (typesLoading) {
    return (
      <div className="flex items-center justify-center h-full">
        <LoadingSpinner size="lg" />
      </div>
    );
  }

  if (typesError) {
    return (
      <div className="flex items-center justify-center h-full">
        <EmptyState
          title="Failed to load object types"
          description={String(typesError)}
        />
      </div>
    );
  }

  return (
    <div className="flex h-full bg-bg-primary" data-testid="explorer-page">
      {/* Left sidebar -- type tree */}
      <aside className="w-64 shrink-0 border-r border-border overflow-y-auto bg-bg-secondary">
        <TypeTree
          objectTypes={objectTypes ?? []}
          selectedType={objectTypeParam ?? null}
          onSelect={handleTypeSelect}
        />
      </aside>

      {/* Right panel */}
      <main className="flex-1 overflow-hidden">
        {objectTypeParam && selectedObjectType ? (
          typeLoading ? (
            <div className="flex items-center justify-center h-full">
              <LoadingSpinner />
            </div>
          ) : (
            <ObjectTypeDetail
              ontologyApiName={ontologyApiName}
              objectType={selectedObjectType}
            />
          )
        ) : (
          <div className="flex flex-col h-full">
            <div className="px-6 py-4 border-b border-border">
              <h2 className="text-lg font-semibold text-text-primary">Schema Graph</h2>
              <p className="text-xs text-text-secondary mt-0.5">
                Visual overview of object types and their relationships.
              </p>
            </div>
            <div className="flex-1 overflow-auto p-4">
              {objectTypes && objectTypes.length > 0 ? (
                <SchemaGraph
                  objectTypes={objectTypes}
                  linkTypes={allLinkTypes ?? []}
                />
              ) : (
                <EmptyState
                  title="No object types"
                  description="This ontology has no object types defined yet."
                />
              )}
            </div>
          </div>
        )}
      </main>
    </div>
  );
}
