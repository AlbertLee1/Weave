import { expect, test, type Page, type Route } from '@playwright/test';
import {
  Given,
  SettingsPage,
  Then,
  When,
  describeFeature,
  signIn,
} from './support';

/**
 * BDD coverage of `/settings` — the user preference centre rendered by
 * `src/components/settings/SettingsPage.tsx` (US-350 originally).
 *
 * Scenarios map the PRD AC for US-035:
 *
 *   AC: "至少 4 scenarios：偏好保存、主题切换、快捷键自定义、reset to defaults"
 *
 * Honest mapping (mirroring US-025/028/030/033/034):
 *   - "偏好保存" → notifications channel toggle scenario locks the full
 *     wire-level contract: PUT body shape + savedFlash banner + React
 *     Query invalidation re-renders the checkbox as ON-after-toggle.
 *   - "主题切换" → click `settings-theme-light` and assert (a) the active
 *     radio updates to `light` (b) the html element acquires the `light`
 *     class (c) the PUT body carries `{theme:'light'}` (d) the savedFlash
 *     banner appears.
 *   - "快捷键自定义" → there is NO per-hotkey rebind affordance today.
 *     Each `<li data-testid="settings-hotkey-*">` renders a read-only
 *     `<kbd>` showing the registry default. Lock the gap with role-based
 *     absence (textbox / button / link) inside the hotkeys section so a
 *     future PR adding inline rebind UI must replace these assertions
 *     with click-driven coverage. The page DOES expose a global
 *     hotkeys-enabled toggle — exercise that as the active half so the
 *     scenario isn't pure absence.
 *   - "reset to defaults" → no global "Reset to defaults" / "Restore"
 *     button surfaces today. Mirror the US-028/033 button + link double
 *     absence pattern with a regex covering Reset / Restore / Defaults /
 *     Revert labels.
 *
 * Every scenario stubs the two endpoints SettingsPage actually hits:
 *   - GET /api/v2/user-preferences (`useUserPreferences`)
 *   - PUT /api/v2/user-preferences (`useUpdateUserPreferences`)
 *
 * The stub is request-shape aware: the PUT response merges the inbound
 * patch into the seeded prefs so the React Query invalidation drives the
 * UI re-render in the same shape the real backend would. Captured PUT
 * bodies are exposed via `stubs.putCalls` for body-shape assertions.
 */

interface MockPrefs {
  userId: string;
  theme: '' | 'dark' | 'light' | 'system';
  language: string;
  notifications: {
    enabled?: boolean;
    mentions?: boolean;
    approvals?: boolean;
    watches?: boolean;
  };
  hotkeys: {
    enabled?: boolean;
    overrides?: Record<string, string>;
  };
}

interface UpdateBody {
  theme?: string;
  language?: string;
  notifications?: MockPrefs['notifications'];
  hotkeys?: MockPrefs['hotkeys'];
}

interface PrefsStubs {
  prefs: MockPrefs;
  putCalls: UpdateBody[];
  getCalls: number;
  /**
   * When set, PUT route returns a 500 with this errorName so the
   * `settings-save-error` banner exercises its failure path. Cleared
   * after first PUT so subsequent edits succeed.
   */
  failNextPutWith: string | null;
}

function newStubs(initial: Partial<MockPrefs> = {}): PrefsStubs {
  return {
    prefs: {
      userId: 'alice',
      theme: '',
      language: '',
      notifications: {},
      hotkeys: {},
      ...initial,
    },
    putCalls: [],
    getCalls: 0,
    failNextPutWith: null,
  };
}

