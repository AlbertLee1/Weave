import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { LayersDragPanel } from './LayersDragPanel';

const layers = [
  { objectType: 'Airport', label: 'Airports' },
  { objectType: 'Flight', label: 'Flights' },
];

function makeDataTransfer() {
  const store: Record<string, string> = {};
  return {
    types: [] as string[],
    effectAllowed: 'none',
    dropEffect: 'none',
    setData(mime: string, value: string) {
      store[mime] = value;
      if (!this.types.includes(mime)) this.types.push(mime);
    },
    getData(mime: string) {
      return store[mime] ?? '';
    },
  };
}

describe('LayersDragPanel (VTX-104)', () => {
  it('renders one chip per layer', () => {
    render(
      <LayersDragPanel
        layers={layers}
        search={async () => []}
        onObjectsLoaded={() => {}}
      />,
    );
    expect(screen.getByTestId('vertex-layer-Airport')).toBeInTheDocument();
    expect(screen.getByTestId('vertex-layer-Flight')).toBeInTheDocument();
  });

  it('loads 50 objects when a layer is dropped on the canvas', async () => {
    const search = vi.fn(async (_t: string, pageSize: number) => {
      return Array.from({ length: pageSize }, (_, i) => ({
        id: `obj-${i}`,
        properties: {},
      }));
    });
    const onLoaded = vi.fn();
    render(<LayersDragPanel layers={layers} search={search} onObjectsLoaded={onLoaded} />);

    const dataTransfer = makeDataTransfer();
    fireEvent.dragStart(screen.getByTestId('vertex-layer-Airport'), { dataTransfer });
    fireEvent.dragOver(screen.getByTestId('vertex-graph-canvas'), { dataTransfer });
    fireEvent.drop(screen.getByTestId('vertex-graph-canvas'), { dataTransfer });

    await waitFor(() => {
      expect(search).toHaveBeenCalledWith('Airport', 50);
      expect(onLoaded).toHaveBeenCalledTimes(1);
    });
    const [objectType, objects] = onLoaded.mock.calls[0];
    expect(objectType).toBe('Airport');
    expect(objects).toHaveLength(50);
  });

  it('surfaces search errors on the canvas without crashing', async () => {
    const search = vi.fn(async () => {
      throw new Error('search failed');
    });
    render(<LayersDragPanel layers={layers} search={search} onObjectsLoaded={() => {}} />);

    const dataTransfer = makeDataTransfer();
    fireEvent.dragStart(screen.getByTestId('vertex-layer-Airport'), { dataTransfer });
    fireEvent.drop(screen.getByTestId('vertex-graph-canvas'), { dataTransfer });
    await waitFor(() => {
      expect(screen.getByTestId('vertex-canvas-error').textContent).toContain('search failed');
    });
  });
});
