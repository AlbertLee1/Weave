import { useEffect, useRef, useState } from 'react';
import { useFormContext, useWatch } from 'react-hook-form';
import type { ActionParameterV2 } from '../../api/types';
import { uploadAttachment } from '../../api/attachments';
import { uploadMedia } from '../../api/media';
import { validateAction } from '../../api/actions';
import { useMarkings } from '../../hooks/useMarkings';
import { INTEGER_PARAM_TYPES, isNumericParamType } from './parameterSchema';

interface ParameterFormProps {
  parameters: Record<string, ActionParameterV2>;
  // When BOTH ontologyApiName and actionApiName are supplied, the form runs
  // debounced, side-effect-free real-time validation against the dedicated
  // POST .../actions/{action}/validate surface as the user edits, red-lining
  // individual fields whose parameters[id].result === 'INVALID' and surfacing
  // submissionCriteria[].configuredFailureMessage as a form-level banner.
  // Both are optional so existing callers (and unit harnesses) that only pass
  // `parameters` keep their current behaviour — validation is strictly opt-in.
  ontologyApiName?: string;
  actionApiName?: string;
}

// Debounce window (ms) between the last keystroke and the validate POST. Long
// enough to coalesce a burst of typing into a single request, short enough to
// feel live.
const VALIDATE_DEBOUNCE_MS = 350;

// RealtimeValidationState is what useRealtimeValidation surfaces to the form:
// the per-parameter ids the server flagged INVALID (→ inline field errors) and
// the form-level submission-criteria failure messages (→ banner).
interface RealtimeValidationState {
  invalidParamIds: Set<string>;
  failureMessages: string[];
}

const EMPTY_VALIDATION: RealtimeValidationState = {
  invalidParamIds: new Set<string>(),
  failureMessages: [],
};

/**
 * useRealtimeValidation drives the debounced /validate round-trip.
 *
 * Design notes (the tricky bits):
 *   - We watch the live form values via useWatch (a fresh object each render)
 *     but key the debounce effect on a STABLE JSON projection of those values,
 *     never the object identity — otherwise the effect would re-fire on every
 *     render and loop.
 *   - The validate POST runs inside a debounce timer cleared on each change,
 *     so a burst of keystrokes collapses to a single request.
 *   - A monotonically increasing request token guards against out-of-order
 *     responses (a slow earlier request resolving after a fast later one):
 *     only the latest token's result is committed to state.
 *   - We call the bare `validateAction` API function (not the useValidateAction
 *     mutation) so the form stays agnostic of a QueryClientProvider — the
 *     validate surface is side-effect-free and has no cache to invalidate, so a
 *     mutation buys nothing here while forcing every ParameterForm host to wrap
 *     a provider. The exported useValidateAction hook remains available for
 *     callers that want a react-query-managed validate.
 *   - When wiring is absent (no ontology/action) the hook is inert: it issues
 *     no requests and reports an empty state.
 */
