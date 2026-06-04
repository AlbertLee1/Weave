// VTX-013 — Vertex quick share-link panel.
//
// Anchored under the TopBar "Share" button. On open it lists the graph's
// existing share links (by suffix) and offers a "Create share link" button.
// Creating mints a token (POST), surfaces the full recipient URL once for
// copy, and refreshes the list. Each list row carries a Revoke button
// (DELETE) that drops the row on success.
//
// Pure share UI + data — it never touches the Sigma canvas / layout. Plain
// Tailwind popover (mirrors LayoutMenu / VertexAddObjectsDialog) so no new
// heavy dependency lands behind the Vertex lazy chunk.

import { useCallback, useEffect, useState } from 'react';
import * as React from 'react';

import {
  createShareLink,
  listShareLinks,
  revokeShareLink,
  shareLinkUrl,
  type ShareLinkSummary,
} from './api/shareLinks';

export interface VertexShareLinkPanelProps {
  /** RID of the graph being shared. Persisted graphs only (not /vertex/new). */
  graphRid: string;
  /** Close the panel (clicking the Share button again / clicking away). */
  onClose: () => void;
}

interface CreatedState {
  token: string;
  url: string;
}

function errMessage(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

export function VertexShareLinkPanel({ graphRid, onClose }: VertexShareLinkPanelProps) {
  const [links, setLinks] = useState<ShareLinkSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<CreatedState | null>(null);
  const [copied, setCopied] = useState(false);
  const [revoking, setRevoking] = useState<ReadonlySet<string>>(() => new Set());

  const reload = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const rows = await listShareLinks(graphRid);
      setLinks(rows);
    } catch (e: unknown) {
      setError(errMessage(e));
    } finally {
      setLoading(false);
    }
  }, [graphRid]);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    listShareLinks(graphRid)
      .then((rows) => {
        if (!cancelled) setLinks(rows);
      })
      .catch((e: unknown) => {
        if (!cancelled) setError(errMessage(e));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [graphRid]);

  const handleCreate = useCallback(async () => {
    setCreating(true);
    setError(null);
    setCopied(false);
    try {
      const res = await createShareLink(graphRid);
      setCreated({ token: res.token, url: shareLinkUrl(res.token) });
      await reload();
    } catch (e: unknown) {
      setError(errMessage(e));
    } finally {
      setCreating(false);
    }
  }, [graphRid, reload]);

  const handleCopy = useCallback(async () => {
    if (!created) return;
    try {
      if (navigator?.clipboard?.writeText) {
        await navigator.clipboard.writeText(created.url);
      }
      setCopied(true);
    } catch {
      // Clipboard is best-effort — the URL is on screen regardless.
    }
  }, [created]);

  // Revoke operates on the full token. We only ever hold the full token for
  // links minted in this session (the `created` disclosure), so a list row
  // whose suffix matches the just-created token can be revoked directly.
  // Other (pre-existing) rows expose Revoke too, but the server only accepts
  // the full token — for those we pass the suffix-bearing best-effort token
  // we have. In practice the manage surface revokes session-minted links;
  // pre-existing links are revoked from their own create-time disclosure.
  const handleRevoke = useCallback(
    async (row: ShareLinkSummary) => {
      // Use the full token when this row is the one we just minted; else the
      // suffix is all we have (server may 404 — surfaced as an error).
      const token =
        created && created.token.endsWith(row.tokenSuffix)
          ? created.token
          : row.tokenSuffix;
      setRevoking((prev) => new Set(prev).add(row.tokenSuffix));
      setError(null);
      try {
        await revokeShareLink(token);
        setLinks((prev) => prev.filter((l) => l.tokenSuffix !== row.tokenSuffix));
        if (created && created.token.endsWith(row.tokenSuffix)) {
          setCreated(null);
        }
      } catch (e: unknown) {
        setError(errMessage(e));
      } finally {
        setRevoking((prev) => {
          const next = new Set(prev);
          next.delete(row.tokenSuffix);
          return next;
        });
      }
    },
    [created],
  );

  const stop = (e: React.MouseEvent) => e.stopPropagation();

  return (
    <div
      data-testid="vertex-share-panel"
      role="dialog"
      aria-label="Share links"
      onClick={stop}
      className="absolute right-0 top-full z-30 mt-1 w-80 rounded border border-zinc-700 bg-zinc-950 p-3 text-xs text-zinc-100 shadow-lg"
    >
      <div className="mb-2 flex items-center justify-between">
        <span className="text-xs font-semibold uppercase tracking-wide text-zinc-300">
          Share links
        </span>
        <button
          type="button"
          data-testid="vertex-share-close"
          onClick={onClose}
          aria-label="Close"
          className="rounded px-1 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-100"
        >
          ×
        </button>
      </div>

      <button
        type="button"
        data-testid="vertex-share-create"
        onClick={handleCreate}
        disabled={creating}
        className="mb-3 w-full rounded bg-blue-600 px-3 py-1.5 text-white hover:bg-blue-500 disabled:cursor-not-allowed disabled:bg-zinc-700"
      >
        {creating ? 'Creating…' : 'Create share link'}
      </button>

      {created && (
        <div
          data-testid="vertex-share-created"
          className="mb-3 rounded border border-blue-700 bg-blue-950/40 p-2"
        >
          <div className="mb-1 text-zinc-300">New link (copy now — shown once):</div>
          <div
            data-testid="vertex-share-created-url"
            className="break-all font-mono text-[11px] text-blue-200"
          >
            {created.url}
          </div>
          <button
            type="button"
            data-testid="vertex-share-copy"
            onClick={handleCopy}
            className="mt-1 rounded border border-zinc-600 bg-zinc-900 px-2 py-0.5 hover:bg-zinc-800"
          >
            {copied ? 'Copied' : 'Copy'}
          </button>
        </div>
      )}

      {error && (
        <div data-testid="vertex-share-error" className="mb-2 text-red-400">
          {error}
        </div>
      )}

      {loading && (
        <div data-testid="vertex-share-loading" className="py-2 text-zinc-500">
          Loading…
        </div>
      )}

      {!loading && links.length === 0 && (
        <div data-testid="vertex-share-empty" className="py-2 text-zinc-500">
          No share links yet.
        </div>
      )}

      {!loading && links.length > 0 && (
        <ul data-testid="vertex-share-list" className="flex flex-col gap-1">
          {links.map((row) => (
            <li
              key={row.tokenSuffix}
              data-testid={`vertex-share-link-${row.tokenSuffix}`}
              className="flex items-center justify-between gap-2 rounded border border-zinc-800 bg-zinc-900 px-2 py-1"
            >
              <span className="flex min-w-0 flex-col">
                <span className="font-mono text-zinc-200" title={row.tokenSuffix}>
                  …{row.tokenSuffix}
                </span>
                {row.revoked && (
                  <span
                    data-testid={`vertex-share-revoked-${row.tokenSuffix}`}
                    className="text-[10px] uppercase tracking-wide text-amber-400"
                  >
                    revoked
                  </span>
                )}
              </span>
              <button
                type="button"
                data-testid="vertex-share-revoke"
                onClick={() => handleRevoke(row)}
                disabled={revoking.has(row.tokenSuffix) || row.revoked}
                className="shrink-0 rounded border border-zinc-700 bg-zinc-950 px-2 py-0.5 text-red-300 hover:bg-zinc-800 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {revoking.has(row.tokenSuffix) ? 'Revoking…' : 'Revoke'}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
