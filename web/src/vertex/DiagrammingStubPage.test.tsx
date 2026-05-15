import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router';
import { DiagrammingStubPage } from './DiagrammingStubPage';

const navigateMock = vi.fn();
vi.mock('react-router', async () => {
  const actual = await vi.importActual<typeof import('react-router')>('react-router');
  return { ...actual, useNavigate: () => navigateMock };
});

function renderAt(rid: string) {
  navigateMock.mockReset();
  return render(
    <MemoryRouter initialEntries={[`/vertex/${rid}/diagramming`]}>
      <Routes>
        <Route path="/vertex/:rid/diagramming" element={<DiagrammingStubPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('DiagrammingStubPage (VTX-114)', () => {
  it('renders the "Coming soon" placeholder', () => {
    renderAt('ri.vertex.main.graph.x');
    expect(screen.getByTestId('vertex-diagramming-stub').textContent).toContain('Coming soon');
  });

  it('Back to Graph navigates to /vertex/<rid>', () => {
    renderAt('ri.vertex.main.graph.x');
    fireEvent.click(screen.getByTestId('vertex-diagramming-back'));
    expect(navigateMock).toHaveBeenCalledWith('/vertex/ri.vertex.main.graph.x');
  });
});
