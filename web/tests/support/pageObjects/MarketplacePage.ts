import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for `/marketplace` — the Marketplace UI rendered by
 * `src/components/marketplace/MarketplacePage.tsx`.
 *
 * Mirrors the data-testid contract established by US-413 / US-414 /
 * US-454 and extended by US-054 / PC-A12 to cover the details drawer
 * (changelog / dependencies / references) and the in-place "Update"
 * flow for installed packages whose built-in equivalent ships a newer
 * version.
 *
 * Lookup helpers keyed on package name / slug so the BDD scenarios
 * never depend on row order — same template as the admin list page
 * objects (US-029 ObjectTypeAdminPage, US-032 InterfaceAdminPage).
 */
export class MarketplacePage {
  readonly page: Page;
  readonly root: Locator;
  readonly tabs: Locator;
  readonly tabInstalled: Locator;
  readonly tabBuiltin: Locator;
  readonly tabBrowse: Locator;

  // Installed section
  readonly installedList: Locator;
  readonly installedLoading: Locator;
  readonly installedError: Locator;
  readonly updateProgress: Locator;

  // Built-in section
  readonly builtinList: Locator;
  readonly builtinLoading: Locator;
  readonly builtinError: Locator;
  readonly builtinProgress: Locator;

  // Browse section
  readonly browseList: Locator;
  readonly browseSearch: Locator;
  readonly browseCount: Locator;
  readonly browseProgress: Locator;

  // Details drawer
  readonly detailsDrawer: Locator;
  readonly detailsPanel: Locator;
  readonly detailsClose: Locator;
  readonly detailsChangelog: Locator;
  readonly detailsChangelogBody: Locator;
  readonly detailsChangelogEmpty: Locator;
  readonly detailsDependencies: Locator;
  readonly detailsDependenciesList: Locator;
  readonly detailsDependenciesEmpty: Locator;
  readonly detailsReferences: Locator;
  readonly detailsReferencesList: Locator;
  readonly detailsReferencesEmpty: Locator;
  readonly detailsCrossLink: Locator;

  // Uninstall dialog
  readonly uninstallDialog: Locator;
  readonly uninstallConfirm: Locator;
  readonly uninstallCancel: Locator;

  constructor(page: Page) {
    this.page = page;
    this.root = page.getByTestId('marketplace-page');
    this.tabs = page.getByTestId('marketplace-tabs');
    this.tabInstalled = page.getByTestId('marketplace-tab-installed');
    this.tabBuiltin = page.getByTestId('marketplace-tab-builtin');
    this.tabBrowse = page.getByTestId('marketplace-tab-browse');

    this.installedList = page.getByTestId('marketplace-list');
    this.installedLoading = page.getByTestId('marketplace-loading');
    this.installedError = page.getByTestId('marketplace-error');
    this.updateProgress = page.getByTestId('marketplace-update-progress');

    this.builtinList = page.getByTestId('marketplace-builtin-list');
    this.builtinLoading = page.getByTestId('marketplace-builtin-loading');
    this.builtinError = page.getByTestId('marketplace-builtin-error');
    this.builtinProgress = page.getByTestId('marketplace-builtin-progress');

    this.browseList = page.getByTestId('marketplace-browse-list');
    this.browseSearch = page.getByTestId('marketplace-browse-search');
    this.browseCount = page.getByTestId('marketplace-browse-count');
    this.browseProgress = page.getByTestId('marketplace-browse-progress');

    this.detailsDrawer = page.getByTestId('marketplace-details-drawer');
    this.detailsPanel = page.getByTestId('marketplace-details-panel');
    this.detailsClose = page.getByTestId('marketplace-details-close');
    this.detailsChangelog = page.getByTestId('marketplace-details-changelog');
    this.detailsChangelogBody = page.getByTestId(
      'marketplace-details-changelog-body',
    );
    this.detailsChangelogEmpty = page.getByTestId(
      'marketplace-details-changelog-empty',
    );
    this.detailsDependencies = page.getByTestId(
      'marketplace-details-dependencies',
    );
    this.detailsDependenciesList = page.getByTestId(
      'marketplace-details-dependencies-list',
    );
    this.detailsDependenciesEmpty = page.getByTestId(
      'marketplace-details-dependencies-empty',
    );
    this.detailsReferences = page.getByTestId('marketplace-details-references');
    this.detailsReferencesList = page.getByTestId(
      'marketplace-details-references-list',
    );
    this.detailsReferencesEmpty = page.getByTestId(
      'marketplace-details-references-empty',
    );
    this.detailsCrossLink = page.getByTestId('marketplace-details-cross-link');

    this.uninstallDialog = page.getByTestId('marketplace-uninstall-dialog');
    this.uninstallConfirm = page.getByTestId('marketplace-uninstall-confirm');
    this.uninstallCancel = page.getByTestId('marketplace-uninstall-cancel');
  }

  async goto(): Promise<void> {
    await this.page.goto('/marketplace');
    await this.page.waitForLoadState('domcontentloaded');
  }

  installedCard(name: string): Locator {
    return this.page.locator(
      `[data-testid="marketplace-card-${name}"][data-package-name="${name}"]`,
    );
  }

  builtinCard(slug: string): Locator {
    return this.page.getByTestId(`marketplace-builtin-card-${slug}`);
  }

  browseCard(name: string): Locator {
    return this.page.locator(
      `[data-testid="marketplace-browse-card-${name}"][data-package-name="${name}"]`,
    );
  }

  detailsButtonForInstalled(name: string): Locator {
    return this.page.getByTestId(`marketplace-details-${name}`);
  }

  detailsButtonForBuiltin(slug: string): Locator {
    return this.page.getByTestId(`marketplace-builtin-details-${slug}`);
  }

  detailsButtonForBrowse(name: string): Locator {
    return this.page.getByTestId(`marketplace-browse-details-${name}`);
  }

  installButtonForBuiltin(slug: string): Locator {
    return this.page.getByTestId(`marketplace-builtin-install-${slug}`);
  }

  installButtonForBrowse(name: string): Locator {
    return this.page.getByTestId(`marketplace-browse-install-${name}`);
  }

  updateButtonFor(name: string): Locator {
    return this.page.getByTestId(`marketplace-update-${name}`);
  }

  updateBadgeFor(name: string): Locator {
    return this.page.getByTestId(`marketplace-update-badge-${name}`);
  }

  uninstallButtonFor(name: string): Locator {
    return this.page.getByTestId(`marketplace-uninstall-${name}`);
  }

  dependencyRow(name: string): Locator {
    return this.page.getByTestId(`marketplace-details-dependency-${name}`);
  }

  referenceRow(key: string): Locator {
    return this.page.getByTestId(
      `marketplace-details-reference-${key.toLowerCase().replace(/\s+/g, '-')}`,
    );
  }
}