function useRealtimeValidation(
  ontologyApiName: string | undefined,
  actionApiName: string | undefined,
): RealtimeValidationState {
  const { control, formState } = useFormContext();
  const values = useWatch({ control });
  // Gate on isDirty so we never validate the pristine/default form on mount —
  // an untouched required field defaults to '' and would be red-lined (with the
  // failure banner) before the user types anything. Validation becomes live the
  // moment the user edits any field (isDirty flips true and stays true).
  const enabled =
    !!ontologyApiName && !!actionApiName && formState.isDirty;

  const [state, setState] = useState<RealtimeValidationState>(EMPTY_VALIDATION);

  // Serialize the watched values into a stable dependency. useWatch returns a
  // new object reference each render, so we must depend on its content, not its
  // identity.
  const valuesKey = JSON.stringify(values ?? {});

  const requestToken = useRef(0);

  useEffect(() => {
    if (!enabled) {
      // Defensive: if validation was turned off, drop any stale state.
      setState((prev) => (prev === EMPTY_VALIDATION ? prev : EMPTY_VALIDATION));
      return;
    }

    const token = ++requestToken.current;
    // Guards against committing a resolved fetch after this effect was torn
    // down (component unmount / re-arm) — the debounce timer is cleared, but an
    // already-in-flight request can still resolve.
    let active = true;
    const timer = setTimeout(() => {
      // Re-parse from the stable key so the request body matches exactly what
      // the dependency captured (avoids depending on the live object).
      const parsed = (JSON.parse(valuesKey) ?? {}) as Record<string, unknown>;
      // Strip undefined entries so omitted optionals don't surface as
      // explicit-null on the wire (mirrors the apply path).
      const cleaned: Record<string, unknown> = {};
      for (const [k, v] of Object.entries(parsed)) {
        if (v !== undefined) cleaned[k] = v;
      }
      void validateAction(
        ontologyApiName as string,
        actionApiName as string,
        cleaned,
      )
        .then((resp) => {
          // Discard out-of-order / superseded responses, or ones that resolve
          // after teardown.
          if (!active || token !== requestToken.current) return;
          const invalidParamIds = new Set<string>();
          for (const [id, pr] of Object.entries(resp.parameters ?? {})) {
            if (pr.result === 'INVALID') invalidParamIds.add(id);
          }
          const failureMessages = (resp.submissionCriteria ?? [])
            .filter((c) => c.result === 'INVALID' && !!c.configuredFailureMessage)
            .map((c) => c.configuredFailureMessage as string);
          setState({ invalidParamIds, failureMessages });
        })
        .catch(() => {
          // Transport / non-2xx errors are not validation outcomes; leave the
          // last good state in place rather than flapping the UI on a blip.
          if (token !== requestToken.current) return;
        });
    }, VALIDATE_DEBOUNCE_MS);

    return () => {
      active = false;
      clearTimeout(timer);
    };
    // valuesKey captures the form content; enabled/actionApiName/ontologyApiName
    // re-arm when wiring changes. mutateRef is a ref (intentionally excluded).
  }, [valuesKey, enabled, actionApiName, ontologyApiName]);

  return state;
}

type UploadKind = 'attachment' | 'media';

/**
 * toRFC3339 normalises an `<input type="datetime-local">` value into the
 * RFC3339 string the backend's timestamp coercion accepts
 * (pkg/types/coerce.go -> time.Parse(time.RFC3339, v)).
 *
 * The control yields "YYYY-MM-DDTHH:mm" (and "YYYY-MM-DDTHH:mm:ss" when a
 * `step` exposes seconds). We treat the wall-clock value as a UTC instant —
 * no local-timezone math — by appending ":00" seconds if absent and a "Z"
 * suffix. Already-zoned input (ending in "Z" or a numeric offset) passes
 * through unchanged so re-normalising is idempotent.
 */
function toRFC3339(local: string): string {
  if (!local) return local;
  if (/(?:Z|[+-]\d{2}:\d{2})$/.test(local)) return local;
  // The control reports "YYYY-MM-DDTHH:mm", "...:ss", or (in some engines with
  // step exposing seconds) "...:ss.SSS". Drop any fractional milliseconds so
  // the wire stays a clean second-granularity RFC3339 instant.
  const trimmed = local.replace(/\.\d+$/, '');
  // Append ":00" seconds only when the time has just hours+minutes.
  const withSeconds = /T\d{2}:\d{2}:\d{2}$/.test(trimmed) ? trimmed : `${trimmed}:00`;
  return `${withSeconds}Z`;
}

interface UploadFieldProps {
  fieldKey: string;
  def: ActionParameterV2;
  kind: UploadKind;
}

/**
 * UploadField renders a file picker for `attachment` / `media` action
 * parameters. On selection it uploads the raw file (attachment) or multipart
 * blob (media), shows progress + the uploaded filename, and writes the
 * returned RID back into the form so the action receives the rid. A "or paste
 * a RID" text fallback stays available for users who already hold a rid.
 */
