import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for `/objectsets/:ontology/diff` — the ObjectSet Diff Viewer
 * rendered by `src/components/objectsets/ObjectSetDiffPage.tsx`.
 *
 * Mirrors the state-branch testid template established by US-021/022/026
 * (root / no-ontology / no-saved-sets / pending / loading / error /
 * results). Scenarios drive the page through the existing ARIA-labeled
 * `<select aria-label="Object Set A|B">` controls (the page already
 * exposes them for screen readers — we reuse them rather than invent new
 * selectors, mirroring the US-027 rule "prefer aria-label, add testids
 * only on wrappers/buttons/rows").
 *
 * Diff sections (`diff-only-in-a` / `diff-only-in-b` / `diff-changed`)
 * keep their pre-existing testids; the four state-branch wrappers and
 * the compute button are new.
 */
export class ObjectSetDiffPage {
  readonly page: Page;
  readonly root: Locator;
  readonly noOntology: Locator;
  readonly noSavedSets: Locator;
  readonly pending: Locator;
  readonly loading: Locator;
  readonly error: Locator;
  readonly results: Locator;
  readonly computeBtn: Locator;
  readonly onlyInA: Locator;
  readonly onlyInB: Locator;
  readonly changed: Locator;

  constructor(page: Page) {
    this.page = page;
    this.root = page.getByTestId('objectset-diff-page');
    this.noOntology = page.getByTestId('objectset-diff-no-ontology');
    this.noSavedSets = page.getByTestId('objectset-diff-no-saved-sets');
    this.pending = page.getByTestId('objectset-diff-pending');
    this.loading = page.getByTestId('objectset-diff-loading');
    this.error = page.getByTestId('objectset-diff-error');
    this.results = page.getByTestId('objectset-diff-results');
    this.computeBtn = page.getByTestId('objectset-diff-compute-btn');
    this.onlyInA = page.getByTestId('diff-only-in-a');
    this.onlyInB = page.getByTestId('diff-only-in-b');
    this.changed = page.getByTestId('diff-changed');
  }

  async goto(ontologyApiName: string): Promise<void> {
    await this.page.goto(
      `/objectsets/${encodeURIComponent(ontologyApiName)}/diff`,
    );
    await this.page.waitForLoadState('domcontentloaded');
  }

  /**
   * Seed two SavedObjectSets into localStorage so the page's pick-lists
   * are populated when the route loads. Returns the seeded ids so
   * scenarios can selectOption(id) on the Object Set A/B dropdowns.
   *
   * Uses the same `weave:objectSets:{ontology}` key the production
   * `useSavedObjectSets` hook reads from (see `lib/objectSetBuilder.ts:
   * localStorageKey`), and the same multi-version schema the hook's
   * `migrateLegacy` step expects.
   */
  async seedSavedObjectSets(
    ontologyApiName: string,
    entries: ReadonlyArray<{
      id: string;
      name: string;
      objectType: string;
    }>,
  ): Promise<void> {
    const key = `weave:objectSets:${ontologyApiName}`;
    const now = new Date().toISOString();
    const payload = entries.map((e) => ({
      id: e.id,
      name: e.name,
      def: { type: 'base', objectType: e.objectType },
      createdAt: now,
      versions: [
        {
          versionId: `${e.id}-v1`,
          def: { type: 'base', objectType: e.objectType },
          createdAt: now,
        },
      ],
      activeVersionId: `${e.id}-v1`,
    }));
    await this.page.addInitScript(
      ({ k, v }: { k: string; v: string }) => {
        window.localStorage.setItem(k, v);
      },
      { k: key, v: JSON.stringify(payload) },
    );
  }

  /** The `<select aria-label="Object Set A">` populated from saved sets. */
  savedASelect(): Locator {
    return this.page.getByLabel('Object Set A');
  }

  /** The `<select aria-label="Object Set B">` populated from saved sets. */
  savedBSelect(): Locator {
    return this.page.getByLabel('Object Set B');
  }
}
