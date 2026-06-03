import { useState } from 'react';
import { useFormContext } from 'react-hook-form';
import type { ActionParameterV2 } from '../../api/types';
import { uploadAttachment } from '../../api/attachments';
import { uploadMedia } from '../../api/media';

interface ParameterFormProps {
  parameters: Record<string, ActionParameterV2>;
}

type UploadKind = 'attachment' | 'media';

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

export function ParameterForm({ parameters }: ParameterFormProps) {
  const entries = Object.entries(parameters ?? {});
  const { register, formState, setValue, watch } = useFormContext();
  const { errors } = formState;

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
      {entries.map(([key, def]) => {
        const paramType = def.dataType?.type ?? 'string';
        const fieldError = errors[key];
        const errorMessage =
          typeof fieldError?.message === 'string' ? fieldError.message : undefined;
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

        if (paramType === 'integer' || paramType === 'double') {
          return (
            <div key={key} className="flex flex-col">
              <label htmlFor={fieldId} className={labelClass}>
                {key}
                {def.required && <span className="text-accent-error ml-1">*</span>}
              </label>
              <input
                id={fieldId}
                type="number"
                step={paramType === 'double' ? 'any' : '1'}
                aria-invalid={errorMessage ? 'true' : 'false'}
                aria-describedby={errorMessage ? errorId : undefined}
                {...register(key, {
                  setValueAs: (v) => {
                    if (v === '' || v === null || v === undefined) return undefined;
                    const n = paramType === 'integer' ? parseInt(String(v), 10) : parseFloat(String(v));
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
              <span id={errorId} role="alert" className="text-xs text-accent-error mt-1">
                {errorMessage}
              </span>
            )}
          </div>
        );
      })}
    </div>
  );
}
