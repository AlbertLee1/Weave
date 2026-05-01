import type { ObjectType } from '../../api/types';
import { Badge } from '../common/Badge';

interface TypeTreeProps {
  objectTypes: ObjectType[];
  selectedType: string | null;
  onSelect: (apiName: string) => void;
}

const statusVariant: Record<string, 'success' | 'warning' | 'error'> = {
  ACTIVE: 'success',
  EXPERIMENTAL: 'warning',
  DEPRECATED: 'error',
};

export function TypeTree({ objectTypes, selectedType, onSelect }: TypeTreeProps) {
  return (
    <nav
      className="flex flex-col gap-0.5 py-2"
      data-testid="type-tree"
      aria-label="Object types"
    >
      <h3 className="px-3 py-1 text-[10px] font-mono uppercase tracking-wider text-text-muted">
        Object Types
      </h3>
      {objectTypes.map((ot) => {
        const isSelected = ot.apiName === selectedType;
        return (
          <button
            key={ot.apiName}
            type="button"
            onClick={() => onSelect(ot.apiName)}
            aria-current={isSelected ? 'page' : undefined}
            className={`flex items-center justify-between gap-2 px-3 py-1.5 text-left text-sm rounded mx-1 transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-cyan/50 ${
              isSelected
                ? 'bg-accent-cyan/10 text-accent-cyan'
                : 'text-text-secondary hover:bg-bg-tertiary hover:text-text-primary'
            }`}
          >
            <span className="font-mono text-xs truncate">{ot.displayName}</span>
            <Badge variant={statusVariant[ot.status] ?? 'default'}>
              {ot.status}
            </Badge>
          </button>
        );
      })}
    </nav>
  );
}
