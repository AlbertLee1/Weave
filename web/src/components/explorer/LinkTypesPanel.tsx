import type { LinkType } from '../../api/types';
import { Badge } from '../common/Badge';
import { EmptyState } from '../common/EmptyState';

interface LinkTypesPanelProps {
  linkTypes: LinkType[];
}

const cardinalityVariant: Record<string, 'info' | 'warning' | 'default'> = {
  ONE_TO_ONE: 'info',
  ONE_TO_MANY: 'warning',
  MANY_TO_MANY: 'default',
};

export function LinkTypesPanel({ linkTypes }: LinkTypesPanelProps) {
  if (linkTypes.length === 0) {
    return (
      <EmptyState
        title="No outgoing link types"
        description="This object type has no outgoing links defined."
      />
    );
  }

  return (
    <ul className="divide-y divide-border" data-testid="link-types-panel">
      {linkTypes.map((lt) => (
        <li
          key={lt.rid}
          className="flex items-center justify-between gap-4 px-4 py-3"
        >
          <div className="flex flex-col gap-0.5 min-w-0">
            <span className="text-sm font-mono text-text-primary truncate">
              {lt.apiName}
            </span>
            <span className="text-xs text-text-secondary truncate">
              &rarr; {lt.linkedObjectTypeApiName}
            </span>
            {lt.description && (
              <span className="text-xs text-text-muted truncate">
                {lt.description}
              </span>
            )}
          </div>
          <div className="flex items-center gap-2 shrink-0">
            {lt.inverseLinkRid && (
              <span
                data-testid="link-inverse-indicator"
                title={`Bidirectional — has inverse link ${lt.inverseLinkRid}`}
                className="text-xs text-text-secondary"
                aria-label="Bidirectional link"
              >
                &harr;
              </span>
            )}
            {lt.propagateMarkings && (
              <Badge variant="info">Propagates markings</Badge>
            )}
            {lt.required ? (
              <Badge variant="warning">Required</Badge>
            ) : (
              <span className="text-xs text-text-muted">Optional</span>
            )}
            <Badge variant={cardinalityVariant[lt.cardinality] ?? 'default'}>
              {lt.cardinality.replace(/_/g, ':')}
            </Badge>
          </div>
        </li>
      ))}
    </ul>
  );
}
