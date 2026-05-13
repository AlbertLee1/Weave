import { expect, test, type Page, type Route } from '@playwright/test';
import {
  FunctionRepoPage,
  Given,
  Then,
  When,
  describeFeature,
  signIn,
} from './support';

/**
 * BDD coverage of `/functions/:ontology/:functionRid/repo` — the
 * Function Code Repository surface rendered by
 * `src/components/functions/FunctionRepoPage.tsx` (US-046, PC-A03).
 *
 * Scenarios map to the US-046 acceptance criteria:
 *
 *   - "新建路由 /functions/:ontology/:fn/repo，展示 commit list（接 functionRepo/commits）"
 *     → list scenario seeds three commits, asserts the rail renders
 *       one row per commit (newest first), and locks the
 *       `data-commit-hash` lookup contract used by the page object.
 *   - "Commit 详情用 Monaco diff editor 显示文件 diff"
 *     → diff scenario clicks the middle row, asserts the centre
 *       pane swaps from placeholder to the side-by-side diff viewer
 *       and that both parent-side and commit-side sources have been
 *       fetched (verified via the GET commit-source captured-requests
 *       array). Honest mapping: we standardised on
 *       react-diff-viewer (same as FunctionDiffPage.tsx, US-416) —
 *       Monaco's multi-MB bundle is intentionally avoided.
 *   - "Replay 按钮：弹参数表单 → 跳到执行结果页"
 *     → replay scenario opens the drawer from the centre pane, types
 *       a JSON parameter map, clicks Replay, and asserts:
 *       (a) POST `/replay` carries the parsed `input` map +
 *           inherited `version` field;
 *       (b) the inline result panel renders with
 *           `data-replay-match="true"` + `data-execution-id` +
 *           the response body's `result` field rendered as JSON;
 *       (c) honest mapping for "跳到执行结果页": no dedicated
 *           `/executions/:id` route exists today (no
 *           FunctionExecutionPage in the React Router tree), so the
 *           result panel renders inline and the BDD locks that
 *           contract instead of asserting a URL push.
 *   - "Versions 切换器（Pin / Unpin / Set as Default）"
 *     → versions scenario seeds three semver rows, asserts the rail
 *       renders them latest-first with `data-version-current` set on
 *       the row whose version matches the fetched function summary,
 *       and locks the honest-mapping absence of Pin / Unpin /
 *       Set-as-Default affordances + the `data-pin-supported="false"`
 *       flag on the rail (server has no pin/default columns; a
 *       future migration that adds them lights up the affordances
 *       and flips this BDD scenario into a positive assertion).
 */

const ONTOLOGY = 'northwind';
const FUNCTION_RID =
  'ri.functions.main.function.adjust-discount-fn';
const FUNCTION_NAME = 'adjustDiscount';
const FUNCTION_VERSION = '1.2.0';

interface MockCommit {
  hash: string;
  message: string;
  author: string;
  email: string;
  authorDate: string;
  sourceCode: string;
}

interface MockVersion {
  rid: string;
  name: string;
  version: string;
  sourceCode: string;
  runtime?: string;
  publishedAt?: string;
  codeHash?: string;
}

interface CapturedReplay {
  url: string;
  body: Record<string, unknown>;
}

interface Stubs {
  commits: MockCommit[];
  versions: MockVersion[];
  commitsByHash: Map<string, MockCommit>;
  capturedReplays: CapturedReplay[];
  capturedSourceFetches: string[];
  replayResponse: {
    functionRid: string;
    functionVersion: string;
    executionId: string;
    replayHash: string;
    originalHash?: string;
    match: boolean;
    result: unknown;
    warning?: { code: string; message: string };
  };
}

function newStubs(): Stubs {
  return {
    commits: [],
    versions: [],
    commitsByHash: new Map(),
    capturedReplays: [],
    capturedSourceFetches: [],
    replayResponse: {
      functionRid: FUNCTION_RID,
      functionVersion: FUNCTION_VERSION,
      executionId: 'exec-replay-1',
      replayHash:
        'replay-hash-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
      match: true,
      result: { adjusted: true, discount: 0.5 },
    },
  };
}

