import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { EmptyState } from '../common/EmptyState';
import { useToastStore } from '../../stores/toastStore';
import {
  deleteInstalledPackage,
  listInstalledPackages,
  setInstalledPackageEnabled,
  type InstalledPackage,
  type PackageManifest,
} from '../../api/packages';
import { ApiRequestError } from '../../api/client';

const PACKAGES_KEY = ['marketplace', 'installed-packages'] as const;

// US-413: local catalog of every .weavepkg installed via US-412's
// /api/v2/pkg/install handler. The page is read-only beyond the
// per-row enable/disable toggle and the uninstall button — package
// installation itself happens via the CLI (`weave-cli pkg install`)
// today, with the front-end install wizard landing in US-454.
export function MarketplacePage() {
  const queryClient = useQueryClient();
  const pushToast = useToastStore((s) => s.push);

  const packagesQuery = useQuery({
    queryKey: PACKAGES_KEY,
    queryFn: listInstalledPackages,
    retry: false,
  });

  const [pendingUninstall, setPendingUninstall] = useState<string | null>(null);

  const enableMutation = useMutation({
    mutationFn: ({ name, enabled }: { name: string; enabled: boolean }) =>
      setInstalledPackageEnabled(name, enabled),
    onSuccess: (_data, { name, enabled }) => {
      queryClient.invalidateQueries({ queryKey: PACKAGES_KEY });
      pushToast({
        message: `${name} ${enabled ? 'enabled' : 'disabled'}`,
        severity: 'success',
      });
    },
    onError: (err, { name }) => {
      pushToast({
        message: `Failed to update ${name}: ${formatError(err)}`,
        severity: 'error',
      });
    },
  });

  const uninstallMutation = useMutation({
    mutationFn: (name: string) => deleteInstalledPackage(name),
    onSuccess: (_data, name) => {
      queryClient.invalidateQueries({ queryKey: PACKAGES_KEY });
      setPendingUninstall(null);
      pushToast({
        message: `${name} uninstalled`,
        severity: 'success',
      });
    },
    onError: (err, name) => {
      setPendingUninstall(null);
      pushToast({
        message: `Failed to uninstall ${name}: ${formatError(err)}`,
        severity: 'error',
      });
    },
  });

  const packages = useMemo(
    () => packagesQuery.data?.data ?? [],
    [packagesQuery.data],
  );

  return (
    <div className="flex flex-col h-full" data-testid="marketplace-page">
      <header className="border-b border-border px-6 py-4">
        <h1 className="text-base font-sans font-semibold text-text-primary">
          Marketplace
        </h1>
        <p className="text-xs text-text-secondary mt-1">
          Local catalog of installed .weavepkg packages. Toggle individual
          packages on or off, or remove them outright.
        </p>
      </header>

      <div className="flex-1 overflow-y-auto px-6 py-6">
        {packagesQuery.isLoading && (
          <p
            className="text-sm text-text-secondary"
            data-testid="marketplace-loading"
          >
            Loading installed packages…
          </p>
        )}

        {packagesQuery.isError && (
          <p
            className="text-sm text-rose-400"
            data-testid="marketplace-error"
          >
            Failed to load installed packages: {formatError(packagesQuery.error)}
          </p>
        )}

        {!packagesQuery.isLoading &&
          !packagesQuery.isError &&
          packages.length === 0 && (
            <EmptyState
              title="No packages installed"
              description="Install a .weavepkg archive via the CLI (weave-cli pkg install) to populate the marketplace catalog."
            />
          )}

        {packages.length > 0 && (
          <ul
            className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4"
            data-testid="marketplace-list"
          >
            {packages.map((pkg) => (
              <PackageCard
                key={pkg.name}
                pkg={pkg}
                onToggle={(enabled) =>
                  enableMutation.mutate({ name: pkg.name, enabled })
                }
                onRequestUninstall={() => setPendingUninstall(pkg.name)}
                togglePending={
                  enableMutation.isPending &&
                  enableMutation.variables?.name === pkg.name
                }
              />
            ))}
          </ul>
        )}
      </div>

      {pendingUninstall && (
        <UninstallConfirmDialog
          name={pendingUninstall}
          pending={uninstallMutation.isPending}
          onCancel={() => setPendingUninstall(null)}
          onConfirm={() => uninstallMutation.mutate(pendingUninstall)}
        />
      )}
    </div>
  );
}

interface PackageCardProps {
  pkg: InstalledPackage;
  onToggle: (enabled: boolean) => void;
  onRequestUninstall: () => void;
  togglePending: boolean;
}

