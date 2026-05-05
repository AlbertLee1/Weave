import { useEffect, useRef, useState } from 'react';
import {
  createWebsocketPresenceClient,
  type PresenceClient,
  type PresenceCursor,
  type PresencePeer,
} from '../lib/collabPresence';

export interface UseCollabPresenceOptions {
  /** A stable room key — multiple clients with the same key see each other. */
  roomId: string;
  user: { id: string; name: string; color?: string };
  /**
   * Disable the hook entirely (no client construction, returns empty peers).
   * Defaults to true when `wsUrl` is provided OR a custom client/factory is
   * supplied. Without `wsUrl`, the hook is a no-op so jsdom tests and
   * single-machine dev loops don't try to open a socket against a missing
   * y-websocket server.
   */
  enabled?: boolean;
  /**
   * Inject a pre-built client (overrides factory + wsUrl). Tests use this
   * with `MockPresenceClient` instances so two consumers can share state
   * inside the same jsdom process.
   */
  client?: PresenceClient | null;
  /** Custom client factory — most tests use `client` directly. */
  factory?: (opts: { roomId: string; user: { id: string; name: string; color?: string } }) => PresenceClient;
  /** Resolved at runtime from `import.meta.env.VITE_COLLAB_WS_URL`. */
  wsUrl?: string;
}

export interface UseCollabPresenceResult {
  peers: PresencePeer[];
  setCursor: (cursor: PresenceCursor | null) => void;
}

function readWsUrl(): string | undefined {
  // import.meta.env may be undefined under non-Vite test runners; guard.
  try {
    const env = import.meta.env;
    if (env && typeof env.VITE_COLLAB_WS_URL === 'string') {
      const trimmed = env.VITE_COLLAB_WS_URL.trim();
      return trimmed === '' ? undefined : trimmed;
    }
  } catch {
    // ignore
  }
  return undefined;
}

export function useCollabPresence(opts: UseCollabPresenceOptions): UseCollabPresenceResult {
  const wsUrl = opts.wsUrl ?? readWsUrl();
  const explicitClient = opts.client ?? null;
  const enabled = opts.enabled ?? (Boolean(explicitClient) || Boolean(opts.factory) || Boolean(wsUrl));
  const [peers, setPeers] = useState<PresencePeer[]>([]);
  // Reset peers synchronously when the hook flips off (e.g. presence disabled
  // mid-session) — uses the render-phase comparison pattern to avoid the
  // react-hooks/set-state-in-effect rule.
  const [prevEnabled, setPrevEnabled] = useState(enabled);
  if (prevEnabled !== enabled) {
    setPrevEnabled(enabled);
    if (!enabled && peers.length > 0) setPeers([]);
  }
  const clientRef = useRef<PresenceClient | null>(null);
  // Track whether we own (and therefore must destroy) the client. Caller-
  // supplied clients survive the hook's lifecycle so two components can share
  // one transport.
  const ownsClientRef = useRef(false);

  const userKey = `${opts.user.id}|${opts.user.name}|${opts.user.color ?? ''}`;
  const factoryRef = useRef(opts.factory);
  useEffect(() => {
    factoryRef.current = opts.factory;
  }, [opts.factory]);

  useEffect(() => {
    if (!enabled) {
      clientRef.current = null;
      ownsClientRef.current = false;
      return undefined;
    }
    let client: PresenceClient | null = explicitClient;
    let owns = false;
    if (!client) {
      const factory = factoryRef.current;
      if (factory) {
        client = factory({ roomId: opts.roomId, user: opts.user });
        owns = true;
      } else if (wsUrl) {
        client = createWebsocketPresenceClient({
          wsUrl,
          roomId: opts.roomId,
          user: opts.user,
        });
        owns = true;
      }
    }
    clientRef.current = client;
    ownsClientRef.current = owns;
    if (!client) return undefined;
    const unsubscribe = client.subscribe((next) => {
      setPeers(next);
    });
    return () => {
      unsubscribe();
      if (owns) client.destroy();
      if (clientRef.current === client) {
        clientRef.current = null;
        ownsClientRef.current = false;
      }
    };
    // We intentionally key on roomId + user identity (stringified) and the
    // explicit client identity. `wsUrl` changes are absorbed because they
    // can only flip at startup-time configuration boundaries.
  }, [enabled, opts.roomId, userKey, explicitClient, wsUrl, opts.user]);

  const setCursor = (cursor: PresenceCursor | null) => {
    const client = clientRef.current;
    if (!client) return;
    client.setLocalState({ cursor });
  };

  return { peers, setCursor };
}
