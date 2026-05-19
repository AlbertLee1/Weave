// US-453: SPA-side wire calls for the admin compliance / GDPR report
// surfaces. The endpoints stream attachment-style binary bodies (HTML,
// PDF, ZIP), so we bypass the JSON-first `request<T>()` helper and go
// through `authedFetch` directly to obtain a Blob.
import { authedFetch } from '../auth/interceptor';
import { ApiRequestError } from './client';
import type { ApiError } from './types';

export type ComplianceReportFormat = 'json' | 'html' | 'pdf';

export interface ComplianceReportRequest {
  format: ComplianceReportFormat;
  // RFC3339 instant. Empty string omits the bound on the wire.
  from?: string;
  to?: string;
}

export interface ReportDownload {
  blob: Blob;
  filename: string;
}

const COMPLIANCE_PATH = '/api/admin/compliance/report';
const GDPR_EXPORT_PATH = '/api/admin/gdpr/export';

const DEFAULT_FILENAME: Record<ComplianceReportFormat, string> = {
  json: 'weave-compliance-report.json',
  html: 'weave-compliance-report.html',
  pdf: 'weave-compliance-report.pdf',
};

const MIME_BY_FORMAT: Record<ComplianceReportFormat, string> = {
  json: 'application/json;charset=utf-8',
  html: 'text/html;charset=utf-8',
  pdf: 'application/pdf',
};

export async function generateComplianceReport(
  req: ComplianceReportRequest,
): Promise<ReportDownload> {
  const body: Record<string, string> = { format: req.format };
  if (req.from) body.from = req.from;
  if (req.to) body.to = req.to;
  const response = await authedFetch(COMPLIANCE_PATH, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!response.ok) {
    throw await readApiError(response);
  }
  const filename =
    parseAttachmentFilename(response.headers.get('Content-Disposition')) ??
    DEFAULT_FILENAME[req.format];
  return {
    blob: await readAttachmentBlob(response, MIME_BY_FORMAT[req.format]),
    filename,
  };
}

export async function generateGDPRExport(
  userId: string,
): Promise<ReportDownload> {
  const response = await authedFetch(GDPR_EXPORT_PATH, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ userId }),
  });
  if (!response.ok) {
    throw await readApiError(response);
  }
  const filename =
    parseAttachmentFilename(response.headers.get('Content-Disposition')) ??
    `gdpr-export-${sanitiseFilenamePart(userId)}.zip`;
  return {
    blob: await readAttachmentBlob(response, 'application/zip'),
    filename,
  };
}

async function readAttachmentBlob(
  response: Response,
  fallbackType: string,
): Promise<Blob> {
  try {
    const blob = await response.blob();
    // The fetch API generally surfaces the upstream Content-Type. When it
    // does not (e.g. the test layer sends a plain JSON body), retype the
    // blob so the saved file opens correctly.
    return blob.type ? blob : new Blob([blob], { type: fallbackType });
  } catch (err) {
    try {
      const buffer = await response.arrayBuffer();
      return new Blob([buffer], {
        type: response.headers.get('Content-Type') || fallbackType,
      });
    } catch {
      throw err;
    }
  }
}

function parseAttachmentFilename(header: string | null): string | null {
  if (!header) return null;
  const params = parseContentDispositionParams(header);
  const encoded = params.get('filename*');
  if (encoded) {
    const decoded = decodeRFC5987Value(encoded);
    if (decoded) return decoded;
  }
  const fallback = params.get('filename')?.trim();
  return fallback && fallback.length > 0 ? fallback : null;
}

function parseContentDispositionParams(header: string): Map<string, string> {
  const params = new Map<string, string>();
  for (const part of splitDispositionHeader(header).slice(1)) {
    const eq = part.indexOf('=');
    if (eq < 1) continue;
    const key = part.slice(0, eq).trim().toLowerCase();
    const value = unquoteHeaderValue(part.slice(eq + 1).trim());
    if (key) params.set(key, value);
  }
  return params;
}

function splitDispositionHeader(header: string): string[] {
  const parts: string[] = [];
  let current = '';
  let inQuotes = false;
  let escaped = false;
  for (const ch of header) {
    if (escaped) {
      current += ch;
      escaped = false;
      continue;
    }
    if (inQuotes && ch === '\\') {
      current += ch;
      escaped = true;
      continue;
    }
    if (ch === '"') {
      inQuotes = !inQuotes;
      current += ch;
      continue;
    }
    if (ch === ';' && !inQuotes) {
      parts.push(current.trim());
      current = '';
      continue;
    }
    current += ch;
  }
  parts.push(current.trim());
  return parts;
}

function unquoteHeaderValue(value: string): string {
  if (value.length >= 2 && value.startsWith('"') && value.endsWith('"')) {
    return value.slice(1, -1).replace(/\\(.)/g, '$1');
  }
  return value;
}

function decodeRFC5987Value(value: string): string | null {
  const firstQuote = value.indexOf("'");
  const secondQuote = value.indexOf("'", firstQuote + 1);
  if (firstQuote <= 0 || secondQuote < 0) return null;
  const charset = value.slice(0, firstQuote).toLowerCase();
  if (charset !== 'utf-8' && charset !== 'utf8') return null;
  try {
    const decoded = decodeURIComponent(value.slice(secondQuote + 1));
    return decoded.trim().length > 0 ? decoded : null;
  } catch {
    return null;
  }
}

function sanitiseFilenamePart(s: string): string {
  const cleaned = s.replace(/[^a-zA-Z0-9_.-]/g, '_');
  return cleaned.length === 0 ? 'user' : cleaned;
}

async function readApiError(response: Response): Promise<ApiRequestError> {
  let payload: Partial<ApiError> = {};
  try {
    const text = await response.text();
    if (text) payload = JSON.parse(text);
  } catch {
    payload = {};
  }
  return new ApiRequestError({
    errorCode: payload.errorCode ?? 'UNKNOWN',
    errorName: payload.errorName ?? response.statusText,
    errorInstanceId: payload.errorInstanceId ?? '',
    parameters: payload.parameters,
    statusCode: response.status,
  });
}

export function triggerBlobDownload(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  URL.revokeObjectURL(url);
}
