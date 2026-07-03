import { ApiRequestError, request } from './client';
import { authedFetch } from '../auth/interceptor';
import type { ApiError } from './types';

// AttachmentMetadata mirrors pkg/attachment store metadata (diskMeta) —
// the descriptor for a file held by an object's attachment-typed property.
//
// sizeBytes is a Foundry SafeLong, so the server serializes it as a decimal
// string (e.g. "1024"), not a JSON number. Parse with Number(...) before
// doing arithmetic.
export interface AttachmentMetadata {
  rid: string;
  filename: string;
  mediaType: string;
  sizeBytes: string;
  createdAt?: string;
}

function propertyBase(
  ontologyApiName: string,
  objectType: string,
  primaryKey: string,
  property: string,
): string {
  return (
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}` +
    `/objects/${encodeURIComponent(objectType)}` +
    `/${encodeURIComponent(primaryKey)}` +
    `/attachments/${encodeURIComponent(property)}`
  );
}

// getAttachmentPropertyMetadata reads the descriptor (filename / size /
// media type) of the attachment held by an object's attachment property.
// The server resolves the attachment RID from the stored property value.
export function getAttachmentPropertyMetadata(
  ontologyApiName: string,
  objectType: string,
  primaryKey: string,
  property: string,
): Promise<AttachmentMetadata> {
  return request<AttachmentMetadata>(
    'GET',
    propertyBase(ontologyApiName, objectType, primaryKey, property),
  );
}

// attachmentPropertyContentUrl is the raw download URL for the file held
// by an object's attachment property (mirrors mediaDownloadUrl — a plain
// URL usable as an <a href>).
export function attachmentPropertyContentUrl(
  ontologyApiName: string,
  objectType: string,
  primaryKey: string,
  property: string,
): string {
  return `${propertyBase(ontologyApiName, objectType, primaryKey, property)}/content`;
}

// uploadAttachment uploads a single file as a new attachment. Unlike media
// upload (`/api/v2/media`, multipart/form-data), the attachment endpoint
// takes the RAW file bytes as the request body with the filename as a query
// param — mirroring pkg/attachment.Handler.Upload:
//
//   POST /api/v2/ontologies/attachments/upload?filename=<name>
//   body: <raw file bytes>
//
// Returns the parsed AttachmentMetadata; callers forward `rid` as the action
// parameter value.
export async function uploadAttachment(file: File): Promise<AttachmentMetadata> {
  const url = `/api/v2/ontologies/attachments/upload?filename=${encodeURIComponent(
    file.name,
  )}`;
  const res = await authedFetch(url, { method: 'POST', body: file });

  if (!res.ok) {
    let errorData: Partial<ApiError> = {};
    try {
      errorData = (await res.json()) as Partial<ApiError>;
    } catch {
      // empty / non-JSON body
    }
    throw new ApiRequestError({
      errorCode: errorData.errorCode ?? 'UNKNOWN',
      errorName: errorData.errorName ?? res.statusText,
      errorInstanceId: errorData.errorInstanceId ?? '',
      parameters: errorData.parameters,
      statusCode: res.status,
    });
  }

  return (await res.json()) as AttachmentMetadata;
}
