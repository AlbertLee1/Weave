import { useState } from 'react';

interface Parameter {
  name: string;
  type: string;
  required: boolean;
  description?: string;
}

interface ParameterBuilderProps {
  value: Parameter[];
  onChange: (params: Parameter[]) => void;
}

const PARAM_TYPES = [
  'string',
  'integer',
  'long',
  'double',
  'boolean',
  'timestamp',
  'date',
  'array',
  'object',
];

const inputClass =
  'bg-bg-tertiary border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-cyan focus:outline-none';

export function ParameterBuilder({ value, onChange }: ParameterBuilderProps) {
  const [showJson, setShowJson] = useState(false);
  const [jsonText, setJsonText] = useState(() =>
    value.length > 0 ? JSON.stringify(value, null, 2) : '',
  );
  const [jsonError, setJsonError] = useState('');

  function addParameter() {
    const updated = [...value, { name: '', type: 'string', required: false }];
    onChange(updated);
    setJsonText(JSON.stringify(updated, null, 2));
  }

  function updateParameter(index: number, field: keyof Parameter, fieldValue: unknown) {
    const updated = value.map((p, i) =>
      i === index ? { ...p, [field]: fieldValue } : p,
    );
    onChange(updated);
    setJsonText(JSON.stringify(updated, null, 2));
  }

  function removeParameter(index: number) {
    const updated = value.filter((_, i) => i !== index);
    onChange(updated);
    setJsonText(JSON.stringify(updated, null, 2));
  }

  function handleJsonChange(text: string) {
    setJsonText(text);
    setJsonError('');
    if (!text.trim()) {
      onChange([]);
      return;
    }
    try {
      const parsed = JSON.parse(text);
      if (!Array.isArray(parsed)) {
        setJsonError('Expected a JSON array of parameters');
        return;
      }
      onChange(parsed as Parameter[]);
    } catch {
      setJsonError('Invalid JSON');
    }
  }

  function toggleView() {
    if (!showJson) {
      setJsonText(value.length > 0 ? JSON.stringify(value, null, 2) : '');
      setJsonError('');
    }
    setShowJson((v) => !v);
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between">
        <span className="text-xs text-text-secondary font-sans">Parameters</span>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={toggleView}
            className="text-xs text-text-secondary hover:text-accent-cyan transition-colors"
          >
            {showJson ? 'Switch to Visual' : 'Switch to JSON'}
          </button>
          {!showJson && (
            <button
              type="button"
              onClick={addParameter}
              className="text-xs bg-accent-cyan/10 text-accent-cyan border border-accent-cyan/30 rounded px-2 py-1 hover:bg-accent-cyan/20 transition-colors"
            >
              + Add Parameter
            </button>
          )}
        </div>
      </div>

      {showJson ? (
        <div className="flex flex-col gap-1">
          <textarea
            value={jsonText}
            onChange={(e) => handleJsonChange(e.target.value)}
            rows={8}
            className={`${inputClass} resize-y ${jsonError ? 'border-red-500' : ''}`}
            placeholder="[]"
          />
          {jsonError && (
            <span className="text-xs text-red-400">{jsonError}</span>
          )}
        </div>
      ) : (
        <div className="flex flex-col gap-2">
          {value.length === 0 ? (
            <p className="text-xs text-text-secondary italic py-3 text-center border border-border rounded bg-bg-tertiary/50">
              No parameters defined. Click '+ Add Parameter' to start.
            </p>
          ) : (
            value.map((param, index) => (
              <div
                key={index}
                className="flex flex-col gap-2 border border-border rounded p-3 bg-bg-tertiary/30"
              >
                <div className="flex items-center gap-2">
                  <input
                    type="text"
                    value={param.name}
                    onChange={(e) => updateParameter(index, 'name', e.target.value)}
                    className={`${inputClass} flex-1 min-w-0`}
                    placeholder="parameterName"
                  />
                  <select
                    value={param.type}
                    onChange={(e) => updateParameter(index, 'type', e.target.value)}
                    className={`${inputClass} w-32 shrink-0`}
                  >
                    {PARAM_TYPES.map((t) => (
                      <option key={t} value={t}>
                        {t}
                      </option>
                    ))}
                  </select>
                  <label className="flex items-center gap-1 text-xs text-text-secondary shrink-0 cursor-pointer select-none">
                    <input
                      type="checkbox"
                      checked={param.required}
                      onChange={(e) => updateParameter(index, 'required', e.target.checked)}
                      className="accent-cyan-400 w-3.5 h-3.5"
                    />
                    required
                  </label>
                  <button
                    type="button"
                    onClick={() => removeParameter(index)}
                    className="text-text-secondary hover:text-red-400 transition-colors shrink-0 px-1"
                    title="Remove parameter"
                  >
                    <svg
                      xmlns="http://www.w3.org/2000/svg"
                      className="w-4 h-4"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth={2}
                      strokeLinecap="round"
                      strokeLinejoin="round"
                    >
                      <polyline points="3 6 5 6 21 6" />
                      <path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" />
                      <path d="M10 11v6" />
                      <path d="M14 11v6" />
                      <path d="M9 6V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2" />
                    </svg>
                  </button>
                </div>
                <input
                  type="text"
                  value={param.description ?? ''}
                  onChange={(e) =>
                    updateParameter(index, 'description', e.target.value || undefined)
                  }
                  className={`${inputClass} w-full`}
                  placeholder="Description..."
                />
              </div>
            ))
          )}
        </div>
      )}
    </div>
  );
}