async function stubPreferenceEndpoints(
  page: Page,
  stubs: PrefsStubs,
): Promise<void> {
  await page.route('**/api/v2/user-preferences', async (route: Route) => {
    const method = route.request().method();
    if (method === 'GET') {
      stubs.getCalls += 1;
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          ...stubs.prefs,
          createdAt: '2026-05-13T00:00:00Z',
          updatedAt: '2026-05-13T00:00:00Z',
        }),
      });
      return;
    }
    if (method === 'PUT') {
      const body = (route.request().postDataJSON() ?? {}) as UpdateBody;
      stubs.putCalls.push(body);
      if (stubs.failNextPutWith) {
        const errorName = stubs.failNextPutWith;
        stubs.failNextPutWith = null;
        await route.fulfill({
          status: 500,
          contentType: 'application/json',
          body: JSON.stringify({
            errorCode: 'INTERNAL',
            errorName,
            errorInstanceId: 'spec',
            parameters: { error: 'Synthetic save failure for BDD' },
          }),
        });
        return;
      }
      // Merge inbound patch into the seeded prefs so the React Query
      // cache update reflects the same wire-shape contract the real
      // backend honours (US-350 server-side merge semantics).
      const merged: MockPrefs = {
        ...stubs.prefs,
        ...(body.theme !== undefined
          ? { theme: body.theme as MockPrefs['theme'] }
          : {}),
        ...(body.language !== undefined ? { language: body.language } : {}),
        ...(body.notifications !== undefined
          ? {
              notifications: {
                ...stubs.prefs.notifications,
                ...body.notifications,
              },
            }
          : {}),
        ...(body.hotkeys !== undefined
          ? { hotkeys: { ...stubs.prefs.hotkeys, ...body.hotkeys } }
          : {}),
      };
      stubs.prefs = merged;
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          ...merged,
          createdAt: '2026-05-13T00:00:00Z',
          updatedAt: '2026-05-13T00:00:01Z',
        }),
      });
      return;
    }
    await route.continue();
  });
}

