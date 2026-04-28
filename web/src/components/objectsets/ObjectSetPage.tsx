import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useParams } from 'react-router';
import { useObjectTypes } from '../../hooks/useObjectTypes';
import {
  useCreateTemporaryObjectSet,
  useSavedObjectSets,
} from '../../hooks/useObjectSets';
import type { ObjectSetDefinition } from '../../api/types';
import type { SavedObjectSet } from '../../lib/objectSetBuilder';
import { ObjectSetComposer } from './ObjectSetComposer';
import { ObjectSetResults, type ShareInfo } from './ObjectSetResults';
import { Modal } from '../common/Modal';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { EmptyState } from '../common/EmptyState';

const TEMP_TTL_MS = 60 * 60 * 1000; // backend in-memory TTL is 1 hour

export function ObjectSetPage() {
  const { ontology } = useParams<{ ontology: string }>();
  const ontologyApiName = ontology ?? '';

  const { data: objectTypes, isLoading: typesLoading } = useObjectTypes(ontologyApiName);
  const objectTypeNames = useMemo(
    () => (objectTypes ?? []).map((ot) => ot.apiName),
    [objectTypes],
  );

  // Tree state mirrors the wire definition.
  const [def, setDef] = useState<ObjectSetDefinition>(() => ({
    type: 'base',
    objectType: '',
  }));

  // Initialise base object type once available.
  useEffect(() => {
    if (
      def.type === 'base' &&
      !def.objectType &&
      objectTypeNames.length > 0
    ) {
      setDef({ type: 'base', objectType: objectTypeNames[0] });
    }
  }, [def, objectTypeNames]);

  // executeKey forces the results pane to refetch on Execute click.
  const [executeKey, setExecuteKey] = useState(0);
  const [executingDef, setExecutingDef] = useState<ObjectSetDefinition | null>(null);

  // Saved object sets
  const { items: savedObjectSets, save, remove } = useSavedObjectSets(ontologyApiName);
  const [saveModalOpen, setSaveModalOpen] = useState(false);
  const [saveName, setSaveName] = useState('');

  // Share link via createTemporary
  const createTempMut = useCreateTemporaryObjectSet(ontologyApiName);
  const [shareInfo, setShareInfo] = useState<ShareInfo | null>(null);

  const handleExecute = useCallback(() => {
    setExecutingDef(def);
    setExecuteKey((k) => k + 1);
    // Auto-share: createTemporary so URL hash can carry the ref.
    createTempMut.mutate(def, {
      onSuccess: (resp) => {
        const expiresAt = Date.now() + TEMP_TTL_MS;
        setShareInfo({ objectSetRid: resp.objectSetRid, expiresAt });
        try {
          window.location.hash = `ref=${resp.objectSetRid}`;
        } catch {
          // ignore
        }
      },
    });
  }, [def, createTempMut]);

  const handleSaveAs = useCallback(() => {
    setSaveName('');
    setSaveModalOpen(true);
  }, []);

  const handleSaveConfirm = useCallback(() => {
    const name = saveName.trim();
    if (!name) return;
    save(name, def);
    setSaveModalOpen(false);
  }, [saveName, def, save]);

  const handleLoadSaved = useCallback((s: SavedObjectSet) => {
    setDef(s.def);
  }, []);

  const handleShare = useCallback(() => {
    if (!shareInfo) return;
    try {
      navigator.clipboard?.writeText(window.location.href);
    } catch {
      // ignore
    }
  }, [shareInfo]);

  if (!ontologyApiName) {
    return (
      <div className="flex items-center justify-center h-full">
        <EmptyState
          title="No ontology selected"
          description="Pick an ontology from the dashboard to start composing."
        />
      </div>
    );
  }

  if (typesLoading) {
    return (
      <div className="flex items-center justify-center h-full">
        <LoadingSpinner />
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full overflow-hidden">
      <div className="flex items-center justify-between px-4 py-3 border-b border-border bg-bg-primary">
        <div>
          <h1 className="text-base font-sans font-semibold text-text-primary">
            Object Set Composer
          </h1>
          <p className="text-xs font-mono text-text-secondary mt-0.5">
            {ontologyApiName}
          </p>
        </div>
        <Link
          to={`/objectsets/${ontologyApiName}/diff`}
          className="bg-bg-tertiary border border-border text-text-primary px-3 py-1.5 rounded text-xs font-mono hover:bg-bg-elevated"
        >
          Diff
        </Link>
      </div>

      <div className="flex-1 grid grid-cols-1 lg:grid-cols-[2fr_3fr] overflow-hidden">
        <div className="border-r border-border overflow-hidden flex flex-col">
          <ObjectSetComposer
            objectTypes={objectTypeNames}
            value={def}
            onChange={setDef}
            onExecute={handleExecute}
            onSaveAs={handleSaveAs}
            onShare={shareInfo ? handleShare : undefined}
            savedObjectSets={savedObjectSets}
            onLoadSaved={handleLoadSaved}
            onDeleteSaved={remove}
          />
        </div>
        <div className="overflow-hidden flex flex-col">
          <ObjectSetResults
            ontologyApiName={ontologyApiName}
            def={executingDef}
            executeKey={executeKey}
            shareInfo={shareInfo}
          />
        </div>
      </div>

      <Modal
        open={saveModalOpen}
        onClose={() => setSaveModalOpen(false)}
        title="Save Object Set"
      >
        <div className="flex flex-col gap-3">
          <label className="text-xs font-sans text-text-secondary">Name</label>
          <input
            value={saveName}
            onChange={(e) => setSaveName(e.target.value)}
            placeholder="My query"
            className="bg-bg-tertiary border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-cyan focus:outline-none"
            autoFocus
          />
          <div className="flex justify-end gap-2 mt-2">
            <button
              type="button"
              onClick={() => setSaveModalOpen(false)}
              className="px-3 py-1.5 bg-bg-tertiary border border-border rounded text-xs font-mono text-text-secondary hover:text-text-primary"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={handleSaveConfirm}
              disabled={!saveName.trim()}
              className="px-3 py-1.5 bg-accent-cyan text-bg-primary rounded text-xs font-mono font-medium disabled:opacity-40"
            >
              Save
            </button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
