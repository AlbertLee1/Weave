import { useState } from 'react';
import type { CreateActionTypeInput } from '../../api/admin';

interface ActionTypeFormProps {
  onSubmit: (values: CreateActionTypeInput) => void;
  initialValues?: Partial<CreateActionTypeInput>;
  isLoading: boolean;
}

export function ActionTypeForm({ onSubmit, initialValues, isLoading }: ActionTypeFormProps) {
  const [apiName, setApiName] = useState(initialValues?.apiName ?? '');
  const [displayName, setDisplayName] = useState(initialValues?.displayName ?? '');
  const [description, setDescription] = useState(initialValues?.description ?? '');
  const [status, setStatus] = useState(initialValues?.status ?? 'ACTIVE');
  const [parametersJson, setParametersJson] = useState(
    initialValues?.parameters ? JSON.stringify(initialValues.parameters, null, 2) : '',
  );
  const [jsonError, setJsonError] = useState('');

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    let parameters: unknown = undefined;
    if (parametersJson.trim()) {
      try {
        parameters = JSON.parse(parametersJson);
        setJsonError('');
      } catch {
        setJsonError('Invalid JSON');
        return;
      }
    }
    onSubmit({
      apiName,
      displayName,
      description: description || undefined,
      status: status || undefined,
      parameters,
    });
  }

  const inputClass =
    'bg-bg-tertiary border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-cyan focus:outline-none';
  const labelClass = 'text-xs text-text-secondary font-sans mb-1';

  return (
    <form onSubmit={handleSubmit} className="flex flex-col gap-4">
      <div className="flex flex-col">
        <label className={labelClass}>API Name</label>
        <input
          type="text"
          value={apiName}
          onChange={(e) => setApiName(e.target.value)}
          required
          className={inputClass}
          placeholder="create-employee"
        />
      </div>

      <div className="flex flex-col">
        <label className={labelClass}>Display Name</label>
        <input
          type="text"
          value={displayName}
          onChange={(e) => setDisplayName(e.target.value)}
          required
          className={inputClass}
          placeholder="Create Employee"
        />
      </div>

      <div className="flex flex-col">
        <label className={labelClass}>Description</label>
        <textarea
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          rows={2}
          className={`${inputClass} resize-y`}
          placeholder="Describe this action type..."
        />
      </div>

      <div className="flex flex-col">
        <label className={labelClass}>Status</label>
        <select
          value={status}
          onChange={(e) => setStatus(e.target.value)}
          className={inputClass}
        >
          <option value="ACTIVE">ACTIVE</option>
          <option value="EXPERIMENTAL">EXPERIMENTAL</option>
          <option value="DEPRECATED">DEPRECATED</option>
        </select>
      </div>

      <div className="flex flex-col">
        <label className={labelClass}>Parameters (JSON)</label>
        <textarea
          value={parametersJson}
          onChange={(e) => {
            setParametersJson(e.target.value);
            setJsonError('');
          }}
          rows={6}
          className={`${inputClass} resize-y ${jsonError ? 'border-accent-error' : ''}`}
          placeholder='{"name": {"type": "string", "required": true}}'
        />
        {jsonError && (
          <span className="text-xs text-accent-error mt-1">{jsonError}</span>
        )}
      </div>

      <button
        type="submit"
        disabled={isLoading}
        className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80 disabled:opacity-50"
      >
        {isLoading ? 'Saving...' : initialValues ? 'Update Action Type' : 'Create Action Type'}
      </button>
    </form>
  );
}
