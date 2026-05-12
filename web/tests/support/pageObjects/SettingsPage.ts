import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for `/settings` — the user preference centre rendered by
 * `src/components/settings/SettingsPage.tsx`.
 *
 * The page surfaces four independent sections (theme / language /
 * notifications / hotkeys). Each section persists immediately to the
 * backend `user_preferences` row through `useUpdateUserPreferences`,
 * with a `settings-saved-flash` banner appearing on success and a
 * `settings-save-error` banner on failure. Degraded deployments (no PG)
 * render a `settings-unavailable-banner` and skip persistence.
 *
 * Selector convention mirrors the rest of the BDD suite: prefer the
 * `data-testid` attributes already declared in the production component
 * (`settings-page`, `settings-theme-{system|light|dark}`,
 * `settings-language-{zh-CN|en}`, `settings-notifications-{enabled|
 * mentions|approvals|watches}`, `settings-hotkeys-enabled`,
 * `settings-hotkey-{id}`) over fragile class / role / text selectors.
 */
export class SettingsPage {
  readonly page: Page;
  readonly root: Locator;
  readonly savedFlash: Locator;
  readonly saveError: Locator;
  readonly unavailableBanner: Locator;
  readonly themeSection: Locator;
  readonly languageSection: Locator;
  readonly notificationsSection: Locator;
  readonly hotkeysSection: Locator;
  readonly notificationsEnabled: Locator;
  readonly hotkeysEnabled: Locator;

  constructor(page: Page) {
    this.page = page;
    this.root = page.getByTestId('settings-page');
    this.savedFlash = page.getByTestId('settings-saved-flash');
    this.saveError = page.getByTestId('settings-save-error');
    this.unavailableBanner = page.getByTestId('settings-unavailable-banner');
    this.themeSection = page.getByTestId('settings-section-theme');
    this.languageSection = page.getByTestId('settings-section-language');
    this.notificationsSection = page.getByTestId(
      'settings-section-notifications',
    );
    this.hotkeysSection = page.getByTestId('settings-section-hotkeys');
    this.notificationsEnabled = page.getByTestId(
      'settings-notifications-enabled',
    );
    this.hotkeysEnabled = page.getByTestId('settings-hotkeys-enabled');
  }

  async goto(): Promise<void> {
    await this.page.goto('/settings');
    await this.page.waitForLoadState('domcontentloaded');
  }

  themeButton(opt: 'system' | 'light' | 'dark'): Locator {
    return this.page.getByTestId(`settings-theme-${opt}`);
  }

  languageButton(loc: 'zh-CN' | 'en'): Locator {
    return this.page.getByTestId(`settings-language-${loc}`);
  }

  notificationChannel(
    key: 'mentions' | 'approvals' | 'watches',
  ): Locator {
    return this.page.getByTestId(`settings-notifications-${key}`);
  }

  /**
   * Row locator for a single hotkey definition. The display surface is
   * read-only today: the `<kbd>` element renders the registry's key
   * pattern verbatim, with no input or "Reset binding" affordance.
   */
  hotkeyRow(id: string): Locator {
    return this.page.getByTestId(`settings-hotkey-${id}`);
  }
}
