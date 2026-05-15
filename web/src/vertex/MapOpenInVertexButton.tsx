// MapOpenInVertexButton — "Open in Vertex" hand-off button used by the
// Map app (VTX-107). Clicking jumps to /vertex/<rid>?focus=<objectId>&hops=1
// so the Vertex page boots with the chosen geographic object selected
// plus a one-hop Search Around already loaded.
//
// The Map page itself (`/map/:ontology`) is owned by another stream and
// may not exist yet; this component is intentionally portable — it only
// needs a navigate fn (defaults to react-router) and the selected
// object descriptor.

import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router';
import '../i18n';

export interface SelectedMapObject {
  ontology: string;
  rid: string;
  objectId: string;
}

export interface MapOpenInVertexButtonProps {
  selected: SelectedMapObject | null;
  /** Inject a navigator for unit tests; defaults to react-router useNavigate. */
  onOpen?: (href: string) => void;
}

export function buildVertexHref(s: SelectedMapObject): string {
  const params = new URLSearchParams({ focus: s.objectId, hops: '1' });
  return `/vertex/${encodeURIComponent(s.rid)}?${params.toString()}`;
}

export function MapOpenInVertexButton({ selected, onOpen }: MapOpenInVertexButtonProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();

  function go() {
    if (!selected) return;
    const href = buildVertexHref(selected);
    if (onOpen) onOpen(href);
    else navigate(href);
  }

  return (
    <button
      type="button"
      data-testid="map-open-in-vertex"
      onClick={go}
      disabled={!selected}
      className="rounded bg-blue-600 px-3 py-1 text-xs text-white disabled:opacity-40"
    >
      {t('vertex.map.openInVertex')}
    </button>
  );
}
