import { useParams } from 'react-router';

interface AdminPlaceholderPageProps {
  section: string;
  description: string;
}

export function AdminPlaceholderPage({
  section,
  description,
}: AdminPlaceholderPageProps) {
  const { ontology } = useParams<{ ontology: string }>();

  return (
    <div className="flex flex-col h-[calc(100vh-3rem)] bg-bg-primary overflow-y-auto">
      <header
        className="px-6 py-4 border-b flex flex-wrap items-center gap-4"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <h1 className="text-base font-semibold tracking-wide text-text-primary">
          Ontology Manager — {section}
        </h1>
        {ontology && (
          <span className="text-xs text-text-secondary uppercase tracking-widest">
            {ontology}
          </span>
        )}
      </header>
      <div className="flex flex-1 items-center justify-center px-6">
        <div className="max-w-md text-center">
          <div className="text-xs uppercase tracking-widest text-accent-cyan mb-2">
            Coming soon
          </div>
          <p className="text-sm text-text-secondary">{description}</p>
        </div>
      </div>
    </div>
  );
}

export function SchemaGraphPage() {
  return (
    <AdminPlaceholderPage
      section="Schema Graph"
      description="A force-directed schema graph will be available here — visualize all object types and link relationships with zoom, pan, and filtering."
    />
  );
}

export function AuditHistoryPage() {
  return (
    <AdminPlaceholderPage
      section="Audit History"
      description="A timeline of ontology metadata changes will be available here — actor, action, before/after diff, and filtering by entity type or date range."
    />
  );
}
