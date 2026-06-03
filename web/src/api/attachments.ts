import { authedFetch } from '../auth/interceptor';
import { ApiRequestError } from './client';
import type { ApiError } from './types';

// AttachmentMetadata mirrors pkg/attachment.Attachment (the wire-compatible
// Foundry AttachmentV2 record) returned by the global upload endpoint.
export interface AttachmentMetadata {
  rid: string;
  filename: string;
  sizeBytes: number;
  mediaType: string;
}

/**
 * Upload a single file as a new attachment.
 *
 * Unlike media upload (`/api/v2/media`, which is multipart/form-data), the
 * attachment endpoint takes the RAW file bytes as the request body and the
 * filename as a query parameter — mirroring pkg/attachment.Handler.Upload:
 *
 *   POST /api/v2/ontologies/attachments/upload?filename=<name>
 *   body: <raw file bytes>
 *
 * Returns the parsed AttachmentMetadata ({ rid, filename, mediaType,
 * sizeBytes }); the caller typically forwards `rid` as the action parameter
 * value.
 */
export async function uploadAttachment(file: File): Promise<AttachmentMetadata> {
  const url = `/api/v2/ontologies/attachments/upload?filename=${encodeURIComponent(
    file.name,
  )}`;
  const res = await authedFetch(url, {
    method: 'POST',
    body: file,
  });

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

/** Metadata URL for an uploaded attachment. */
export function attachmentMetadataUrl(rid: string): string {
  return `/api/v2/ontologies/attachments/${encodeURIComponent(rid)}`;
}

/** Content (download) URL for an uploaded attachment. */
export function attachmentContentUrl(rid: string): string {
  return `/api/v2/ontologies/attachments/${encodeURIComponent(rid)}/content`;
}
