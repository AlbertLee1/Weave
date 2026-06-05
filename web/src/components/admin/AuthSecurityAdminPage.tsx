import { useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import type {
  RevokeTokenRequest,
  RevokeTokenResponse,
  RotateSigningKeysResponse,
} from '../../api/authSecurity';
import { rotateSigningKeys, revokeToken } from '../../api/authSecurity';
import { describeApiError } from '../../api/describeError';
import { useToastStore } from '../../stores/toastStore';
import { Modal } from '../common/Modal';

// AuthSecurityAdminPage is the global admin surface for the two low-level auth
// operations the backend exposes: JWT signing-key rotation and per-JTI token
// revocation. Both are destructive / security-sensitive, so each lives in its
// own section with independent error handling. This is distinct from the
// per-user "My Sessions" management in SettingsPage — here an operator
// blacklists any token by its raw jti.

// formatTimestamp renders a server timestamp for display. These are raw RFC3339
// instants from the backend; we surface them verbatim (rather than via
// toLocaleString) so the displayed value is stable and machine-correlatable
// across operator locales / timezones. Empty values fall back to an em-dash.
function formatTimestamp(iso?: string | null): string {
  if (!iso) return '—';
  return iso;
}

// datetimeLocalToRFC3339 converts an <input type="datetime-local"> value into
// the RFC3339 timestamp the backend's expiresAt parser requires, interpreting
// the wall-clock value as UTC. The input value is `YYYY-MM-DDTHH:MM` and may
// additionally carry seconds (`:SS`) depending on the browser / step, so we
// append a bare `Z` (not `:00Z`, which would corrupt a seconds-bearing value
// into an unparseable string). Returns null for an unparseable value so the
// caller can surface a clean error instead of throwing on toISOString().
function datetimeLocalToRFC3339(value: string): string | null {
  const d = new Date(`${value}Z`);
  if (Number.isNaN(d.getTime())) return null;
  return d.toISOString();
}

const inputClass =
  'w-full px-3 py-1.5 text-sm rounded bg-bg-tertiary text-text-primary outline-none border border-transparent focus:border-accent-cyan/40 disabled:opacity-60';

export function AuthSecurityAdminPage() {
  return (
    <div
      data-testid="auth-security-admin-page"
      className="flex flex-col h-[calc(100vh-3rem)] bg-bg-primary overflow-y-auto"
    >
      <header
        className="px-6 py-4 border-b flex flex-wrap items-center gap-4"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <h1 className="text-base font-semibold tracking-wide text-text-primary">
          Auth Security
        </h1>
        <span className="text-xs text-text-secondary uppercase tracking-widest">
          Key rotation &amp; token revocation
        </span>
      </header>

      <div className="flex-1 px-6 py-6 flex flex-col gap-8 max-w-3xl">
        <SigningKeysSection />
        <RevokeTokenSection />
      </div>
    </div>
  );
}

function SigningKeysSection() {
  const pushToast = useToastStore((s) => s.push);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [result, setResult] = useState<RotateSigningKeysResponse | null>(null);

  const rotate = useMutation({
    mutationFn: () => rotateSigningKeys(),
  });

  async function onConfirm() {
    setSubmitError(null);
    try {
      const res = await rotate.mutateAsync();
      setResult(res);
      setConfirmOpen(false);
    } catch (err) {
      const msg = describeApiError(err, 'Failed to rotate signing keys.');
      setSubmitError(msg);
      // Close the confirm dialog so the error surfaces inline in the section,
      // where the operator can read it and re-trigger the rotation.
      setConfirmOpen(false);
      pushToast({ message: msg, severity: 'error' });
    }
  }

  return (
    <section
      data-testid="auth-keys-section"
      className="rounded border p-5"
      style={{
        borderColor: 'rgba(31,41,55,0.5)',
        background: 'rgba(13,17,23,0.4)',
      }}
    >
      <h2 className="text-sm font-semibold text-text-primary">
        JWT Signing Keys
      </h2>
      <p className="text-xs text-text-secondary mt-1">
        Rotating generates a fresh RSA key pair and makes it the active signing
        key. Existing keys stay in the ring so already-issued tokens keep
        verifying.
      </p>

      <button
        type="button"
        data-testid="auth-keys-rotate-btn"
        onClick={() => {
          setSubmitError(null);
          setConfirmOpen(true);
        }}
        className="mt-4 px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 transition-colors"
      >
        Rotate signing keys
      </button>

      {!result && (
        <p
          data-testid="auth-keys-placeholder"
          className="mt-4 text-xs text-text-muted"
        >
          The backend has no endpoint to list the keyring. After you rotate,
          the resulting key ring is shown here.
        </p>
      )}

      {result && (
        <div data-testid="auth-keys-result" className="mt-4 flex flex-col gap-3">
          <div className="text-xs text-text-secondary">
            Active key id:{' '}
            <span
              data-testid="auth-keys-active-id"
              className="font-mono text-sm px-2 py-0.5 rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40"
            >
              {result.activeKeyId}
            </span>
          </div>
          <div>
            <div className="text-[10px] uppercase tracking-widest text-text-secondary mb-1">
              Key ring
            </div>
            <ul
              data-testid="auth-keys-ring"
              className="flex flex-col gap-1 font-mono text-xs"
            >
              {result.keyIds.map((kid) => (
                <li
                  key={kid}
                  data-testid={`auth-keys-ring-id-${kid}`}
                  className={
                    kid === result.activeKeyId
                      ? 'text-accent-cyan'
                      : 'text-text-secondary'
                  }
                >
                  {kid}
                  {kid === result.activeKeyId && (
                    <span className="ml-2 text-[10px] uppercase tracking-widest text-accent-cyan">
                      active
                    </span>
                  )}
                </li>
              ))}
            </ul>
          </div>
          <div
            data-testid="auth-keys-rotated-at"
            className="text-xs text-text-secondary"
          >
            Rotated at: {formatTimestamp(result.rotatedAt)}
          </div>
        </div>
      )}

      {submitError && (
        <p
          role="alert"
          data-testid="auth-keys-rotate-error"
          className="mt-3 text-xs text-accent-error"
        >
          {submitError}
        </p>
      )}

      {confirmOpen && (
        <Modal
          open
          onClose={() => setConfirmOpen(false)}
          title="Rotate signing keys"
        >
          <div
            data-testid="auth-keys-rotate-confirm"
            className="flex flex-col gap-3"
          >
            <p className="text-sm text-text-primary">
              Rotate the JWT signing keys now?
            </p>
            <p className="text-xs text-text-secondary">
              This affects <span className="font-semibold">all future token
              issuance</span> — every newly signed token uses the new active
              key. This action cannot be undone.
            </p>
            <div className="flex justify-end gap-2 pt-2">
              <button
                type="button"
                data-testid="auth-keys-rotate-cancel"
                onClick={() => setConfirmOpen(false)}
                className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
              >
                Cancel
              </button>
              <button
                type="button"
                data-testid="auth-keys-rotate-confirm-btn"
                onClick={onConfirm}
                disabled={rotate.isPending}
                className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 disabled:opacity-40 disabled:cursor-not-allowed"
              >
                {rotate.isPending ? 'Rotating…' : 'Rotate keys'}
              </button>
            </div>
          </div>
        </Modal>
      )}
    </section>
  );
}

function RevokeTokenSection() {
  const pushToast = useToastStore((s) => s.push);
  const [jti, setJti] = useState('');
  const [userId, setUserId] = useState('');
  const [reason, setReason] = useState('');
  const [expiresAt, setExpiresAt] = useState('');
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [result, setResult] = useState<RevokeTokenResponse | null>(null);

  const revoke = useMutation({
    mutationFn: ({ id, body }: { id: string; body: RevokeTokenRequest }) =>
      revokeToken(id, body),
  });

  const canSubmit = jti.trim() !== '' && !revoke.isPending;

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitError(null);
    const id = jti.trim();
    if (id === '') return;

    const body: RevokeTokenRequest = {};
    if (userId.trim() !== '') body.userId = userId.trim();
    if (reason.trim() !== '') body.reason = reason.trim();
    if (expiresAt.trim() !== '') {
      const rfc = datetimeLocalToRFC3339(expiresAt);
      if (rfc === null) {
        const msg = 'Expires At is not a valid date/time.';
        setSubmitError(msg);
        setResult(null);
        return;
      }
      body.expiresAt = rfc;
    }

    try {
      const res = await revoke.mutateAsync({ id, body });
      setResult(res);
      // Clear the form on success so the operator can revoke the next token.
      setJti('');
      setUserId('');
      setReason('');
      setExpiresAt('');
    } catch (err) {
      const msg = describeApiError(err, 'Failed to revoke token.');
      setSubmitError(msg);
      setResult(null);
      pushToast({ message: msg, severity: 'error' });
    }
  }

  return (
    <section
      data-testid="auth-revoke-section"
      className="rounded border p-5"
      style={{
        borderColor: 'rgba(31,41,55,0.5)',
        background: 'rgba(13,17,23,0.4)',
      }}
    >
      <h2 className="text-sm font-semibold text-text-primary">Revoke Token</h2>
      <p className="text-xs text-text-secondary mt-1">
        Blacklist a single token by its <span className="font-mono">jti</span>.
        The token stops verifying immediately. The optional fields are recorded
        for the audit trail.
      </p>

      <form
        onSubmit={onSubmit}
        data-testid="auth-revoke-form"
        className="mt-4 flex flex-col gap-4"
      >
        <Field label="JTI" required>
          <input
            type="text"
            data-testid="auth-revoke-jti"
            value={jti}
            onChange={(e) => setJti(e.target.value)}
            className={inputClass + ' font-mono'}
            placeholder="b3c1…token-id"
          />
        </Field>
        <Field label="User ID" hint="Optional. The token's subject.">
          <input
            type="text"
            data-testid="auth-revoke-user-id"
            value={userId}
            onChange={(e) => setUserId(e.target.value)}
            className={inputClass + ' font-mono'}
            placeholder="user:alice"
          />
        </Field>
        <Field label="Reason" hint="Optional. Recorded with the revocation.">
          <input
            type="text"
            data-testid="auth-revoke-reason"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            className={inputClass}
            placeholder="compromised laptop"
          />
        </Field>
        <Field
          label="Expires At"
          hint="Optional. When the revocation entry can be evicted (defaults to the token's own expiry)."
        >
          <input
            type="datetime-local"
            data-testid="auth-revoke-expires-at"
            value={expiresAt}
            onChange={(e) => setExpiresAt(e.target.value)}
            className={inputClass}
          />
        </Field>

        {submitError && (
          <p
            role="alert"
            data-testid="auth-revoke-error"
            className="text-xs text-accent-error"
          >
            {submitError}
          </p>
        )}
        {result && (
          <p
            role="status"
            data-testid="auth-revoke-success"
            className="text-xs text-emerald-400"
          >
            Revoked <span className="font-mono">{result.jti}</span> at{' '}
            {formatTimestamp(result.revokedAt)}.
          </p>
        )}

        <div className="flex justify-end">
          <button
            type="submit"
            data-testid="auth-revoke-submit"
            disabled={!canSubmit}
            className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-error/20 text-accent-error border border-accent-error/40 hover:bg-accent-error/30 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {revoke.isPending ? 'Revoking…' : 'Revoke token'}
          </button>
        </div>
      </form>
    </section>
  );
}

function Field({
  label,
  required,
  hint,
  children,
}: {
  label: string;
  required?: boolean;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <label className="flex flex-col gap-1 text-xs text-text-secondary">
      <span className="uppercase tracking-widest">
        {label}
        {required && <span className="text-accent-error"> *</span>}
      </span>
      {children}
      {hint && <span className="text-[11px] text-text-muted">{hint}</span>}
    </label>
  );
}
