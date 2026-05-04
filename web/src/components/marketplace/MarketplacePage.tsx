import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { EmptyState } from '../common/EmptyState';
import { useToastStore } from '../../stores/toastStore';
import {
  deleteInstalledPackage,
  installBuiltinPackage,
  listBuiltinPackages,
  listInstalledPackages,
  setInstalledPackageEnabled,
  type BuiltinPackageMetadata,
  type InstalledPackage,
  type PackageManifest,
} from '../../api/packages';
import { ApiRequestError } from '../../api/client';

const PACKAGES_KEY = ['marketplace', 'installed-packages'] as const;
const BUILTIN_KEY = ['marketplace', 'builtin-packages'] as const;

type Tab = 'installed' | 'builtin';

// US-413 / US-414: local catalog of installed .weavepkg packages plus a
// "Built-in" section serving the embedded example catalog
// (Northwind / Chinook / IoT-demo). The Built-in tab supports one-click
// install via POST /api/v2/pkg/builtin/{slug}/install; uploaded packages
// continue to land via the CLI (`weave-cli pkg install`) until the
// upload wizard arrives in US-454.
export function MarketplacePage() {
  const queryClient = useQueryClient();
  const pushToast = useToastStore((s) => s.push);
  const [tab, setTab] = useState<Tab>('installed');

  const packagesQuery = useQuery({
    queryKey: PACKAGES_KEY,
    queryFn: listInstalledPackages,
    retry: false,
  });

  const builtinQuery = useQuery({
    queryKey: BUILTIN_KEY,
    queryFn: listBuiltinPackages,
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

  const installBuiltinMutation = useMutation({
    mutationFn: (slug: string) => installBuiltinPackage(slug),
    onSuccess: (resp, slug) => {
      queryClient.invalidateQueries({ queryKey: PACKAGES_KEY });
      pushToast({
        message: `${resp.name} v${resp.version} installed (ontology ${resp.ontology})`,
        severity: 'success',
      });
      // Drop the user on the Installed tab so they can verify the new row.
      setTab('installed');
      void slug;
    },
    onError: (err, slug) => {
      pushToast({
        message: `Failed to install ${slug}: ${formatError(err)}`,
        severity: 'error',
      });
    },
  });

  const packages = useMemo(
    () => packagesQuery.data?.data ?? [],
    [packagesQuery.data],
  );

  const builtinPackages = useMemo(
    () => builtinQuery.data?.data ?? [],
    [builtinQuery.data],
  );

  const installedSlugs = useMemo(() => {
    const set = new Set<string>();
    for (const p of packages) set.add(p.name);
    return set;
  }, [packages]);

  return (
    <div className="flex flex-col h-full" data-testid="marketplace-page">
      <header className="border-b border-border px-6 py-4">
        <h1 className="text-base font-sans font-semibold text-text-primary">
          Marketplace
        </h1>
        <p className="text-xs text-text-secondary mt-1">
          Browse the bundled example packages or manage the .weavepkg
          archives that have already been installed.
        </p>
        <nav
          className="mt-3 flex gap-2 border-b border-border/60"
          aria-label="Marketplace sections"
          data-testid="marketplace-tabs"
        >
          <TabButton
            active={tab === 'installed'}
            onClick={() => setTab('installed')}
            testId="marketplace-tab-installed"
          >
            Installed{packages.length > 0 ? ` · ${packages.length}` : ''}
          </TabButton>
          <TabButton
            active={tab === 'builtin'}
            onClick={() => setTab('builtin')}
            testId="marketplace-tab-builtin"
          >
            Built-in{builtinPackages.length > 0 ? ` · ${builtinPackages.length}` : ''}
          </TabButton>
        </nav>
      </header>

      <div className="flex-1 overflow-y-auto px-6 py-6">
        {tab === 'installed' && (
          <InstalledSection
            isLoading={packagesQuery.isLoading}
            isError={packagesQuery.isError}
            error={packagesQuery.error}
            packages={packages}
            onToggle={(name, enabled) =>
              enableMutation.mutate({ name, enabled })
            }
            onRequestUninstall={(name) => setPendingUninstall(name)}
            togglingName={
              enableMutation.isPending
                ? enableMutation.variables?.name ?? null
                : null
            }
          />
        )}

        {tab === 'builtin' && (
          <BuiltinSection
            isLoading={builtinQuery.isLoading}
            isError={builtinQuery.isError}
            error={builtinQuery.error}
            packages={builtinPackages}
            installedSlugs={installedSlugs}
            onInstall={(slug) => installBuiltinMutation.mutate(slug)}
            installingSlug={
              installBuiltinMutation.isPending
                ? installBuiltinMutation.variables ?? null
                : null
            }
          />
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

interface TabButtonProps {
  active: boolean;
  onClick: () => void;
  testId: string;
  children: React.ReactNode;
}

function TabButton({ active, onClick, testId, children }: TabButtonProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      data-testid={testId}
      data-active={active ? 'true' : 'false'}
      className={[
        'px-3 py-1.5 text-xs font-medium border-b-2 -mb-px transition-colors',
        active
          ? 'border-accent-cyan text-text-primary'
          : 'border-transparent text-text-secondary hover:text-text-primary',
      ].join(' ')}
    >
      {children}
    </button>
  );
}

interface InstalledSectionProps {
  isLoading: boolean;
  isError: boolean;
  error: unknown;
  packages: InstalledPackage[];
  onToggle: (name: string, enabled: boolean) => void;
  onRequestUninstall: (name: string) => void;
  togglingName: string | null;
}

function InstalledSection({
  isLoading,
  isError,
  error,
  packages,
  onToggle,
  onRequestUninstall,
  togglingName,
}: InstalledSectionProps) {
  if (isLoading) {
    return (
      <p
        className="text-sm text-text-secondary"
        data-testid="marketplace-loading"
      >
        Loading installed packages…
      </p>
    );
  }
  if (isError) {
    return (
      <p
        className="text-sm text-rose-400"
        data-testid="marketplace-error"
      >
        Failed to load installed packages: {formatError(error)}
      </p>
    );
  }
  if (packages.length === 0) {
    return (
      <EmptyState
        title="No packages installed"
        description="Install a .weavepkg archive via the CLI (weave-cli pkg install) or open the Built-in tab for a one-click example."
      />
    );
  }
  return (
    <ul
      className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4"
      data-testid="marketplace-list"
    >
      {packages.map((pkg) => (
        <PackageCard
          key={pkg.name}
          pkg={pkg}
          onToggle={(enabled) => onToggle(pkg.name, enabled)}
          onRequestUninstall={() => onRequestUninstall(pkg.name)}
          togglePending={togglingName === pkg.name}
        />
      ))}
    </ul>
  );
}

interface BuiltinSectionProps {
  isLoading: boolean;
  isError: boolean;
  error: unknown;
  packages: BuiltinPackageMetadata[];
  installedSlugs: Set<string>;
  onInstall: (slug: string) => void;
  installingSlug: string | null;
}

function BuiltinSection({
  isLoading,
  isError,
  error,
  packages,
  installedSlugs,
  onInstall,
  installingSlug,
}: BuiltinSectionProps) {
  if (isLoading) {
    return (
      <p
        className="text-sm text-text-secondary"
        data-testid="marketplace-builtin-loading"
      >
        Loading built-in packages…
      </p>
    );
  }
  if (isError) {
    return (
      <p
        className="text-sm text-rose-400"
        data-testid="marketplace-builtin-error"
      >
        Failed to load built-in packages: {formatError(error)}
      </p>
    );
  }
  if (packages.length === 0) {
    return (
      <EmptyState
        title="No built-in packages"
        description="The server binary did not ship any embedded example packages."
      />
    );
  }
  return (
    <ul
      className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4"
      data-testid="marketplace-builtin-list"
    >
      {packages.map((pkg) => (
        <BuiltinPackageCard
          key={pkg.slug}
          pkg={pkg}
          alreadyInstalled={installedSlugs.has(pkg.name)}
          onInstall={() => onInstall(pkg.slug)}
          installPending={installingSlug === pkg.slug}
        />
      ))}
    </ul>
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

interface BuiltinPackageCardProps {
  pkg: BuiltinPackageMetadata;
  alreadyInstalled: boolean;
  onInstall: () => void;
  installPending: boolean;
}

function BuiltinPackageCard({
  pkg,
  alreadyInstalled,
  onInstall,
  installPending,
}: BuiltinPackageCardProps) {
  return (
    <li
      data-testid={`marketplace-builtin-card-${pkg.slug}`}
      data-already-installed={alreadyInstalled ? 'true' : 'false'}
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
            v{pkg.version} · ontology{' '}
            <span className="text-text-primary">{pkg.ontologyApiName}</span>
          </p>
        </div>
        <span
          className="inline-flex items-center gap-1 px-2 py-0.5 text-[10px] font-medium rounded-full border border-accent-cyan/40 text-accent-cyan bg-accent-cyan/10"
          data-testid={`marketplace-builtin-badge-${pkg.slug}`}
        >
          Built-in
        </span>
      </div>

      {pkg.description && (
        <p className="text-xs text-text-secondary line-clamp-3">
          {pkg.description}
        </p>
      )}

      <dl className="text-[11px] text-text-secondary grid grid-cols-2 gap-y-1">
        {pkg.author && (
          <>
            <dt className="font-medium">Author</dt>
            <dd className="text-text-primary truncate" title={pkg.author}>
              {pkg.author}
            </dd>
          </>
        )}
        {pkg.license && (
          <>
            <dt className="font-medium">License</dt>
            <dd className="text-text-primary">{pkg.license}</dd>
          </>
        )}
        <dt className="font-medium">Object Types</dt>
        <dd className="text-text-primary">{pkg.objectTypeCount}</dd>
        <dt className="font-medium">Link Types</dt>
        <dd className="text-text-primary">{pkg.linkTypeCount}</dd>
        {pkg.actionTypeCount > 0 && (
          <>
            <dt className="font-medium">Actions</dt>
            <dd className="text-text-primary">{pkg.actionTypeCount}</dd>
          </>
        )}
        {pkg.migrationCount > 0 && (
          <>
            <dt className="font-medium">Migrations</dt>
            <dd className="text-text-primary">{pkg.migrationCount}</dd>
          </>
        )}
      </dl>

      <div className="flex items-center justify-between gap-3 pt-2 border-t border-border/60 mt-auto">
        {alreadyInstalled ? (
          <span
            className="text-[11px] text-emerald-300"
            data-testid={`marketplace-builtin-already-${pkg.slug}`}
          >
            Already installed
          </span>
        ) : (
          <span className="text-[11px] text-text-secondary">
            Embedded in this server build
          </span>
        )}
        <button
          type="button"
          onClick={onInstall}
          disabled={installPending || alreadyInstalled}
          data-testid={`marketplace-builtin-install-${pkg.slug}`}
          className={[
            'px-2.5 py-1 text-[11px] font-medium rounded transition-colors',
            alreadyInstalled
              ? 'border border-border text-text-secondary cursor-not-allowed'
              : 'border border-accent-cyan/40 text-accent-cyan hover:bg-accent-cyan/10',
            installPending && !alreadyInstalled ? 'opacity-60 cursor-wait' : '',
          ].join(' ')}
        >
          {installPending && !alreadyInstalled
            ? 'Installing…'
            : alreadyInstalled
              ? 'Installed'
              : 'Install'}
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
