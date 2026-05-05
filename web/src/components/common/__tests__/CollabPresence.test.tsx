import { describe, it, expect, beforeEach, vi } from 'vitest';
import { act } from 'react';
import { render, screen, fireEvent, cleanup } from '@testing-library/react';
import { CollabPresenceProvider } from '../CollabPresenceProvider';
import { CollabCursorOverlay } from '../CollabCursorOverlay';
import { InlineEditField } from '../InlineEditField';
import {
  MockPresenceClient,
  __resetMockPresenceForTests,
} from '../../../lib/collabPresence';
import {
  useCollabSurfaceRef,
  useCollabPeers,
} from '../../../lib/collabPresenceContext';

beforeEach(() => {
  __resetMockPresenceForTests();
  cleanup();
});

// Test surface that wires the registered surface ref to a position:relative
// container so the overlay anchors correctly. Mirrors the production wiring
// in ObjectDetail.tsx.
function TestSurface({ children }: { children: React.ReactNode }) {
  const registerSurface = useCollabSurfaceRef();
  return (
    <div
      data-testid="surface"
      style={{ position: 'relative' }}
      ref={registerSurface}
    >
      {children}
      <CollabCursorOverlay />
    </div>
  );
}

function PeerCount() {
  const peers = useCollabPeers();
  return <span data-testid="peer-count">{peers.length}</span>;
}

describe('CollabPresenceProvider', () => {
  it('a peer cursor surfaces to the overlay when published', () => {
    const local = new MockPresenceClient({
      roomId: 'room-1',
      user: { id: 'u-local', name: 'Me' },
    });
    const peer = new MockPresenceClient({
      roomId: 'room-1',
      user: { id: 'u-peer', name: 'Bob', color: '#22d3ee' },
    });

    render(
      <CollabPresenceProvider
        roomId="room-1"
        user={{ id: 'u-local', name: 'Me' }}
        client={local}
      >
        <TestSurface>
          <PeerCount />
          <input data-collab-field="name" defaultValue="Alice" readOnly />
        </TestSurface>
      </CollabPresenceProvider>,
    );

    expect(screen.getByTestId('peer-count')).toHaveTextContent('1');
    expect(screen.queryByTestId('collab-cursor-overlay')).not.toBeInTheDocument();

    act(() => {
      peer.setLocalState({ cursor: { field: 'name', selectionStart: 2, selectionEnd: 2 } });
    });

    const overlay = screen.getByTestId('collab-cursor-overlay');
    expect(overlay).toBeInTheDocument();
    const cursor = overlay.querySelector(`[data-testid^="collab-cursor-"]`) as HTMLElement;
    expect(cursor).toBeInTheDocument();
    expect(cursor.getAttribute('data-peer-name')).toBe('Bob');

    local.destroy();
    peer.destroy();
  });

  it('does not render an overlay when there are no peers', () => {
    const local = new MockPresenceClient({
      roomId: 'room-2',
      user: { id: 'u-local', name: 'Me' },
    });
    render(
      <CollabPresenceProvider
        roomId="room-2"
        user={{ id: 'u-local', name: 'Me' }}
        client={local}
      >
        <TestSurface>
          <input data-collab-field="x" defaultValue="" readOnly />
        </TestSurface>
      </CollabPresenceProvider>,
    );
    expect(screen.queryByTestId('collab-cursor-overlay')).not.toBeInTheDocument();
    local.destroy();
  });

  it('peer-leaves removes the cursor from the overlay', () => {
    const local = new MockPresenceClient({
      roomId: 'room-3',
      user: { id: 'u-local', name: 'Me' },
    });
    const peer = new MockPresenceClient({
      roomId: 'room-3',
      user: { id: 'u-peer', name: 'Carol' },
    });

    render(
      <CollabPresenceProvider
        roomId="room-3"
        user={{ id: 'u-local', name: 'Me' }}
        client={local}
      >
        <TestSurface>
          <PeerCount />
          <input data-collab-field="name" defaultValue="Alice" readOnly />
        </TestSurface>
      </CollabPresenceProvider>,
    );

    act(() => {
      peer.setLocalState({ cursor: { field: 'name', selectionStart: 0, selectionEnd: 0 } });
    });
    expect(screen.getByTestId('peer-count')).toHaveTextContent('1');
    expect(screen.getByTestId('collab-cursor-overlay')).toBeInTheDocument();

    act(() => {
      peer.destroy();
    });
    expect(screen.getByTestId('peer-count')).toHaveTextContent('0');
    expect(screen.queryByTestId('collab-cursor-overlay')).not.toBeInTheDocument();

    local.destroy();
  });

  it('InlineEditField publishes its caret position when collabFieldKey is set', () => {
    const local = new MockPresenceClient({
      roomId: 'room-4',
      user: { id: 'u-local', name: 'Me' },
    });
    const peer = new MockPresenceClient({
      roomId: 'room-4',
      user: { id: 'u-peer', name: 'Dave' },
    });

    render(
      <CollabPresenceProvider
        roomId="room-4"
        user={{ id: 'u-local', name: 'Me' }}
        client={local}
      >
        <TestSurface>
          <InlineEditField
            value="Alice"
            onSave={vi.fn()}
            collabFieldKey="name"
            testId="ief"
          />
        </TestSurface>
      </CollabPresenceProvider>,
    );

    fireEvent.click(screen.getByTestId('ief-display'));
    const input = screen.getByTestId('ief-input') as HTMLInputElement;
    // Move caret to position 3.
    input.setSelectionRange(3, 3);
    fireEvent.select(input);

    const peers = peer.getPeers();
    expect(peers).toHaveLength(1);
    expect(peers[0].cursor).toEqual({
      field: 'name',
      selectionStart: 3,
      selectionEnd: 3,
    });

    local.destroy();
    peer.destroy();
  });

  it('disables presence when no client / factory / wsUrl is supplied', () => {
    render(
      <CollabPresenceProvider
        roomId="room-5"
        user={{ id: 'u-local', name: 'Me' }}
      >
        <PeerCount />
      </CollabPresenceProvider>,
    );
    expect(screen.getByTestId('peer-count')).toHaveTextContent('0');
  });

  it('useCollabCursorPublisher is a no-op outside a provider', () => {
    // No provider wrapping — still renders without throwing, and the
    // InlineEditField stays interactive.
    render(
      <InlineEditField
        value="Alice"
        onSave={vi.fn()}
        collabFieldKey="name"
        testId="bare"
      />,
    );
    fireEvent.click(screen.getByTestId('bare-display'));
    const input = screen.getByTestId('bare-input') as HTMLInputElement;
    fireEvent.change(input, { target: { value: 'Bob' } });
    expect(input.value).toBe('Bob');
  });
});
