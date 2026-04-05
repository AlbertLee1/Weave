import { useState } from 'react';
import type { CreateActionTypeInput } from '../../api/admin';
import { ParameterBuilder } from './ParameterBuilder';

interface Parameter {
  name: string;
  type: string;
  required: boolean;
  description?: string;
}

interface ActionTypeFormProps {
  onSubmit: (values: CreateActionTypeInput) => void;
  initialValues?: Partial<CreateActionTypeInput>;
  isLoading: boolean;
}

function parseInitialParams(parameters: unknown): Parameter[] {
  if (!parameters) return [];
  if (Array.isArray(parameters)) return parameters as Parameter[];
  return [];
}

export function ActionTypeForm({ onSubmit, initialValues, isLoading }: ActionTypeFormProps) {
  const [apiName, setApiName] = useState(initialValues?.apiName ?? '');
  const [displayName, setDisplayName] = useState(initialValues?.displayName ?? '');
  const [description, setDescription] = useState(initialValues?.description ?? '');
  const [status, setStatus] = useState(initialValues?.status ?? 'ACTIVE');
  const [params, setParams] = useState<Parameter[]>(() =>
    parseInitialParams(initialValues?.parameters),
  );

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    onSubmit({
      apiName,
      displayName,
      description: description || undefined,
      status: status || undefined,
      parameters: params.length > 0 ? params : undefined,
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

      <ParameterBuilder value={params} onChange={setParams} />

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
