export interface OpenInQuiverInput {
  ontology: string;
  objectRid: string;
  property?: string;
  objectType?: string;
  primaryKey?: string;
}

export interface OpenInExplorerInput {
  ontology: string;
  objectRid: string;
  objectType?: string;
}

function requireNonEmpty(value: string, name: string): void {
  if (typeof value !== 'string' || value.length === 0) {
    throw new Error(`${name} is required`);
  }
}

function appendIfPresent(
  params: URLSearchParams,
  key: string,
  value: string | undefined,
): void {
  if (value !== undefined && value.length > 0) {
    params.set(key, value);
  }
}

export function buildOpenInQuiverUrl(input: OpenInQuiverInput): string {
  requireNonEmpty(input.ontology, 'ontology');
  requireNonEmpty(input.objectRid, 'objectRid');
  const params = new URLSearchParams();
  params.set('objectRid', input.objectRid);
  appendIfPresent(params, 'property', input.property);
  appendIfPresent(params, 'objectType', input.objectType);
  appendIfPresent(params, 'primaryKey', input.primaryKey);
  return `/quiver/${encodeURIComponent(input.ontology)}?${params.toString()}`;
}

export function buildOpenInExplorerUrl(input: OpenInExplorerInput): string {
  requireNonEmpty(input.ontology, 'ontology');
  requireNonEmpty(input.objectRid, 'objectRid');
  const params = new URLSearchParams();
  params.set('objectRid', input.objectRid);
  const path = input.objectType
    ? `/explorer/${encodeURIComponent(input.ontology)}/${encodeURIComponent(input.objectType)}`
    : `/explorer/${encodeURIComponent(input.ontology)}`;
  return `${path}?${params.toString()}`;
}

export function openInNewTab(
  url: string,
  win: Window | undefined = typeof window === 'undefined' ? undefined : window,
): void {
  requireNonEmpty(url, 'url');
  if (!win || typeof win.open !== 'function') return;
  win.open(url, '_blank', 'noopener,noreferrer');
}
