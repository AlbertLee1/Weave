import { useState } from 'react';
import type { ObjectType } from '../../api/types';
import type { CreateLinkTypeInput } from '../../api/admin';

interface LinkTypeFormProps {
  onSubmit: (values: CreateLinkTypeInput) => void;
  objectTypes: ObjectType[];
  isLoading: boolean;
}

export function LinkTypeForm({ onSubmit, objectTypes, isLoading }: LinkTypeFormProps) {
  const [apiName, setApiName] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [sourceObjectType, setSourceObjectType] = useState('');
  const [targetObjectType, setTargetObjectType] = useState('');
  const [cardinality, setCardinality] = useState<CreateLinkTypeInput['cardinality']>('ONE_TO_MANY');

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    onSubmit({
      apiName,
      displayName,
      objectTypeApiName: sourceObjectType,
      linkedObjectTypeApiName: targetObjectType,
      cardinality,
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
          placeholder="employee-department"
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
          placeholder="Employee Department"
        />
      </div>

      <div className="flex flex-col">
        <label className={labelClass}>Source Object Type</label>
        <select
          value={sourceObjectType}
          onChange={(e) => setSourceObjectType(e.target.value)}
          required
          className={inputClass}
        >
          <option value="">Select source...</option>
          {objectTypes.map((ot) => (
            <option key={ot.rid} value={ot.rid}>
              {ot.displayName} ({ot.apiName})
            </option>
          ))}
        </select>
      </div>

      <div className="flex flex-col">
        <label className={labelClass}>Target Object Type</label>
        <select
          value={targetObjectType}
          onChange={(e) => setTargetObjectType(e.target.value)}
          required
          className={inputClass}
        >
          <option value="">Select target...</option>
          {objectTypes.map((ot) => (
            <option key={ot.rid} value={ot.rid}>
              {ot.displayName} ({ot.apiName})
            </option>
          ))}
        </select>
      </div>

      <div className="flex flex-col">
        <label className={labelClass}>Cardinality</label>
        <select
          value={cardinality}
          onChange={(e) => setCardinality(e.target.value as CreateLinkTypeInput['cardinality'])}
          className={inputClass}
        >
          <option value="ONE_TO_ONE">ONE_TO_ONE</option>
          <option value="ONE_TO_MANY">ONE_TO_MANY</option>
          <option value="MANY_TO_MANY">MANY_TO_MANY</option>
        </select>
      </div>

      <button
        type="submit"
        disabled={isLoading}
        className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80 disabled:opacity-50"
      >
        {isLoading ? 'Creating...' : 'Create Link Type'}
      </button>
    </form>
  );
}
