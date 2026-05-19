import { describe, it, expect } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Routes, Route, useLocation } from 'react-router';
import { VertexDiagrammingRedirect } from './VertexDiagrammingRedirect';

function WorkspaceSink() {
  const location = useLocation();
  return <div data-testid="vertex-workspace-route">{location.pathname}</div>;
}

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/vertex/:rid/diagramming" element={<VertexDiagrammingRedirect />} />
        <Route path="/vertex/:rid" element={<WorkspaceSink />} />
        <Route path="*" element={<div data-testid="not-found-route">Not found</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('VertexDiagrammingRedirect (SELF-461)', () => {
  it('Given /vertex/{rid}/diagramming When route matches Then redirects to the live workspace', async () => {
    renderAt('/vertex/ri.vertex.main.graph.x/diagramming');

    await waitFor(() => {
      expect(screen.getByTestId('vertex-workspace-route')).toHaveTextContent(
        '/vertex/ri.vertex.main.graph.x',
      );
    });

    expect(screen.queryByTestId('not-found-route')).not.toBeInTheDocument();
    expect(screen.queryByText(/coming soon/i)).not.toBeInTheDocument();
  });
});
