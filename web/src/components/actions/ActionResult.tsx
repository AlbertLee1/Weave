import type { ActionApplyResponse } from '../../api/types';

interface ActionResultProps {
  result: ActionApplyResponse | null;
}

export function ActionResult({ result }: ActionResultProps) {
  if (!result) return null;

  const edits = result.edits;

  if (!edits) {
    return (
      <div className="border border-border rounded p-4 bg-bg-tertiary">
        <div className="text-sm text-text-secondary">Action executed successfully with no edits.</div>
      </div>
    );
  }

  const total =
    edits.addedObjectCount + edits.modifiedObjectsCount + edits.deletedObjectsCount;

  return (
    <div className="border border-border rounded bg-bg-tertiary">
      <div className="px-4 py-2 border-b border-border">
        <span className="text-xs font-medium text-text-primary">
          {total} edit{total !== 1 ? 's' : ''} applied
        </span>
      </div>
      <div className="px-4 py-3 flex flex-col gap-2">
        {edits.addedObjectCount > 0 && (
          <div className="flex items-center gap-2">
            <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-green-900/30 text-green-400">
              addObject
            </span>
            <span className="text-xs text-text-secondary">{edits.addedObjectCount}</span>
          </div>
        )}
        {edits.modifiedObjectsCount > 0 && (
          <div className="flex items-center gap-2">
            <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-blue-900/30 text-blue-400">
              modifyObject
            </span>
            <span className="text-xs text-text-secondary">{edits.modifiedObjectsCount}</span>
          </div>
        )}
        {edits.deletedObjectsCount > 0 && (
          <div className="flex items-center gap-2">
            <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-red-900/30 text-red-400">
              deleteObject
            </span>
            <span className="text-xs text-text-secondary">{edits.deletedObjectsCount}</span>
          </div>
        )}
        {edits.addedLinksCount > 0 && (
          <div className="flex items-center gap-2">
            <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-green-900/30 text-green-400">
              addLink
            </span>
            <span className="text-xs text-text-secondary">{edits.addedLinksCount}</span>
          </div>
        )}
        {edits.deletedLinksCount > 0 && (
          <div className="flex items-center gap-2">
            <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-red-900/30 text-red-400">
              deleteLink
            </span>
            <span className="text-xs text-text-secondary">{edits.deletedLinksCount}</span>
          </div>
        )}
      </div>
    </div>
  );
}
