import { useMemo, useState, type ReactNode } from 'react';
import { useCollabPresence } from '../../hooks/useCollabPresence';
import {
  CollabPresenceContext,
  type CollabPresenceContextValue,
} from '../../lib/collabPresenceContext';
import type { PresenceClient } from '../../lib/collabPresence';

export interface CollabPresenceProviderProps {
  /** Stable room identifier — same key across clients = same presence room. */
  roomId: string;
  user: { id: string; name: string; color?: string };
  children: ReactNode;
  /** Disable the provider (returns empty peers + no-op setCursor). */
  enabled?: boolean;
  /** Test injection — pass a `MockPresenceClient` instance. */
  client?: PresenceClient | null;
  factory?: (opts: { roomId: string; user: { id: string; name: string; color?: string } }) => PresenceClient;
}

export function CollabPresenceProvider({
  roomId,
  user,
  children,
  enabled,
  client,
  factory,
}: CollabPresenceProviderProps) {
  const { peers, setCursor } = useCollabPresence({
    roomId,
    user,
    enabled,
    client,
    factory,
  });
  const [surface, setSurface] = useState<HTMLElement | null>(null);
  const value = useMemo<CollabPresenceContextValue>(
    () => ({ peers, setCursor, registerSurface: setSurface, surface }),
    [peers, setCursor, surface],
  );
  return (
    <CollabPresenceContext.Provider value={value}>
      {children}
    </CollabPresenceContext.Provider>
  );
}
