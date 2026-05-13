import { useMemo, useState } from 'react';
import { useParams } from 'react-router';
import { useMutation } from '@tanstack/react-query';
import { useDatasetHistory } from '../../hooks/useDatasetHistory';
import {
  rollbackDataset,
  type DatasetRollbackResponse,
  type DatasetTransaction,
} from '../../api/datasets';
import { Modal } from '../common/Modal';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { useToastStore } from '../../stores/toastStore';

// US-053 / PC-A10 — Dataset Rollback wizard.
//
// The wizard walks the operator through four explicit gates so the
// destructive POST /api/v2/datasets/{rid}/rollback never fires accidentally:
//
//   1. Pick — select the target transaction from the audit chain.
//   2. Preview — see how many transactions / objects would be touched.
//   3. Confirm — re-type the dataset apiName, click "Roll back".
//   4. Run — submit, watch the indeterminate progress bar, and read the
//      success summary toast.
//
// The page is permission-gated upstream (AdminGuard) so the routing layer
// already rejects non-admin requests. The handler tolerates a missing
// store + missing snapshot reader (degraded mode returns zero counts),
// so the wizard does not attempt a pre-flight before the user confirms.
type WizardStep = 'pick' | 'preview' | 'confirm' | 'success';

function shortTxId(txId: string): string {
  return txId.length > 12 ? `${txId.slice(0, 11)}…` : txId;
}

function formatTimestamp(iso: string): string {
  if (!iso) return '—';
  try {
    return new Date(iso).toLocaleString();
  } catch {
    return iso;
  }
}

export function DatasetRollbackPage() {
  const { dataset } = useParams<{ dataset: string }>();
  const ontology = dataset ?? '';

  const { data, isLoading, error, refetch } = useDatasetHistory(ontology);
  const transactions = useMemo<DatasetTransaction[]>(
    () => data?.transactions ?? [],
    [data],
  );

  const [step, setStep] = useState<WizardStep>('pick');
  const [targetTxId, setTargetTxId] = useState<string>('');
  const [confirmText, setConfirmText] = useState<string>('');
  const [summary, setSummary] = useState<DatasetRollbackResponse | null>(null);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const pushToast = useToastStore((s) => s.push);

  const targetTx = useMemo(
    () => transactions.find((t) => t.txId === targetTxId) ?? null,
    [transactions, targetTxId],
  );

  // Transactions strictly newer than the picked target — these are the
  // ones the server will mark as rolled back. Mirrors the handler's
  // `ListAfterCommittedAt` filter so the preview is honest.
  const newerTransactions = useMemo<DatasetTransaction[]>(() => {
    if (!targetTx) return [];
    const cutoff = new Date(targetTx.committedAt).valueOf();
    if (Number.isNaN(cutoff)) return [];
    return transactions.filter((t) => {
      if (t.txId === targetTx.txId) return false;
      const at = new Date(t.committedAt).valueOf();
      return !Number.isNaN(at) && at > cutoff;
    });
  }, [transactions, targetTx]);

  const totalAffectedEdits = useMemo(
    () => newerTransactions.reduce((sum, t) => sum + (t.editsCount ?? 0), 0),
    [newerTransactions],
  );

  const mutation = useMutation({
    mutationFn: ({
      rid,
      tx,
    }: {
      rid: string;
      tx: string;
    }) => rollbackDataset(rid, tx),
    onSuccess: (resp) => {
      setSummary(resp);
      setStep('success');
      setSubmitError(null);
      pushToast({
        message: `Rollback complete — ${resp.rolledBackTxIds.length} tx marked rolled back`,
        severity: 'success',
      });
      void refetch();
    },
    onError: (err: Error) => {
      setSubmitError(err.message);
      pushToast({ message: `Rollback failed: ${err.message}`, severity: 'error' });
    },
  });

  if (!ontology) {
    return (
      <div
        data-testid="dataset-rollback-no-dataset"
        className="flex items-center justify-center h-[calc(100vh-3rem)] text-text-secondary text-sm"
      >
        Select an ontology from the dashboard first.
      </div>
    );
  }

  function resetWizard() {
    setStep('pick');
    setTargetTxId('');
    setConfirmText('');
    setSummary(null);
    setSubmitError(null);
  }

  function startSubmit() {
    if (!targetTx) return;
    mutation.mutate({ rid: ontology, tx: targetTx.txId });
  }

  const confirmTyped = confirmText.trim() === ontology;

  return (
    <div
      data-testid="dataset-rollback-page"
      data-dataset-rid={ontology}
      data-step={step}
      className="flex flex-col h-[calc(100vh-3rem)] bg-bg-primary overflow-y-auto"
    >
      <header
        className="px-6 py-4 border-b flex flex-wrap items-center gap-4"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <h1 className="text-base font-semibold tracking-wide text-text-primary">
          Dataset Rollback
        </h1>
        <span className="text-xs text-text-secondary uppercase tracking-widest">
          {ontology}
        </span>
        <div className="flex-1" />
        <StepRibbon current={step} />
      </header>

      <div className="flex-1 px-6 py-6">
        {isLoading && (
          <div
            data-testid="dataset-rollback-loading"
            className="flex items-center justify-center py-20"
          >
            <LoadingSpinner size="lg" />
          </div>
        )}
        {!isLoading && error && (
          <p
            data-testid="dataset-rollback-error"
            className="text-sm text-accent-error"
          >
            Failed to load dataset history: {(error as Error).message}
          </p>
        )}
        {!isLoading && !error && step === 'pick' && (
          <PickStep
            transactions={transactions}
            targetTxId={targetTxId}
            onPick={(id) => setTargetTxId(id)}
            onNext={() => setStep('preview')}
          />
        )}
        {!isLoading && !error && step === 'preview' && targetTx && (
          <PreviewStep
            targetTx={targetTx}
            newerTransactions={newerTransactions}
            totalAffectedEdits={totalAffectedEdits}
            onBack={() => setStep('pick')}
            onNext={() => setStep('confirm')}
          />
        )}
        {!isLoading && !error && step === 'confirm' && targetTx && (
          <ConfirmStep
            ontology={ontology}
            targetTx={targetTx}
            confirmText={confirmText}
            confirmTyped={confirmTyped}
            running={mutation.isPending}
            error={submitError}
            onConfirmTextChange={setConfirmText}
            onBack={() => setStep('preview')}
            onSubmit={startSubmit}
          />
        )}
        {!isLoading && !error && step === 'success' && summary && (
          <SuccessStep
            summary={summary}
            onClose={resetWizard}
          />
        )}
      </div>

      {mutation.isPending && (
        <RollbackProgressModal
          targetTxId={targetTx?.txId ?? ''}
        />
      )}
    </div>
  );
}

