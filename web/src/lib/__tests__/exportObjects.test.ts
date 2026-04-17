import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
  escapeCsvField,
  toCsv,
  toJsonEnvelope,
  resolveExportColumns,
  fetchAllForExport,
  exportObjects,
  triggerDownload,
} from '../exportObjects';
import type { ObjectType, WireObject } from '../../api/types';

vi.mock('../../api/objects', () => ({
  listObjects: vi.fn(),
  searchObjects: vi.fn(),
}));

import { listObjects, searchObjects } from '../../api/objects';

describe('escapeCsvField', () => {
  it('returns empty string for null/undefined', () => {
    expect(escapeCsvField(null)).toBe('');
    expect(escapeCsvField(undefined)).toBe('');
  });

  it('passes simple values through', () => {
    expect(escapeCsvField('hello')).toBe('hello');
    expect(escapeCsvField(42)).toBe('42');
    expect(escapeCsvField(true)).toBe('true');
  });

  it('quotes values containing commas, quotes, or newlines', () => {
    expect(escapeCsvField('a,b')).toBe('"a,b"');
    expect(escapeCsvField('he said "hi"')).toBe('"he said ""hi"""');
    expect(escapeCsvField('line1\nline2')).toBe('"line1\nline2"');
  });

  it('stringifies objects and arrays', () => {
    expect(escapeCsvField({ a: 1 })).toBe('"{""a"":1}"');
    expect(escapeCsvField([1, 2])).toBe('"[1,2]"');
  });
});

describe('toCsv', () => {
  it('produces a header row and data rows separated by CRLF', () => {
    const rows: WireObject[] = [
      { __rid: 'r1', __primaryKey: '1', __apiName: 'Employee', id: '1', name: 'Alice' },
      { __rid: 'r2', __primaryKey: '2', __apiName: 'Employee', id: '2', name: 'Bob, Jr.' },
    ];
    const csv = toCsv(rows, ['id', 'name']);
    expect(csv).toBe('id,name\r\n1,Alice\r\n2,"Bob, Jr."');
  });

  it('produces header-only when rows are empty', () => {
    const csv = toCsv([], ['id', 'name']);
    expect(csv).toBe('id,name');
  });
});

describe('toJsonEnvelope', () => {
  it('wraps rows with metadata', () => {
    const rows: WireObject[] = [
      { __rid: 'r1', __primaryKey: '1', __apiName: 'Employee', id: '1' },
    ];
    const at = new Date('2026-04-18T12:00:00Z');
    const env = toJsonEnvelope(rows, 'Employee', at);
    expect(env).toEqual({
      data: rows,
      metadata: {
        objectType: 'Employee',
        exportedAt: '2026-04-18T12:00:00.000Z',
        count: 1,
      },
    });
  });
});

describe('resolveExportColumns', () => {
  it('returns property apiNames', () => {
    const ot: ObjectType = {
      rid: 'ri',
      apiName: 'Employee',
      displayName: 'Employee',
      primaryKey: 'id',
      status: 'ACTIVE',
      visibility: 'NORMAL',
      properties: {
        id: { dataType: { type: 'string' }, rid: 'ri.id' },
        name: { dataType: { type: 'string' }, rid: 'ri.name' },
      },
    };
    expect(resolveExportColumns(ot)).toEqual(['id', 'name']);
  });

  it('returns empty array for missing/undefined properties', () => {
    expect(resolveExportColumns(undefined)).toEqual([]);
  });
});

describe('fetchAllForExport', () => {
  beforeEach(() => {
    vi.mocked(listObjects).mockReset();
    vi.mocked(searchObjects).mockReset();
  });

  it('paginates through listObjects until no nextPageToken', async () => {
    vi.mocked(listObjects)
      .mockResolvedValueOnce({
        data: [{ __rid: 'r1', __primaryKey: '1', __apiName: 'Employee' }],
        nextPageToken: 'p2',
      })
      .mockResolvedValueOnce({
        data: [{ __rid: 'r2', __primaryKey: '2', __apiName: 'Employee' }],
        nextPageToken: 'p3',
      })
      .mockResolvedValueOnce({
        data: [{ __rid: 'r3', __primaryKey: '3', __apiName: 'Employee' }],
      });

    const progress: number[] = [];
    const rows = await fetchAllForExport(
      {
        ontologyApiName: 'ont',
        objectType: 'Employee',
        select: ['id'],
        hasActiveSearch: false,
      },
      (count) => progress.push(count),
    );

    expect(rows).toHaveLength(3);
    expect(listObjects).toHaveBeenCalledTimes(3);
    expect(searchObjects).not.toHaveBeenCalled();
    expect(progress).toEqual([1, 2, 3]);
    expect(vi.mocked(listObjects).mock.calls[0][0]).toMatchObject({
      pageToken: undefined,
    });
    expect(vi.mocked(listObjects).mock.calls[1][0]).toMatchObject({
      pageToken: 'p2',
    });
  });

  it('uses searchObjects when hasActiveSearch is true', async () => {
    vi.mocked(searchObjects).mockResolvedValueOnce({
      data: [{ __rid: 'r1', __primaryKey: '1', __apiName: 'Employee' }],
    });

    const rows = await fetchAllForExport({
      ontologyApiName: 'ont',
      objectType: 'Employee',
      select: ['id'],
      where: { type: 'eq', field: 'id', value: '1' },
      hasActiveSearch: true,
    });

    expect(rows).toHaveLength(1);
    expect(searchObjects).toHaveBeenCalledTimes(1);
    expect(listObjects).not.toHaveBeenCalled();
  });
});

