import { request } from './client';

// AttachmentMetadata mirrors pkg/attachment store metadata (diskMeta) —
// the descriptor for a file held by an object's attachment-typed property.
export interface AttachmentMetadata {
  rid: string;
  filename: string;
  mediaType: string;
  sizeBytes: number;
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