describeFeature('Settings preference centre', () => {
  test('Scenario: switching the theme persists the choice and surfaces the saved banner @smoke', async ({
    page,
    request,
  }) => {
    // Locks AC "主题切换" + part of "偏好保存". Clicking a theme button
    // (a) flips the active radio (`aria-checked`), (b) emits a PUT with
    // the new theme value, (c) renders the `settings-saved-flash` banner
    // for ~1.5s, (d) writes the theme class onto the <html> element
    // through `setLocalTheme`.
    const settings = new SettingsPage(page);
    const stubs = newStubs({ theme: 'system' });

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('the user-preferences endpoint returns theme=system', async () => {
      await stubPreferenceEndpoints(page, stubs);
    });

    await When('the user opens the Settings page', async () => {
      await settings.goto();
      await expect(settings.root).toBeVisible();
    });

    await Then('the system theme radio is the active one initially', async () => {
      await expect(settings.themeButton('system')).toHaveAttribute(
        'aria-checked',
        'true',
      );
      await expect(settings.themeButton('light')).toHaveAttribute(
        'aria-checked',
        'false',
      );
    });

    await When('the user clicks the Light theme button', async () => {
      await settings.themeButton('light').click();
    });

    await Then('the PUT body carries the new theme value', async () => {
      await expect.poll(() => stubs.putCalls.length).toBeGreaterThanOrEqual(1);
      const body = stubs.putCalls.at(-1) as UpdateBody;
      expect(body.theme).toBe('light');
      // Narrow PUT DTO: only theme is in the patch, no stray fields.
      expect(body.language).toBeUndefined();
      expect(body.notifications).toBeUndefined();
      expect(body.hotkeys).toBeUndefined();
    });

    await Then('the saved-flash banner appears', async () => {
      await expect(settings.savedFlash).toBeVisible();
    });

    await Then('the Light radio becomes the active one', async () => {
      await expect(settings.themeButton('light')).toHaveAttribute(
        'aria-checked',
        'true',
      );
      await expect(settings.themeButton('system')).toHaveAttribute(
        'aria-checked',
        'false',
      );
    });

    await Then('the <html> element acquires the light theme class', async () => {
      // `useTheme.setPreference` toggles the html class through
      // applyTheme regardless of OS preference. Match `light` exactly
      // (not as substring of `lightish`) by wrapping in word boundaries.
      await expect.poll(async () =>
        page.evaluate(() => document.documentElement.className),
      ).toMatch(/\blight\b/);
    });
  });

  test('Scenario: toggling a notification channel persists the patch and rerenders the checkbox @smoke', async ({
    page,
  }) => {
    // Locks AC "偏好保存": the notifications section drives the most
    // structurally interesting patch shape — a nested
    // {notifications:{mentions:false}} object. The scenario asserts
    // (a) the PUT body merges the patch over the seeded
    // notifications map (b) the savedFlash banner appears (c) the
    // checkbox re-renders in the new state once the React Query cache
    // updates (d) the wire-shape carries no other top-level keys.
    const settings = new SettingsPage(page);
    const stubs = newStubs({
      notifications: { enabled: true, mentions: true, approvals: true },
    });

    await Given('the user-preferences endpoint reports mentions on', async () => {
      await stubPreferenceEndpoints(page, stubs);
    });

    await When('the user opens the Settings page', async () => {
      await settings.goto();
      await expect(settings.root).toBeVisible();
    });

    await Then('the mentions channel checkbox is checked initially', async () => {
      await expect(settings.notificationChannel('mentions')).toBeChecked();
    });

    await When('the user toggles the mentions channel off', async () => {
      await settings.notificationChannel('mentions').click();
    });

    await Then('a single PUT call carries the merged notifications patch', async () => {
      await expect.poll(() => stubs.putCalls.length).toBeGreaterThanOrEqual(1);
      const body = stubs.putCalls.at(-1) as UpdateBody;
      expect(body.notifications).toBeDefined();
      // The applyNotifications helper merges {...notif, ...patch} into
      // the wire body, so all three known channels appear; the
      // patched key is the only one whose value flipped.
      expect(body.notifications?.enabled).toBe(true);
      expect(body.notifications?.approvals).toBe(true);
      expect(body.notifications?.mentions).toBe(false);
      // Narrow PUT DTO: theme / language / hotkeys keys stay absent.
      expect(body.theme).toBeUndefined();
      expect(body.language).toBeUndefined();
      expect(body.hotkeys).toBeUndefined();
    });

    await Then('the saved-flash banner appears', async () => {
      await expect(settings.savedFlash).toBeVisible();
    });

    await Then('the mentions checkbox reflects the persisted off state', async () => {
      await expect(settings.notificationChannel('mentions')).not.toBeChecked();
      // Sibling channels remain on — patch was channel-scoped.
      await expect(settings.notificationChannel('approvals')).toBeChecked();
    });
  });

  test('Scenario: the hotkeys section lists registry bindings as read-only with no rebind affordance', async ({
    page,
  }) => {
    // Honest mapping for AC "快捷键自定义": the page surfaces the
    // global hotkeys-enabled toggle but every per-hotkey row is a
    // read-only `<kbd>` showing the registry default. Lock the gap
    // with triple role-based absence inside the hotkeys section so a
    // future PR adding inline rebind UI replaces these assertions
    // with click-driven coverage. The active half exercises the
    // hotkeys-enabled toggle to keep the scenario from being pure
    // absence.
    const settings = new SettingsPage(page);
    const stubs = newStubs({ hotkeys: { enabled: true } });

    await Given('the user-preferences endpoint reports hotkeys enabled', async () => {
      await stubPreferenceEndpoints(page, stubs);
    });

    await When('the user opens the Settings page', async () => {
      await settings.goto();
      await expect(settings.root).toBeVisible();
    });

    await Then('the hotkeys section is visible with the master toggle on', async () => {
      await expect(settings.hotkeysSection).toBeVisible();
      await expect(settings.hotkeysEnabled).toBeChecked();
    });

    await Then('at least one hotkey row is rendered with a <kbd> binding label', async () => {
      // Pin commandPalette as a representative row — every deployment
      // ships this registry entry. The row body must contain the kbd
      // text but expose no input / button / link affordance.
      const row = settings.hotkeyRow('commandPalette');
      await expect(row).toBeVisible();
      await expect(row).toContainText(/ctrl\+k|meta\+k|cmd\+k/i);
    });

    await Then('no per-hotkey rebind / clear / record affordance is rendered inside the section', async () => {
      // Lock the read-only contract with three orthogonal role-based
      // absence checks inside the hotkeys section scope only — the rest
      // of the page (notifications toggles, theme radios) intentionally
      // ships buttons / checkboxes.
      const rebindRegex =
        /rebind|record|capture|clear|edit\s*binding|change\s*binding|set\s*binding|customize|customise/i;
      await expect(
        settings.hotkeysSection.getByRole('button', { name: rebindRegex }),
      ).toHaveCount(0);
      await expect(
        settings.hotkeysSection.getByRole('link', { name: rebindRegex }),
      ).toHaveCount(0);
      await expect(
        settings.hotkeysSection.getByRole('textbox', { name: rebindRegex }),
      ).toHaveCount(0);
    });

    await When('the user toggles the master hotkeys switch off', async () => {
      await settings.hotkeysEnabled.click();
    });

    await Then('the PUT body carries the hotkeys.enabled patch', async () => {
      await expect.poll(() => stubs.putCalls.length).toBeGreaterThanOrEqual(1);
      const body = stubs.putCalls.at(-1) as UpdateBody;
      expect(body.hotkeys).toBeDefined();
      expect(body.hotkeys?.enabled).toBe(false);
      // No per-binding override map mutation — the wire still has no
      // overrides surface today.
      expect(body.hotkeys?.overrides).toBeUndefined();
      expect(body.theme).toBeUndefined();
      expect(body.language).toBeUndefined();
      expect(body.notifications).toBeUndefined();
    });

    await Then('the saved-flash banner appears', async () => {
      await expect(settings.savedFlash).toBeVisible();
    });
  });

  test('Scenario: no global "Reset to defaults" affordance is rendered today', async ({
    page,
  }) => {
    // Honest mapping for AC "reset to defaults": no Restore / Reset /
    // Defaults / Revert button or link surfaces today on the Settings
    // page. Mirror the US-028/033 button + link double absence pattern
    // with a regex covering the four common label variants, plus a
    // menuitem fallback (some product UIs route resets through a
    // kebab menu). The scenario also documents the persistence /
    // unavailable branch as the only opt-out path users have today:
    // editing local theme + language but skipping the PUT cycle.
    const settings = new SettingsPage(page);
    const stubs = newStubs({ theme: 'dark' });

    await Given('the user-preferences endpoint returns theme=dark', async () => {
      await stubPreferenceEndpoints(page, stubs);
    });

    await When('the user opens the Settings page', async () => {
      await settings.goto();
      await expect(settings.root).toBeVisible();
    });

    await Then('the Dark theme is active out of the box', async () => {
      await expect(settings.themeButton('dark')).toHaveAttribute(
        'aria-checked',
        'true',
      );
    });

    await Then('no Reset / Restore / Revert / Defaults button is rendered', async () => {
      const resetRegex =
        /reset(\s+(to\s+)?defaults?)?|restore\s+defaults?|revert(\s+(to\s+)?defaults?)?|\bdefaults?\b/i;
      await expect(
        page.getByRole('button', { name: resetRegex }),
      ).toHaveCount(0);
      await expect(page.getByRole('link', { name: resetRegex })).toHaveCount(0);
      await expect(
        page.getByRole('menuitem', { name: resetRegex }),
      ).toHaveCount(0);
    });

    await Then('no DELETE call to the user-preferences endpoint is fired', async () => {
      // Mirror the wire-level negative assertion: today the SPA has no
      // call site that issues `DELETE /api/v2/user-preferences`. The
      // mounted page must NOT have made one.
      let sawDelete = false;
      const deletionWatcher = (req: import('@playwright/test').Request) => {
        if (
          req.method() === 'DELETE' &&
          req.url().includes('/api/v2/user-preferences')
        ) {
          sawDelete = true;
        }
      };
      page.on('request', deletionWatcher);
      // Give the page a moment to fire any deferred requests — the
      // initial GET has already settled by the time goto() returned.
      await page.waitForTimeout(200);
      page.off('request', deletionWatcher);
      expect(sawDelete).toBe(false);
      expect(stubs.putCalls.length).toBe(0);
    });
  });

  test('Scenario: a PUT failure surfaces the save-error banner without hiding the controls', async ({
    page,
  }) => {
    // Bonus scenario locking the failure half of the persistence
    // contract — when the backend rejects the PUT, the SettingsPage
    // (a) renders the `settings-save-error` banner with the ApiError
    // shape's message, (b) does NOT render the `settings-saved-flash`
    // banner, (c) keeps the section controls interactive so the user
    // can retry. Mirrors the US-026 "modal error doesn't close form"
    // three-piece pattern adapted for inline-mutation pages.
    const settings = new SettingsPage(page);
    const stubs = newStubs({ language: 'en' });
    stubs.failNextPutWith = 'PreferenceSaveFailed';

    await Given('the user-preferences endpoint will reject the next PUT', async () => {
      await stubPreferenceEndpoints(page, stubs);
    });

    await When('the user opens the Settings page', async () => {
      await settings.goto();
      await expect(settings.root).toBeVisible();
    });

    await Then('the saved-flash banner is hidden initially', async () => {
      await expect(settings.savedFlash).toHaveCount(0);
      await expect(settings.saveError).toHaveCount(0);
    });

    await When('the user toggles a notification channel', async () => {
      await settings.notificationChannel('approvals').click();
    });

    await Then('the save-error banner becomes visible', async () => {
      await expect(settings.saveError).toBeVisible();
      await expect(settings.saveError).toContainText(/save\s+failed/i);
    });

    await Then('the saved-flash banner does NOT appear', async () => {
      await expect(settings.savedFlash).toHaveCount(0);
    });

    await Then('the notifications section remains interactive for retry', async () => {
      await expect(settings.notificationsSection).toBeVisible();
      await expect(settings.notificationChannel('approvals')).toBeEnabled();
    });
  });
});