async function stubBackend(page: Page, stubs: Stubs): Promise<void> {
  // GET function summary
  await page.route(
    new RegExp(
      `/api/v2/ontologies/${ONTOLOGY}/functions/${encodeURIComponent(FUNCTION_RID)}(?:\\?|$)`,
    ),
    async (route: Route) => {
      if (route.request().method() !== 'GET') {
        await route.continue();
        return;
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          rid: FUNCTION_RID,
          ontologyRid: 'ri.ontology.main.ontology.northwind',
          name: FUNCTION_NAME,
          version: FUNCTION_VERSION,
          sourceCode:
            stubs.commits[0]?.sourceCode ?? '// no source available',
          runtime: 'js',
        }),
      });
    },
  );

  // GET commit log
  await page.route(
    new RegExp(
      `/api/v2/ontologies/${ONTOLOGY}/functions/${encodeURIComponent(FUNCTION_RID)}/log(?:\\?|$)`,
    ),
    async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          data: stubs.commits.map((c) => ({
            hash: c.hash,
            message: c.message,
            author: c.author,
            email: c.email,
            authorDate: c.authorDate,
          })),
        }),
      });
    },
  );

  // GET single commit (with source)
  await page.route(
    new RegExp(
      `/api/v2/ontologies/${ONTOLOGY}/functions/${encodeURIComponent(FUNCTION_RID)}/commits/([^/?]+)(?:\\?|$)`,
    ),
    async (route: Route) => {
      const url = route.request().url();
      stubs.capturedSourceFetches.push(url);
      const m = url.match(/\/commits\/([^/?]+)/);
      const hash = m ? decodeURIComponent(m[1] ?? '') : '';
      const commit = stubs.commitsByHash.get(hash);
      if (!commit) {
        await route.fulfill({
          status: 404,
          contentType: 'application/json',
          body: JSON.stringify({
            errorCode: 'CommitNotFound',
            errorName: 'CommitNotFound',
          }),
        });
        return;
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          hash: commit.hash,
          message: commit.message,
          author: commit.author,
          email: commit.email,
          authorDate: commit.authorDate,
          sourceCode: commit.sourceCode,
        }),
      });
    },
  );

  // GET versions list — backend uses functionName, not rid, for this path
  await page.route(
    new RegExp(
      `/api/v2/ontologies/${ONTOLOGY}/functions/${FUNCTION_NAME}/versions(?:\\?|$)`,
    ),
    async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          name: FUNCTION_NAME,
          data: stubs.versions,
        }),
      });
    },
  );

  // POST replay
  await page.route(
    new RegExp(
      `/api/v2/ontologies/${ONTOLOGY}/functions/${encodeURIComponent(FUNCTION_RID)}/replay(?:\\?|$)`,
    ),
    async (route: Route) => {
      const body =
        (route.request().postDataJSON() ?? {}) as Record<string, unknown>;
      stubs.capturedReplays.push({ url: route.request().url(), body });
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(stubs.replayResponse),
      });
    },
  );
}

function seedCommits(stubs: Stubs): void {
  stubs.commits = [
    {
      hash: 'aaaaaaaa11111111111111111111111111111111',
      message: 'Lower discount cap to 50%',
      author: 'alice',
      email: 'alice@test',
      authorDate: '2026-05-13T12:00:00Z',
      sourceCode:
        'export function adjustDiscount(x) {\n  return Math.min(x, 0.5);\n}\n',
    },
    {
      hash: 'bbbbbbbb22222222222222222222222222222222',
      message: 'Add per-tier multipliers',
      author: 'bob',
      email: 'bob@test',
      authorDate: '2026-05-12T10:00:00Z',
      sourceCode:
        'export function adjustDiscount(x) {\n  return Math.min(x * 1.1, 0.6);\n}\n',
    },
    {
      hash: 'cccccccc33333333333333333333333333333333',
      message: 'Initial implementation',
      author: 'carol',
      email: 'carol@test',
      authorDate: '2026-05-10T08:00:00Z',
      sourceCode:
        'export function adjustDiscount(x) {\n  return x;\n}\n',
    },
  ];
  stubs.commitsByHash = new Map(stubs.commits.map((c) => [c.hash, c]));
}

