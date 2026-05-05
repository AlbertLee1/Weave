import { useTranslation } from 'react-i18next';
import type { ObjectSetConflict } from '../../lib/objectSetSnapshotCache';

// OfflineConflictBanner (US-451). Surfaces a server-vs-cache divergence the
// user must resolve. Two-button decision: keep-mine swaps the displayed rows
// back to the cached snapshot AND leaves the cache untouched; use-server
// overwrites the cache with the server payload as the new baseline. Hidden
// when conflict is null (no divergence) so the banner never flashes during
// a clean refresh.
export interface OfflineConflictBannerProps {
  conflict: ObjectSetConflict | null;
  onKeepMine: () => void;
  onUseServer: () => void;
}

export function OfflineConflictBanner({
  conflict,
  onKeepMine,
  onUseServer,
}: OfflineConflictBannerProps) {
  const { t } = useTranslation();
  if (!conflict) return null;
  return (
    <div
      role="alert"
      data-testid="offline-conflict-banner"
      className="flex flex-wrap items-center gap-3 px-4 py-2 text-xs font-mono"
      style={{
        background: 'rgba(245, 158, 11, 0.12)',
        color: '#F59E0B',
        borderBottom: '1px solid rgba(245, 158, 11, 0.3)',
      }}
    >
      <div className="flex flex-col gap-0.5">
        <span className="font-medium">{t('offline.conflictTitle')}</span>
        <span className="text-[11px] opacity-80">
          {t('offline.conflictDescription')}
        </span>
        <span className="text-[11px] opacity-80">
          {t('offline.addedSummary', { count: conflict.added.length })}
          {' · '}
          {t('offline.removedSummary', { count: conflict.removed.length })}
        </span>
      </div>
      <div className="flex items-center gap-2 ml-auto">
        <button
          type="button"
          onClick={onKeepMine}
          className="px-3 py-1 rounded border border-[rgba(245,158,11,0.5)] hover:bg-[rgba(245,158,11,0.15)] transition-colors"
        >
          {t('offline.keepMine')}
        </button>
        <button
          type="button"
          onClick={onUseServer}
          className="px-3 py-1 rounded bg-[rgba(245,158,11,0.85)] text-bg-primary hover:bg-[rgba(245,158,11,1)] transition-colors"
        >
          {t('offline.useServer')}
        </button>
      </div>
    </div>
  );
}
