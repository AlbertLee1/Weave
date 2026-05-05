// Yjs-backed collaborative presence client (US-450).
//
// Wraps `y-websocket`'s `WebsocketProvider` and the awareness protocol from
// `y-protocols/awareness` behind a narrow `PresenceClient` shape so the React
// hook (`useCollabPresence`) and overlay (`CollabCursorOverlay`) can drive
// presence without importing yjs directly. Tests inject `MockPresenceClient`
// instead — the same wire surface, no sockets, no global mutations.

import * as Y from 'yjs';
import { WebsocketProvider } from 'y-websocket';
import { Awareness } from 'y-protocols/awareness';

export interface PresenceCursor {
  /** dom key of the focused field (matches `data-collab-field` attribute) */
  field: string;
  /** caret position within the field's value, in code-units */
  selectionStart: number;
  /** selection anchor, equals selectionStart for a collapsed caret */
  selectionEnd: number;
}

export interface PresenceUserMeta {
  id: string;
  name: string;
  color: string;
}

export interface PresenceState {
  user: PresenceUserMeta;
  cursor: PresenceCursor | null;
  updatedAt: number;
}

export interface PresencePeer extends PresenceState {
  clientID: number;
}

export type PresenceListener = (peers: PresencePeer[]) => void;

export interface PresenceClient {
  /** Replace local user metadata + cursor; partial fields are merged. */
  setLocalState(partial: Partial<Omit<PresenceState, 'updatedAt'>>): void;
  /** Snapshot peer list (excludes the local client). */
  getPeers(): PresencePeer[];
  /** Subscribe to peer updates; returns an unsubscribe function. */
  subscribe(listener: PresenceListener): () => void;
  /** Tear down the underlying transport. Idempotent. */
  destroy(): void;
}

// 8-color palette tuned for the dark theme used by the SPA — same hue family
// as the Quiver chart palette so peer cursors stay legible against bg-tertiary.
export const PRESENCE_PALETTE = [
  '#22d3ee',
  '#a78bfa',
  '#f472b6',
  '#facc15',
  '#34d399',
  '#fb923c',
  '#60a5fa',
  '#f87171',
] as const;

export function pickPresenceColor(seed: string): string {
  if (!seed) return PRESENCE_PALETTE[0];
  let h = 0;
  for (let i = 0; i < seed.length; i += 1) {
    h = ((h << 5) - h + seed.charCodeAt(i)) | 0;
  }
  return PRESENCE_PALETTE[Math.abs(h) % PRESENCE_PALETTE.length];
}

function isPresenceState(value: unknown): value is PresenceState {
  if (!value || typeof value !== 'object') return false;
  const v = value as Record<string, unknown>;
  if (!v.user || typeof v.user !== 'object') return false;
  const u = v.user as Record<string, unknown>;
  return typeof u.id === 'string' && typeof u.name === 'string' && typeof u.color === 'string';
}

function collectPeers(awareness: Awareness, excludeClientID: number): PresencePeer[] {
  const peers: PresencePeer[] = [];
  awareness.getStates().forEach((state, clientID) => {
    if (clientID === excludeClientID) return;
    if (!isPresenceState(state)) return;
    peers.push({ clientID, ...state });
  });
  // Stable order so React keys don't churn between awareness re-emits.
  peers.sort((a, b) => a.clientID - b.clientID);
  return peers;
}

export interface WebsocketPresenceOptions {
  wsUrl: string;
  roomId: string;
  user: { id: string; name: string; color?: string };
}

class WebsocketPresenceClient implements PresenceClient {
  private readonly doc: Y.Doc;
  private readonly provider: WebsocketProvider;
  private readonly awareness: Awareness;
  private readonly listeners = new Set<PresenceListener>();
  private localState: PresenceState;
  private destroyed = false;

  constructor(opts: WebsocketPresenceOptions) {
    this.doc = new Y.Doc();
    this.provider = new WebsocketProvider(opts.wsUrl, opts.roomId, this.doc, {
      connect: true,
    });
    this.awareness = this.provider.awareness;
    const color = opts.user.color ?? pickPresenceColor(opts.user.id);
    this.localState = {
      user: { id: opts.user.id, name: opts.user.name, color },
      cursor: null,
      updatedAt: Date.now(),
    };
    this.awareness.setLocalState(this.localState);
    this.awareness.on('change', this.handleChange);
  }

  private handleChange = (): void => {
    if (this.destroyed) return;
    const peers = collectPeers(this.awareness, this.awareness.clientID);
    this.listeners.forEach((cb) => {
      cb(peers);
    });
  };

