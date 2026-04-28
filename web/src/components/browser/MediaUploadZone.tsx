import { useCallback, useState } from 'react';
import { useDropzone, type FileRejection } from 'react-dropzone';
import { Modal } from '../common/Modal';
import {
  mediaDownloadUrl,
  type MediaAsset,
  type UploadProgress,
} from '../../api/media';
import { useUploadMedia, useDeleteMedia } from '../../hooks/useMedia';

/**
 * DefaultMaxUploadBytes mirrors pkg/media.DefaultMaxUploadBytes (10 MiB)
 * — this gates the UI client-side; the server still enforces it authoritatively.
 */
export const DefaultMaxUploadBytes = 10 * 1024 * 1024;

export interface MediaUploadZoneProps {
  /** Property name this zone is bound to (shown as the section header). */
  propertyName: string;
  /** Stored media RIDs for this property. Multiple = array-typed property. */
  values: string[];
  /** Whether the bound property accepts many media items (isArray). */
  multiple?: boolean;
  /** Called with the updated RID list whenever uploads/deletes succeed. */
  onChange?: (values: string[]) => void;
  /** Optional realm forwarded to the upload API. */
  realm?: string;
  /** Per-file byte cap (defaults to 10 MiB, matching server). */
  maxBytes?: number;
  /** Optional assets map if parent already knows metadata (filename/mime/size)
   *  for existing RIDs — used for richer thumbnail captions. */
  knownAssets?: Record<string, MediaAsset>;
}

interface InFlightUpload {
  id: string;
  file: File;
  loaded: number;
  total: number;
  status: 'uploading' | 'error';
  errorMessage?: string;
  controller?: AbortController;
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

function isImageMime(mime?: string): boolean {
  return !!mime && mime.startsWith('image/');
}

export function MediaUploadZone({
  propertyName,
  values,
  multiple = false,
  onChange,
  realm,
  maxBytes = DefaultMaxUploadBytes,
  knownAssets,
}: MediaUploadZoneProps) {
  const uploadMutation = useUploadMedia();
  const deleteMutation = useDeleteMedia();
  const [uploads, setUploads] = useState<InFlightUpload[]>([]);
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);

  const patchUpload = useCallback(
    (id: string, patch: Partial<InFlightUpload>) => {
      setUploads((prev) =>
        prev.map((u) => (u.id === id ? { ...u, ...patch } : u)),
      );
    },
    [],
  );

  const dropHandler = useCallback(
    async (accepted: File[], rejections: FileRejection[]) => {
      if (rejections.length > 0) {
        // Surface the first rejection reason as a transient error tile; the
        // full react-dropzone error shape is enum + message.
        const first = rejections[0];
        const msg = first.errors[0]?.message ?? 'File rejected';
        const id = crypto.randomUUID();
        setUploads((prev) => [
          ...prev,
          {
            id,
            file: first.file,
            loaded: 0,
            total: first.file.size,
            status: 'error',
            errorMessage: msg,
          },
        ]);
      }
      if (accepted.length === 0) return;

      // Single-value properties only allow one active upload at a time; drop
      // extras on the floor rather than silently overwrite.
      const files = multiple ? accepted : accepted.slice(0, 1);

      for (const file of files) {
        const id = crypto.randomUUID();
        const controller = new AbortController();
        setUploads((prev) => [
          ...prev,
          {
            id,
            file,
            loaded: 0,
            total: file.size,
            status: 'uploading',
            controller,
          },
        ]);
        try {
          const asset = await uploadMutation.mutateAsync({
            file,
            realm,
            signal: controller.signal,
            onProgress: (p: UploadProgress) => {
              patchUpload(id, { loaded: p.loaded, total: p.total });
            },
          });
          setUploads((prev) => prev.filter((u) => u.id !== id));
          const next = multiple ? [...values, asset.rid] : [asset.rid];
          onChange?.(next);
        } catch (err) {
          // Cancelled uploads disappear silently; everything else stays as a
          // visible error tile so the user can see what failed.
          if (
            controller.signal.aborted ||
            (err instanceof DOMException && err.name === 'AbortError')
          ) {
            setUploads((prev) => prev.filter((u) => u.id !== id));
            continue;
          }
          patchUpload(id, {
            status: 'error',
            errorMessage: err instanceof Error ? err.message : 'Upload failed',
            controller: undefined,
          });
        }
      }
    },
    [multiple, onChange, patchUpload, realm, uploadMutation, values],
  );

  const { getRootProps, getInputProps, isDragActive, isDragReject } =
    useDropzone({
      onDrop: dropHandler,
      multiple,
      maxSize: maxBytes,
      noKeyboard: false,
    });

  const cancelUpload = useCallback((id: string) => {
    setUploads((prev) => {
      const target = prev.find((u) => u.id === id);
      target?.controller?.abort();
      // Optimistically drop the row; the catch branch in dropHandler will
      // also be a no-op once it observes the aborted signal.
      return prev.filter((u) => u.id !== id);
    });
  }, []);

  const handleConfirmDelete = useCallback(async () => {
    if (!confirmDelete) return;
    const rid = confirmDelete;
    try {
      await deleteMutation.mutateAsync(rid);
      onChange?.(values.filter((v) => v !== rid));
      setConfirmDelete(null);
    } catch {
      // Keep dialog open on error — the mutation's state surfaces the reason.
    }
  }, [confirmDelete, deleteMutation, onChange, values]);

  // Surface persistent error state from the mutations (e.g. 4xx from server).
  const uploadServerError =
    uploadMutation.isError && uploads.every((u) => u.status !== 'uploading')
      ? uploadMutation.error?.message
      : null;

