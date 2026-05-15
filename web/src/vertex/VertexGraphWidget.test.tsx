import { describe, it, expect, vi } from 'vitest';
import { act, render, screen, waitFor, fireEvent } from '@testing-library/react';
import { VertexGraphWidget } from './VertexGraphWidget';

describe('VertexGraphWidget (VTX-105)', () => {
  it('renders the toolbar + canvas with the graph rid after loading', async () => {
    render(
      <VertexGraphWidget
        graphRid="ri.vertex.main.graph.alpha"
        loader={async () => ({ cameraZoom: 1 })}
      />,
    );
    await waitFor(() => {
      expect(screen.getByTestId('vertex-widget-canvas').textContent).toContain('graph ri.vertex.main.graph.alpha');
    });
    expect(screen.getByTestId('vertex-widget-toolbar')).toBeInTheDocument();
  });

  it('Save is disabled when no overrideGraphRid is configured', async () => {
    render(
      <VertexGraphWidget
        graphRid="ri.vertex.main.graph.alpha"
        loader={async () => ({})}
      />,
    );
    await waitFor(() => screen.getByTestId('vertex-widget-canvas'));
    expect((screen.getByTestId('vertex-widget-save') as HTMLButtonElement).disabled).toBe(true);
  });

  it('PATCHes the graph state to overrideGraphRid when Save is clicked', async () => {
    const saver = vi.fn(async () => {});
    const onSave = vi.fn();
    render(
      <VertexGraphWidget
        graphRid="ri.vertex.main.graph.alpha"
        selectedNodeRid="ri.vertex.main.node.42"
        overrideGraphRid="ri.vertex.main.graph.alpha-override"
        loader={async () => ({ cameraZoom: 1 })}
        saver={saver}
        onSave={onSave}
      />,
    );
    const btn = await screen.findByTestId('vertex-widget-save');
    await waitFor(() => {
      expect((btn as HTMLButtonElement).disabled).toBe(false);
    });
    fireEvent.click(btn);
    await waitFor(() => {
      expect(saver).toHaveBeenCalledWith(
        'ri.vertex.main.graph.alpha-override',
        expect.objectContaining({ selectedNodeRid: 'ri.vertex.main.node.42' }),
      );
    });
    expect(onSave).toHaveBeenCalled();
    expect(screen.getByTestId('vertex-widget-saved')).toBeInTheDocument();
  });

  it('VTX-120: Cmd+S saves the graph when an override RID is configured', async () => {
    const saver = vi.fn(async () => {});
    const onSave = vi.fn();
    render(
      <VertexGraphWidget
        graphRid="ri.vertex.main.graph.alpha"
        overrideGraphRid="ri.vertex.main.graph.alpha-override"
        loader={async () => ({ cameraZoom: 1 })}
        saver={saver}
        onSave={onSave}
      />,
    );
    await waitFor(() => {
      expect((screen.getByTestId('vertex-widget-save') as HTMLButtonElement).disabled).toBe(false);
    });
    act(() => {
      fireEvent.keyDown(document, { key: 's', code: 'KeyS', metaKey: true });
    });
    await waitFor(() => {
      expect(saver).toHaveBeenCalledWith(
        'ri.vertex.main.graph.alpha-override',
        expect.any(Object),
      );
    });
    expect(onSave).toHaveBeenCalled();
  });

  it('VTX-120: Cmd+S is a no-op when no override RID is configured', async () => {
    const saver = vi.fn(async () => {});
    render(
      <VertexGraphWidget
        graphRid="ri.vertex.main.graph.alpha"
        loader={async () => ({ cameraZoom: 1 })}
        saver={saver}
      />,
    );
    await waitFor(() => screen.getByTestId('vertex-widget-canvas'));
    act(() => {
      fireEvent.keyDown(document, { key: 's', code: 'KeyS', metaKey: true });
    });
    expect(saver).not.toHaveBeenCalled();
  });
});
