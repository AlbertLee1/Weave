import { useState } from 'react';
import { useNavigate } from 'react-router';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useOntologies } from '../../hooks/useOntologies';
import { useObjectTypes } from '../../hooks/useObjectTypes';
import { createOntology, type CreateOntologyInput } from '../../api/admin';
import type { Ontology } from '../../api/types';
import { Modal } from '../common/Modal';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { EmptyState } from '../common/EmptyState';
import { OntologyCard } from './OntologyCard';
import { StatsBar } from './StatsBar';

function OntologyCardWithCount({ ontology, onClick }: { ontology: Ontology; onClick: () => void }) {
  const { data: objectTypes } = useObjectTypes(ontology.apiName);
  return (
    <OntologyCard
      ontology={ontology}
      objectTypeCount={objectTypes?.length ?? 0}
      onClick={onClick}
    />
  );
}

export function DashboardPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { data: ontologies, isLoading, error } = useOntologies();

  const [modalOpen, setModalOpen] = useState(false);
  const [form, setForm] = useState<CreateOntologyInput>({
    apiName: '',
    displayName: '',
    description: '',
  });

  const mutation = useMutation({
    mutationFn: createOntology,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['ontologies'] });
      setModalOpen(false);
      setForm({ apiName: '', displayName: '', description: '' });
    },
  });

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!form.apiName.trim() || !form.displayName.trim()) return;
    mutation.mutate({
      apiName: form.apiName.trim(),
      displayName: form.displayName.trim(),
      description: form.description?.trim() || undefined,
    });
  }

  const totalObjectTypes = 0; // computed per-card via OntologyCardWithCount

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-96">
        <LoadingSpinner size="lg" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex items-center justify-center h-96">
        <p className="text-sm text-accent-error">
          Failed to load ontologies: {(error as Error).message}
        </p>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-bg-primary p-6">
      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-lg font-medium text-text-primary font-sans">Dashboard</h1>
          <p className="text-xs text-text-secondary mt-0.5">Manage your ontologies and data models</p>
        </div>
        <button
          onClick={() => setModalOpen(true)}
          className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium bg-accent-cyan/10 text-accent-cyan border border-accent-cyan/20 hover:bg-accent-cyan/20 transition-colors"
        >
          <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
            <path d="M12 5v14M5 12h14" />
          </svg>
          Create Ontology
        </button>
      </div>

      {/* Stats */}
      <div className="mb-6">
        <StatsBar
          ontologyCount={ontologies?.length ?? 0}
          objectTypeCount={totalObjectTypes}
        />
      </div>

      {/* Ontology Grid */}
      {!ontologies || ontologies.length === 0 ? (
        <EmptyState
          title="No ontologies yet"
          description="Create your first ontology to start defining your data model."
          action={
            <button
              onClick={() => setModalOpen(true)}
              className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium bg-accent-cyan/10 text-accent-cyan border border-accent-cyan/20 hover:bg-accent-cyan/20 transition-colors"
            >
              Create Ontology
            </button>
          }
        />
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {ontologies.map((ontology) => (
            <OntologyCardWithCount
              key={ontology.rid}
              ontology={ontology}
              onClick={() => navigate(`/explorer/${ontology.apiName}`)}
            />
          ))}
        </div>
      )}

      {/* Create Ontology Modal */}
      <Modal open={modalOpen} onClose={() => setModalOpen(false)} title="Create Ontology">
        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="block text-xs font-medium text-text-secondary mb-1.5 font-sans">
              API Name
            </label>
            <input
              type="text"
              value={form.apiName}
              onChange={(e) => setForm((f) => ({ ...f, apiName: e.target.value }))}
              placeholder="my-ontology"
              required
              className="w-full px-3 py-2 text-sm font-mono text-text-primary bg-bg-tertiary border border-border rounded-md placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent-cyan/50 focus:border-accent-cyan/40"
            />
          </div>

          <div>
            <label className="block text-xs font-medium text-text-secondary mb-1.5 font-sans">
              Display Name
            </label>
            <input
              type="text"
              value={form.displayName}
              onChange={(e) => setForm((f) => ({ ...f, displayName: e.target.value }))}
              placeholder="My Ontology"
              required
              className="w-full px-3 py-2 text-sm text-text-primary bg-bg-tertiary border border-border rounded-md placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent-cyan/50 focus:border-accent-cyan/40"
            />
          </div>

          <div>
            <label className="block text-xs font-medium text-text-secondary mb-1.5 font-sans">
              Description
            </label>
            <textarea
              value={form.description}
              onChange={(e) => setForm((f) => ({ ...f, description: e.target.value }))}
              placeholder="Optional description..."
              rows={3}
              className="w-full px-3 py-2 text-sm text-text-primary bg-bg-tertiary border border-border rounded-md placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent-cyan/50 focus:border-accent-cyan/40 resize-none"
            />
          </div>

          {mutation.isError && (
            <p className="text-xs text-accent-error">
              {(mutation.error as Error).message || 'Failed to create ontology'}
            </p>
          )}

          <div className="flex items-center justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={() => setModalOpen(false)}
              className="px-3 py-1.5 text-xs font-medium text-text-secondary hover:text-text-primary transition-colors"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={mutation.isPending || !form.apiName.trim() || !form.displayName.trim()}
              className="px-3 py-1.5 rounded-md text-xs font-medium bg-accent-cyan text-bg-primary hover:bg-accent-cyan/90 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
            >
              {mutation.isPending ? 'Creating...' : 'Create'}
            </button>
          </div>
        </form>
      </Modal>
    </div>
  );
}
