import { useQuery } from '@tanstack/react-query';
import {
  attachmentPropertyContentUrl,
  getAttachmentPropertyMetadata,
} from '../../api/attachments';

interface AttachmentLinkProps {
  ontologyApiName: string;
  objectType: string;
  primaryKey: string;
  property: string;
}

function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return '';
  const units = ['B', 'KB', 'MB', 'GB'];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i += 1;
  }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

// AttachmentLink renders a download link for the file held by an object's
// attachment-typed property. It resolves the filename/size via the
// metadata endpoint but the link itself works even if that fetch fails —
// the content URL is a plain href, mirroring the media download pattern.
export function AttachmentLink({
  ontologyApiName,
  objectType,
  primaryKey,
  property,
}: AttachmentLinkProps) {
  const meta = useQuery({
    queryKey: ['attachment-meta', ontologyApiName, objectType, primaryKey, property],
    queryFn: () =>
      getAttachmentPropertyMetadata(ontologyApiName, objectType, primaryKey, property),
    retry: false,
    staleTime: 60_000,
  });

  const href = attachmentPropertyContentUrl(
    ontologyApiName,
    objectType,
    primaryKey,
    property,
  );
  const filename = meta.data?.filename || 'Download';
  // sizeBytes is a SafeLong string on the wire; coerce before formatting.
  const size = meta.data ? formatBytes(Number(meta.data.sizeBytes)) : '';

  return (
    <div className="flex items-center gap-2">
      <a
        href={href}
        download={meta.data?.filename || undefined}
        data-testid={`attachment-download-${property}`}
        className="inline-flex items-center gap-1.5 rounded border border-border bg-bg-elevated px-2.5 py-1.5 text-xs text-accent-cyan hover:border-accent-cyan break-all"
      >
        <svg
          aria-hidden
          viewBox="0 0 16 16"
          className="h-3.5 w-3.5 flex-shrink-0 fill-current"
        >
          <path d="M8 1a.75.75 0 0 1 .75.75v6.69l1.72-1.72a.75.75 0 1 1 1.06 1.06l-3 3a.75.75 0 0 1-1.06 0l-3-3a.75.75 0 0 1 1.06-1.06l1.72 1.72V1.75A.75.75 0 0 1 8 1ZM3 12.5a.75.75 0 0 1 .75.75v.25h8.5v-.25a.75.75 0 0 1 1.5 0v.5A1.25 1.25 0 0 1 12.5 15h-9A1.25 1.25 0 0 1 2.25 13.75v-.5A.75.75 0 0 1 3 12.5Z" />
        </svg>
        {filename}
      </a>
      {size && <span className="text-[10px] font-mono text-text-secondary">{size}</span>}
    </div>
  );
}