function UploadField({ fieldKey, def, kind }: UploadFieldProps) {
  const { setValue, watch, formState } = useFormContext();
  const [status, setStatus] = useState<'idle' | 'uploading' | 'done'>('idle');
  const [uploadedName, setUploadedName] = useState<string | null>(null);
  const [uploadError, setUploadError] = useState<string | null>(null);

  const fieldId = `param-${fieldKey}`;
  const errorId = `${fieldId}-error`;
  const fieldError = formState.errors[fieldKey];
  const errorMessage =
    typeof fieldError?.message === 'string' ? fieldError.message : undefined;

  const inputClass =
    'bg-bg-tertiary border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-cyan focus:outline-none w-full';
  const inputErrorClass =
    'bg-bg-tertiary border border-accent-error rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-error focus:outline-none w-full';
  const labelClass = 'text-xs text-text-secondary font-sans mb-1';

  const currentRid = watch(fieldKey);

  const handleFile = async (file: File | undefined) => {
    if (!file) return;
    setUploadError(null);
    setStatus('uploading');
    try {
      const result =
        kind === 'attachment'
          ? await uploadAttachment(file)
          : await uploadMedia(file);
      setValue(fieldKey, result.rid, { shouldValidate: true, shouldDirty: true });
      setUploadedName(file.name);
      setStatus('done');
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      setUploadError(`Upload failed: ${message}`);
      setStatus('idle');
    }
  };

  return (
    <div className="flex flex-col">
      <label htmlFor={`${fieldId}-file`} className={labelClass}>
        {fieldKey} ({kind})
        {def.required && <span className="text-accent-error ml-1">*</span>}
      </label>

      <div className="bg-bg-tertiary border border-border rounded p-3 flex flex-col gap-2">
        <input
          id={`${fieldId}-file`}
          data-testid={`${fieldId}-file`}
          type="file"
          disabled={status === 'uploading'}
          onChange={(e) => {
            const file = e.target.files?.[0];
            // Reset the input's value so picking the SAME file again (e.g. to
            // retry after an upload error) still fires onChange.
            e.target.value = '';
            void handleFile(file);
          }}
          className="text-xs text-text-secondary font-mono file:mr-3 file:rounded file:border file:border-border file:bg-bg-secondary file:px-3 file:py-1 file:text-text-primary file:cursor-pointer disabled:opacity-50"
        />

        {status === 'uploading' && (
          <span
            data-testid={`${fieldId}-uploading`}
            className="text-xs text-accent-cyan"
            aria-live="polite"
          >
            Uploading…
          </span>
        )}

        {status === 'done' && uploadedName && (
          <span
            data-testid={`${fieldId}-uploaded`}
            className="text-xs text-text-primary font-mono break-all"
          >
            ✓ {uploadedName}
          </span>
        )}

        {uploadError && (
          <span
            id={`${fieldId}-upload-error`}
            data-testid={`${fieldId}-upload-error`}
            role="alert"
            className="text-xs text-accent-error"
          >
            {uploadError}
          </span>
        )}

        <input
          id={fieldId}
          data-testid={`${fieldId}-rid`}
          type="text"
          value={typeof currentRid === 'string' ? currentRid : ''}
          aria-invalid={errorMessage ? 'true' : 'false'}
          aria-describedby={errorMessage ? errorId : undefined}
          onChange={(e) => {
            setUploadedName(null);
            setStatus('idle');
            setValue(fieldKey, e.target.value, {
              shouldValidate: true,
              shouldDirty: true,
            });
          }}
          onBlur={() =>
            setValue(fieldKey, watch(fieldKey), {
              shouldValidate: true,
              shouldTouch: true,
            })
          }
          className={errorMessage ? inputErrorClass : inputClass}
          placeholder="or paste an existing RID"
        />
      </div>

      {def.description && (
        <span className="text-xs text-text-secondary mt-1">{def.description}</span>
      )}
      {errorMessage && (
        <span id={errorId} role="alert" className="text-xs text-accent-error mt-1">
          {errorMessage}
        </span>
      )}
    </div>
  );
}

