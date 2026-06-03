import { useEffect, useMemo, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { EmptyState } from '../common/EmptyState';
import { useToastStore } from '../../stores/toastStore';
import {
  deleteInstalledPackage,
  installBuiltinPackage,
  installPackage,
  listBuiltinPackages,
  listInstalledPackages,
  setInstalledPackageEnabled,
  type BuiltinInstallConflictMode,
  type BuiltinPackageMetadata,
  type InstalledPackage,
  type PackageInstallRequest,
  type PackageManifest,
} from '../../api/packages';
import { ApiRequestError } from '../../api/client';

const PACKAGES_KEY = ['marketplace', 'installed-packages'] as const;
const BUILTIN_KEY = ['marketplace', 'builtin-packages'] as const;

type Tab = 'installed' | 'builtin' | 'browse';

// US-454: phases the install progress bar advances through while the
// `POST /api/v2/pkg/builtin/{slug}/install` round-trip is in flight. The
// server returns a single JSON response (no streaming progress events) so
// the SPA fakes phase progression with a deterministic timer — the user
// gets visible feedback that the install is running, and the animation
// snaps to 100% when the mutation resolves.
const INSTALL_PHASES = [
  { key: 'validating', label: 'Validating manifest', durationMs: 400 },
  { key: 'importing', label: 'Importing ontology entities', durationMs: 800 },
  { key: 'migrating', label: 'Running migrations', durationMs: 600 },
  { key: 'finalizing', label: 'Finalizing', durationMs: 300 },
] as const;
const INSTALL_TOTAL_MS = INSTALL_PHASES.reduce(
  (acc, p) => acc + p.durationMs,
  0,
);

// US-413 / US-414 / US-454: local catalog of installed .weavepkg packages
// plus a "Built-in" section serving the embedded example catalog
// (Northwind / Chinook / IoT-demo) plus a "Browse" tab that aggregates the
// built-in catalog and any installed rows behind a search box.
// The Built-in and Browse tabs both drive the one-click install via
// POST /api/v2/pkg/builtin/{slug}/install with a phase-based progress bar
// while the request is in flight; uploaded archives continue to land via
// the CLI (`weave-cli pkg install`).
//
// US-054 / PC-A12 completion: each card exposes a "Details" affordance
// that opens a slide-out drawer rendering changelog / dependencies /
// reference docs; the Installed section adds an "Update" button when a
// built-in equivalent reports a different version, which calls the
// install endpoint with `onConflict=overwrite`.
type DetailsTarget =
  | { source: 'installed'; name: string }
  | { source: 'builtin'; slug: string };

export function MarketplacePage() {
  const queryClient = useQueryClient();
  const pushToast = useToastStore((s) => s.push);
  const [tab, setTab] = useState<Tab>('installed');
  const [browseQuery, setBrowseQuery] = useState('');
  const [detailsTarget, setDetailsTarget] = useState<DetailsTarget | null>(
    null,
  );

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

  // US-412 (UI surface): install a .weavepkg envelope uploaded through the
  // Installed tab's file picker. The handler at POST /api/v2/pkg/install
  // returns the same PackageInstallResponse shape as the built-in path, so
  // the success toast mirrors installBuiltinMutation.
  const uploadInstallMutation = useMutation({
    mutationFn: (body: PackageInstallRequest) => installPackage(body),
    onSuccess: (resp) => {
      queryClient.invalidateQueries({ queryKey: PACKAGES_KEY });
      pushToast({
        message: `${resp.name} v${resp.version} installed (ontology ${resp.ontology})`,
        severity: 'success',
      });
    },
    onError: (err, body) => {
      pushToast({
        message: `Failed to install ${body.manifest.name ?? 'package'}: ${formatError(err)}`,
        severity: 'error',
      });
    },
  });

  // US-054: "Update" reuses the install endpoint with onConflict=overwrite
  // so the importer runs in `replace` mode against the existing ontology
  // entities. We split it from `installBuiltinMutation` so the toast +
  // progress bar can distinguish "fresh install" from "in-place update".
  const updateBuiltinMutation = useMutation({
    mutationFn: (slug: string) => installBuiltinPackage(slug, 'overwrite'),
    onSuccess: (resp) => {
      queryClient.invalidateQueries({ queryKey: PACKAGES_KEY });
      pushToast({
        message: `${resp.name} updated to v${resp.version}`,
        severity: 'success',
      });
    },
    onError: (err, slug) => {
      pushToast({
        message: `Failed to update ${slug}: ${formatError(err)}`,
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

  // US-054: per-name lookup so the Installed card can offer an "Update"
  // affordance when a built-in equivalent reports a different version.
  // Keyed by package name (mirrors how the registry deduplicates rows on
  // the way in via `installed_packages.name`).
  const builtinByName = useMemo(() => {
    const map = new Map<string, BuiltinPackageMetadata>();
    for (const b of builtinPackages) map.set(b.name, b);
    return map;
  }, [builtinPackages]);

  const detailsContent = useMemo<DetailsContent | null>(() => {
    if (!detailsTarget) return null;
    if (detailsTarget.source === 'installed') {
      const row = packages.find((p) => p.name === detailsTarget.name);
      if (!row) return null;
      const builtin = builtinByName.get(row.name);
      return {
        kind: 'installed',
        name: row.name,
        installed: row,
        builtin,
      };
    }
    const row = builtinPackages.find((b) => b.slug === detailsTarget.slug);
    if (!row) return null;
    const installedRow = packages.find((p) => p.name === row.name);
    return {
      kind: 'builtin',
      name: row.name,
      builtin: row,
      installed: installedRow,
    };
  }, [detailsTarget, packages, builtinPackages, builtinByName]);

  // US-054: any in-flight install or update should keep the progress bar
  // tied to the slug actually being mutated (overwrite vs fresh install
  // share the same backend endpoint but the UI affordance differs).
  const installingSlug = installBuiltinMutation.isPending
    ? installBuiltinMutation.variables ?? null
    : null;
  const updatingSlug = updateBuiltinMutation.isPending
    ? updateBuiltinMutation.variables ?? null
    : null;

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
          <TabButton
            active={tab === 'browse'}
            onClick={() => setTab('browse')}
            testId="marketplace-tab-browse"
          >
            Browse
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
            builtinByName={builtinByName}
            onToggle={(name, enabled) =>
              enableMutation.mutate({ name, enabled })
            }
            onRequestUninstall={(name) => setPendingUninstall(name)}
            onUpdate={(slug) => updateBuiltinMutation.mutate(slug)}
            onShowDetails={(name) =>
              setDetailsTarget({ source: 'installed', name })
            }
            togglingName={
              enableMutation.isPending
                ? enableMutation.variables?.name ?? null
                : null
            }
            updatingSlug={updatingSlug}
            onUploadInstall={(body) => uploadInstallMutation.mutate(body)}
            uploadPending={uploadInstallMutation.isPending}
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
            onShowDetails={(slug) =>
              setDetailsTarget({ source: 'builtin', slug })
            }
            installingSlug={installingSlug}
          />
        )}

        {tab === 'browse' && (
          <BrowseSection
            isLoading={builtinQuery.isLoading || packagesQuery.isLoading}
            isError={builtinQuery.isError}
            error={builtinQuery.error}
            installed={packages}
            builtin={builtinPackages}
            installedSlugs={installedSlugs}
            search={browseQuery}
            onSearch={setBrowseQuery}
            onInstall={(slug) => installBuiltinMutation.mutate(slug)}
            onShowDetails={(target) => setDetailsTarget(target)}
            installingSlug={installingSlug}
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

      {detailsContent && (
        <PackageDetailsDrawer
          content={detailsContent}
          onClose={() => setDetailsTarget(null)}
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
  builtinByName: Map<string, BuiltinPackageMetadata>;
  onToggle: (name: string, enabled: boolean) => void;
  onRequestUninstall: (name: string) => void;
  onUpdate: (slug: string) => void;
  onShowDetails: (name: string) => void;
  togglingName: string | null;
  updatingSlug: string | null;
  onUploadInstall: (body: PackageInstallRequest) => void;
  uploadPending: boolean;
}

function InstalledSection({
  isLoading,
  isError,
  error,
  packages,
  builtinByName,
  onToggle,
  onRequestUninstall,
  onUpdate,
  onShowDetails,
  togglingName,
  updatingSlug,
  onUploadInstall,
  uploadPending,
}: InstalledSectionProps) {
  // The upload affordance lives above the loading / error / empty branches so
  // an operator can install a .weavepkg even before the catalog has loaded
  // (or while it is empty).
  const uploadControl = (
    <UploadInstallControl
      onInstall={onUploadInstall}
      installPending={uploadPending}
    />
  );

  if (isLoading) {
    return (
      <div className="flex flex-col gap-4">
        {uploadControl}
        <p
          className="text-sm text-text-secondary"
          data-testid="marketplace-loading"
        >
          Loading installed packages…
        </p>
      </div>
    );
  }
  if (isError) {
    return (
      <div className="flex flex-col gap-4">
        {uploadControl}
        <p
          className="text-sm text-rose-400"
          data-testid="marketplace-error"
        >
          Failed to load installed packages: {formatError(error)}
        </p>
      </div>
    );
  }
  if (packages.length === 0) {
    return (
      <div className="flex flex-col gap-4">
        {uploadControl}
        <EmptyState
          title="No packages installed"
          description="Upload a .weavepkg.json envelope above, install a binary archive via the CLI (weave-cli pkg install), or open the Built-in tab for a one-click example."
        />
      </div>
    );
  }
  return (
    <div className="flex flex-col gap-4">
      {uploadControl}
      {updatingSlug && (
        <InstallProgressBar
          slug={updatingSlug}
          testId="marketplace-update-progress"
          label="Updating"
        />
      )}
      <ul
        className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4"
        data-testid="marketplace-list"
      >
        {packages.map((pkg) => {
          const builtin = builtinByName.get(pkg.name);
          const updateAvailable =
            builtin !== undefined && builtin.version !== pkg.version;
          return (
            <PackageCard
              key={pkg.name}
              pkg={pkg}
              updateAvailable={updateAvailable}
              builtinMatch={builtin}
              onToggle={(enabled) => onToggle(pkg.name, enabled)}
              onRequestUninstall={() => onRequestUninstall(pkg.name)}
              onUpdate={builtin ? () => onUpdate(builtin.slug) : undefined}
              onShowDetails={() => onShowDetails(pkg.name)}
              togglePending={togglingName === pkg.name}
              updatePending={builtin ? updatingSlug === builtin.slug : false}
            />
          );
        })}
      </ul>
    </div>
  );
}

// US-412 (UI surface): outcome of parsing a selected file into a
// PackageInstallRequest. `ok` carries the validated envelope plus the
// derived display name; `error` carries an operator-facing message.
type ParsedEnvelope =
  | { ok: true; request: Omit<PackageInstallRequest, 'onConflict'> }
  | { ok: false; error: string };

// parseWeavepkgEnvelope validates an uploaded file's text into the install
// envelope the server expects. A .weavepkg is a binary ZIP that the browser
// cannot unpack without a heavyweight dependency, so the UI accepts the
// JSON envelope shape instead — either a `.weavepkg.json` produced by the
// exporter, or a hand-authored `{manifest, ontology, migrations}` body
// equivalent to what `weave-cli pkg install` POSTs. Binary archives are
// detected via the ZIP local-file-header magic and rejected with a hint to
// use the CLI.
function parseWeavepkgEnvelope(
  text: string,
  filename: string,
): ParsedEnvelope {
  // ZIP archives start with the local file header signature "PK\x03\x04".
  if (text.startsWith('PK')) {
    return {
      ok: false,
      error: `${filename} is a binary .weavepkg archive. Install it with the CLI (weave-cli pkg install) or upload its .weavepkg.json envelope.`,
    };
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch {
    return {
      ok: false,
      error: `${filename} is not valid JSON. Upload a .weavepkg.json envelope or use the CLI for binary archives.`,
    };
  }
  if (typeof parsed !== 'object' || parsed === null) {
    return { ok: false, error: `${filename} is not a package envelope.` };
  }
  const obj = parsed as Record<string, unknown>;
  const manifest = obj.manifest;
  if (typeof manifest !== 'object' || manifest === null) {
    return {
      ok: false,
      error: `${filename} is missing a "manifest" object.`,
    };
  }
  const m = manifest as Record<string, unknown>;
  if (typeof m.name !== 'string' || m.name.trim() === '') {
    return { ok: false, error: 'manifest.name is required.' };
  }
  if (typeof m.version !== 'string' || m.version.trim() === '') {
    return { ok: false, error: 'manifest.version is required.' };
  }
  if (obj.ontology === undefined || obj.ontology === null) {
    return { ok: false, error: 'ontology body is required.' };
  }
  let migrations: PackageInstallRequest['migrations'];
  if (obj.migrations !== undefined) {
    if (!Array.isArray(obj.migrations)) {
      return { ok: false, error: 'migrations must be an array when present.' };
    }
    migrations = obj.migrations as PackageInstallRequest['migrations'];
  }
  return {
    ok: true,
    request: {
      manifest: manifest as PackageManifest,
      ontology: obj.ontology,
      ...(migrations ? { migrations } : {}),
    },
  };
}

interface UploadInstallControlProps {
  onInstall: (body: PackageInstallRequest) => void;
  installPending: boolean;
}

// UploadInstallControl is the Installed-tab header affordance that lets an
// operator install a package straight from the browser instead of dropping
// to the CLI. It reads the selected file as text, validates it into a
// PackageInstallRequest, lets the operator pick an onConflict strategy, and
// POSTs the envelope to /api/v2/pkg/install.
function UploadInstallControl({
  onInstall,
  installPending,
}: UploadInstallControlProps) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [filename, setFilename] = useState<string>('');
  const [parsed, setParsed] = useState<
    Omit<PackageInstallRequest, 'onConflict'> | null
  >(null);
  const [error, setError] = useState<string>('');
  const [onConflict, setOnConflict] =
    useState<BuiltinInstallConflictMode>('fail');

  const handleFile = async (file: File | undefined) => {
    setError('');
    setParsed(null);
    if (!file) {
      setFilename('');
      return;
    }
    setFilename(file.name);
    let text: string;
    try {
      text = await file.text();
    } catch {
      setError(`Could not read ${file.name}.`);
      return;
    }
    const result = parseWeavepkgEnvelope(text, file.name);
    if (result.ok) {
      setParsed(result.request);
    } else {
      setError(result.error);
    }
  };

  const reset = () => {
    setFilename('');
    setParsed(null);
    setError('');
    if (inputRef.current) inputRef.current.value = '';
  };

  return (
    <div
      className="border border-border rounded-lg bg-bg-secondary/50 p-4 flex flex-col gap-3"
      data-testid="marketplace-upload-control"
    >
      <div>
        <h2 className="text-sm font-sans font-semibold text-text-primary">
          Install from file
        </h2>
        <p className="text-[11px] text-text-secondary mt-0.5">
          Upload a <span className="font-mono">.weavepkg.json</span> envelope
          (manifest + ontology). For binary <span className="font-mono">.weavepkg</span>{' '}
          archives use the CLI: <span className="font-mono">weave-cli pkg install</span>.
        </p>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <label className="flex items-center gap-2 text-xs text-text-secondary">
          <span className="sr-only">Choose package file</span>
          <input
            ref={inputRef}
            type="file"
            accept=".weavepkg,.json,application/json"
            data-testid="marketplace-upload-input"
            onChange={(e) => {
              void handleFile(e.target.files?.[0]);
            }}
            className="text-xs file:mr-2 file:px-2.5 file:py-1 file:text-[11px] file:font-medium file:rounded file:border file:border-accent-cyan/40 file:text-accent-cyan file:bg-transparent file:cursor-pointer hover:file:bg-accent-cyan/10 text-text-secondary"
          />
        </label>

        <label className="flex items-center gap-1.5 text-[11px] text-text-secondary">
          <span>On conflict</span>
          <select
            value={onConflict}
            data-testid="marketplace-upload-onconflict"
            onChange={(e) =>
              setOnConflict(e.target.value as BuiltinInstallConflictMode)
            }
            className="px-2 py-1 text-[11px] bg-bg-tertiary border border-border rounded text-text-primary focus:outline-none focus:border-accent-cyan"
          >
            <option value="fail">fail</option>
            <option value="overwrite">overwrite</option>
            <option value="skip">skip</option>
          </select>
        </label>

        <button
          type="button"
          disabled={!parsed || installPending}
          data-testid="marketplace-upload-install"
          onClick={() => {
            if (!parsed) return;
            onInstall({ ...parsed, onConflict });
          }}
          className={[
            'px-2.5 py-1 text-[11px] font-medium rounded transition-colors border border-accent-cyan/40 text-accent-cyan hover:bg-accent-cyan/10',
            !parsed || installPending ? 'opacity-60 cursor-not-allowed' : '',
          ].join(' ')}
        >
          {installPending ? 'Installing…' : 'Install'}
        </button>

        {filename && (
          <button
            type="button"
            onClick={reset}
            disabled={installPending}
            data-testid="marketplace-upload-clear"
            className="px-2 py-1 text-[11px] text-text-secondary border border-border rounded hover:bg-bg-tertiary transition-colors"
          >
            Clear
          </button>
        )}
      </div>

      {filename && (
        <p
          className="text-[11px] text-text-secondary"
          data-testid="marketplace-upload-filename"
        >
          Selected: <span className="font-mono text-text-primary">{filename}</span>
          {parsed && (
            <span className="text-emerald-300">
              {' · '}
              {parsed.manifest.name} v{parsed.manifest.version}
            </span>
          )}
        </p>
      )}

      {error && (
        <p
          className="text-[11px] text-rose-400"
          data-testid="marketplace-upload-error"
        >
          {error}
        </p>
      )}
    </div>
  );
}

interface BuiltinSectionProps {
  isLoading: boolean;
  isError: boolean;
  error: unknown;
  packages: BuiltinPackageMetadata[];
  installedSlugs: Set<string>;
  onInstall: (slug: string) => void;
  onShowDetails: (slug: string) => void;
  installingSlug: string | null;
}

function BuiltinSection({
  isLoading,
  isError,
  error,
  packages,
  installedSlugs,
  onInstall,
  onShowDetails,
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
    <div className="flex flex-col gap-4">
      {installingSlug && (
        <InstallProgressBar slug={installingSlug} testId="marketplace-builtin-progress" />
      )}
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
            onShowDetails={() => onShowDetails(pkg.slug)}
            installPending={installingSlug === pkg.slug}
          />
        ))}
      </ul>
    </div>
  );
}

interface BrowseSectionProps {
  isLoading: boolean;
  isError: boolean;
  error: unknown;
  installed: InstalledPackage[];
  builtin: BuiltinPackageMetadata[];
  installedSlugs: Set<string>;
  search: string;
  onSearch: (q: string) => void;
  onInstall: (slug: string) => void;
  onShowDetails: (target: DetailsTarget) => void;
  installingSlug: string | null;
}

interface BrowseEntry {
  key: string;
  name: string;
  description: string;
  source: 'builtin' | 'installed';
  installed: boolean;
  builtin?: BuiltinPackageMetadata;
  installedRow?: InstalledPackage;
}

function BrowseSection({
  isLoading,
  isError,
  error,
  installed,
  builtin,
  installedSlugs,
  search,
  onSearch,
  onInstall,
  onShowDetails,
  installingSlug,
}: BrowseSectionProps) {
  const entries = useMemo<BrowseEntry[]>(() => {
    const byName = new Map<string, BrowseEntry>();
    for (const pkg of builtin) {
      byName.set(pkg.name, {
        key: `builtin:${pkg.slug}`,
        name: pkg.name,
        description: pkg.description ?? '',
        source: 'builtin',
        installed: installedSlugs.has(pkg.name),
        builtin: pkg,
      });
    }
    for (const row of installed) {
      const existing = byName.get(row.name);
      if (existing) {
        existing.installed = true;
        existing.installedRow = row;
      } else {
        byName.set(row.name, {
          key: `installed:${row.name}`,
          name: row.name,
          description: row.manifest?.description ?? '',
          source: 'installed',
          installed: true,
          installedRow: row,
        });
      }
    }
    return Array.from(byName.values()).sort((a, b) =>
      a.name.localeCompare(b.name),
    );
  }, [builtin, installed, installedSlugs]);

  const trimmed = search.trim().toLowerCase();
  const filtered = useMemo(
    () =>
      trimmed === ''
        ? entries
        : entries.filter(
            (e) =>
              e.name.toLowerCase().includes(trimmed) ||
              e.description.toLowerCase().includes(trimmed),
          ),
    [entries, trimmed],
  );

  if (isLoading) {
    return (
      <p
        className="text-sm text-text-secondary"
        data-testid="marketplace-browse-loading"
      >
        Loading catalog…
      </p>
    );
  }
  if (isError) {
    return (
      <p
        className="text-sm text-rose-400"
        data-testid="marketplace-browse-error"
      >
        Failed to load catalog: {formatError(error)}
      </p>
    );
  }

  return (
    <div className="flex flex-col gap-4" data-testid="marketplace-browse-section">
      <div className="flex items-center gap-3">
        <label className="flex-1 flex items-center gap-2">
          <span className="sr-only">Search packages</span>
          <input
            type="search"
            value={search}
            onChange={(e) => onSearch(e.target.value)}
            placeholder="Search packages by name or description"
            data-testid="marketplace-browse-search"
            className="flex-1 px-3 py-1.5 text-xs bg-bg-tertiary border border-border rounded text-text-primary placeholder:text-text-secondary focus:outline-none focus:border-accent-cyan"
          />
        </label>
        <span
          className="text-[11px] text-text-secondary"
          data-testid="marketplace-browse-count"
        >
          {filtered.length} of {entries.length}
        </span>
      </div>

      {installingSlug && (
        <InstallProgressBar
          slug={installingSlug}
          testId="marketplace-browse-progress"
        />
      )}

      {filtered.length === 0 ? (
        <EmptyState
          title={
            entries.length === 0 ? 'Catalog is empty' : 'No packages match your search'
          }
          description={
            entries.length === 0
              ? 'Install a .weavepkg archive via the CLI or open the Built-in tab.'
              : 'Try a different search term or clear the filter.'
          }
        />
      ) : (
        <ul
          className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4"
          data-testid="marketplace-browse-list"
        >
          {filtered.map((entry) => (
            <BrowseEntryCard
              key={entry.key}
              entry={entry}
              onInstall={(slug) => onInstall(slug)}
              onShowDetails={() =>
                onShowDetails(
                  entry.builtin
                    ? { source: 'builtin', slug: entry.builtin.slug }
                    : { source: 'installed', name: entry.name },
                )
              }
              installPending={
                entry.builtin ? installingSlug === entry.builtin.slug : false
              }
            />
          ))}
        </ul>
      )}
    </div>
  );
}

interface BrowseEntryCardProps {
  entry: BrowseEntry;
  onInstall: (slug: string) => void;
  onShowDetails: () => void;
  installPending: boolean;
}

function BrowseEntryCard({
  entry,
  onInstall,
  onShowDetails,
  installPending,
}: BrowseEntryCardProps) {
  const installable = !entry.installed && entry.builtin;
  const version = entry.builtin?.version ?? entry.installedRow?.version ?? '';
  const ontology =
    entry.builtin?.ontologyApiName ?? entry.installedRow?.ontology ?? '';
  return (
    <li
      data-testid={`marketplace-browse-card-${entry.name}`}
      data-package-name={entry.name}
      data-installed={entry.installed ? 'true' : 'false'}
      className="border border-border rounded-lg bg-bg-secondary/50 p-4 flex flex-col gap-3"
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h3
            className="text-sm font-sans font-semibold text-text-primary truncate"
            title={entry.name}
          >
            {entry.name}
          </h3>
          <p className="text-[11px] font-mono text-text-secondary mt-0.5">
            {version ? `v${version}` : ''}
            {ontology ? ` · ontology ` : ''}
            {ontology && (
              <span className="text-text-primary">{ontology}</span>
            )}
          </p>
        </div>
        <span
          data-testid={`marketplace-browse-status-${entry.name}`}
          className={[
            'inline-flex items-center px-2 py-0.5 text-[10px] font-medium rounded-full border',
            entry.installed
              ? 'border-emerald-500/40 text-emerald-300 bg-emerald-500/10'
              : 'border-accent-cyan/40 text-accent-cyan bg-accent-cyan/10',
          ].join(' ')}
        >
          {entry.installed ? 'Installed' : 'Available'}
        </span>
      </div>

      {entry.description && (
        <p className="text-xs text-text-secondary line-clamp-3">
          {entry.description}
        </p>
      )}

      <div className="flex items-center justify-between gap-3 pt-2 border-t border-border/60 mt-auto">
        <span
          className="text-[11px] text-text-secondary"
          data-testid={`marketplace-browse-source-${entry.name}`}
        >
          {entry.source === 'builtin' ? 'Built-in catalog' : 'Local install'}
        </span>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={onShowDetails}
            data-testid={`marketplace-browse-details-${entry.name}`}
            className="px-2.5 py-1 text-[11px] font-medium rounded transition-colors border border-border text-text-secondary hover:bg-bg-tertiary"
          >
            Details
          </button>
          {installable && entry.builtin ? (
            <button
              type="button"
              onClick={() => onInstall(entry.builtin!.slug)}
              disabled={installPending}
              data-testid={`marketplace-browse-install-${entry.name}`}
              className={[
                'px-2.5 py-1 text-[11px] font-medium rounded transition-colors border border-accent-cyan/40 text-accent-cyan hover:bg-accent-cyan/10',
                installPending ? 'opacity-60 cursor-wait' : '',
              ].join(' ')}
            >
              {installPending ? 'Installing…' : 'Install'}
            </button>
          ) : (
            <span
              className="text-[11px] text-text-secondary"
              data-testid={`marketplace-browse-already-${entry.name}`}
            >
              Already installed
            </span>
          )}
        </div>
      </div>
    </li>
  );
}

interface InstallProgressBarProps {
  slug: string;
  testId: string;
  label?: string;
}

// InstallProgressBar drives a deterministic phase-based progress animation
// while the one-click install mutation is in flight. The `slug` prop is the
// reset key — when the active install changes (new slug, or install
// resolved) the timer restarts from phase 0. The component never reaches
// 100% on its own; the parent unmounts it when the mutation resolves so the
// final state is "snap to gone" rather than "linger at full".
function InstallProgressBar({ slug, testId, label }: InstallProgressBarProps) {
  const [elapsedMs, setElapsedMs] = useState(0);
  const startedAtRef = useRef<number>(0);

  useEffect(() => {
    startedAtRef.current = Date.now();
    setElapsedMs(0);
    let frame = 0;
    let running = true;
    const tick = () => {
      if (!running) return;
      const now = Date.now();
      setElapsedMs(now - startedAtRef.current);
      frame = window.setTimeout(tick, 80);
    };
    frame = window.setTimeout(tick, 80);
    return () => {
      running = false;
      window.clearTimeout(frame);
    };
  }, [slug]);

  const cap = INSTALL_TOTAL_MS - 200;
  const clamped = Math.min(elapsedMs, cap);
  const percent = Math.min(95, Math.round((clamped / INSTALL_TOTAL_MS) * 100));

  let acc = 0;
  let phaseLabel: string = INSTALL_PHASES[0].label;
  for (const phase of INSTALL_PHASES) {
    if (clamped <= acc + phase.durationMs) {
      phaseLabel = phase.label;
      break;
    }
    acc += phase.durationMs;
    phaseLabel = phase.label;
  }

  return (
    <div
      role="progressbar"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={percent}
      aria-label={`${label ?? 'Installing'} ${slug}`}
      data-testid={testId}
      data-slug={slug}
      className="border border-accent-cyan/30 bg-accent-cyan/5 rounded-md px-3 py-2 flex flex-col gap-1.5"
    >
      <div className="flex items-center justify-between text-[11px] text-text-secondary">
        <span>
          {label ?? 'Installing'}{' '}
          <span className="text-text-primary font-mono">{slug}</span>
          {' · '}
          <span data-testid={`${testId}-phase`}>{phaseLabel}</span>
        </span>
        <span data-testid={`${testId}-percent`} className="font-mono">
          {percent}%
        </span>
      </div>
      <div className="h-1.5 w-full bg-bg-tertiary rounded overflow-hidden">
        <div
          className="h-full bg-accent-cyan transition-all duration-150 ease-out"
          style={{ width: `${percent}%` }}
        />
      </div>
    </div>
  );
}

interface PackageCardProps {
  pkg: InstalledPackage;
  updateAvailable: boolean;
  builtinMatch?: BuiltinPackageMetadata;
  onToggle: (enabled: boolean) => void;
  onRequestUninstall: () => void;
  onUpdate?: () => void;
  onShowDetails: () => void;
  togglePending: boolean;
  updatePending: boolean;
}

function PackageCard({
  pkg,
  updateAvailable,
  builtinMatch,
  onToggle,
  onRequestUninstall,
  onUpdate,
  onShowDetails,
  togglePending,
  updatePending,
}: PackageCardProps) {
  const manifest = pkg.manifest ?? null;
  const dependencies = manifestDependencies(manifest);
  const description = manifest?.description ?? '';
  const author = manifest?.author ?? '';
  const license = manifest?.license ?? '';

  return (
    <li
      data-testid={`marketplace-card-${pkg.name}`}
      data-package-name={pkg.name}
      data-package-version={pkg.version}
      data-enabled={pkg.enabled ? 'true' : 'false'}
      data-update-available={updateAvailable ? 'true' : 'false'}
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
        <div className="flex flex-col items-end gap-1">
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
          {updateAvailable && builtinMatch && (
            <span
              className="inline-flex items-center px-2 py-0.5 text-[10px] font-medium rounded-full border border-amber-500/40 text-amber-200 bg-amber-500/10"
              data-testid={`marketplace-update-badge-${pkg.name}`}
              data-target-version={builtinMatch.version}
            >
              Update to v{builtinMatch.version}
            </span>
          )}
        </div>
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
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={onShowDetails}
            data-testid={`marketplace-details-${pkg.name}`}
            className="px-2.5 py-1 text-[11px] font-medium text-text-secondary border border-border rounded hover:bg-bg-tertiary transition-colors"
          >
            Details
          </button>
          {updateAvailable && onUpdate && (
            <button
              type="button"
              onClick={onUpdate}
              disabled={updatePending}
              data-testid={`marketplace-update-${pkg.name}`}
              className={[
                'px-2.5 py-1 text-[11px] font-medium text-amber-200 border border-amber-500/40 rounded hover:bg-amber-500/10 transition-colors',
                updatePending ? 'opacity-60 cursor-wait' : '',
              ].join(' ')}
            >
              {updatePending ? 'Updating…' : 'Update'}
            </button>
          )}
          <button
            type="button"
            onClick={onRequestUninstall}
            data-testid={`marketplace-uninstall-${pkg.name}`}
            className="px-2.5 py-1 text-[11px] font-medium text-rose-300 border border-rose-500/40 rounded hover:bg-rose-500/10 transition-colors"
          >
            Uninstall
          </button>
        </div>
      </div>
    </li>
  );
}

interface BuiltinPackageCardProps {
  pkg: BuiltinPackageMetadata;
  alreadyInstalled: boolean;
  onInstall: () => void;
  onShowDetails: () => void;
  installPending: boolean;
}

function BuiltinPackageCard({
  pkg,
  alreadyInstalled,
  onInstall,
  onShowDetails,
  installPending,
}: BuiltinPackageCardProps) {
  return (
    <li
      data-testid={`marketplace-builtin-card-${pkg.slug}`}
      data-package-name={pkg.name}
      data-package-slug={pkg.slug}
      data-package-version={pkg.version}
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
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={onShowDetails}
            data-testid={`marketplace-builtin-details-${pkg.slug}`}
            className="px-2.5 py-1 text-[11px] font-medium rounded transition-colors border border-border text-text-secondary hover:bg-bg-tertiary"
          >
            Details
          </button>
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

// US-054: details drawer state shape. `kind` discriminates whether the row
// originated from the .weavepkg registry (`installed`) or the embedded
// example catalog (`builtin`); the matching peer is folded in so the
// drawer can cross-reference an installed row with its catalog version.
type DetailsContent =
  | {
      kind: 'installed';
      name: string;
      installed: InstalledPackage;
      builtin?: BuiltinPackageMetadata;
    }
  | {
      kind: 'builtin';
      name: string;
      builtin: BuiltinPackageMetadata;
      installed?: InstalledPackage;
    };

interface PackageDetailsDrawerProps {
  content: DetailsContent;
  onClose: () => void;
}

// PackageDetailsDrawer is the AC's "包详情抽屉" — a slide-in panel that
// surfaces the manifest's changelog/description, the dependency graph,
// and any reference docs (manifest contents for installed rows, ontology
// + entity counts for built-in rows). The drawer is render-only — every
// affordance (install / update / uninstall) lives back on the card so
// the operator can always see the state badge while deciding.
function PackageDetailsDrawer({ content, onClose }: PackageDetailsDrawerProps) {
  const manifest =
    content.kind === 'installed' ? content.installed.manifest : null;
  const builtin =
    content.kind === 'builtin' ? content.builtin : content.builtin;
  const installed =
    content.kind === 'installed' ? content.installed : content.installed;
  const dependencies = (() => {
    if (content.kind === 'installed') {
      return manifestDependencies(manifest);
    }
    return (content.builtin.dependencies ?? []).map(
      (d) => [d.name, d.version] as [string, string],
    );
  })();
  const description =
    content.kind === 'installed'
      ? manifest?.description ?? ''
      : content.builtin.description ?? '';
  const author =
    content.kind === 'installed'
      ? manifest?.author ?? ''
      : content.builtin.author ?? '';
  const license =
    content.kind === 'installed'
      ? manifest?.license ?? ''
      : content.builtin.license ?? '';
  const version =
    content.kind === 'installed'
      ? content.installed.version
      : content.builtin.version;
  const ontology =
    content.kind === 'installed'
      ? content.installed.ontology
      : content.builtin.ontologyApiName;
  const contents = manifest?.contents ?? null;
  // References surface differs by source — for installed rows we list
  // the manifest's referenced `contents` blocks; for built-in rows we
  // expose the catalog stats (object types, link types, etc).
  const references: Array<[string, string]> = (() => {
    const out: Array<[string, string]> = [];
    if (content.kind === 'builtin') {
      out.push(['Object Types', String(content.builtin.objectTypeCount)]);
      out.push(['Link Types', String(content.builtin.linkTypeCount)]);
      if (content.builtin.actionTypeCount > 0) {
        out.push(['Action Types', String(content.builtin.actionTypeCount)]);
      }
      if (content.builtin.functionCount > 0) {
        out.push(['Functions', String(content.builtin.functionCount)]);
      }
      if (content.builtin.migrationCount > 0) {
        out.push(['Migrations', String(content.builtin.migrationCount)]);
      }
      if (content.builtin.minWeaveVersion) {
        out.push(['Min Weave', content.builtin.minWeaveVersion]);
      }
    } else {
      out.push(['Migrations', String(content.installed.migrations.length)]);
      if (contents) {
        for (const key of Object.keys(contents)) {
          const v = contents[key];
          if (typeof v === 'number') {
            out.push([key, String(v)]);
          } else if (Array.isArray(v)) {
            out.push([key, String(v.length)]);
          }
        }
      }
      if (manifest?.minWeaveVersion) {
        out.push(['Min Weave', manifest.minWeaveVersion]);
      }
    }
    return out;
  })();
  const installedAt =
    content.kind === 'installed' ? content.installed.installedAt : '';
  const showCrossLink = content.kind === 'installed' ? builtin : installed;
  const crossLinkLabel = (() => {
    if (content.kind === 'installed' && builtin) {
      if (builtin.version === content.installed.version) {
        return `Up-to-date with built-in v${builtin.version}`;
      }
      return `Built-in catalog ships v${builtin.version}`;
    }
    if (content.kind === 'builtin' && installed) {
      return `Currently installed at v${installed.version}`;
    }
    return '';
  })();

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label={`Package details for ${content.name}`}
      data-testid="marketplace-details-drawer"
      data-package-name={content.name}
      data-source={content.kind}
      className="fixed inset-0 z-50 flex justify-end bg-black/40"
      onClick={(e) => {
        // Click outside the drawer panel closes the drawer.
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <aside
        className="h-full w-full max-w-md bg-bg-secondary border-l border-border shadow-2xl flex flex-col"
        data-testid="marketplace-details-panel"
      >
        <header className="flex items-start justify-between gap-3 px-5 py-4 border-b border-border">
          <div className="min-w-0">
            <h2 className="text-sm font-sans font-semibold text-text-primary truncate">
              {content.name}
            </h2>
            <p className="text-[11px] font-mono text-text-secondary mt-1">
              v{version}
              {ontology ? (
                <>
                  {' · ontology '}
                  <span className="text-text-primary">{ontology}</span>
                </>
              ) : null}
            </p>
          </div>
          <button
            type="button"
            onClick={onClose}
            data-testid="marketplace-details-close"
            className="px-2 py-1 text-[11px] text-text-secondary border border-border rounded hover:bg-bg-tertiary transition-colors"
          >
            Close
          </button>
        </header>

        <div className="flex-1 overflow-y-auto px-5 py-4 flex flex-col gap-5">
          <section data-testid="marketplace-details-changelog">
            <h3 className="text-[11px] uppercase tracking-wider text-text-secondary mb-1.5">
              Changelog
            </h3>
            {description ? (
              <p
                className="text-xs text-text-primary whitespace-pre-wrap"
                data-testid="marketplace-details-changelog-body"
              >
                {description}
              </p>
            ) : (
              <p
                className="text-xs text-text-secondary italic"
                data-testid="marketplace-details-changelog-empty"
              >
                No changelog provided by the package manifest.
              </p>
            )}
          </section>

          <section data-testid="marketplace-details-dependencies">
            <h3 className="text-[11px] uppercase tracking-wider text-text-secondary mb-1.5">
              Dependencies
            </h3>
            {dependencies.length === 0 ? (
              <p
                className="text-xs text-text-secondary italic"
                data-testid="marketplace-details-dependencies-empty"
              >
                No dependencies declared.
              </p>
            ) : (
              <ul
                className="flex flex-col gap-1 text-xs"
                data-testid="marketplace-details-dependencies-list"
              >
                {dependencies.map(([name, ver]) => (
                  <li
                    key={name}
                    data-testid={`marketplace-details-dependency-${name}`}
                    data-dependency-name={name}
                    data-dependency-version={ver}
                    className="font-mono text-text-primary flex items-center justify-between gap-3 border border-border/60 rounded px-2 py-1"
                  >
                    <span>{name}</span>
                    <span className="text-text-secondary">{ver}</span>
                  </li>
                ))}
              </ul>
            )}
          </section>

          <section data-testid="marketplace-details-references">
            <h3 className="text-[11px] uppercase tracking-wider text-text-secondary mb-1.5">
              References
            </h3>
            {references.length === 0 ? (
              <p
                className="text-xs text-text-secondary italic"
                data-testid="marketplace-details-references-empty"
              >
                No references declared.
              </p>
            ) : (
              <dl
                className="text-xs grid grid-cols-2 gap-y-1"
                data-testid="marketplace-details-references-list"
              >
                {references.map(([key, value]) => (
                  <div
                    key={key}
                    className="contents"
                    data-testid={`marketplace-details-reference-${key.toLowerCase().replace(/\s+/g, '-')}`}
                    data-reference-key={key}
                    data-reference-value={value}
                  >
                    <dt className="text-text-secondary">{key}</dt>
                    <dd className="text-text-primary font-mono truncate">
                      {value}
                    </dd>
                  </div>
                ))}
              </dl>
            )}
          </section>

          <section
            className="text-[11px] text-text-secondary border-t border-border/60 pt-3 flex flex-col gap-1"
            data-testid="marketplace-details-meta"
          >
            {author && (
              <div>
                <span className="text-text-secondary">Author: </span>
                <span className="text-text-primary">{author}</span>
              </div>
            )}
            {license && (
              <div>
                <span className="text-text-secondary">License: </span>
                <span className="text-text-primary">{license}</span>
              </div>
            )}
            {installedAt && (
              <div>
                <span className="text-text-secondary">Installed: </span>
                <span className="text-text-primary">
                  {formatTimestamp(installedAt)}
                </span>
              </div>
            )}
            {showCrossLink && crossLinkLabel && (
              <div data-testid="marketplace-details-cross-link">
                {crossLinkLabel}
              </div>
            )}
          </section>
        </div>
      </aside>
    </div>
  );
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
