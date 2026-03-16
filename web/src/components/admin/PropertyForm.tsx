import { useState } from 'react';
import type { CreatePropertyInput } from '../../api/admin';

interface PropertyFormProps {
  onSubmit: (values: CreatePropertyInput) => void;
  isLoading: boolean;
}

export function PropertyForm({ onSubmit, isLoading }: PropertyFormProps) {
  const [apiName, setApiName] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [baseType, setBaseType] = useState('string');
  const [isArray, setIsArray] = useState(false);
  const [isNullable, setIsNullable] = useState(false);
  const [isSearchable, setIsSearchable] = useState(false);
  const [isSortable, setIsSortable] = useState(false);

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    onSubmit({
      apiName,
      displayName: displayName || undefined,
      baseType,
      isArray,
      isNullable,
      isSearchable,
      isSortable,
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
          placeholder="first_name"
        />
      </div>

      <div className="flex flex-col">
        <label className={labelClass}>Display Name</label>
        <input
          type="text"
          value={displayName}
          onChange={(e) => setDisplayName(e.target.value)}
          className={inputClass}
          placeholder="First Name"
        />
      </div>

      <div className="flex flex-col">
        <label className={labelClass}>Base Type</label>
        <select
          value={baseType}
          onChange={(e) => setBaseType(e.target.value)}
          className={inputClass}
        >
          <option value="string">string</option>
          <option value="integer">integer</option>
          <option value="long">long</option>
          <option value="double">double</option>
          <option value="boolean">boolean</option>
          <option value="timestamp">timestamp</option>
          <option value="date">date</option>
        </select>
      </div>

      <div className="flex flex-col gap-2">
        <label className={labelClass}>Options</label>
        <div className="grid grid-cols-2 gap-3">
          <label className="flex items-center gap-2 text-sm text-text-primary cursor-pointer">
            <input
              type="checkbox"
              checked={isArray}
              onChange={(e) => setIsArray(e.target.checked)}
              className="accent-accent-cyan"
            />
            <span className="font-mono text-xs">isArray</span>
          </label>
          <label className="flex items-center gap-2 text-sm text-text-primary cursor-pointer">
            <input
              type="checkbox"
              checked={isNullable}
              onChange={(e) => setIsNullable(e.target.checked)}
              className="accent-accent-cyan"
            />
            <span className="font-mono text-xs">isNullable</span>
          </label>
          <label className="flex items-center gap-2 text-sm text-text-primary cursor-pointer">
            <input
              type="checkbox"
              checked={isSearchable}
              onChange={(e) => setIsSearchable(e.target.checked)}
              className="accent-accent-cyan"
            />
            <span className="font-mono text-xs">isSearchable</span>
          </label>
          <label className="flex items-center gap-2 text-sm text-text-primary cursor-pointer">
            <input
              type="checkbox"
              checked={isSortable}
              onChange={(e) => setIsSortable(e.target.checked)}
              className="accent-accent-cyan"
            />
            <span className="font-mono text-xs">isSortable</span>
          </label>
        </div>
      </div>

      <button
        type="submit"
        disabled={isLoading}
        className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80 disabled:opacity-50"
      >
        {isLoading ? 'Creating...' : 'Create Property'}
      </button>
    </form>
  );
}
