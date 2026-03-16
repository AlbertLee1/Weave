interface ParameterFormProps {
  parameters: unknown;
  values: Record<string, unknown>;
  onChange: (values: Record<string, unknown>) => void;
}

interface ParameterDef {
  type?: string;
  required?: boolean;
  description?: string;
}

export function ParameterForm({ parameters, values, onChange }: ParameterFormProps) {
  const paramDefs = (parameters ?? {}) as Record<string, ParameterDef>;
  const entries = Object.entries(paramDefs);

  if (entries.length === 0) {
    return (
      <div className="text-xs text-text-secondary py-4">
        No parameters defined for this action.
      </div>
    );
  }

  function handleChange(key: string, value: unknown) {
    onChange({ ...values, [key]: value });
  }

  const inputClass =
    'bg-bg-tertiary border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-cyan focus:outline-none w-full';
  const labelClass = 'text-xs text-text-secondary font-sans mb-1';

  return (
    <div className="flex flex-col gap-4">
      {entries.map(([key, def]) => {
        const paramType = def.type ?? 'string';
        const currentValue = values[key];

        if (paramType === 'boolean') {
          return (
            <div key={key} className="flex flex-col">
              <label className="flex items-center gap-2 cursor-pointer">
                <input
                  type="checkbox"
                  checked={Boolean(currentValue)}
                  onChange={(e) => handleChange(key, e.target.checked)}
                  className="accent-accent-cyan"
                />
                <span className="text-sm font-mono text-text-primary">{key}</span>
                {def.required && <span className="text-accent-error text-xs">*</span>}
              </label>
              {def.description && (
                <span className="text-xs text-text-secondary mt-1">{def.description}</span>
              )}
            </div>
          );
        }

        if (paramType === 'integer' || paramType === 'double') {
          return (
            <div key={key} className="flex flex-col">
              <label className={labelClass}>
                {key}
                {def.required && <span className="text-accent-error ml-1">*</span>}
              </label>
              <input
                type="number"
                step={paramType === 'double' ? 'any' : '1'}
                value={currentValue != null ? String(currentValue) : ''}
                onChange={(e) => {
                  const v = e.target.value;
                  handleChange(
                    key,
                    v === '' ? undefined : paramType === 'integer' ? parseInt(v, 10) : parseFloat(v),
                  );
                }}
                className={inputClass}
                placeholder={paramType}
              />
              {def.description && (
                <span className="text-xs text-text-secondary mt-1">{def.description}</span>
              )}
            </div>
          );
        }

        if (paramType === 'array') {
          return (
            <div key={key} className="flex flex-col">
              <label className={labelClass}>
                {key} (comma-separated)
                {def.required && <span className="text-accent-error ml-1">*</span>}
              </label>
              <input
                type="text"
                value={Array.isArray(currentValue) ? (currentValue as string[]).join(', ') : String(currentValue ?? '')}
                onChange={(e) => {
                  const v = e.target.value;
                  handleChange(
                    key,
                    v ? v.split(',').map((s) => s.trim()) : [],
                  );
                }}
                className={inputClass}
                placeholder="value1, value2, value3"
              />
              {def.description && (
                <span className="text-xs text-text-secondary mt-1">{def.description}</span>
              )}
            </div>
          );
        }

        // Default: string text input
        return (
          <div key={key} className="flex flex-col">
            <label className={labelClass}>
              {key}
              {def.required && <span className="text-accent-error ml-1">*</span>}
            </label>
            <input
              type="text"
              value={currentValue != null ? String(currentValue) : ''}
              onChange={(e) => handleChange(key, e.target.value || undefined)}
              className={inputClass}
              placeholder={def.description ?? paramType}
            />
            {def.description && (
              <span className="text-xs text-text-secondary mt-1">{def.description}</span>
            )}
          </div>
        );
      })}
    </div>
  );
}