  setLocalState(partial: Partial<Omit<PresenceState, 'updatedAt'>>): void {
    if (this.destroyed) return;
    this.localState = {
      ...this.localState,
      ...partial,
      user: { ...this.localState.user, ...(partial.user ?? {}) },
      cursor: partial.cursor === undefined ? this.localState.cursor : partial.cursor,
      updatedAt: Date.now(),
    };
    this.awareness.setLocalState(this.localState);
  }

  getPeers(): PresencePeer[] {
    if (this.destroyed) return [];
    return collectPeers(this.awareness, this.awareness.clientID);
  }

  subscribe(listener: PresenceListener): () => void {
    this.listeners.add(listener);
    listener(this.getPeers());
    return () => {
      this.listeners.delete(listener);
    };
  }

  destroy(): void {
    if (this.destroyed) return;
    this.destroyed = true;
    this.awareness.off('change', this.handleChange);
    try {
      this.provider.destroy();
    } catch {
      // y-websocket throws when destroying a never-connected provider in
      // jsdom; we don't care because the doc/listeners are already gone.
    }
    this.doc.destroy();
    this.listeners.clear();
  }
}

export function createWebsocketPresenceClient(opts: WebsocketPresenceOptions): PresenceClient {
  return new WebsocketPresenceClient(opts);
}

// MockPresenceClient — in-process fake suitable for tests + storybook. Two
// instances with the same `room` id share peer state via the room registry,
// emulating a y-websocket round-trip without opening a socket.
const mockRooms = new Map<string, Set<MockPresenceClient>>();

export interface MockPresenceOptions {
  roomId: string;
  user: { id: string; name: string; color?: string };
}

export class MockPresenceClient implements PresenceClient {
  private readonly roomId: string;
  private readonly listeners = new Set<PresenceListener>();
  private readonly clientID: number;
  private localState: PresenceState;
  private destroyed = false;

  constructor(opts: MockPresenceOptions) {
    this.roomId = opts.roomId;
    this.clientID = nextMockClientID();
    const color = opts.user.color ?? pickPresenceColor(opts.user.id);
    this.localState = {
      user: { id: opts.user.id, name: opts.user.name, color },
      cursor: null,
      updatedAt: Date.now(),
    };
    addToRoom(this.roomId, this);
    this.broadcast();
  }

  getClientID(): number {
    return this.clientID;
  }

  getLocalState(): PresenceState {
    return this.localState;
  }

  setLocalState(partial: Partial<Omit<PresenceState, 'updatedAt'>>): void {
    if (this.destroyed) return;
    this.localState = {
      ...this.localState,
      ...partial,
      user: { ...this.localState.user, ...(partial.user ?? {}) },
      cursor: partial.cursor === undefined ? this.localState.cursor : partial.cursor,
      updatedAt: Date.now(),
    };
    this.broadcast();
  }

  getPeers(): PresencePeer[] {
    if (this.destroyed) return [];
    const room = mockRooms.get(this.roomId);
    if (!room) return [];
    const peers: PresencePeer[] = [];
    room.forEach((peer) => {
      if (peer === this) return;
      peers.push({ clientID: peer.clientID, ...peer.localState });
    });
    peers.sort((a, b) => a.clientID - b.clientID);
    return peers;
  }

  subscribe(listener: PresenceListener): () => void {
    this.listeners.add(listener);
    listener(this.getPeers());
    return () => {
      this.listeners.delete(listener);
    };
  }

  destroy(): void {
    if (this.destroyed) return;
    this.destroyed = true;
    removeFromRoom(this.roomId, this);
    this.listeners.clear();
    this.broadcast();
  }

  private broadcast(): void {
    const room = mockRooms.get(this.roomId);
    if (!room) return;
    room.forEach((peer) => {
      if (peer.destroyed) return;
      peer.notify();
    });
  }

  private notify(): void {
    const peers = this.getPeers();
    this.listeners.forEach((cb) => {
      cb(peers);
    });
  }
}

let mockClientCounter = 1;
function nextMockClientID(): number {
  mockClientCounter += 1;
  return mockClientCounter;
}

function addToRoom(roomId: string, client: MockPresenceClient): void {
  let room = mockRooms.get(roomId);
  if (!room) {
    room = new Set();
    mockRooms.set(roomId, room);
  }
  room.add(client);
}

function removeFromRoom(roomId: string, client: MockPresenceClient): void {
  const room = mockRooms.get(roomId);
  if (!room) return;
  room.delete(client);
  if (room.size === 0) mockRooms.delete(roomId);
}

export function __resetMockPresenceForTests(): void {
  mockRooms.clear();
  mockClientCounter = 1;
}
