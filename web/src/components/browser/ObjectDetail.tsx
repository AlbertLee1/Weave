import { useCallback, useMemo, useState } from 'react';
import type { DataType, ObjectType, WireObject } from '../../api/types';
import { SlidePanel } from '../common/SlidePanel';
import { useOutgoingLinkTypes } from '../../hooks/useObjectTypes';
import { useProperties } from '../../hooks/useProperties';
import { useActionTypes, useApplyAction } from '../../hooks/useActions';
import { LinkedObjectsTab } from './LinkedObjectsTab';
import { TimeSeriesChart } from '../common/TimeSeriesChart';
import { MediaUploadZone } from './MediaUploadZone';
import { ObjectActivityPanel } from './ObjectActivityPanel';
import { ObjectDiffPanel } from './ObjectDiffPanel';
import { MarkdownPreview } from '../common/MarkdownEditor';
import { InlineEditField } from '../common/InlineEditField';
import { findModifyActionForProperty } from './findModifyAction';

function baseTypeOf(dt: DataType): string {
  if (dt.type === 'array' && dt.itemType) return dt.itemType.type;
  return dt.type;
}

function isArrayType(dt: DataType): boolean {
  return dt.type === 'array';
}

// Recognise `typeConfig.format === "markdown"` on a string-typed property as
// a hint to render values with the Markdown preview component instead of raw
// monospace text. typeConfig is JSONB; treat it permissively.
function isMarkdownFormat(typeConfig: unknown): boolean {
  if (!typeConfig || typeof typeConfig !== 'object') return false;
  const tc = typeConfig as Record<string, unknown>;
  return tc.format === 'markdown';
}

function coerceMediaValues(val: unknown): string[] {
  if (val === null || val === undefined) return [];
  if (Array.isArray(val)) {
    return val.filter((v): v is string => typeof v === 'string');
  }
  if (typeof val === 'string' && val !== '') return [val];
  return [];
}

interface ObjectDetailProps {
  object: WireObject | null;
  objectType: ObjectType;
  open: boolean;
  onClose: () => void;
  ontologyApiName: string;
}

type DetailTab = 'properties' | 'activity' | 'diff';

