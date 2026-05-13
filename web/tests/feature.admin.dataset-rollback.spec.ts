import { expect, test, type Page, type Route } from '@playwright/test';
import {
  DatasetRollbackPage,
  Given,
  Then,
  When,
  describeFeature,
  signIn,
} from './support';

/**
 * BDD coverage of `/admin/datasets/:dataset/rollback` — the Dataset
 * Rollback wizard rendered by
 * `src/components/admin/DatasetRollbackPage.tsx` (US-053 / PC-A10).
 *
 * AC mapping → scenario:
 *
 *   "Wizard：选 transaction → 预览影响 → 二次确认（输入 dataset 名）→
 *    启动 rollback job" → wizard-walks-pick-preview-confirm-success scenario
 *
 *   "Job 进度条 + 完成后 toast" → progress-modal-shown-during-mutation +
 *    success-step-renders-summary scenarios
 *
 *   "新增 spec 至少 3 scenarios" → four scenarios below (one smoke each for
 *    pick / preview / confirm-success + one for the confirmation guard).
 *
 * Every scenario stubs the dataset history + rollback endpoints through
 * `page.route` so the wizard renders deterministic fixtures without
 * touching a real backend. Wire shapes match
 * `cmd/server/dataset_rollback_handler.go` byte-for-byte so the spec stays
 * compatible with the production endpoint.
 */

const ONTOLOGY = 'northwind';
const TX_LATEST = 'tx-aaaaaaaa-1111-7222-9333-444444444444';
const TX_MIDDLE = 'tx-cccccccc-3333-7444-9555-666666666666';
const TX_TARGET = 'tx-bbbbbbbb-2222-7333-9444-555555555555';
const TX_NEW_HEAD = 'tx-dddddddd-4444-7555-9666-777777777777';

interface MockTransaction {
  txId: string;
  parentTxId?: string;
  committedAt: string;
  editsCount: number;
  rolledBackAt?: string;
  rolledBackToTxId?: string;
}

const sampleChain: MockTransaction[] = [
  {
    txId: TX_LATEST,
    parentTxId: TX_MIDDLE,
    committedAt: '2026-05-12T18:30:00Z',
    editsCount: 4,
  },
  {
    txId: TX_MIDDLE,
    parentTxId: TX_TARGET,
    committedAt: '2026-05-12T12:15:00Z',
    editsCount: 3,
  },
  {
    txId: TX_TARGET,
    committedAt: '2026-05-11T09:15:00Z',
    editsCount: 2,
  },
];

interface RollbackStubState {
  historyCalls: number;
  rollbackCalls: Array<{ to: string | null }>;
  failNext: boolean;
}

function makeState(): RollbackStubState {
  return { historyCalls: 0, rollbackCalls: [], failNext: false };
}

async function stubDatasetEndpoints(
  page: Page,
  state: RollbackStubState,
  transactions: MockTransaction[],
  options: { rollbackDelayMs?: number } = {},
): Promise<void> {
  await page.route(
    `**/api/v2/datasets/${ONTOLOGY}/history*`,
    async (route: Route) => {
      state.historyCalls += 1;
      const decorated = transactions.map((t) => ({
        ...t,
        ontologyApiName: ONTOLOGY,
        userId: 'user:alice',
      }));
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ transactions: decorated }),
      });
    },
  );

  await page.route(
    `**/api/v2/datasets/${ONTOLOGY}/rollback*`,
    async (route: Route) => {
      const req = route.request();
      if (req.method() !== 'POST') {
        await route.continue();
        return;
      }
      const url = new URL(req.url());
      const to = url.searchParams.get('to');
      state.rollbackCalls.push({ to });
      if (state.failNext) {
        state.failNext = false;
        await route.fulfill({
          status: 400,
          contentType: 'application/json',
          body: JSON.stringify({
            errorCode: 'RollbackTargetNotFound',
            errorName: 'BadRequest',
            errorInstanceId: 'stub',
            statusCode: 400,
            parameters: { to: to ?? '' },
          }),
        });
        return;
      }
      // Optional artificial delay so the progress modal scenario can
      // observe the in-flight state without race-conditions.
      if (options.rollbackDelayMs && options.rollbackDelayMs > 0) {
        await new Promise((r) => setTimeout(r, options.rollbackDelayMs));
      }
      const newer = transactions
        .filter((t) => {
          if (t.txId === to) return false;
          const target = transactions.find((x) => x.txId === to);
          if (!target) return false;
          return (
            new Date(t.committedAt).valueOf() >
            new Date(target.committedAt).valueOf()
          );
        })
        .map((t) => t.txId);
      const target = transactions.find((t) => t.txId === to);
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          rolledBackTxIds: newer,
          restoredObjects: 3,
          deletedObjects: 1,
          targetTx: target ?? null,
          newTransaction: {
            txId: TX_NEW_HEAD,
            parentTxId: to,
            ontologyApiName: ONTOLOGY,
            committedAt: new Date().toISOString(),
            editsCount: 4,
            rolledBackToTxId: to,
          },
        }),
      });
    },
  );
}

