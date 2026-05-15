// Diagramming-mode stub page (VTX-114). The full Diagramming surface is
// out of scope for v1 — this is the placeholder Vertex routes to when a
// user follows a link to /vertex/{rid}/diagramming. PRD acceptance:
// shows a "Coming soon" message + a back-to-Graph link.

import { useNavigate, useParams } from 'react-router';

export function DiagrammingStubPage() {
  const navigate = useNavigate();
  const { rid } = useParams();

  function back() {
    if (rid) navigate(`/vertex/${rid}`);
    else navigate('/vertex');
  }

  return (
    <main
      data-testid="vertex-diagramming-stub"
      className="mx-auto flex max-w-2xl flex-col items-start gap-4 p-6"
    >
      <h1 className="text-xl font-semibold">Diagramming · Coming soon</h1>
      <p className="text-sm text-zinc-600 dark:text-zinc-400">
        Diagramming mode is still in beta upstream. Vertex v1 ships with Graph
        mode only; the route is reserved so future links from external systems
        do not 404. Track progress in PRD VTX-114.
      </p>
      <button
        type="button"
        data-testid="vertex-diagramming-back"
        onClick={back}
        className="rounded bg-blue-600 px-3 py-1 text-sm text-white"
      >
        Back to Graph
      </button>
    </main>
  );
}