describe('exportObjects (integration)', () => {
  let anchors: HTMLAnchorElement[] = [];
  let createdUrls: string[] = [];
  const createdObjectUrl = vi.fn((blob: Blob) => {
    const fakeUrl = `blob:fake-${createdUrls.length}`;
    createdUrls.push(fakeUrl);
    // Attach the blob to the url for later inspection
    (createdObjectUrl as unknown as { blobs: Blob[] }).blobs =
      (createdObjectUrl as unknown as { blobs?: Blob[] }).blobs ?? [];
    (createdObjectUrl as unknown as { blobs: Blob[] }).blobs.push(blob);
    return fakeUrl;
  });
  const revokeObjectUrl = vi.fn();

  beforeEach(() => {
    vi.mocked(listObjects).mockReset();
    vi.mocked(searchObjects).mockReset();
    anchors = [];
    createdUrls = [];
    (createdObjectUrl as unknown as { blobs: Blob[] }).blobs = [];

    vi.stubGlobal('URL', {
      createObjectURL: createdObjectUrl,
      revokeObjectURL: revokeObjectUrl,
    });

    const origCreate = document.createElement.bind(document);
    vi.spyOn(document, 'createElement').mockImplementation(
      (tagName: string) => {
        const el = origCreate(tagName) as HTMLAnchorElement;
        if (tagName === 'a') {
          anchors.push(el);
          el.click = vi.fn();
        }
        return el;
      },
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  const objectType: ObjectType = {
    rid: 'ri',
    apiName: 'Employee',
    displayName: 'Employee',
    primaryKey: 'id',
    status: 'ACTIVE',
    visibility: 'NORMAL',
    properties: {
      id: { dataType: { type: 'string' }, rid: 'ri.id' },
      name: { dataType: { type: 'string' }, rid: 'ri.name' },
    },
  };

  it('exports CSV with filename {apiName}-export.csv', async () => {
    vi.mocked(listObjects).mockResolvedValueOnce({
      data: [
        { __rid: 'r1', __primaryKey: '1', __apiName: 'Employee', id: '1', name: 'Alice' },
      ],
    });

    const result = await exportObjects(
      'csv',
      {
        ontologyApiName: 'ont',
        objectType: 'Employee',
        select: ['id', 'name'],
        hasActiveSearch: false,
      },
      objectType,
    );

    expect(result.filename).toBe('Employee-export.csv');
    expect(result.count).toBe(1);
    expect(anchors).toHaveLength(1);
    expect(anchors[0].download).toBe('Employee-export.csv');
  });

  it('exports JSON envelope with filename {apiName}-export.json', async () => {
    vi.mocked(listObjects).mockResolvedValueOnce({
      data: [
        { __rid: 'r1', __primaryKey: '1', __apiName: 'Employee', id: '1', name: 'Alice' },
      ],
    });

    const result = await exportObjects(
      'json',
      {
        ontologyApiName: 'ont',
        objectType: 'Employee',
        select: ['id', 'name'],
        hasActiveSearch: false,
      },
      objectType,
    );

    expect(result.filename).toBe('Employee-export.json');
    expect(result.count).toBe(1);
    expect(anchors[0].download).toBe('Employee-export.json');

    const blobs = (createdObjectUrl as unknown as { blobs: Blob[] }).blobs;
    expect(blobs).toHaveLength(1);
    const text = await blobs[0].text();
    const parsed = JSON.parse(text);
    expect(parsed.data).toHaveLength(1);
    expect(parsed.metadata.objectType).toBe('Employee');
    expect(parsed.metadata.count).toBe(1);
    expect(typeof parsed.metadata.exportedAt).toBe('string');
  });
});

describe('triggerDownload', () => {
  it('creates blob URL, anchor, and revokes URL', () => {
    const anchors: HTMLAnchorElement[] = [];
    const createdObjectUrl = vi.fn(() => 'blob:x');
    const revokeObjectUrl = vi.fn();

    vi.stubGlobal('URL', {
      createObjectURL: createdObjectUrl,
      revokeObjectURL: revokeObjectUrl,
    });

    const origCreate = document.createElement.bind(document);
    const spy = vi
      .spyOn(document, 'createElement')
      .mockImplementation((tagName: string) => {
        const el = origCreate(tagName) as HTMLAnchorElement;
        if (tagName === 'a') {
          anchors.push(el);
          el.click = vi.fn();
        }
        return el;
      });

    triggerDownload('hello', 'test.txt', 'text/plain');

    expect(createdObjectUrl).toHaveBeenCalledOnce();
    expect(anchors[0].download).toBe('test.txt');
    expect(anchors[0].href).toBe('blob:x');
    expect(anchors[0].click).toHaveBeenCalledOnce();
    expect(revokeObjectUrl).toHaveBeenCalledWith('blob:x');

    spy.mockRestore();
    vi.unstubAllGlobals();
  });
});
