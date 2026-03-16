import { useState } from 'react';
import type { CreateOntologyInput } from '../../api/admin';

interface OntologyFormProps {
  onSubmit: (values: CreateOntologyInput) => void;
  initialValues?: Partial<CreateOntologyInput>;
  isLoading: boolean;
}

export function OntologyForm({ onSubmit, initialValues, isLoading }: OntologyFormProps) {
  const [apiName, setApiName] = useState(initialValues?.apiName ?? '');
  const [displayName, setDisplayName] = useState(initialValues?.displayName ?? '');
  const [description, setDescription] = useState(initialValues?.description ?? '');

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    onSubmit({
      apiName,
      displayName,
      description: description || undefined,
    });
  }

  return (
    <form onSubmit={handleSubmit} className="flex flex-col gap-4">
      <div className="flex flex-col">
        <label className="text-xs text-text-secondary font-sans mb-1">API Name</label>
        <input
          type="text"
          value={apiName}
          onChange={(e) => setApiName(e.target.value)}
          required
          className="bg-bg-tertiary border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-cyan focus:outline-none"
          placeholder="my-ontology"
        />
      </div>

      <div className="flex flex-col">
        <label className="text-xs text-text-secondary font-sans mb-1">Display Name</label>
        <input
          type="text"
          value={displayName}
          onChange={(e) => setDisplayName(e.target.value)}
          required
          className="bg-bg-tertiary border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-cyan focus:outline-none"
          placeholder="My Ontology"
        />
      </div>

      <div className="flex flex-col">
        <label className="text-xs text-text-secondary font-sans mb-1">Description</label>
        <textarea
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          rows={3}
          className="bg-bg-tertiary border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-cyan focus:outline-none resize-y"
          placeholder="Describe this ontology..."
        />
      </div>

      <button
        type="submit"
        disabled={isLoading}
        className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80 disabled:opacity-50"
      >
        {isLoading ? 'Creating...' : 'Create Ontology'}
      </button>
    </form>
  );
}