  return (
    <section data-testid="media-upload-zone">
      <h3 className="text-xs font-sans font-medium text-text-secondary uppercase tracking-wider mb-3">
        {propertyName}
      </h3>

      {/* Existing media thumbnails */}
      {values.length > 0 && (
        <ul
          className="grid grid-cols-2 gap-2 mb-3"
          data-testid="media-thumbnails"
        >
          {values.map((rid) => {
            const asset = knownAssets?.[rid];
            const url = mediaDownloadUrl(rid);
            const showImage = isImageMime(asset?.mime);
            return (
              <li
                key={rid}
                className="relative rounded border border-border/60 bg-bg-elevated overflow-hidden group"
              >
                {showImage ? (
                  <img
                    src={url}
                    alt={asset?.filename ?? rid}
                    className="w-full h-24 object-cover"
                  />
                ) : (
                  <div className="flex items-center justify-center h-24 text-text-muted">
                    <svg
                      className="w-6 h-6"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2"
                    >
                      <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
                      <path d="M14 2v6h6" />
                    </svg>
                  </div>
                )}
                <div className="flex items-center justify-between px-2 py-1 text-[10px] font-mono text-text-secondary truncate">
                  <a
                    href={url}
                    target="_blank"
                    rel="noreferrer"
                    className="truncate hover:text-accent-cyan"
                    title={asset?.filename ?? rid}
                  >
                    {asset?.filename ?? rid}
                  </a>
                  <button
                    type="button"
                    onClick={() => setConfirmDelete(rid)}
                    className="ml-1 shrink-0 text-text-muted hover:text-accent-error transition-colors"
                    aria-label={`Delete ${asset?.filename ?? rid}`}
                  >
                    <svg
                      className="w-3.5 h-3.5"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2"
                    >
                      <path d="M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" />
                    </svg>
                  </button>
                </div>
              </li>
            );
          })}
        </ul>
      )}

      {/* Dropzone */}
      <div
        {...getRootProps({
          className: [
            'rounded border border-dashed px-4 py-6 text-center transition-colors cursor-pointer',
            isDragReject
              ? 'border-accent-error bg-accent-error/10'
              : isDragActive
                ? 'border-accent-cyan bg-accent-cyan/10'
                : 'border-border hover:border-accent-cyan/60',
          ].join(' '),
        })}
        data-testid="dropzone"
      >
        <input {...getInputProps()} data-testid="dropzone-input" />
        <p className="text-xs font-sans text-text-secondary">
          {isDragActive
            ? '松开以上传…'
            : `将文件拖到此处或点击选择 · 最大 ${formatBytes(maxBytes)}`}
        </p>
      </div>

      {/* In-flight upload list */}
      {uploads.length > 0 && (
        <ul className="mt-3 space-y-2" data-testid="upload-list">
          {uploads.map((u) => {
            const pct = u.total > 0 ? Math.round((u.loaded / u.total) * 100) : 0;
            return (
              <li
                key={u.id}
                className="rounded border border-border/60 bg-bg-elevated/60 px-3 py-2"
              >
                <div className="flex items-center justify-between text-xs font-sans text-text-primary">
                  <span className="truncate" title={u.file.name}>
                    {u.file.name}
                  </span>
                  <div className="flex items-center gap-2 shrink-0 ml-2">
                    <span className="text-text-muted">
                      {u.status === 'error' ? '失败' : `${pct}%`}
                    </span>
                    {u.status === 'uploading' && (
                      <button
                        type="button"
                        onClick={() => cancelUpload(u.id)}
                        className="text-text-muted hover:text-accent-error transition-colors"
                        aria-label={`Cancel ${u.file.name}`}
                      >
                        <svg
                          className="w-3.5 h-3.5"
                          viewBox="0 0 24 24"
                          fill="none"
                          stroke="currentColor"
                          strokeWidth="2"
                        >
                          <path d="M18 6L6 18M6 6l12 12" />
                        </svg>
                      </button>
                    )}
                  </div>
                </div>
                <div className="mt-1 h-1 bg-bg-tertiary rounded overflow-hidden">
                  <div
                    data-testid="progress-fill"
                    className={[
                      'h-full transition-all',
                      u.status === 'error'
                        ? 'bg-accent-error'
                        : 'bg-accent-cyan',
                    ].join(' ')}
                    style={{ width: `${u.status === 'error' ? 100 : pct}%` }}
                  />
                </div>
                {u.status === 'error' && u.errorMessage && (
                  <p className="mt-1 text-[10px] text-accent-error">
                    {u.errorMessage}
                  </p>
                )}
              </li>
            );
          })}
        </ul>
      )}

      {uploadServerError && (
        <p className="mt-2 text-[10px] text-accent-error" role="alert">
          {uploadServerError}
        </p>
      )}

      {/* Delete confirmation dialog */}
      <Modal
        open={!!confirmDelete}
        onClose={() => setConfirmDelete(null)}
        title="删除媒体文件？"
      >
        <p className="text-sm text-text-secondary mb-4">
          此操作不可撤销。文件引用将从该属性移除，若没有其他记录引用该文件，底层 blob 也会被回收。
        </p>
        <div className="flex justify-end gap-2">
          <button
            type="button"
            onClick={() => setConfirmDelete(null)}
            className="px-3 py-1.5 text-xs rounded border border-border text-text-primary hover:bg-bg-elevated transition-colors"
          >
            取消
          </button>
          <button
            type="button"
            onClick={handleConfirmDelete}
            disabled={deleteMutation.isPending}
            className="px-3 py-1.5 text-xs rounded bg-accent-error/80 text-white hover:bg-accent-error disabled:opacity-50 transition-colors"
          >
            {deleteMutation.isPending ? '删除中…' : '确认删除'}
          </button>
        </div>
      </Modal>
    </section>
  );
}
