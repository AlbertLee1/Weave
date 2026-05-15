import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, act } from '@testing-library/react';

import { EMPTY_SELECTION, type SelectionState } from '../features/vertex/selections/selectionState';

// Module-scoped stubs the test cases mutate. Mirrors the pattern from
// VertexNodeOverlay.test.tsx.
type SigmaStub = {
  graphToViewport: (p: { x: number; y: number }) => { x: number; y: number };
  getDimensions: () => { width: number; height: number };
  getGraph: () => {
    nodes: () => string[];
    getNodeAttribute: (id: string, key: string) => unknown;
  };
  getContainer: () => HTMLElement;
};

interface SigmaPayload {
  event: { original: MouseEvent };
}

interface SigmaNodePayload extends SigmaPayload {
  node: string;
}

type EventHandlers = {
  clickNode?: (p: SigmaNodePayload) => void;
  clickStage?: (p: SigmaPayload) => void;
  downStage?: (p: SigmaPayload) => void;
  mousemovebody?: (p: { x: number; y: number; original: MouseEvent }) => void;
  mouseup?: (p: { x: number; y: number; original: MouseEvent }) => void;
};

let sigmaStub: SigmaStub;
let lastHandlers: EventHandlers | null;

vi.mock('@react-sigma/core', () => ({
  useSigma: () => sigmaStub,
  useRegisterEvents: () => (handlers: EventHandlers) => {
    lastHandlers = handlers;
  },
}));

import { VertexSelectionLayer } from './VertexSelectionLayer';

function buildSigma(positions: Record<string, { x: number; y: number }>): SigmaStub {
  const container = document.createElement('div');
  Object.defineProperty(container, 'getBoundingClientRect', {
    value: () => ({ left: 0, top: 0, width: 800, height: 600, right: 800, bottom: 600 }),
    configurable: true,
  });
  return {
    graphToViewport: (p) => ({ x: p.x, y: p.y }),
    getDimensions: () => ({ width: 800, height: 600 }),
    getGraph: () => ({
      nodes: () => Object.keys(positions),
      getNodeAttribute: (id, key) => {
        const node = positions[id];
        if (!node) return undefined;
        if (key === 'x') return node.x;
        if (key === 'y') return node.y;
        return undefined;
      },
    }),
    getContainer: () => container,
  };
}

function makeMouseEvent(opts: { shiftKey?: boolean; ctrlKey?: boolean; metaKey?: boolean; clientX?: number; clientY?: number } = {}): MouseEvent {
  return new MouseEvent('click', {
    bubbles: true,
    cancelable: true,
    shiftKey: opts.shiftKey ?? false,
    ctrlKey: opts.ctrlKey ?? false,
    metaKey: opts.metaKey ?? false,
    clientX: opts.clientX ?? 0,
    clientY: opts.clientY ?? 0,
  });
}

beforeEach(() => {
  lastHandlers = null;
});

