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

function OntologyCardWithCount({ ontology, onClick, index }: { ontology: Ontology; onClick: () => void; index: number }) {
  const { data: objectTypes } = useObjectTypes(ontology.apiName);
  return (
    <div style={{ animation: `fadeInUp 420ms ${80 + index * 60}ms ease-out both` }}>
      <OntologyCard
        ontology={ontology}
        objectTypeCount={objectTypes?.length ?? 0}
        onClick={onClick}
      />
    </div>
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

  const totalObjectTypes = 0;

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
    <div className="min-h-screen bg-bg-primary p-6 pb-16">

      {/* ── Hero Header ──────────────────────────────────────────── */}
      <div
        className="relative mb-8 rounded-2xl overflow-hidden px-8 py-10"
        style={{
          background: 'linear-gradient(135deg, rgba(13,17,23,0.95) 0%, rgba(22,27,38,0.9) 100%)',
          border: '1px solid rgba(245,158,11,0.15)',
          boxShadow: '0 0 60px rgba(245,158,11,0.05), inset 0 1px 0 rgba(245,158,11,0.08)',
          animation: 'fadeInUp 500ms ease-out both',
        }}
      >
        {/* Geometric grid pattern overlay */}
        <div
          className="absolute inset-0 pointer-events-none"
          style={{
            backgroundImage: `
              linear-gradient(rgba(245,158,11,0.04) 1px, transparent 1px),
              linear-gradient(90deg, rgba(245,158,11,0.04) 1px, transparent 1px)
            `,
            backgroundSize: '32px 32px',
          }}
        />
        {/* Radial glow top-right */}
        <div
          className="absolute top-0 right-0 w-64 h-64 pointer-events-none"
          style={{
            background: 'radial-gradient(circle at top right, rgba(245,158,11,0.1) 0%, transparent 60%)',
          }}
        />
        {/* Radial glow bottom-left */}
        <div
          className="absolute bottom-0 left-0 w-48 h-48 pointer-events-none"
          style={{
            background: 'radial-gradient(circle at bottom left, rgba(20,184,166,0.06) 0%, transparent 60%)',
          }}
        />

        <div className="relative flex items-end justify-between gap-6 flex-wrap">
          <div>
            {/* Eyebrow */}
            <div
              className="inline-flex items-center gap-2 mb-3 px-3 py-1 rounded-full text-xs font-medium"
              style={{
                background: 'rgba(20,184,166,0.1)',
                border: '1px solid rgba(20,184,166,0.25)',
                color: '#14B8A6',
                fontFamily: 'var(--font-mono)',
                animation: 'fadeInUp 500ms 60ms ease-out both',
              }}
            >
              <span
                className="w-1.5 h-1.5 rounded-full"
                style={{ background: '#14B8A6', boxShadow: '0 0 6px rgba(20,184,166,0.8)' }}
              />
              Ontology Layer Engine
            </div>

            {/* Main title */}
            <h1
              className="text-5xl font-bold tracking-tight leading-none mb-3"
              style={{
                fontFamily: 'var(--font-sans)',
                background: 'linear-gradient(135deg, #F59E0B 0%, #FCD34D 40%, #14B8A6 100%)',
                WebkitBackgroundClip: 'text',
                WebkitTextFillColor: 'transparent',
                backgroundClip: 'text',
                animation: 'fadeInUp 500ms 120ms ease-out both',
              }}
            >
              WEAVE
            </h1>

            <p
              className="text-sm text-text-secondary max-w-md leading-relaxed"
              style={{
                fontFamily: 'var(--font-sans)',
                fontWeight: 300,
                animation: 'fadeInUp 500ms 200ms ease-out both',
              }}
            >
              Define your data universe. Model objects, relationships, and actions in a unified ontology layer.
            </p>
          </div>

          {/* Create CTA */}
          <div style={{ animation: 'fadeInUp 500ms 280ms ease-out both' }}>
            <button
              onClick={() => setModalOpen(true)}
              className="inline-flex items-center gap-2 px-5 py-2.5 rounded-xl text-sm font-semibold transition-all duration-200 focus:outline-none"
              style={{
                fontFamily: 'var(--font-sans)',
                background: 'linear-gradient(135deg, #F59E0B 0%, #D97706 100%)',
                color: '#080B16',
                boxShadow: '0 4px 20px rgba(245,158,11,0.35), 0 1px 0 rgba(255,255,255,0.1) inset',
              }}
              onMouseEnter={(e) => {
                (e.currentTarget as HTMLButtonElement).style.boxShadow =
                  '0 6px 28px rgba(245,158,11,0.55), 0 1px 0 rgba(255,255,255,0.1) inset';
                (e.currentTarget as HTMLButtonElement).style.transform = 'translateY(-1px)';
              }}
              onMouseLeave={(e) => {
                (e.currentTarget as HTMLButtonElement).style.boxShadow =
                  '0 4px 20px rgba(245,158,11,0.35), 0 1px 0 rgba(255,255,255,0.1) inset';
                (e.currentTarget as HTMLButtonElement).style.transform = 'translateY(0)';
              }}
            >
              <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5">
                <path d="M12 5v14M5 12h14" />
              </svg>
              New Ontology
            </button>
          </div>
        </div>
      </div>

      {/* ── Stats Bar ─────────────────────────────────────────────── */}
      <div className="mb-8">
        <StatsBar
          ontologyCount={ontologies?.length ?? 0}
          objectTypeCount={totalObjectTypes}
        />
      </div>

      {/* ── Section heading ───────────────────────────────────────── */}
      <div
        className="flex items-center justify-between mb-4"
        style={{ animation: 'fadeInUp 400ms 300ms ease-out both' }}
      >
        <div className="flex items-center gap-3">
          <h2
            className="text-xs font-semibold uppercase tracking-widest text-text-secondary"
            style={{ fontFamily: 'var(--font-sans)' }}
          >
            Ontologies
          </h2>
          {ontologies && ontologies.length > 0 && (
            <span
              className="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium"
              style={{
                background: 'rgba(245,158,11,0.1)',
                border: '1px solid rgba(245,158,11,0.2)',
                color: '#F59E0B',
                fontFamily: 'var(--font-sans)',
              }}
            >
              {ontologies.length}
            </span>
          )}
        </div>
      </div>

      {/* ── Ontology Grid / Empty State ───────────────────────────── */}
      {!ontologies || ontologies.length === 0 ? (
        <EmptyState
          title="No ontologies yet"
          description="Create your first ontology to start defining your data model and object relationships."
          action={
            <button
              onClick={() => setModalOpen(true)}
              className="inline-flex items-center gap-2 px-5 py-2.5 rounded-xl text-sm font-semibold transition-all duration-200"
              style={{
                fontFamily: 'var(--font-sans)',
                background: 'linear-gradient(135deg, #F59E0B 0%, #D97706 100%)',
                color: '#080B16',
                boxShadow: '0 4px 20px rgba(245,158,11,0.3)',
              }}
              onMouseEnter={(e) => {
                (e.currentTarget as HTMLButtonElement).style.boxShadow =
                  '0 6px 28px rgba(245,158,11,0.5)';
              }}
              onMouseLeave={(e) => {
                (e.currentTarget as HTMLButtonElement).style.boxShadow =
                  '0 4px 20px rgba(245,158,11,0.3)';
              }}
            >
              <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5">
                <path d="M12 5v14M5 12h14" />
              </svg>
              Create your first ontology
            </button>
          }
        />
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {ontologies.map((ontology, i) => (
            <OntologyCardWithCount
              key={ontology.rid}
              ontology={ontology}
              index={i}
              onClick={() => navigate(`/explorer/${ontology.apiName}`)}
            />
          ))}
        </div>
      )}

      {/* ── Create Ontology Modal ─────────────────────────────────── */}
      <Modal open={modalOpen} onClose={() => setModalOpen(false)} title="New Ontology">
        <form onSubmit={handleSubmit} className="space-y-5">
          <div>
            <label
              className="block text-xs font-medium text-text-secondary mb-1.5"
              style={{ fontFamily: 'var(--font-sans)' }}
            >
              API Name
            </label>
            <input
              type="text"
              value={form.apiName}
              onChange={(e) => setForm((f) => ({ ...f, apiName: e.target.value }))}
              placeholder="my-ontology"
              required
              className="w-full px-3 py-2.5 text-sm text-text-primary bg-bg-tertiary border border-border rounded-lg placeholder:text-text-muted focus:outline-none transition-all"
              style={{ fontFamily: 'var(--font-mono)' }}
              onFocus={(e) => {
                e.currentTarget.style.borderColor = 'rgba(245,158,11,0.45)';
                e.currentTarget.style.boxShadow = '0 0 0 3px rgba(245,158,11,0.1)';
              }}
              onBlur={(e) => {
                e.currentTarget.style.borderColor = '';
                e.currentTarget.style.boxShadow = '';
              }}
            />
          </div>

          <div>
            <label
              className="block text-xs font-medium text-text-secondary mb-1.5"
              style={{ fontFamily: 'var(--font-sans)' }}
            >
              Display Name
            </label>
            <input
              type="text"
              value={form.displayName}
              onChange={(e) => setForm((f) => ({ ...f, displayName: e.target.value }))}
              placeholder="My Ontology"
              required
              className="w-full px-3 py-2.5 text-sm text-text-primary bg-bg-tertiary border border-border rounded-lg placeholder:text-text-muted focus:outline-none transition-all"
              style={{ fontFamily: 'var(--font-sans)' }}
              onFocus={(e) => {
                e.currentTarget.style.borderColor = 'rgba(245,158,11,0.45)';
                e.currentTarget.style.boxShadow = '0 0 0 3px rgba(245,158,11,0.1)';
              }}
              onBlur={(e) => {
                e.currentTarget.style.borderColor = '';
                e.currentTarget.style.boxShadow = '';
              }}
            />
          </div>

          <div>
            <label
              className="block text-xs font-medium text-text-secondary mb-1.5"
              style={{ fontFamily: 'var(--font-sans)' }}
            >
              Description
              <span className="ml-1 text-text-muted font-normal">(optional)</span>
            </label>
            <textarea
              value={form.description}
              onChange={(e) => setForm((f) => ({ ...f, description: e.target.value }))}
              placeholder="Describe what this ontology models..."
              rows={3}
              className="w-full px-3 py-2.5 text-sm text-text-primary bg-bg-tertiary border border-border rounded-lg placeholder:text-text-muted focus:outline-none transition-all resize-none leading-relaxed"
              style={{ fontFamily: 'var(--font-sans)' }}
              onFocus={(e) => {
                e.currentTarget.style.borderColor = 'rgba(245,158,11,0.45)';
                e.currentTarget.style.boxShadow = '0 0 0 3px rgba(245,158,11,0.1)';
              }}
              onBlur={(e) => {
                e.currentTarget.style.borderColor = '';
                e.currentTarget.style.boxShadow = '';
              }}
            />
          </div>

          {mutation.isError && (
            <p className="text-xs text-accent-error px-1">
              {(mutation.error as Error).message || 'Failed to create ontology'}
            </p>
          )}

          <div className="flex items-center justify-end gap-3 pt-1">
            <button
              type="button"
              onClick={() => setModalOpen(false)}
              className="px-4 py-2 text-sm font-medium text-text-secondary hover:text-text-primary transition-colors rounded-lg hover:bg-bg-elevated"
              style={{ fontFamily: 'var(--font-sans)' }}
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={mutation.isPending || !form.apiName.trim() || !form.displayName.trim()}
              className="inline-flex items-center gap-2 px-5 py-2 rounded-xl text-sm font-semibold transition-all duration-200 disabled:opacity-40 disabled:cursor-not-allowed"
              style={{
                fontFamily: 'var(--font-sans)',
                background: 'linear-gradient(135deg, #F59E0B 0%, #D97706 100%)',
                color: '#080B16',
                boxShadow: '0 3px 16px rgba(245,158,11,0.3)',
              }}
            >
              {mutation.isPending ? (
                <>
                  <span
                    className="w-3.5 h-3.5 rounded-full border-2 border-current border-t-transparent inline-block"
                    style={{ animation: 'spin 0.7s linear infinite' }}
                  />
                  Creating…
                </>
              ) : (
                'Create Ontology'
              )}
            </button>
          </div>
        </form>
      </Modal>

      <style>{`
        @keyframes spin { to { transform: rotate(360deg); } }
      `}</style>
    </div>
  );
}