function PackageCard({
  pkg,
  onToggle,
  onRequestUninstall,
  togglePending,
}: PackageCardProps) {
  const manifest = pkg.manifest ?? null;
  const dependencies = manifestDependencies(manifest);
  const description = manifest?.description ?? '';
  const author = manifest?.author ?? '';
  const license = manifest?.license ?? '';

  return (
    <li
      data-testid={`marketplace-card-${pkg.name}`}
      data-enabled={pkg.enabled ? 'true' : 'false'}
      className="border border-border rounded-lg bg-bg-secondary/50 p-4 flex flex-col gap-3"
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h3
            className="text-sm font-sans font-semibold text-text-primary truncate"
            title={pkg.name}
          >
            {pkg.name}
          </h3>
          <p className="text-[11px] font-mono text-text-secondary mt-0.5">
            v{pkg.version} · ontology <span className="text-text-primary">{pkg.ontology}</span>
          </p>
        </div>
        <span
          className={[
            'inline-flex items-center gap-1 px-2 py-0.5 text-[10px] font-medium rounded-full border',
            pkg.enabled
              ? 'border-emerald-500/40 text-emerald-300 bg-emerald-500/10'
              : 'border-border text-text-secondary bg-bg-tertiary',
          ].join(' ')}
          data-testid={`marketplace-status-${pkg.name}`}
        >
          {pkg.enabled ? 'Enabled' : 'Disabled'}
        </span>
      </div>

      {description && (
        <p className="text-xs text-text-secondary line-clamp-3">{description}</p>
      )}

      <dl className="text-[11px] text-text-secondary grid grid-cols-2 gap-y-1">
        {author && (
          <>
            <dt className="font-medium">Author</dt>
            <dd className="text-text-primary truncate" title={author}>
              {author}
            </dd>
          </>
        )}
        {license && (
          <>
            <dt className="font-medium">License</dt>
            <dd className="text-text-primary">{license}</dd>
          </>
        )}
        <dt className="font-medium">Migrations</dt>
        <dd className="text-text-primary">{pkg.migrations.length}</dd>
        <dt className="font-medium">Installed</dt>
        <dd className="text-text-primary truncate" title={pkg.installedAt}>
          {formatTimestamp(pkg.installedAt)}
        </dd>
      </dl>

      {dependencies.length > 0 && (
        <div className="text-[11px]">
          <p className="text-text-secondary font-medium mb-1">Dependencies</p>
          <ul
            className="flex flex-wrap gap-1"
            data-testid={`marketplace-deps-${pkg.name}`}
          >
            {dependencies.map(([dep, ver]) => (
              <li
                key={dep}
                className="px-1.5 py-0.5 rounded bg-bg-tertiary text-text-primary font-mono"
              >
                {dep}
                {ver ? `@${ver}` : ''}
              </li>
            ))}
          </ul>
        </div>
      )}

      <div className="flex items-center justify-between gap-3 pt-2 border-t border-border/60 mt-auto">
        <label className="flex items-center gap-2 text-xs text-text-secondary cursor-pointer">
          <input
            type="checkbox"
            checked={pkg.enabled}
            disabled={togglePending}
            onChange={(e) => onToggle(e.target.checked)}
            data-testid={`marketplace-toggle-${pkg.name}`}
            className="accent-accent-cyan"
          />
          <span>{pkg.enabled ? 'Enabled' : 'Disabled'}</span>
        </label>
        <button
          type="button"
          onClick={onRequestUninstall}
          data-testid={`marketplace-uninstall-${pkg.name}`}
          className="px-2.5 py-1 text-[11px] font-medium text-rose-300 border border-rose-500/40 rounded hover:bg-rose-500/10 transition-colors"
        >
          Uninstall
        </button>
      </div>
    </li>
  );
}

interface UninstallConfirmDialogProps {
  name: string;
  pending: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}

function UninstallConfirmDialog({
  name,
  pending,
  onCancel,
  onConfirm,
}: UninstallConfirmDialogProps) {
  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label={`Uninstall ${name}`}
      data-testid="marketplace-uninstall-dialog"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 px-4"
    >
      <div className="w-full max-w-md bg-bg-secondary border border-border rounded-lg p-5 shadow-xl">
        <h2 className="text-sm font-sans font-semibold text-text-primary mb-2">
          Uninstall {name}?
        </h2>
        <p className="text-xs text-text-secondary mb-4">
          The catalog row will be removed so the marketplace hides it. On-disk
          migrations and ontology entities are NOT touched — drop tables and
          remove ontology entries via the operator workflow if you also want a
          full teardown.
        </p>
        <div className="flex justify-end gap-2">
          <button
            type="button"
            onClick={onCancel}
            disabled={pending}
            data-testid="marketplace-uninstall-cancel"
            className="px-3 py-1.5 text-xs text-text-secondary border border-border rounded hover:bg-bg-tertiary transition-colors"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={onConfirm}
            disabled={pending}
            data-testid="marketplace-uninstall-confirm"
            className="px-3 py-1.5 text-xs font-medium text-rose-100 bg-rose-600/80 border border-rose-500 rounded hover:bg-rose-600 transition-colors disabled:opacity-50"
          >
            {pending ? 'Uninstalling…' : 'Uninstall'}
          </button>
        </div>
      </div>
    </div>
  );
}

function manifestDependencies(
  manifest: PackageManifest | null,
): Array<[string, string]> {
  if (!manifest?.dependencies) return [];
  return Object.entries(manifest.dependencies).map(([k, v]) => [k, String(v)]);
}

function formatTimestamp(iso: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toISOString().slice(0, 10);
}

function formatError(err: unknown): string {
  if (err instanceof ApiRequestError) {
    return err.errorName || err.errorCode;
  }
  if (err instanceof Error) {
    return err.message;
  }
  return 'unknown error';
}