interface MarkingSelectFieldProps {
  fieldKey: string;
  def: ActionParameterV2;
  multiple: boolean;
}

/**
 * MarkingSelectField renders a `<select>` populated from the marking catalog
 * (`listMarkings()` via the shared `useMarkings` hook) for `marking`-typed
 * action parameters.
 *
 * Wire-format contract: a marking is identified everywhere on the backend by
 * its NAME string — the grant API body carries `marking` (the name) and the
 * policy engine (pkg/security/policy_engine.go RuleTypeMarkingSubset) compares
 * marking NAME strings against an object's marking set. `marking` has no
 * dedicated Coerce/Validate case (pkg/types) so it passes through unchanged.
 * We therefore emit the marking's `name` (scalar) or an array of names
 * (array-of-marking) — never the human display name.
 *
 * The option labels show the human display name (falling back to the name).
 * Empty selection collapses to '' when required (Zod min(1)/min-length blocks
 * apply) or `undefined` when optional, mirroring the other scalar branches;
 * the array variant collapses to [].
 *
 * Degraded states: while the catalog is loading, or if it fails to load, or if
 * it is empty, we fall back to a plain text input keyed by the canonical
 * `param-<key>` testid so the parameter stays usable (the user can hand-type a
 * marking name).
 */
function MarkingSelectField({ fieldKey, def, multiple }: MarkingSelectFieldProps) {
  const { setValue, watch, formState, register } = useFormContext();
  const { data: markings, isLoading, isError } = useMarkings();

  // Registering the SCALAR field lets the submit-time `setValueAs` run even for
  // an untouched optional default: '' collapses to undefined so the field is
  // omitted from the wire payload (required '' stays '' so Zod min(1) blocks),
  // exactly mirroring the date / string branches. We still drive value/onChange
  // ourselves so the controlled <select> reflects the current form value. The
  // multi (array) variant must NOT register with this String()-coercing
  // setValueAs — it would mangle the array value into a string on submit.
  const scalarReg = register(fieldKey, {
    setValueAs: (v) => {
      if (multiple) return v;
      if (v === '' || v === null || v === undefined) {
        return def.required ? '' : undefined;
      }
      return String(v);
    },
  });

  const fieldId = `param-${fieldKey}`;
  const errorId = `${fieldId}-error`;
  const fieldError = formState.errors[fieldKey];
  const errorMessage =
    typeof fieldError?.message === 'string' ? fieldError.message : undefined;

  const inputClass =
    'bg-bg-tertiary border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-cyan focus:outline-none w-full';
  const inputErrorClass =
    'bg-bg-tertiary border border-accent-error rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-error focus:outline-none w-full';
  const labelClass = 'text-xs text-text-secondary font-sans mb-1';

  const label = (
    <label htmlFor={fieldId} className={labelClass}>
      {fieldKey}
      {multiple ? ' (markings)' : ''}
      {def.required && <span className="text-accent-error ml-1">*</span>}
    </label>
  );
  const footer = (
    <>
      {def.description && (
        <span className="text-xs text-text-secondary mt-1">{def.description}</span>
      )}
      {errorMessage && (
        <span id={errorId} role="alert" className="text-xs text-accent-error mt-1">
          {errorMessage}
        </span>
      )}
    </>
  );

  const current = watch(fieldKey);

  // Degraded fallback: a non-transient empty/errored catalog → a text input so
  // the user can still hand-type a marking name / names. Loading is transient
  // and handled below by a disabled select shell, so the rendered element type
  // stays stable (select) once a usable catalog arrives — no input→select flip.
  const degraded = !isLoading && (isError || (markings?.length ?? 0) === 0);
  if (degraded) {
    if (multiple) {
      const display = Array.isArray(current) ? (current as string[]).join(', ') : '';
      return (
        <div className="flex flex-col">
          {label}
          <input
            id={fieldId}
            data-testid={fieldId}
            type="text"
            value={display}
            aria-invalid={errorMessage ? 'true' : 'false'}
            aria-describedby={errorMessage ? errorId : undefined}
            onChange={(e) => {
              const v = e.target.value;
              setValue(fieldKey, v ? v.split(',').map((s) => s.trim()) : [], {
                shouldValidate: true,
                shouldDirty: true,
              });
            }}
            onBlur={() =>
              setValue(fieldKey, watch(fieldKey), {
                shouldValidate: true,
                shouldTouch: true,
              })
            }
            className={errorMessage ? inputErrorClass : inputClass}
            placeholder={isLoading ? 'Loading markings…' : 'marking1, marking2'}
          />
          {footer}
        </div>
      );
    }
    return (
      <div className="flex flex-col">
        {label}
        <input
          {...scalarReg}
          id={fieldId}
          data-testid={fieldId}
          type="text"
          value={typeof current === 'string' ? current : ''}
          aria-invalid={errorMessage ? 'true' : 'false'}
          aria-describedby={errorMessage ? errorId : undefined}
          onChange={(e) => {
            const v = e.target.value;
            setValue(fieldKey, v === '' ? (def.required ? '' : undefined) : v, {
              shouldValidate: true,
              shouldDirty: true,
            });
          }}
          onBlur={(e) => {
            void scalarReg.onBlur(e);
            setValue(fieldKey, watch(fieldKey), {
              shouldValidate: true,
              shouldTouch: true,
            });
          }}
          className={errorMessage ? inputErrorClass : inputClass}
          placeholder={isLoading ? 'Loading markings…' : 'marking name'}
        />
        {footer}
      </div>
    );
  }

  const options = markings ?? [];

  if (multiple) {
    const selected = Array.isArray(current) ? (current as string[]) : [];
    return (
      <div className="flex flex-col">
        {label}
        <select
          id={fieldId}
          data-testid={fieldId}
          multiple
          disabled={isLoading}
          aria-invalid={errorMessage ? 'true' : 'false'}
          aria-describedby={errorMessage ? errorId : undefined}
          onChange={(e) => {
            const names = Array.from(e.target.selectedOptions).map((o) => o.value);
            setValue(fieldKey, names, { shouldValidate: true, shouldDirty: true });
          }}
          onBlur={() =>
            setValue(fieldKey, watch(fieldKey), {
              shouldValidate: true,
              shouldTouch: true,
            })
          }
          className={`${errorMessage ? inputErrorClass : inputClass} min-h-[6rem]`}
        >
          {options.map((m) => (
            <option key={m.name} value={m.name} selected={selected.includes(m.name)}>
              {m.displayName || m.name}
            </option>
          ))}
        </select>
        {footer}
      </div>
    );
  }

  const scalar = typeof current === 'string' ? current : '';
  return (
    <div className="flex flex-col">
      {label}
      <select
        // Spreading the register() result wires RHF's ref/name so the
        // submit-time `setValueAs` runs (collapsing an untouched optional ''
        // to undefined). The controlled value/onChange/onBlur below override
        // the spread so the <select> still reflects the live form value.
        {...scalarReg}
        id={fieldId}
        data-testid={fieldId}
        disabled={isLoading}
        value={scalar}
        aria-invalid={errorMessage ? 'true' : 'false'}
        aria-describedby={errorMessage ? errorId : undefined}
        onChange={(e) => {
          const v = e.target.value;
          setValue(fieldKey, v === '' ? (def.required ? '' : undefined) : v, {
            shouldValidate: true,
            shouldDirty: true,
          });
        }}
        onBlur={(e) => {
          void scalarReg.onBlur(e);
          setValue(fieldKey, watch(fieldKey), {
            shouldValidate: true,
            shouldTouch: true,
          });
        }}
        className={errorMessage ? inputErrorClass : inputClass}
      >
        <option value="">{isLoading ? 'Loading markings…' : 'Select a marking…'}</option>
        {options.map((m) => (
          <option key={m.name} value={m.name}>
            {m.displayName || m.name}
          </option>
        ))}
      </select>
      {footer}
    </div>
  );
}

