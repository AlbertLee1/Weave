import { useState } from 'react';
import type { CreateObjectTypeInput } from '../../api/admin';

interface ObjectTypeFormProps {
  onSubmit: (values: CreateObjectTypeInput) => void;
  initialValues?: Partial<CreateObjectTypeInput>;
  isLoading: boolean;
}

export function ObjectTypeForm({ onSubmit, initialValues, isLoading }: ObjectTypeFormProps) {
  const [apiName, setApiName] = useState(initialValues?.apiName ?? '');
  const [displayName, setDisplayName] = useState(initialValues?.displayName ?? '');
  const [pluralDisplayName, setPluralDisplayName] = useState(initialValues?.pluralDisplayName ?? '');
  const [primaryKey, setPrimaryKey] = useState(initialValues?.primaryKey ?? '');
  const [description, setDescription] = useState(initialValues?.description ?? '');
  const [status, setStatus] = useState(initialValues?.status ?? 'ACTIVE');
  const [visibility, setVisibility] = useState(initialValues?.visibility ?? 'NORMAL');
  const [icon, setIcon] = useState(initialValues?.icon ?? '');
  const [color, setColor] = useState(initialValues?.color ?? '');

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    onSubmit({
      apiName,
      displayName,
      pluralDisplayName: pluralDisplayName || undefined,
      primaryKey,
      description: description || undefined,
      status,
      visibility,
      icon: icon || undefined,
      color: color || undefined,
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
          placeholder="employee"
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
          placeholder="Employee"
        />
      </div>

      <div className="flex flex-col">
        <label className={labelClass}>Plural Display Name</label>
        <input
          type="text"
          value={pluralDisplayName}
          onChange={(e) => setPluralDisplayName(e.target.value)}
          className={inputClass}
          placeholder="Employees"
        />
      </div>

      <div className="flex flex-col">
        <label className={labelClass}>Primary Key</label>
        <input
          type="text"
          value={primaryKey}
          onChange={(e) => setPrimaryKey(e.target.value)}
          required
          className={inputClass}
          placeholder="id"
        />
      </div>

      <div className="flex flex-col">
        <label className={labelClass}>Description</label>
        <textarea
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          rows={2}
          className={`${inputClass} resize-y`}
          placeholder="Describe this object type..."
        />
      </div>

      <div className="grid grid-cols-2 gap-4">
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
          <label className={labelClass}>Visibility</label>
          <select
            value={visibility}
            onChange={(e) => setVisibility(e.target.value)}
            className={inputClass}
          >
            <option value="PROMINENT">PROMINENT</option>
            <option value="NORMAL">NORMAL</option>
            <option value="HIDDEN">HIDDEN</option>
          </select>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-4">
        <div className="flex flex-col">
          <label className={labelClass}>Icon</label>
          <input
            type="text"
            value={icon}
            onChange={(e) => setIcon(e.target.value)}
            className={inputClass}
            placeholder="cube"
          />
        </div>

        <div className="flex flex-col">
          <label className={labelClass}>Color</label>
          <input
            type="text"
            value={color}
            onChange={(e) => setColor(e.target.value)}
            className={inputClass}
            placeholder="#3b82f6"
          />
        </div>
      </div>

      <button
        type="submit"
        disabled={isLoading}
        className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80 disabled:opacity-50"
      >
        {isLoading ? 'Saving...' : initialValues ? 'Update Object Type' : 'Create Object Type'}
      </button>
    </form>
  );
}