describe('VTX-020 VertexSelectionLayer', () => {
  it('Given_userClicksNode_When_noModifier_Then_replacesSelectionWithThatRid', () => {
    sigmaStub = buildSigma({ 'ri.A': { x: 0, y: 0 } });
    const states: SelectionState[] = [];
    const onChange = (next: SelectionState) => states.push(next);

    render(
      <VertexSelectionLayer selection={EMPTY_SELECTION} onSelectionChange={onChange} />,
    );

    act(() => {
      lastHandlers?.clickNode?.({ node: 'ri.A', event: { original: makeMouseEvent() } });
    });

    expect(states).toHaveLength(1);
    expect(Array.from(states[0])).toEqual(['ri.A']);
  });

  it('Given_userCtrlClicksUnselectedNode_When_clickNode_Then_addsToSelection', () => {
    sigmaStub = buildSigma({ 'ri.A': { x: 0, y: 0 }, 'ri.B': { x: 0, y: 0 } });
    const states: SelectionState[] = [];
    const onChange = (next: SelectionState) => states.push(next);
    const start: SelectionState = new Set(['ri.A']);

    render(<VertexSelectionLayer selection={start} onSelectionChange={onChange} />);

    act(() => {
      lastHandlers?.clickNode?.({ node: 'ri.B', event: { original: makeMouseEvent({ ctrlKey: true }) } });
    });

    expect(Array.from(states[0]).sort()).toEqual(['ri.A', 'ri.B']);
  });

  it('Given_userMetaClicksSelectedNode_When_clickNode_Then_removesFromSelection', () => {
    sigmaStub = buildSigma({ 'ri.A': { x: 0, y: 0 }, 'ri.B': { x: 0, y: 0 } });
    const states: SelectionState[] = [];
    const onChange = (next: SelectionState) => states.push(next);
    const start: SelectionState = new Set(['ri.A', 'ri.B']);

    render(<VertexSelectionLayer selection={start} onSelectionChange={onChange} />);

    act(() => {
      lastHandlers?.clickNode?.({ node: 'ri.A', event: { original: makeMouseEvent({ metaKey: true }) } });
    });

    expect(Array.from(states[0])).toEqual(['ri.B']);
  });

  it('Given_userClicksEmptyStage_When_noShift_Then_clearsSelection', () => {
    sigmaStub = buildSigma({ 'ri.A': { x: 0, y: 0 } });
    const states: SelectionState[] = [];
    const onChange = (next: SelectionState) => states.push(next);
    const start: SelectionState = new Set(['ri.A']);

    render(<VertexSelectionLayer selection={start} onSelectionChange={onChange} />);

    act(() => {
      lastHandlers?.clickStage?.({ event: { original: makeMouseEvent() } });
    });

    expect(states).toHaveLength(1);
    expect(states[0].size).toBe(0);
  });

  it('Given_shiftDragOverNodes_When_releaseMouse_Then_selectionContainsNodesInRect', () => {
    // Nodes A (10,10), B (60,60) inside the box; C (120,120) outside.
    sigmaStub = buildSigma({
      'ri.A': { x: 10, y: 10 },
      'ri.B': { x: 60, y: 60 },
      'ri.C': { x: 120, y: 120 },
    });
    const states: SelectionState[] = [];
    const onChange = (next: SelectionState) => states.push(next);

    render(<VertexSelectionLayer selection={EMPTY_SELECTION} onSelectionChange={onChange} />);

    act(() => {
      // Shift held → start box-select at (0,0) and end at (80,80).
      lastHandlers?.downStage?.({ event: { original: makeMouseEvent({ shiftKey: true, clientX: 0, clientY: 0 }) } });
      lastHandlers?.mousemovebody?.({ x: 80, y: 80, original: makeMouseEvent({ shiftKey: true, clientX: 80, clientY: 80 }) });
      lastHandlers?.mouseup?.({ x: 80, y: 80, original: makeMouseEvent({ shiftKey: true, clientX: 80, clientY: 80 }) });
    });

    expect(states.length).toBeGreaterThanOrEqual(1);
    const final = states[states.length - 1];
    expect(final.has('ri.A')).toBe(true);
    expect(final.has('ri.B')).toBe(true);
    expect(final.has('ri.C')).toBe(false);
  });

  it('Given_shiftBoxSelectFires_When_renders_Then_overlayDivAppearsDuringDrag', () => {
    sigmaStub = buildSigma({ 'ri.A': { x: 50, y: 50 } });
    const { queryByTestId } = render(
      <VertexSelectionLayer selection={EMPTY_SELECTION} onSelectionChange={() => {}} />,
    );

    expect(queryByTestId('vertex-box-select-overlay')).not.toBeInTheDocument();

    act(() => {
      lastHandlers?.downStage?.({ event: { original: makeMouseEvent({ shiftKey: true, clientX: 0, clientY: 0 }) } });
      lastHandlers?.mousemovebody?.({ x: 100, y: 100, original: makeMouseEvent({ shiftKey: true, clientX: 100, clientY: 100 }) });
    });

    const overlay = queryByTestId('vertex-box-select-overlay');
    expect(overlay).toBeInTheDocument();

    act(() => {
      lastHandlers?.mouseup?.({ x: 100, y: 100, original: makeMouseEvent({ shiftKey: true, clientX: 100, clientY: 100 }) });
    });
    expect(queryByTestId('vertex-box-select-overlay')).not.toBeInTheDocument();
  });
});