const STEP_LABELS: Record<WizardStep, string> = {
  pick: '1. Pick transaction',
  preview: '2. Preview impact',
  confirm: '3. Confirm',
  success: '4. Done',
};

function StepRibbon({ current }: { current: WizardStep }) {
  const order: WizardStep[] = ['pick', 'preview', 'confirm', 'success'];
  return (
    <ol
      data-testid="dataset-rollback-step-ribbon"
      className="flex items-center gap-2 text-[11px] uppercase tracking-widest text-text-secondary"
    >
      {order.map((step) => {
        const active = step === current;
        return (
          <li
            key={step}
            data-step={step}
            data-active={active}
            className={`px-2 py-1 rounded border ${
              active
                ? 'border-accent-cyan/60 text-accent-cyan'
                : 'border-transparent'
            }`}
          >
            {STEP_LABELS[step]}
          </li>
        );
      })}
    </ol>
  );
}

function PickStep({
  transactions,
  targetTxId,
  onPick,
  onNext,
}: {
  transactions: DatasetTransaction[];
  targetTxId: string;
  onPick: (txId: string) => void;
  onNext: () => void;
}) {
  const canAdvance = !!targetTxId;
  return (
    <div data-testid="dataset-rollback-pick-step" className="flex flex-col gap-4">
      <p className="text-sm text-text-primary">
        Choose the dataset transaction to roll back to. Every transaction
        committed after the picked target will be marked rolled back and the
        affected objects restored to their state at that target's commit.
      </p>
      {transactions.length === 0 ? (
        <div
          data-testid="dataset-rollback-empty"
          className="rounded border px-6 py-10 text-center"
          style={{
            borderColor: 'rgba(31,41,55,0.5)',
            background: 'rgba(13,17,23,0.4)',
          }}
        >
          <p className="text-sm text-text-primary font-semibold">
            No transactions on file
          </p>
          <p className="text-xs text-text-secondary mt-2">
            This dataset has no committed transactions yet, so there is
            nothing to roll back to.
          </p>
        </div>
      ) : (
        <div
          data-testid="dataset-rollback-tx-table"
          className="rounded border overflow-hidden"
          style={{
            borderColor: 'rgba(31,41,55,0.5)',
            background: 'rgba(13,17,23,0.4)',
          }}
        >
          <table className="w-full text-sm">
            <thead className="text-[10px] uppercase tracking-widest text-text-secondary">
              <tr className="border-b" style={{ borderColor: 'rgba(31,41,55,0.5)' }}>
                <th className="text-left px-4 py-2 font-medium">Pick</th>
                <th className="text-left px-4 py-2 font-medium">Transaction</th>
                <th className="text-left px-4 py-2 font-medium">Committed</th>
                <th className="text-right px-4 py-2 font-medium">Edits</th>
                <th className="text-left px-4 py-2 font-medium">Status</th>
              </tr>
            </thead>
            <tbody>
              {transactions.map((tx) => {
                const rolledBack = !!tx.rolledBackAt;
                return (
                  <tr
                    key={tx.txId}
                    data-testid="dataset-rollback-tx-row"
                    data-tx-id={tx.txId}
                    data-tx-rolled-back={rolledBack}
                    className="border-b last:border-0 hover:bg-bg-tertiary/30"
                    style={{ borderColor: 'rgba(31,41,55,0.5)' }}
                  >
                    <td className="px-4 py-2">
                      <input
                        type="radio"
                        name="rollback-target"
                        data-testid="dataset-rollback-tx-radio"
                        data-tx-id={tx.txId}
                        value={tx.txId}
                        checked={tx.txId === targetTxId}
                        onChange={() => onPick(tx.txId)}
                        aria-label={`Pick ${tx.txId}`}
                      />
                    </td>
                    <td className="px-4 py-2 font-mono text-xs text-text-secondary">
                      {shortTxId(tx.txId)}
                    </td>
                    <td className="px-4 py-2 text-xs text-text-secondary">
                      {formatTimestamp(tx.committedAt)}
                    </td>
                    <td className="px-4 py-2 text-right text-xs text-text-secondary">
                      {tx.editsCount}
                    </td>
                    <td className="px-4 py-2 text-xs">
                      {rolledBack ? (
                        <span className="text-amber-400">rolled back</span>
                      ) : (
                        <span className="text-emerald-400">live</span>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      <div className="flex justify-end gap-2 pt-2">
        <button
          type="button"
          data-testid="dataset-rollback-pick-next"
          disabled={!canAdvance}
          onClick={onNext}
          className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 disabled:opacity-40 disabled:cursor-not-allowed"
        >
          Continue
        </button>
      </div>
    </div>
  );
}

function PreviewStep({
  targetTx,
  newerTransactions,
  totalAffectedEdits,
  onBack,
  onNext,
}: {
  targetTx: DatasetTransaction;
  newerTransactions: DatasetTransaction[];
  totalAffectedEdits: number;
  onBack: () => void;
  onNext: () => void;
}) {
  return (
    <div data-testid="dataset-rollback-preview-step" className="flex flex-col gap-4">
      <div
        className="rounded border px-4 py-3"
        style={{
          borderColor: 'rgba(31,41,55,0.5)',
          background: 'rgba(13,17,23,0.4)',
        }}
      >
        <p className="text-[11px] uppercase tracking-widest text-text-secondary">
          Target transaction
        </p>
        <p
          data-testid="dataset-rollback-preview-target"
          data-tx-id={targetTx.txId}
          className="mt-1 font-mono text-xs text-text-primary"
        >
          {targetTx.txId}
        </p>
        <p className="text-xs text-text-secondary mt-1">
          committed {formatTimestamp(targetTx.committedAt)} ·{' '}
          {targetTx.editsCount} edits
        </p>
      </div>

      <div
        className="rounded border px-4 py-3"
        style={{
          borderColor: 'rgba(31,41,55,0.5)',
          background: 'rgba(13,17,23,0.4)',
        }}
      >
        <p className="text-[11px] uppercase tracking-widest text-text-secondary">
          Impact summary
        </p>
        <ul className="mt-2 flex flex-col gap-1 text-sm text-text-primary">
          <li>
            <span
              data-testid="dataset-rollback-impact-tx-count"
              className="font-semibold"
            >
              {newerTransactions.length}
            </span>{' '}
            transactions will be marked rolled back.
          </li>
          <li>
            <span
              data-testid="dataset-rollback-impact-edits-count"
              className="font-semibold"
            >
              {totalAffectedEdits}
            </span>{' '}
            edits across those transactions will be reverted.
          </li>
        </ul>
        {newerTransactions.length === 0 && (
          <p
            data-testid="dataset-rollback-impact-empty"
            className="mt-2 text-xs text-amber-400"
          >
            No newer transactions exist — rollback is a metadata-only no-op.
          </p>
        )}
      </div>

      {newerTransactions.length > 0 && (
        <div
          data-testid="dataset-rollback-impact-list"
          className="rounded border overflow-hidden"
          style={{
            borderColor: 'rgba(31,41,55,0.5)',
            background: 'rgba(13,17,23,0.4)',
          }}
        >
          <table className="w-full text-sm">
            <thead className="text-[10px] uppercase tracking-widest text-text-secondary">
              <tr className="border-b" style={{ borderColor: 'rgba(31,41,55,0.5)' }}>
                <th className="text-left px-4 py-2 font-medium">Transaction</th>
                <th className="text-left px-4 py-2 font-medium">Committed</th>
                <th className="text-right px-4 py-2 font-medium">Edits</th>
              </tr>
            </thead>
            <tbody>
              {newerTransactions.map((tx) => (
                <tr
                  key={tx.txId}
                  data-testid="dataset-rollback-impact-row"
                  data-tx-id={tx.txId}
                  className="border-b last:border-0"
                  style={{ borderColor: 'rgba(31,41,55,0.5)' }}
                >
                  <td className="px-4 py-2 font-mono text-xs text-text-secondary">
                    {shortTxId(tx.txId)}
                  </td>
                  <td className="px-4 py-2 text-xs text-text-secondary">
                    {formatTimestamp(tx.committedAt)}
                  </td>
                  <td className="px-4 py-2 text-right text-xs text-text-secondary">
                    {tx.editsCount}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <div className="flex justify-end gap-2 pt-2">
        <button
          type="button"
          data-testid="dataset-rollback-preview-back"
          onClick={onBack}
          className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
        >
          Back
        </button>
        <button
          type="button"
          data-testid="dataset-rollback-preview-next"
          onClick={onNext}
          className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30"
        >
          Continue to confirm
        </button>
      </div>
    </div>
  );
}

function ConfirmStep({
  ontology,
  targetTx,
  confirmText,
  confirmTyped,
  running,
  error,
  onConfirmTextChange,
  onBack,
  onSubmit,
}: {
  ontology: string;
  targetTx: DatasetTransaction;
  confirmText: string;
  confirmTyped: boolean;
  running: boolean;
  error: string | null;
  onConfirmTextChange: (text: string) => void;
  onBack: () => void;
  onSubmit: () => void;
}) {
  return (
    <div data-testid="dataset-rollback-confirm-step" className="flex flex-col gap-4 max-w-2xl">
      <div
        className="rounded border px-4 py-3"
        style={{
          borderColor: 'rgba(220,38,38,0.4)',
          background: 'rgba(60,8,8,0.25)',
        }}
      >
        <p className="text-sm font-semibold text-accent-error">
          This action is destructive.
        </p>
        <p className="text-xs text-text-secondary mt-1">
          The Bleve indexes for every object touched since{' '}
          <span className="font-mono">{shortTxId(targetTx.txId)}</span> will be
          rewritten from the prior snapshot, and a fresh bookkeeping
          transaction will be stamped as the new chain head. The newer
          transactions remain in the audit table marked rolled back, but the
          live state matches the target.
        </p>
      </div>
      <label className="flex flex-col gap-2 text-xs text-text-secondary">
        <span className="uppercase tracking-widest">
          Type <span className="font-mono text-text-primary">{ontology}</span>{' '}
          to confirm
        </span>
        <input
          type="text"
          data-testid="dataset-rollback-confirm-input"
          value={confirmText}
          onChange={(e) => onConfirmTextChange(e.target.value)}
          autoFocus
          className="w-full px-3 py-1.5 text-sm rounded bg-bg-tertiary text-text-primary outline-none border border-transparent focus:border-accent-cyan/40 font-mono"
          placeholder={ontology}
        />
      </label>
      {error && (
        <p
          role="alert"
          data-testid="dataset-rollback-confirm-error"
          className="text-xs text-accent-error"
        >
          {error}
        </p>
      )}
      <div className="flex justify-end gap-2 pt-2">
        <button
          type="button"
          data-testid="dataset-rollback-confirm-back"
          onClick={onBack}
          disabled={running}
          className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary disabled:opacity-40"
        >
          Back
        </button>
        <button
          type="button"
          data-testid="dataset-rollback-confirm-submit"
          disabled={!confirmTyped || running}
          onClick={onSubmit}
          className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-error/20 text-accent-error border border-accent-error/40 hover:bg-accent-error/30 disabled:opacity-40 disabled:cursor-not-allowed"
        >
          {running ? 'Rolling back…' : 'Roll back'}
        </button>
      </div>
    </div>
  );
}

function SuccessStep({
  summary,
  onClose,
}: {
  summary: DatasetRollbackResponse;
  onClose: () => void;
}) {
  return (
    <div data-testid="dataset-rollback-success-step" className="flex flex-col gap-4">
      <div
        className="rounded border px-4 py-3"
        style={{
          borderColor: 'rgba(16,185,129,0.4)',
          background: 'rgba(6,40,33,0.25)',
        }}
      >
        <p className="text-sm font-semibold text-emerald-400">
          Rollback complete.
        </p>
        <p className="text-xs text-text-secondary mt-1">
          The dataset is now consistent with the state at the picked target
          transaction. The bookkeeping row recorded below pins the new chain
          head.
        </p>
      </div>
      <ul className="flex flex-col gap-2 text-sm">
        <li
          data-testid="dataset-rollback-success-rolled-back"
          data-count={summary.rolledBackTxIds.length}
        >
          <span className="font-semibold">{summary.rolledBackTxIds.length}</span>{' '}
          transactions marked rolled back.
        </li>
        <li
          data-testid="dataset-rollback-success-restored"
          data-count={summary.restoredObjects}
        >
          <span className="font-semibold">{summary.restoredObjects}</span>{' '}
          objects restored from the prior snapshot.
        </li>
        <li
          data-testid="dataset-rollback-success-deleted"
          data-count={summary.deletedObjects}
        >
          <span className="font-semibold">{summary.deletedObjects}</span>{' '}
          objects deleted (created after the target).
        </li>
      </ul>
      {summary.newTransaction && (
        <div
          data-testid="dataset-rollback-success-new-tx"
          data-tx-id={summary.newTransaction.txId}
          className="rounded border px-4 py-3"
          style={{
            borderColor: 'rgba(31,41,55,0.5)',
            background: 'rgba(13,17,23,0.4)',
          }}
        >
          <p className="text-[11px] uppercase tracking-widest text-text-secondary">
            New chain head
          </p>
          <p className="mt-1 font-mono text-xs text-text-primary">
            {summary.newTransaction.txId}
          </p>
          <p className="text-xs text-text-secondary mt-1">
            committed {formatTimestamp(summary.newTransaction.committedAt)}
          </p>
        </div>
      )}
      <div className="flex justify-end pt-2">
        <button
          type="button"
          data-testid="dataset-rollback-success-close"
          onClick={onClose}
          className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30"
        >
          Done
        </button>
      </div>
    </div>
  );
}

function RollbackProgressModal({ targetTxId }: { targetTxId: string }) {
  return (
    <Modal open onClose={() => {}} title="Rollback in progress…">
      <div
        data-testid="dataset-rollback-progress-modal"
        className="flex flex-col gap-3 px-1 pb-2"
      >
        <p className="text-xs text-text-secondary">
          Replaying historical state for affected objects. Do not close this
          window.
        </p>
        <div
          aria-label="Rollback progress"
          role="progressbar"
          aria-valuemin={0}
          aria-valuemax={100}
          data-testid="dataset-rollback-progress-bar"
          className="relative h-2 w-full rounded bg-bg-tertiary overflow-hidden"
        >
          <span
            className="absolute inset-y-0 left-0 w-1/3 rounded bg-accent-cyan/70 animate-pulse"
          />
        </div>
        <p className="text-[11px] font-mono text-text-secondary truncate">
          target: {targetTxId}
        </p>
      </div>
    </Modal>
  );
}
