import { useCallback, useContext, useMemo, useRef, useState, type ReactNode } from 'react';
import { Link } from 'react-router';
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
import { RelationshipGraph } from './RelationshipGraph';
import { CommentsTab } from './CommentsTab';
import { ObjectTimeSeriesTab } from './ObjectTimeSeriesTab';
import { WatchButton } from './WatchButton';
import { ReactionBar } from './ReactionBar';
import { MarkdownPreview } from '../common/MarkdownEditor';
import { InlineEditField } from '../common/InlineEditField';
import { CollabPresenceProvider } from '../common/CollabPresenceProvider';
import {
  useCollabPeers,
  useCollabSurfaceRef,
} from '../../lib/collabPresenceContext';
import { CollabCursorOverlay } from '../common/CollabCursorOverlay';
import type { PresenceClient } from '../../lib/collabPresence';
import { AuthContext } from '../../auth/AuthContext';
import { findModifyActionForProperty } from './findModifyAction';
import { useTimeTravelActive } from './useTimeTravel';

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
  /**
   * Test seam — pass a `MockPresenceClient` to drive the collaborative
   * cursor overlay without a live y-websocket server. Production callers
   * leave this undefined; the provider then resolves the WS URL from
   * `import.meta.env.VITE_COLLAB_WS_URL` and silently disables the overlay
   * when the env var is unset.
   */
  presenceClient?: PresenceClient | null;
}

type DetailTab =
  | 'properties'
  | 'relationships'
  | 'activity'
  | 'diff'
  | 'comments'
  | 'timeseries';

const TABS: { key: DetailTab; label: string }[] = [
  { key: 'properties', label: 'Properties' },
  { key: 'relationships', label: 'Relationships' },
  { key: 'activity', label: 'Activity' },
  { key: 'diff', label: 'Diff' },
  { key: 'comments', label: 'Comments' },
  { key: 'timeseries', label: 'TimeSeries' },
];

export function ObjectDetail({
  object,
  objectType,
  open,
  onClose,
  ontologyApiName,
  presenceClient,
}: ObjectDetailProps) {
  const auth = useContext(AuthContext);
  const authUser = auth?.user ?? null;

  // US-048: while the page is rendering a historical snapshot, every
  // mutation affordance in this panel must go cold. The store-backed
  // hook is the single source of truth — the parent BrowserPage reads
  // the same flag to disable Live / Bulk actions.
  const timeTravelActive = useTimeTravelActive(ontologyApiName);

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

  const roomId = object
    ? `weave:object:${ontologyApiName}:${objectType.apiName}:${String(object.__primaryKey)}`
    : '';
  const presenceUser = useMemo(
    () => ({
      id: authUser?.id ?? 'anon',
      name: authUser?.name || authUser?.email || 'Anonymous',
    }),
    [authUser?.id, authUser?.name, authUser?.email],
  );

  return (
    <SlidePanel open={open} onClose={onClose} title={title}>
      {object && (
        <CollabPresenceProvider
          roomId={roomId}
          user={presenceUser}
          client={presenceClient}
        >
        <div className="space-y-4" data-testid="object-detail-tabs">
          <div
            className="flex justify-end -mt-2 gap-2 items-center"
            data-testid="object-detail-actions"
          >
            {/* US-047: entry point for the Interface Methods console. The
                console is a focused page (`/methods/:ontology/:ot/:pk`) so
                the polymorphic-dispatch param form + result panel have
                room without crowding the detail slide-in. */}
            <Link
              to={`/methods/${ontologyApiName}/${objectType.apiName}/${encodeURIComponent(String(object.__primaryKey ?? ''))}`}
              className="px-2 py-1 text-[11px] font-mono border border-border rounded-sm text-text-secondary hover:text-text-primary hover:border-text-secondary"
              data-testid="object-detail-interface-methods-btn"
              data-object-type-api-name={objectType.apiName}
              data-primary-key={String(object.__primaryKey ?? '')}
            >
              Interface Methods
            </Link>
            <WatchButton targetRid={object.__rid ?? null} />
          </div>
          <div data-testid="object-detail-reactions">
            <ReactionBar targetRid={object.__rid ?? null} />
          </div>
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
            <CollabPropertiesSurface>
              <CollabPeerBadges />
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
                        // Historical (time-travel) mode forces every field
                        // back to read-only — the asOf snapshot has no live
                        // write path so editing must be impossible.
                        const editable =
                          baseTypeOf(prop.dataType) === 'string' &&
                          !isArrayType(prop.dataType) &&
                          !isMarkdown &&
                          !timeTravelActive;
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
                                  collabFieldKey={name}
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
            </CollabPropertiesSurface>
          )}

          {activeTab === 'relationships' && (
            <section data-testid="object-detail-relationships">
              <RelationshipGraph
                ontologyApiName={ontologyApiName}
                rootObjectType={objectType.apiName}
                rootPrimaryKey={String(object.__primaryKey)}
              />
            </section>
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

          {activeTab === 'comments' && (
            <section data-testid="object-detail-comments">
              <CommentsTab targetRid={object.__rid ?? ''} />
            </section>
          )}

          {activeTab === 'timeseries' && (
            <section data-testid="object-detail-timeseries">
              <ObjectTimeSeriesTab
                ontologyApiName={ontologyApiName}
                objectType={objectType}
                primaryKey={String(object.__primaryKey ?? '')}
              />
            </section>
          )}
        </div>
        </CollabPresenceProvider>
      )}
    </SlidePanel>
  );
}

interface CollabPropertiesSurfaceProps {
  children: ReactNode;
}

// Wraps the properties tab content in a position:relative surface and
// registers it with the presence context so `CollabCursorOverlay` can anchor
// peer cursors. The overlay renders inside the same element so its z-index
// stacks correctly with the inline-edit fields.
function CollabPropertiesSurface({ children }: CollabPropertiesSurfaceProps) {
  const registerSurface = useCollabSurfaceRef();
  const ref = useRef<HTMLDivElement | null>(null);
  const setRef = (el: HTMLDivElement | null) => {
    ref.current = el;
    registerSurface(el);
  };
  return (
    <div
      ref={setRef}
      className="space-y-6 relative"
      data-testid="object-detail-properties"
    >
      {children}
      <CollabCursorOverlay />
    </div>
  );
}

// Tiny pill row showing connected peers — surfaced as a non-blocking
// affordance so users can tell at a glance who else is editing this record.
function CollabPeerBadges() {
  const peers = useCollabPeers();
  if (peers.length === 0) return null;
  return (
    <div
      className="flex items-center gap-1 flex-wrap"
      data-testid="collab-peer-badges"
    >
      <span className="text-[10px] font-mono uppercase tracking-wider text-text-secondary">
        {peers.length === 1 ? '1 collaborator' : `${peers.length} collaborators`}
      </span>
      {peers.map((peer) => (
        <span
          key={peer.clientID}
          data-testid={`collab-peer-${peer.clientID}`}
          className="text-[10px] font-mono px-1 py-px rounded-sm text-bg-primary"
          style={{ backgroundColor: peer.user.color }}
          title={peer.user.name}
        >
          {peer.user.name}
        </span>
      ))}
    </div>
  );
}