const TABS: { key: DetailTab; label: string }[] = [
  { key: 'properties', label: 'Properties' },
  { key: 'activity', label: 'Activity' },
  { key: 'diff', label: 'Diff' },
];

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

  // Pull the rich Property metadata (typeConfig included) so we can detect
  // markdown-formatted string properties. Only loaded while the panel is open
  // to avoid spurious fetches for every row hover. Empty fallback keeps the
  // detail panel functional when the property list endpoint is unreachable.
  const { data: properties } = useProperties(ontologyApiName, objectType.rid);
  const markdownNames = useMemo(() => {
    const set = new Set<string>();
    if (!properties) return set;
    for (const p of properties) {
      if (p.baseType === 'string' && isMarkdownFormat(p.typeConfig)) {
        set.add(p.apiName);
      }
    }
    return set;
  }, [properties]);

  // Inline editing wires through a discovered modifyObject ActionType. Without
  // a matching action the field stays read-only — same shape as the bulk
  // delete toolbar's deleteObject discovery.
  const { data: actionTypes } = useActionTypes(ontologyApiName);
  const applyAction = useApplyAction(ontologyApiName);
  const buildSaveHandler = useCallback(
    (propertyName: string) => {
      if (!object || !actionTypes) return null;
      const match = findModifyActionForProperty(
        actionTypes,
        objectType.apiName,
        propertyName,
      );
      if (!match) return null;
      return async (next: string) => {
        const params: Record<string, unknown> = {
          [match.primaryKeyParam]: String(object.__primaryKey ?? ''),
        };
        // Non-edited bound properties default to their current value so the
        // modifyObject rule doesn't blank them out.
        for (const [boundProp, paramId] of Object.entries(match.propertyParams)) {
          if (boundProp === propertyName) continue;
          const cur = object[boundProp];
          if (cur !== undefined && cur !== null) params[paramId] = cur;
        }
        params[match.propertyParams[propertyName]] = next;
        await applyAction.mutateAsync({
          action: match.action.apiName,
          parameters: params,
        });
      };
    },
    [object, actionTypes, objectType.apiName, applyAction],
  );

  const [activeTab, setActiveTab] = useState<DetailTab>('properties');

  const title = object
    ? `${objectType.displayName} - ${String(object.__primaryKey)}`
    : objectType.displayName;

  return (
    <SlidePanel open={open} onClose={onClose} title={title}>
      {object && (
        <div className="space-y-4" data-testid="object-detail-tabs">
          <div
            className="flex border-b border-border"
            role="tablist"
            aria-label="Object detail tabs"
          >
            {TABS.map((tab) => (
              <button
                key={tab.key}
                type="button"
                role="tab"
                aria-selected={activeTab === tab.key}
                onClick={() => setActiveTab(tab.key)}
                className={`px-3 py-2 text-xs font-mono transition-colors border-b-2 ${
                  activeTab === tab.key
                    ? 'border-accent-cyan text-accent-cyan'
                    : 'border-transparent text-text-secondary hover:text-text-primary'
                }`}
                data-testid={`object-detail-tab-${tab.key}`}
              >
                {tab.label}
              </button>
            ))}
          </div>

          {activeTab === 'properties' && (
            <div className="space-y-6" data-testid="object-detail-properties">
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

                  {/* Remaining properties (non-timeseries, non-media) */}
                  {objectType.properties &&
                    Object.entries(objectType.properties)
                      .filter(([name]) => name !== objectType.primaryKey)
                      .filter(
                        ([, prop]) =>
                          baseTypeOf(prop.dataType) !== 'timeseries' &&
                          baseTypeOf(prop.dataType) !== 'media',
                      )
                      .map(([name, prop]) => {
                        const val = object[name];
                        const isMarkdown = markdownNames.has(name);
                        let display: string;
                        if (val === null || val === undefined) {
                          display = '-';
                        } else if (typeof val === 'object') {
                          display = JSON.stringify(val, null, 2);
                        } else {
                          display = String(val);
                        }
                        // Inline editing: scalar string properties get an
                        // InlineEditField whenever a matching modifyObject
                        // ActionType is registered. Markdown-formatted, array,
                        // and non-string scalars keep the read-only render.
                        const editable =
                          baseTypeOf(prop.dataType) === 'string' &&
                          !isArrayType(prop.dataType) &&
                          !isMarkdown;
                        const onSave = editable
                          ? buildSaveHandler(name)
                          : null;
                        return (
                          <div key={name} className="flex items-start gap-3">
                            <dt className="w-1/3 text-xs font-sans text-text-secondary truncate shrink-0">
                              {name}
                            </dt>
                            <dd className="flex-1 min-w-0">
                              {isMarkdown && typeof val === 'string' && val !== '' ? (
                                <MarkdownPreview
                                  source={val}
                                  testId={`markdown-property-${name}`}
                                />
                              ) : onSave ? (
                                <InlineEditField
                                  value={typeof val === 'string' ? val : ''}
                                  onSave={onSave}
                                  testId={`inline-edit-${name}`}
                                  ariaLabel={`Edit ${name}`}
                                  placeholder="-"
                                />
                              ) : (
                                <span className="block text-xs font-mono text-text-primary break-all whitespace-pre-wrap">
                                  {display}
                                </span>
                              )}
                            </dd>
                          </div>
                        );
                      })}
                </dl>
              </section>

              {/* Time-series properties: one chart per property */}
              {objectType.properties &&
                Object.entries(objectType.properties)
                  .filter(
                    ([, prop]) => baseTypeOf(prop.dataType) === 'timeseries',
                  )
                  .map(([name]) => (
                    <section key={`ts-${name}`}>
                      <h3 className="text-xs font-sans font-medium text-text-secondary uppercase tracking-wider mb-3">
                        {name}
                      </h3>
                      <TimeSeriesChart
                        ontologyApiName={ontologyApiName}
                        objectType={objectType.apiName}
                        primaryKey={String(object.__primaryKey)}
                        property={name}
                        label={name}
                      />
                    </section>
                  ))}

              {/* Media properties: dropzone upload + thumbnails + delete */}
              {objectType.properties &&
                Object.entries(objectType.properties)
                  .filter(([, prop]) => baseTypeOf(prop.dataType) === 'media')
                  .map(([name, prop]) => (
                    <MediaUploadZone
                      key={`media-${name}`}
                      propertyName={name}
                      values={coerceMediaValues(object[name])}
                      multiple={isArrayType(prop.dataType)}
                    />
                  ))}

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

          {activeTab === 'activity' && (
            <section data-testid="object-detail-activity">
              <ObjectActivityPanel
                ontologyApiName={ontologyApiName}
                objectType={objectType.apiName}
                primaryKey={String(object.__primaryKey)}
              />
            </section>
          )}

          {activeTab === 'diff' && (
            <section data-testid="object-detail-diff">
              <ObjectDiffPanel
                ontologyApiName={ontologyApiName}
                objectType={objectType.apiName}
                primaryKey={String(object.__primaryKey)}
              />
            </section>
          )}
        </div>
      )}
    </SlidePanel>
  );
}
