import { useState, useEffect, useRef } from 'react';
import type { CreateObjectTypeInput } from '../../api/admin';
import { toApiName, toPluralName } from '../../utils/naming';

interface ObjectTypeFormProps {
  onSubmit: (values: CreateObjectTypeInput) => void;
  initialValues?: Partial<CreateObjectTypeInput>;
  isLoading: boolean;
}

export function ObjectTypeForm({ onSubmit, initialValues, isLoading }: ObjectTypeFormProps) {
  const [step, setStep] = useState(1);

  // Step 1 fields
  const [displayName, setDisplayName] = useState(initialValues?.displayName ?? '');
  const [apiName, setApiName] = useState(initialValues?.apiName ?? '');
  const [pluralDisplayName, setPluralDisplayName] = useState(initialValues?.pluralDisplayName ?? '');
  const [description, setDescription] = useState(initialValues?.description ?? '');

  // Step 2 fields
  const [primaryKey, setPrimaryKey] = useState(initialValues?.primaryKey ?? 'id');
  const [status, setStatus] = useState(initialValues?.status ?? 'ACTIVE');
  const [visibility, setVisibility] = useState(initialValues?.visibility ?? 'NORMAL');
  const [icon, setIcon] = useState(initialValues?.icon ?? '');
  const [color, setColor] = useState(initialValues?.color ?? '');

  // Track whether user has manually edited apiName / pluralDisplayName
  const apiNameManuallyEdited = useRef(false);
  const pluralManuallyEdited = useRef(false);

  useEffect(() => {
    if (!apiNameManuallyEdited.current) {
      setApiName(toApiName(displayName));
    }
    if (!pluralManuallyEdited.current) {
      setPluralDisplayName(toPluralName(displayName));
    }
  }, [displayName]);

  function handleApiNameChange(value: string) {
    apiNameManuallyEdited.current = true;
    setApiName(value);
  }

  function handlePluralChange(value: string) {
    pluralManuallyEdited.current = true;
    setPluralDisplayName(value);
  }

  function handleNext(e: React.FormEvent) {
    e.preventDefault();
    if (!displayName.trim()) return;
    setStep(2);
  }

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

  const steps = ['Basic Info', 'Settings'];

  return (
    <div className="flex flex-col gap-5">
      {/* Step indicator */}
      <div className="flex items-center gap-3">
        {steps.map((label, idx) => {
          const num = idx + 1;
          const isActive = step === num;
          const isDone = step > num;
          return (
            <div key={num} className="flex items-center gap-2">
              <div
                className={`w-6 h-6 rounded-full flex items-center justify-center text-xs font-semibold select-none ${
                  isActive
                    ? 'bg-accent-cyan text-bg-primary'
                    : isDone
                    ? 'bg-accent-cyan/40 text-accent-cyan'
                    : 'bg-bg-tertiary text-text-secondary border border-border'
                }`}
              >
                {num}
              </div>
              <span
                className={`text-xs font-sans ${
                  isActive ? 'text-accent-cyan font-medium' : 'text-text-secondary'
                }`}
              >
                {label}
              </span>
              {idx < steps.length - 1 && (
                <div className="w-8 h-px bg-border mx-1" />
              )}
            </div>
          );
        })}
      </div>

      {/* Step 1 */}
      {step === 1 && (
        <form onSubmit={handleNext} className="flex flex-col gap-4">
          <div className="flex flex-col">
            <label className={labelClass}>Display Name *</label>
            <input
              type="text"
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
              required
              autoFocus
              className={inputClass}
              placeholder="Employee"
            />
          </div>

          <div className="flex flex-col">
            <label className={labelClass}>API Name</label>
            <input
              type="text"
              value={apiName}
              onChange={(e) => handleApiNameChange(e.target.value)}
              className={inputClass}
              placeholder="employee"
            />
          </div>

          <div className="flex flex-col">
            <label className={labelClass}>Plural Display Name</label>
            <input
              type="text"
              value={pluralDisplayName}
              onChange={(e) => handlePluralChange(e.target.value)}
              className={inputClass}
              placeholder="Employees"
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

          <div className="flex justify-end">
            <button
              type="submit"
              className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80"
            >
              Next →
            </button>
          </div>
        </form>
      )}

      {/* Step 2 */}
      {step === 2 && (
        <form onSubmit={handleSubmit} className="flex flex-col gap-4">
          <div className="flex flex-col">
            <label className={labelClass}>Primary Key</label>
            <input
              type="text"
              value={primaryKey}
              onChange={(e) => setPrimaryKey(e.target.value)}
              className={inputClass}
              placeholder="id"
            />
            <span className="text-xs text-text-secondary mt-1">
              Unique identifier for each object. Defaults to &apos;id&apos;.
            </span>
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

          <div className="flex justify-between">
            <button
              type="button"
              onClick={() => setStep(1)}
              className="px-4 py-2 rounded text-sm font-medium text-text-secondary hover:text-text-primary border border-border hover:border-accent-cyan"
            >
              ← Back
            </button>
            <button
              type="submit"
              disabled={isLoading}
              className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80 disabled:opacity-50"
            >
              {isLoading ? 'Saving...' : 'Create'}
            </button>
          </div>
        </form>
      )}
    </div>
  );
}
