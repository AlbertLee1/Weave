import type { ActionApplyResponse } from '../../api/types';
import { Badge } from '../common/Badge';

interface ActionResultProps {
  result: ActionApplyResponse | null;
}

const editVariant: Record<string, 'success' | 'info' | 'error'> = {
  addObject: 'success',
  modifyObject: 'info',
  deleteObject: 'error',
};

export function ActionResult({ result }: ActionResultProps) {
  if (!result) return null;

  const edits = result.edits ?? [];

  if (edits.length === 0) {
    return (
      <div className="border border-border rounded p-4 bg-bg-tertiary">
        <div className="text-sm text-text-secondary">Action executed successfully with no edits.</div>
      </div>
    );
  }

  return (
    <div className="border border-border rounded bg-bg-tertiary">
      <div className="px-4 py-2 border-b border-border">
        <span className="text-xs font-medium text-text-primary">
          {edits.length} edit{edits.length !== 1 ? 's' : ''} applied
        </span>
      </div>
      <div className="flex flex-col divide-y divide-border">
        {edits.map((edit, i) => (
          <div key={i} className="px-4 py-3 flex flex-col gap-1">
            <div className="flex items-center gap-2">
              <Badge variant={editVariant[edit.type] ?? 'default'}>{edit.type}</Badge>
              <span className="text-xs font-mono text-text-primary">{edit.objectType}</span>
              <span className="text-xs text-text-secondary">pk={String(edit.primaryKey)}</span>
            </div>
            {edit.properties && Object.keys(edit.properties).length > 0 && (
              <div className="mt-1 flex flex-wrap gap-2">
                {Object.entries(edit.properties).map(([prop, val]) => (
                  <span
                    key={prop}
                    className="text-xs font-mono bg-bg-primary px-2 py-0.5 rounded text-text-secondary"
                  >
                    {prop}={JSON.stringify(val)}
                  </span>
                ))}
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
