import type { ObjectSetDefinition } from '../api/types';

export const OBJECT_SET_URL_PARAM = 'def';

const KNOWN_DEFINITION_TYPES = new Set([
  'base',
  'static',
  'filter',
  'union',
  'intersect',
  'subtract',
  'searchAround',
  'reference',
  'withProperties',
  'nearestNeighbors',
  'asType',
  'asBaseObjectTypes',
  'interfaceBase',
  'interfaceLinkSearchAround',
  'methodInput',
]);

// Node test fallback only; browser path uses btoa/atob below.
declare const Buffer: {
  from(input: string, encoding: string): { toString(encoding: string): string };
};

function toBase64Url(input: string): string {
  // Encode UTF-8 -> base64 -> base64url (no +, /, =).
  const utf8 = new TextEncoder().encode(input);
  let binary = '';
  for (let i = 0; i < utf8.length; i += 1) {
    binary += String.fromCharCode(utf8[i]);
  }
  const b64 = typeof btoa === 'function' ? btoa(binary) : Buffer.from(binary, 'binary').toString('base64');
  return b64.replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

function fromBase64Url(input: string): string | null {
  if (!input) return null;
  const padded = input.replace(/-/g, '+').replace(/_/g, '/');
  const padLen = (4 - (padded.length % 4)) % 4;
  const standard = padded + '='.repeat(padLen);
  if (!/^[A-Za-z0-9+/=]*$/.test(standard)) return null;
  try {
    const binary = typeof atob === 'function' ? atob(standard) : Buffer.from(standard, 'base64').toString('binary');
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i += 1) {
      bytes[i] = binary.charCodeAt(i);
    }
    return new TextDecoder('utf-8', { fatal: false }).decode(bytes);
  } catch {
    return null;
  }
}

export function encodeDefinitionToParam(def: ObjectSetDefinition): string {
  return toBase64Url(JSON.stringify(def));
}

export function decodeDefinitionFromParam(param: string): ObjectSetDefinition | null {
  const decoded = fromBase64Url(param);
  if (decoded == null) return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(decoded);
  } catch {
    return null;
  }
  if (!parsed || typeof parsed !== 'object') return null;
  const type = (parsed as { type?: unknown }).type;
  if (typeof type !== 'string' || !KNOWN_DEFINITION_TYPES.has(type)) return null;
  return parsed as ObjectSetDefinition;
}

export function serializeDefinitionToSearch(def: ObjectSetDefinition): string {
  const params = new URLSearchParams();
  params.set(OBJECT_SET_URL_PARAM, encodeDefinitionToParam(def));
  return `?${params.toString()}`;
}

export function parseDefinitionFromSearch(search: string): ObjectSetDefinition | null {
  const trimmed = search.startsWith('?') ? search.slice(1) : search;
  if (!trimmed) return null;
  const params = new URLSearchParams(trimmed);
  const raw = params.get(OBJECT_SET_URL_PARAM);
  if (!raw) return null;
  return decodeDefinitionFromParam(raw);
}
