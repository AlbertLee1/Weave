import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import {
  MapOpenInVertexButton,
  buildVertexHref,
  type SelectedMapObject,
} from './MapOpenInVertexButton';

const sample: SelectedMapObject = {
  ontology: 'aviation',
  rid: 'ri.vertex.main.graph.aviation-map',
  objectId: 'JFK',
};

describe('MapOpenInVertexButton (VTX-107)', () => {
  it('builds an href that carries focus + 1-hop search around', () => {
    const href = buildVertexHref(sample);
    expect(href).toContain('/vertex/');
    expect(href).toContain('focus=JFK');
    expect(href).toContain('hops=1');
  });

  it('is disabled when no object is selected', () => {
    render(
      <MemoryRouter>
        <MapOpenInVertexButton selected={null} />
      </MemoryRouter>,
    );
    expect((screen.getByTestId('map-open-in-vertex') as HTMLButtonElement).disabled).toBe(true);
  });

  it('navigates to /vertex/<rid>?focus=&hops=1 when clicked', () => {
    const open = vi.fn();
    render(
      <MemoryRouter>
        <MapOpenInVertexButton selected={sample} onOpen={open} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByTestId('map-open-in-vertex'));
    expect(open).toHaveBeenCalledTimes(1);
    expect(open.mock.calls[0][0]).toBe(buildVertexHref(sample));
  });
});
