import { createContext, useContext } from 'react';
import type { PresenceCursor, PresencePeer } from './collabPresence';

export interface CollabPresenceContextValue {
  peers: PresencePeer[];
  /** Publish (or clear) the local cursor position. Pass null on blur. */
  setCursor: (cursor: PresenceCursor | null) => void;
  /** Surface element registry — driven by the overlay to anchor peer cursors. */
  registerSurface: (el: HTMLElement | null) => void;
  surface: HTMLElement | null;
}

const noop = (): void => {};

export const CollabPresenceContext = createContext<CollabPresenceContextValue>({
  peers: [],
  setCursor: noop,
  registerSurface: noop,
  surface: null,
});

/**
 * useCollabCursorPublisher returns a stable callback that publishes the
 * caller's cursor + selection to the surrounding presence room. When no
 * provider is mounted (or presence is disabled) it returns a no-op so call
 * sites can opt-in unconditionally.
 */
export function useCollabCursorPublisher(): (cursor: PresenceCursor | null) => void {
  const ctx = useContext(CollabPresenceContext);
  return ctx.setCursor;
}

/**
 * useCollabSurfaceRef wires a container element to the provider so
 * `CollabCursorOverlay` knows which DOM region to project peer cursors over.
 */
export function useCollabSurfaceRef(): (el: HTMLElement | null) => void {
  return useContext(CollabPresenceContext).registerSurface;
}

/** Snapshot the connected peer list. Re-renders on awareness updates. */
export function useCollabPeers(): PresencePeer[] {
  return useContext(CollabPresenceContext).peers;
}
