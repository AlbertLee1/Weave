import { useNavigate } from 'react-router';
import { useTranslation } from 'react-i18next';
import { useOntologies } from '../../hooks/useOntologies';
import { useObjectTypes } from '../../hooks/useObjectTypes';
import type { Ontology } from '../../api/types';
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
  const { data: ontologies, isLoading, error } = useOntologies();
  const { t } = useTranslation();

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
          {t('dashboardPage.failedToLoad', { message: (error as Error).message })}
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
              {t('dashboard.eyebrow')}
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
              {t('dashboard.title')}
            </h1>

            <p
              className="text-sm text-text-secondary max-w-md leading-relaxed"
              style={{
                fontFamily: 'var(--font-sans)',
                fontWeight: 300,
                animation: 'fadeInUp 500ms 200ms ease-out both',
              }}
            >
              {t('dashboard.subtitle')}
            </p>
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
            {t('dashboardPage.sectionOntologies')}
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
          title={t('dashboardPage.emptyTitle')}
          description={t('dashboardPage.emptyDescription')}
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
    </div>
  );
}
