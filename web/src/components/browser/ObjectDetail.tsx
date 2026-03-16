import type { ObjectType, WireObject } from '../../api/types';
import { SlidePanel } from '../common/SlidePanel';
import { useOutgoingLinkTypes } from '../../hooks/useObjectTypes';
import { LinkedObjectsTab } from './LinkedObjectsTab';

interface ObjectDetailProps {
  object: WireObject | null;
  objectType: ObjectType;
  open: boolean;
  onClose: () => void;
  ontologyApiName: string;
}

export function ObjectDetail({
  object,
  objectType,
  open,
  onClose,
  ontologyApiName,
}: ObjectDetailProps) {
  const { data: linkTypes } = useOutgoingLinkTypes(
    ontologyApiName,
    objectType.apiName,
  );

  const title = object
    ? `${objectType.displayName} - ${String(object.__primaryKey)}`
    : objectType.displayName;

  return (
    <SlidePanel open={open} onClose={onClose} title={title}>
      {object && (
        <div className="space-y-6">
          {/* Property key-value pairs */}
          <section>
            <h3 className="text-xs font-sans font-medium text-text-secondary uppercase tracking-wider mb-3">
              Properties
            </h3>
            <dl className="space-y-2">
              {/* Primary key first */}
              <div className="flex items-start gap-3">
                <dt className="w-1/3 text-xs font-sans text-text-secondary truncate shrink-0">
                  {objectType.primaryKey}
                </dt>
                <dd className="flex-1 text-xs font-mono text-accent-cyan break-all">
                  {String(object.__primaryKey ?? '')}
                </dd>
              </div>

              {/* Remaining properties */}
              {objectType.properties &&
                Object.entries(objectType.properties)
                  .filter(([name]) => name !== objectType.primaryKey)
                  .map(([name]) => {
                    const val = object[name];
                    let display: string;
                    if (val === null || val === undefined) {
                      display = '-';
                    } else if (typeof val === 'object') {
                      display = JSON.stringify(val, null, 2);
                    } else {
                      display = String(val);
                    }
                    return (
                      <div key={name} className="flex items-start gap-3">
                        <dt className="w-1/3 text-xs font-sans text-text-secondary truncate shrink-0">
                          {name}
                        </dt>
                        <dd className="flex-1 text-xs font-mono text-text-primary break-all whitespace-pre-wrap">
                          {display}
                        </dd>
                      </div>
                    );
                  })}
            </dl>
          </section>

          {/* Linked objects section */}
          {linkTypes && linkTypes.length > 0 && (
            <section>
              <h3 className="text-xs font-sans font-medium text-text-secondary uppercase tracking-wider mb-3">
                Linked Objects
              </h3>
              <LinkedObjectsTab
                ontologyApiName={ontologyApiName}
                objectType={objectType.apiName}
                primaryKey={String(object.__primaryKey)}
                linkTypes={linkTypes}
              />
            </section>
          )}
        </div>
      )}
    </SlidePanel>
  );
}