describeFeature('Admin: Dataset Rollback wizard', () => {
  test('Scenario: the picker lists every dataset transaction newest-first @smoke', async ({
    page,
    request,
  }) => {
    const wizard = new DatasetRollbackPage(page);
    const state = makeState();

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('the dataset history endpoint advertises three transactions', async () => {
      await stubDatasetEndpoints(page, state, sampleChain);
    });

    await When('the user opens the rollback wizard', async () => {
      await wizard.goto(ONTOLOGY);
    });

    await Then('the wizard root + step ribbon render at the pick step', async () => {
      await expect(wizard.root).toBeVisible();
      await expect(wizard.root).toHaveAttribute('data-step', 'pick');
      await expect(wizard.stepRibbon).toBeVisible();
      await expect(wizard.pickStep).toBeVisible();
    });

    await Then('the transaction table lists every recorded tx as a row', async () => {
      await expect(wizard.txTable).toBeVisible();
      await expect(wizard.txRows).toHaveCount(sampleChain.length);
      await expect(wizard.txRowFor(TX_LATEST)).toBeVisible();
      await expect(wizard.txRowFor(TX_MIDDLE)).toBeVisible();
      await expect(wizard.txRowFor(TX_TARGET)).toBeVisible();
    });

    await Then('the Continue button is disabled until a target is picked', async () => {
      await expect(wizard.pickNext).toBeDisabled();
    });
  });

  test('Scenario: walking the wizard from pick → preview → confirm → success rolls back the dataset @smoke', async ({
    page,
  }) => {
    const wizard = new DatasetRollbackPage(page);
    const state = makeState();

    await Given('the dataset history endpoint is stubbed', async () => {
      await stubDatasetEndpoints(page, state, sampleChain);
    });

    await Given('the user is on the rollback wizard', async () => {
      await wizard.goto(ONTOLOGY);
      await expect(wizard.txTable).toBeVisible();
    });

    await When('the user picks the target transaction', async () => {
      await wizard.txRadioFor(TX_TARGET).check();
      await wizard.pickNext.click();
    });

    await Then(
      'the preview step shows two newer transactions and seven edits to revert',
      async () => {
        await expect(wizard.root).toHaveAttribute('data-step', 'preview');
        await expect(wizard.previewStep).toBeVisible();
        await expect(wizard.previewTarget).toHaveAttribute(
          'data-tx-id',
          TX_TARGET,
        );
        await expect(wizard.impactTxCount).toHaveText('2');
        // TX_LATEST (4) + TX_MIDDLE (3) = 7 edits reverted.
        await expect(wizard.impactEditsCount).toHaveText('7');
        await expect(wizard.impactRows).toHaveCount(2);
        await expect(wizard.impactRowFor(TX_LATEST)).toBeVisible();
        await expect(wizard.impactRowFor(TX_MIDDLE)).toBeVisible();
      },
    );

    await When('the user continues to the confirm step', async () => {
      await wizard.previewNext.click();
    });

    await Then('the destructive submit button is disabled until the dataset name is typed', async () => {
      await expect(wizard.root).toHaveAttribute('data-step', 'confirm');
      await expect(wizard.confirmStep).toBeVisible();
      await expect(wizard.confirmSubmit).toBeDisabled();
    });

    await When('the user types the dataset name and submits', async () => {
      await wizard.confirmInput.fill(ONTOLOGY);
      await expect(wizard.confirmSubmit).toBeEnabled();
      await wizard.confirmSubmit.click();
    });

    await Then('the rollback POST fires with the picked target', async () => {
      await expect.poll(() => state.rollbackCalls.length).toBeGreaterThanOrEqual(1);
      expect(state.rollbackCalls[state.rollbackCalls.length - 1]!.to).toBe(
        TX_TARGET,
      );
    });

    await Then('the success step renders the server-returned summary counts', async () => {
      await expect(wizard.root).toHaveAttribute('data-step', 'success');
      await expect(wizard.successStep).toBeVisible();
      // Server fixture replies with rolledBackTxIds=[TX_LATEST,TX_MIDDLE]
      // (the two newer than TX_TARGET) + restored=3, deleted=1.
      await expect(wizard.successRolledBack).toHaveAttribute('data-count', '2');
      await expect(wizard.successRestored).toHaveAttribute('data-count', '3');
      await expect(wizard.successDeleted).toHaveAttribute('data-count', '1');
      await expect(wizard.successNewTx).toHaveAttribute(
        'data-tx-id',
        TX_NEW_HEAD,
      );
    });
  });

  test('Scenario: the progress modal is mounted while the rollback request is in-flight @smoke', async ({
    page,
  }) => {
    const wizard = new DatasetRollbackPage(page);
    const state = makeState();

    await Given('the rollback handler responds with a 500ms delay', async () => {
      await stubDatasetEndpoints(page, state, sampleChain, {
        rollbackDelayMs: 500,
      });
    });

    await Given('the user is on the rollback wizard', async () => {
      await wizard.goto(ONTOLOGY);
      await expect(wizard.txTable).toBeVisible();
    });

    await When('the user picks the target tx and walks to the confirm step', async () => {
      await wizard.txRadioFor(TX_TARGET).check();
      await wizard.pickNext.click();
      await wizard.previewNext.click();
      await wizard.confirmInput.fill(ONTOLOGY);
    });

    await When('the user submits the rollback', async () => {
      await wizard.confirmSubmit.click();
    });

    await Then('the progress modal appears while the request is in flight', async () => {
      await expect(wizard.progressModal).toBeVisible();
      await expect(wizard.progressBar).toBeVisible();
      await expect(wizard.progressBar).toHaveAttribute('role', 'progressbar');
    });

    await Then('the progress modal closes once the response lands', async () => {
      await expect(wizard.progressModal).toBeHidden();
      await expect(wizard.successStep).toBeVisible();
    });
  });

  test('Scenario: the confirm step blocks submit until the typed dataset name matches @smoke', async ({
    page,
  }) => {
    const wizard = new DatasetRollbackPage(page);
    const state = makeState();

    await Given('the dataset history is stubbed', async () => {
      await stubDatasetEndpoints(page, state, sampleChain);
    });

    await Given('the user is on the confirm step', async () => {
      await wizard.goto(ONTOLOGY);
      await wizard.txRadioFor(TX_TARGET).check();
      await wizard.pickNext.click();
      await wizard.previewNext.click();
      await expect(wizard.confirmStep).toBeVisible();
    });

    await Then('the destructive submit is disabled when the input is empty', async () => {
      await expect(wizard.confirmInput).toHaveValue('');
      await expect(wizard.confirmSubmit).toBeDisabled();
    });

    await When('the user types an incorrect dataset name', async () => {
      await wizard.confirmInput.fill('wrong-name');
    });

    await Then('the destructive submit remains disabled', async () => {
      await expect(wizard.confirmSubmit).toBeDisabled();
    });

    await When('the user corrects the dataset name to match', async () => {
      await wizard.confirmInput.fill(ONTOLOGY);
    });

    await Then('the destructive submit unlocks', async () => {
      await expect(wizard.confirmSubmit).toBeEnabled();
    });

    await Then('no rollback POST has fired yet', async () => {
      expect(state.rollbackCalls).toHaveLength(0);
    });
  });
});
