import type { LinkType } from '../../api/types';
import { useLinkedObjects } from '../../hooks/useObjects';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { EmptyState } from '../common/EmptyState';

interface LinkedObjectsTabProps {
  ontologyApiName: string;
  objectType: string;
  primaryKey: string;
  linkTypes: LinkType[];
}

function LinkedObjectGroup({
  ontologyApiName,
  objectType,
  primaryKey,
  linkType,
}: {
  ontologyApiName: string;
  objectType: string;
  primaryKey: string;
  linkType: LinkType;
}) {
  const { data, isLoading } = useLinkedObjects({
    ontologyApiName,
    objectType,
    primaryKey,
    linkType: linkType.apiName,
    pageSize: 10,
  });

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <h4 className="text-xs font-sans font-medium text-text-primary">
          {linkType.displayName}
        </h4>
        <span className="text-xs font-mono text-text-muted">
          {linkType.linkedObjectTypeApiName}
        </span>
        {data?.totalCount && (
          <span className="text-xs font-mono text-text-secondary">
            ({data.totalCount})
          </span>
        )}
      </div>

      {isLoading && <LoadingSpinner size="sm" />}

      {!isLoading && (!data?.data || data.data.length === 0) && (
        <p className="text-xs text-text-muted font-mono py-2">No linked objects</p>
      )}

      {!isLoading && data?.data && data.data.length > 0 && (
        <div className="overflow-x-auto border border-border rounded">
          <table className="w-full text-xs" data-testid="linked-objects-table">
            <thead>
              <tr className="bg-bg-tertiary border-b border-border">
                {Object.keys(data.data[0])
                  .filter((k) => !k.startsWith('__') || k === '__primaryKey')
                  .slice(0, 5)
                  .map((key) => (
                    <th
                      key={key}
                      className="px-2 py-1.5 text-left font-mono text-text-secondary font-medium"
                    >
                      {key === '__primaryKey' ? 'Primary Key' : key}
                    </th>
                  ))}
              </tr>
            </thead>
            <tbody>
              {data.data.map((obj, i) => {
                const visibleKeys = Object.keys(obj)
                  .filter((k) => !k.startsWith('__') || k === '__primaryKey')
                  .slice(0, 5);
                return (
                  <tr
                    key={i}
                    className="border-b border-border last:border-b-0"
                  >
                    {visibleKeys.map((key) => (
                      <td
                        key={key}
                        className={`px-2 py-1.5 font-mono ${
                          key === '__primaryKey'
                            ? 'text-accent-cyan'
                            : 'text-text-primary'
                        }`}
                      >
                        {obj[key] === null || obj[key] === undefined
                          ? ''
                          : typeof obj[key] === 'object'
                            ? JSON.stringify(obj[key])
                            : String(obj[key])}
                      </td>
                    ))}
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

export function LinkedObjectsTab({
  ontologyApiName,
  objectType,
  primaryKey,
  linkTypes,
}: LinkedObjectsTabProps) {
  if (linkTypes.length === 0) {
    return (
      <EmptyState
        title="No link types"
        description="This object type has no outgoing link types defined."
      />
    );
  }

  return (
    <div className="space-y-4">
      {linkTypes.map((lt) => (
        <LinkedObjectGroup
          key={lt.rid}
          ontologyApiName={ontologyApiName}
          objectType={objectType}
          primaryKey={primaryKey}
          linkType={lt}
        />
      ))}
    </div>
  );
}