export function ParameterForm({
  parameters,
  ontologyApiName,
  actionApiName,
}: ParameterFormProps) {
  const entries = Object.entries(parameters ?? {});
  const { register, formState, setValue, watch } = useFormContext();
  const { errors } = formState;

  // Debounced, side-effect-free real-time validation. Inert (no requests, empty
  // state) unless both ontologyApiName and actionApiName are supplied. Hooks
  // must run unconditionally, so this is called before the early return below.
  const realtime = useRealtimeValidation(ontologyApiName, actionApiName);

  if (entries.length === 0) {
    return (
      <div className="text-xs text-text-secondary py-4">
        No parameters defined for this action.
      </div>
    );
  }

  const inputClass =
    'bg-bg-tertiary border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-cyan focus:outline-none w-full';
  const inputErrorClass =
    'bg-bg-tertiary border border-accent-error rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-error focus:outline-none w-full';
  const labelClass = 'text-xs text-text-secondary font-sans mb-1';

  return (
    <div className="flex flex-col gap-4">
      {/* Form-level banner: submissionCriteria failure messages from the
          dedicated /validate surface. Rendered above the fields so the user
          sees the action-wide reason a draft is rejected. */}
      {realtime.failureMessages.length > 0 && (
        <div
          data-testid="action-validate-banner"
          role="alert"
          className="border border-accent-error rounded p-3 bg-accent-error/10 flex flex-col gap-1"
        >
          {realtime.failureMessages.map((msg, i) => (
            <span key={i} className="text-xs text-text-primary">
              {msg}
            </span>
          ))}
        </div>
      )}
      {entries.map(([key, def]) => {
        const paramType = def.dataType?.type ?? 'string';
        const fieldError = errors[key];
        // Combine the client-side (Zod) field error with the server-side
        // real-time INVALID flag. The Zod message wins for display when both
        // are present; otherwise a generic "Invalid value" stands in so the
        // field is still red-lined from the /validate report alone.
        const zodMessage =
          typeof fieldError?.message === 'string' ? fieldError.message : undefined;
        const realtimeInvalid = realtime.invalidParamIds.has(key);
        const errorMessage =
          zodMessage ?? (realtimeInvalid ? 'Invalid value' : undefined);
        const fieldId = `param-${key}`;
        const errorId = `${fieldId}-error`;

        if (paramType === 'boolean') {
          return (
            <div key={key} className="flex flex-col">
              <label className="flex items-center gap-2 cursor-pointer">
                <input
                  id={fieldId}
                  type="checkbox"
                  {...register(key)}
                  className="accent-accent-cyan"
                />
                <span className="text-sm font-mono text-text-primary">{key}</span>
                {def.required && <span className="text-accent-error text-xs">*</span>}
              </label>
              {def.description && (
                <span className="text-xs text-text-secondary mt-1">{def.description}</span>
              )}
              {errorMessage && (
                <span id={errorId} role="alert" className="text-xs text-accent-error mt-1">
                  {errorMessage}
                </span>
              )}
            </div>
          );
        }

        if (isNumericParamType(paramType)) {
          const isIntegerFamily = INTEGER_PARAM_TYPES.includes(paramType);
          return (
            <div key={key} className="flex flex-col">
              <label htmlFor={fieldId} className={labelClass}>
                {key}
                {def.required && <span className="text-accent-error ml-1">*</span>}
              </label>
              <input
                id={fieldId}
                data-testid={fieldId}
                type="number"
                step={isIntegerFamily ? '1' : 'any'}
                aria-invalid={errorMessage ? 'true' : 'false'}
                aria-describedby={errorMessage ? errorId : undefined}
                {...register(key, {
                  // Always parseFloat — even for the integer family — so a
                  // fractional entry (e.g. "1.5" for a short) is preserved and
                  // rejected by the Zod `.int()` check, mirroring the backend
                  // coerce* "has fractional part" error. parseInt would silently
                  // truncate it to a valid integer.
                  setValueAs: (v) => {
                    if (v === '' || v === null || v === undefined) return undefined;
                    const n = parseFloat(String(v));
                    return Number.isNaN(n) ? undefined : n;
                  },
                })}
                className={errorMessage ? inputErrorClass : inputClass}
                placeholder={paramType}
              />
              {def.description && (
                <span className="text-xs text-text-secondary mt-1">{def.description}</span>
              )}
              {errorMessage && (
                <span id={errorId} role="alert" className="text-xs text-accent-error mt-1">
                  {errorMessage}
                </span>
              )}
            </div>
          );
        }

        if (paramType === 'attachment' || paramType === 'media') {
          return (
            <UploadField key={key} fieldKey={key} def={def} kind={paramType} />
          );
        }

        if (paramType === 'marking') {
          return (
            <MarkingSelectField key={key} fieldKey={key} def={def} multiple={false} />
          );
        }

        // Array of marking → a multi-select picker. Detected before the generic
        // array branch so the element type drives the renderer.
        if (paramType === 'array' && def.dataType?.itemType?.type === 'marking') {
          return (
            <MarkingSelectField key={key} fieldKey={key} def={def} multiple={true} />
          );
        }

        if (paramType === 'date') {
          // Native date picker. The control value and the wire value share the
          // same canonical form: "YYYY-MM-DD" (Go: time.Parse("2006-01-02")).
          // We register the field (rather than driving it with setValue) so the
          // submit-time setValueAs runs even for an untouched default: empty
          // collapses to '' when required (Zod's min(1) blocks apply) or
          // undefined when optional (the field is omitted from the wire
          // payload), exactly mirroring the default string branch.
          return (
            <div key={key} className="flex flex-col">
              <label htmlFor={fieldId} className={labelClass}>
                {key}
                {def.required && <span className="text-accent-error ml-1">*</span>}
              </label>
              <input
                id={fieldId}
                data-testid={fieldId}
                type="date"
                aria-invalid={errorMessage ? 'true' : 'false'}
                aria-describedby={errorMessage ? errorId : undefined}
                {...register(key, {
                  setValueAs: (v) => {
                    if (v === '' || v === null || v === undefined) {
                      return def.required ? '' : undefined;
                    }
                    return String(v);
                  },
                })}
                className={errorMessage ? inputErrorClass : inputClass}
              />
              {def.description && (
                <span className="text-xs text-text-secondary mt-1">{def.description}</span>
              )}
              {errorMessage && (
                <span id={errorId} role="alert" className="text-xs text-accent-error mt-1">
                  {errorMessage}
                </span>
              )}
            </div>
          );
        }

        if (paramType === 'timestamp') {
          // Native datetime-local picker. The control speaks "YYYY-MM-DDTHH:mm"
          // (and "...:ss" with step=1), but the backend parses time.RFC3339
          // ("2006-01-02T15:04:05Z07:00"). We normalise on both sides:
          //   - render: strip an RFC3339 wire value down to the control's shape
          //     via a controlled `value` (so a prefilled instant shows up).
          //   - submit: setValueAs appends seconds (if absent) + a "Z" UTC
          //     marker so the wire value is always a valid RFC3339 instant,
          //     and runs for untouched defaults too (optional empty -> omitted).
          const current = watch(key);
          const wire = typeof current === 'string' ? current : '';
          // Strip the zone marker + any fractional seconds to "YYYY-MM-DDTHH:mm[:ss]"
          // for the control (no timezone math — wall-clock is preserved as-is).
          const display = wire
            ? wire.replace(/(Z|[+-]\d{2}:\d{2})$/, '').replace(/\.\d+$/, '')
            : '';
          const reg = register(key, {
            setValueAs: (v) => {
              if (v === '' || v === null || v === undefined) {
                return def.required ? '' : undefined;
              }
              return toRFC3339(String(v));
            },
          });
          return (
            <div key={key} className="flex flex-col">
              <label htmlFor={fieldId} className={labelClass}>
                {key}
                {def.required && <span className="text-accent-error ml-1">*</span>}
              </label>
              <input
                id={fieldId}
                data-testid={fieldId}
                type="datetime-local"
                step="1"
                value={display}
                aria-invalid={errorMessage ? 'true' : 'false'}
                aria-describedby={errorMessage ? errorId : undefined}
                name={reg.name}
                ref={reg.ref}
                onChange={(e) => {
                  const v = e.target.value;
                  setValue(
                    key,
                    v === '' ? (def.required ? '' : undefined) : toRFC3339(v),
                    { shouldValidate: true, shouldDirty: true },
                  );
                }}
                onBlur={(e) => {
                  void reg.onBlur(e);
                  setValue(key, watch(key), { shouldValidate: true, shouldTouch: true });
                }}
                className={errorMessage ? inputErrorClass : inputClass}
              />
              {def.description && (
                <span className="text-xs text-text-secondary mt-1">{def.description}</span>
              )}
              {errorMessage && (
                <span id={errorId} role="alert" className="text-xs text-accent-error mt-1">
                  {errorMessage}
                </span>
              )}
            </div>
          );
        }

        if (paramType === 'array') {
          // Array values round-trip through a comma-separated text field.
          // We render a controlled-like view by reading the form value and
          // calling setValue on every keystroke; register handles validation
          // while the input shows a string projection of the array.
          const current = watch(key);
          const display = Array.isArray(current) ? (current as string[]).join(', ') : '';
          return (
            <div key={key} className="flex flex-col">
              <label htmlFor={fieldId} className={labelClass}>
                {key} (comma-separated)
                {def.required && <span className="text-accent-error ml-1">*</span>}
              </label>
              <input
                id={fieldId}
                type="text"
                value={display}
                aria-invalid={errorMessage ? 'true' : 'false'}
                aria-describedby={errorMessage ? errorId : undefined}
                onChange={(e) => {
                  const v = e.target.value;
                  setValue(
                    key,
                    v ? v.split(',').map((s) => s.trim()) : [],
                    { shouldValidate: true, shouldDirty: true },
                  );
                }}
                onBlur={() => setValue(key, watch(key), { shouldValidate: true, shouldTouch: true })}
                className={errorMessage ? inputErrorClass : inputClass}
                placeholder="value1, value2, value3"
              />
              {def.description && (
                <span className="text-xs text-text-secondary mt-1">{def.description}</span>
              )}
              {errorMessage && (
                <span id={errorId} role="alert" className="text-xs text-accent-error mt-1">
                  {errorMessage}
                </span>
              )}
            </div>
          );
        }

        // Default: string text input
        return (
          <div key={key} className="flex flex-col">
            <label htmlFor={fieldId} className={labelClass}>
              {key}
              {def.required && <span className="text-accent-error ml-1">*</span>}
            </label>
            <input
              id={fieldId}
              data-testid={fieldId}
              type="text"
              aria-invalid={errorMessage ? 'true' : 'false'}
              aria-describedby={errorMessage ? errorId : undefined}
              {...register(key, {
                setValueAs: (v) => {
                  if (v === '' || v === null || v === undefined) return def.required ? '' : undefined;
                  return String(v);
                },
              })}
              className={errorMessage ? inputErrorClass : inputClass}
              placeholder={def.description ?? paramType}
            />
            {def.description && (
              <span className="text-xs text-text-secondary mt-1">{def.description}</span>
            )}
            {errorMessage && (
              <span
                id={errorId}
                role="alert"
                {...(realtimeInvalid ? { 'data-testid': `${fieldId}-validate-error` } : {})}
                className="text-xs text-accent-error mt-1"
              >
                {errorMessage}
              </span>
            )}
          </div>
        );
      })}
    </div>
  );
}