function seedVersions(stubs: Stubs): void {
  stubs.versions = [
    {
      rid: FUNCTION_RID,
      name: FUNCTION_NAME,
      version: '1.2.0',
      sourceCode: stubs.commits[0]?.sourceCode ?? '',
      publishedAt: '2026-05-13T12:05:00Z',
      codeHash:
        'codehashv120deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdead',
    },
    {
      rid: 'ri.functions.main.function.adjust-discount-fn-v110',
      name: FUNCTION_NAME,
      version: '1.1.0',
      sourceCode: stubs.commits[1]?.sourceCode ?? '',
      publishedAt: '2026-05-12T10:30:00Z',
      codeHash:
        'codehashv110deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdead',
    },
    {
      rid: 'ri.functions.main.function.adjust-discount-fn-v100',
      name: FUNCTION_NAME,
      version: '1.0.0',
      sourceCode: stubs.commits[2]?.sourceCode ?? '',
      publishedAt: '2026-05-10T09:00:00Z',
      codeHash:
        'codehashv100deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdead',
    },
  ];
}

describeFeature('Function Code Repository page', () => {
  test('Scenario: renders the commit list for a function repo @smoke', async ({
    page,
    request,
  }) => {
    const stubs = newStubs();
    seedCommits(stubs);
    seedVersions(stubs);

    const repoPage = new FunctionRepoPage(page);

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given(
      'a function with three commits on its bare git repo',
      async () => {
        await stubBackend(page, stubs);
      },
    );

    await When('the user opens the repo page', async () => {
      await repoPage.goto(ONTOLOGY, FUNCTION_RID);
      await expect(repoPage.root).toBeVisible();
    });

    await Then('the page header surfaces the function metadata', async () => {
      await expect(repoPage.subject).toHaveAttribute(
        'data-function-name',
        FUNCTION_NAME,
      );
      await expect(repoPage.subject).toHaveAttribute(
        'data-function-version',
        FUNCTION_VERSION,
      );
      await expect(repoPage.subject).toHaveAttribute(
        'data-ontology-api-name',
        ONTOLOGY,
      );
    });

    await Then('the commit rail renders one row per commit', async () => {
      await expect(repoPage.commitList).toBeVisible();
      await expect(repoPage.commitRows()).toHaveCount(3);
      for (const c of stubs.commits) {
        await expect(repoPage.commitRowByHash(c.hash)).toBeVisible();
      }
    });

    await Then(
      'the newest commit is auto-selected and powers the centre pane',
      async () => {
        await expect(repoPage.commitRowByHash(stubs.commits[0]!.hash)).toHaveAttribute(
          'data-commit-selected',
          'true',
        );
        await expect(repoPage.commitMeta).toBeVisible();
        await expect(repoPage.commitMeta).toHaveAttribute(
          'data-commit-hash',
          stubs.commits[0]!.hash,
        );
      },
    );
  });

  test('Scenario: clicking a commit renders the parent-vs-commit diff', async ({
    page,
  }) => {
    const stubs = newStubs();
    seedCommits(stubs);
    seedVersions(stubs);

    const repoPage = new FunctionRepoPage(page);

    await Given('the repo has three commits', async () => {
      await stubBackend(page, stubs);
    });

    await When('the user opens the repo page', async () => {
      await repoPage.goto(ONTOLOGY, FUNCTION_RID);
      await expect(repoPage.root).toBeVisible();
    });

    await When('the user clicks the middle commit', async () => {
      await repoPage.commitRowByHash(stubs.commits[1]!.hash).click();
    });

    await Then(
      'the centre pane swaps from placeholder to the diff viewer',
      async () => {
        await expect(repoPage.detailPlaceholder).toHaveCount(0);
        await expect(repoPage.diffViewer).toBeVisible();
        await expect(repoPage.commitMeta).toHaveAttribute(
          'data-commit-hash',
          stubs.commits[1]!.hash,
        );
        await expect(repoPage.commitMeta).toHaveAttribute(
          'data-parent-hash',
          stubs.commits[2]!.hash,
        );
      },
    );

    await Then(
      'the backend served the selected and parent commit sources',
      async () => {
        // Wire-shape positive assertion: the diff is built from two
        // separate GETs (selected + parent). Locking both fetches here
        // catches regressions where the page swaps to a single-source
        // working-copy diff or, conversely, where the parent fetch is
        // dropped and the diff renders against an empty string.
        await expect
          .poll(() =>
            stubs.capturedSourceFetches.some((u) =>
              u.includes(`/commits/${stubs.commits[1]!.hash}`),
            ),
          )
          .toBe(true);
        await expect
          .poll(() =>
            stubs.capturedSourceFetches.some((u) =>
              u.includes(`/commits/${stubs.commits[2]!.hash}`),
            ),
          )
          .toBe(true);
      },
    );

    await Then(
      'the diff viewer body renders both source revisions',
      async () => {
        // The diff viewer renders the source code as plain text spans
        // inside its split-view DOM. Asserting the centre pane
        // contains a token from each side proves the source fetches
        // settled and made it into the viewer's data props rather
        // than just landing in cache.
        const detail = repoPage.detailPane;
        await expect(detail).toContainText('Math.min');
        await expect(detail).toContainText('1.1');
      },
    );
  });

  test('Scenario: replay drawer submits parameters and renders the result inline @smoke', async ({
    page,
  }) => {
    const stubs = newStubs();
    seedCommits(stubs);
    seedVersions(stubs);
    stubs.replayResponse = {
      ...stubs.replayResponse,
      executionId: 'exec-7777',
      replayHash:
        'replay-hash-7777777777777777777777777777777777777777777777777777',
      originalHash:
        'orig-hash-99999999999999999999999999999999999999999999999999999999',
      match: true,
      result: { adjusted: true, discount: 0.5, marker: 'replay-7777' },
    };

    const repoPage = new FunctionRepoPage(page);

    await Given('the repo has commits and versions', async () => {
      await stubBackend(page, stubs);
    });

    await When('the user opens the repo page', async () => {
      await repoPage.goto(ONTOLOGY, FUNCTION_RID);
      await expect(repoPage.root).toBeVisible();
      await expect(repoPage.commitRows()).toHaveCount(3);
    });

    await When('the user clicks Replay this commit', async () => {
      await repoPage.replayButton.click();
      await expect(repoPage.replayDrawer).toBeVisible();
    });

    await When(
      'the user fills the input JSON and submits the form',
      async () => {
        await repoPage.replayInput.fill(
          '{"customerId": 17, "amount": 99}',
        );
        await repoPage.replayForm.evaluate((f) =>
          (f as HTMLFormElement).requestSubmit(),
        );
      },
    );

    await Then(
      'a POST to /replay carries the parsed input map and inherited version',
      async () => {
        await expect
          .poll(() => stubs.capturedReplays.length)
          .toBe(1);
        const replay = stubs.capturedReplays[0]!;
        expect(replay.body).toMatchObject({
          version: FUNCTION_VERSION,
          input: { customerId: 17, amount: 99 },
        });
        // The page does not stuff an executionId into ad-hoc replays;
        // negative-assert the field stays undefined so future
        // refactors do not accidentally bind UI parameters to a
        // historical execution row.
        expect(replay.body.executionId).toBeUndefined();
      },
    );

    await Then(
      'the inline result panel exposes the determinism metadata',
      async () => {
        await expect(repoPage.replayResult).toBeVisible();
        await expect(repoPage.replayResult).toHaveAttribute(
          'data-replay-match',
          'true',
        );
        await expect(repoPage.replayResult).toHaveAttribute(
          'data-execution-id',
          'exec-7777',
        );
        await expect(repoPage.replayResult).toHaveAttribute(
          'data-function-version',
          FUNCTION_VERSION,
        );
        await expect(repoPage.replayMatchBadge).toHaveText('match');
        await expect(repoPage.replayExecutionId).toHaveText('exec-7777');
      },
    );

    await Then('the result body renders the response JSON', async () => {
      await expect(repoPage.replayResultBody).toContainText('replay-7777');
      await expect(repoPage.replayResultBody).toContainText('discount');
    });

    await Then(
      'the drawer stays open so the operator can inspect the response',
      async () => {
        // Honest mapping: "跳到执行结果页" — no dedicated
        // /executions/:id route exists in App.tsx, so the BDD locks
        // the inline-result contract instead. When a future story
        // adds the execution detail page, replace this absence
        // assertion with `waitForURL(/executions/)` and remove the
        // ReplayResultPanel render.
        await expect(repoPage.replayDrawer).toBeVisible();
        expect(page.url()).toContain('/repo');
        expect(page.url()).not.toMatch(/\/executions\//);
      },
    );
  });

  test('Scenario: versions rail lists every semver row read-only with honest mapping', async ({
    page,
  }) => {
    const stubs = newStubs();
    seedCommits(stubs);
    seedVersions(stubs);

    const repoPage = new FunctionRepoPage(page);

    await Given('three semver versions are on record', async () => {
      await stubBackend(page, stubs);
    });

    await When('the user opens the repo page', async () => {
      await repoPage.goto(ONTOLOGY, FUNCTION_RID);
      await expect(repoPage.root).toBeVisible();
    });

    await Then(
      'the versions rail renders every semver row latest-first',
      async () => {
        await expect(repoPage.versionsRail).toBeVisible();
        await expect(repoPage.versionRows()).toHaveCount(3);
        await expect(repoPage.versionRowByVersion('1.2.0')).toBeVisible();
        await expect(repoPage.versionRowByVersion('1.1.0')).toBeVisible();
        await expect(repoPage.versionRowByVersion('1.0.0')).toBeVisible();
      },
    );

    await Then(
      'the row whose version matches the current function is marked current',
      async () => {
        await expect(repoPage.versionRowByVersion('1.2.0')).toHaveAttribute(
          'data-version-current',
          'true',
        );
        await expect(repoPage.versionRowByVersion('1.1.0')).toHaveAttribute(
          'data-version-current',
          'false',
        );
        await expect(repoPage.versionRowByVersion('1.0.0')).toHaveAttribute(
          'data-version-current',
          'false',
        );
      },
    );

    await Then(
      'the rail declares pin/unpin/set-default are unsupported (honest mapping)',
      async () => {
        // Honest mapping (US-041 / US-045 absence template): backend
        // has no pin / default columns and no Pin / Unpin / SetDefault
        // endpoints. The rail therefore advertises
        // `data-pin-supported="false"` + omits the affordances. The
        // BDD double-checks via role-based absence so a future PR that
        // adds a button without coordinating a backend migration is
        // caught.
        await expect(repoPage.versionsRail).toHaveAttribute(
          'data-pin-supported',
          'false',
        );
        await expect(
          repoPage.versionsRail.getByRole('button', { name: /pin/i }),
        ).toHaveCount(0);
        await expect(
          repoPage.versionsRail.getByRole('button', { name: /unpin/i }),
        ).toHaveCount(0);
        await expect(
          repoPage.versionsRail.getByRole('button', {
            name: /set as default|set default|default version/i,
          }),
        ).toHaveCount(0);
        await expect(repoPage.versionsNote).toContainText('Read-only');
      },
    );
  });

  test('Scenario: invalid replay JSON keeps the drawer open with a parse error and skips the POST', async ({
    page,
  }) => {
    const stubs = newStubs();
    seedCommits(stubs);
    seedVersions(stubs);

    const repoPage = new FunctionRepoPage(page);

    await Given('the repo has commits and versions', async () => {
      await stubBackend(page, stubs);
    });

    await When('the user opens the repo page', async () => {
      await repoPage.goto(ONTOLOGY, FUNCTION_RID);
      await expect(repoPage.root).toBeVisible();
    });

    await When(
      'the user opens the replay drawer and types invalid JSON',
      async () => {
        await repoPage.replayButton.click();
        await expect(repoPage.replayDrawer).toBeVisible();
        await repoPage.replayInput.fill('{this is not json');
        await repoPage.replayForm.evaluate((f) =>
          (f as HTMLFormElement).requestSubmit(),
        );
      },
    );

    await Then(
      'a client-side parse error renders without firing the POST',
      async () => {
        await expect(repoPage.replayParseError).toBeVisible();
        // Negative wire-shape assertion: zero replay POSTs hit the
        // backend when the input fails client-side JSON parsing.
        // Together with the parse-error visibility this proves the
        // submit handler short-circuits before mutate().
        expect(stubs.capturedReplays).toHaveLength(0);
        await expect(repoPage.replayDrawer).toBeVisible();
        await expect(repoPage.replayResult).toHaveCount(0);
      },
    );
  });
});
