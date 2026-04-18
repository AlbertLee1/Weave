import { authedFetch } from '../auth/interceptor';
import { useAuthStore } from '../auth/authStore';
import { ApiRequestError } from './client';
import type { ApiError } from './types';

// MediaAsset mirrors pkg/oms.MediaAsset: the catalog row returned by the
// upload endpoint.
export interface MediaAsset {
  rid: string;
  realm: string;
  filename?: string;
  mime: string;
  sizeBytes: number;
  sha256: string;
  path: string;
  createdBy?: string;
  createdAt: string;
}

// UploadProgress reports bytes transferred during an XHR upload.
export interface UploadProgress {
  loaded: number;
  total: number;
}

export interface UploadMediaOptions {
  realm?: string;
  onProgress?: (progress: UploadProgress) => void;
  signal?: AbortSignal;
}

/**
 * Upload a single file via POST /api/v2/media as multipart/form-data.
 * Uses XMLHttpRequest (rather than fetch) because the fetch API has no
 * standardized upload-progress event; XHR's `upload.onprogress` is the
 * only portable way to surface "bytes sent" to the UI.
 */
export function uploadMedia(
  file: File,
  options: UploadMediaOptions = {},
): Promise<MediaAsset> {
  return new Promise((resolve, reject) => {
    const form = new FormData();
    form.append('file', file, file.name);
    if (options.realm) {
      form.append('realm', options.realm);
    }

    const xhr = new XMLHttpRequest();
    xhr.open('POST', '/api/v2/media', true);
    xhr.withCredentials = true;

    const token = useAuthStore.getState().accessToken;
    if (token) {
      xhr.setRequestHeader('Authorization', `Bearer ${token}`);
    }

    if (options.onProgress) {
      xhr.upload.onprogress = (ev) => {
        if (ev.lengthComputable) {
          options.onProgress!({ loaded: ev.loaded, total: ev.total });
        }
      };
    }

    if (options.signal) {
      const abort = () => xhr.abort();
      if (options.signal.aborted) {
        xhr.abort();
        reject(new DOMException('Aborted', 'AbortError'));
        return;
      }
      options.signal.addEventListener('abort', abort, { once: true });
    }

    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        try {
          resolve(JSON.parse(xhr.responseText) as MediaAsset);
        } catch (e) {
          reject(e);
        }
        return;
      }
      let errorData: Partial<ApiError> = {};
      try {
        errorData = JSON.parse(xhr.responseText) as Partial<ApiError>;
      } catch {
        // fall through
      }
      reject(
        new ApiRequestError({
          errorCode: errorData.errorCode ?? 'UNKNOWN',
          errorName: errorData.errorName ?? xhr.statusText,
          errorInstanceId: errorData.errorInstanceId ?? '',
          parameters: errorData.parameters,
          statusCode: xhr.status,
        }),
      );
    };
    xhr.onerror = () =>
      reject(
        new ApiRequestError({
          errorCode: 'NetworkError',
          errorName: 'NetworkError',
          errorInstanceId: '',
          statusCode: 0,
        }),
      );
    xhr.onabort = () => reject(new DOMException('Aborted', 'AbortError'));

    xhr.send(form);
  });
}

/**
 * DELETE /api/v2/media/{rid}. Returns when the server accepts the delete;
 * the blob is reclaimed server-side iff no other row references the same
 * (realm, sha256).
 */
export async function deleteMedia(rid: string): Promise<void> {
  const res = await authedFetch(`/api/v2/media/${encodeURIComponent(rid)}`, {
    method: 'DELETE',
  });
  if (!res.ok) {
    let errorData: Partial<ApiError> = {};
    try {
      errorData = (await res.json()) as Partial<ApiError>;
    } catch {
      // empty body
    }
    throw new ApiRequestError({
      errorCode: errorData.errorCode ?? 'UNKNOWN',
      errorName: errorData.errorName ?? res.statusText,
      errorInstanceId: errorData.errorInstanceId ?? '',
      parameters: errorData.parameters,
      statusCode: res.status,
    });
  }
}

/** Download URL for an uploaded media asset. */
export function mediaDownloadUrl(rid: string): string {
  return `/api/v2/media/${encodeURIComponent(rid)}`;
}
