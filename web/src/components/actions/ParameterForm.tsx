import { useFormContext } from 'react-hook-form';
import type { ActionParameterV2 } from '../../api/types';

interface ParameterFormProps {
  parameters: Record<string, ActionParameterV2>;
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
