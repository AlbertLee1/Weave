import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
  escapeCsvField,
  toCsv,
  toJsonEnvelope,
  resolveExportColumns,
  fetchAllForExport,
  exportObjects,
  triggerDownload,
  buildXlsxWorkbook,
  computeAggregationSheet,
} from '../exportObjects';
import * as XLSX from 'xlsx';
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

describe('computeAggregationSheet', () => {
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
      salary: { dataType: { type: 'integer' }, rid: 'ri.salary' },
    },
  };

  it('returns count + non-null + distinct for every column', () => {
    const rows: WireObject[] = [
      { __rid: 'r1', __primaryKey: '1', __apiName: 'Employee', id: '1', name: 'Alice', salary: 100 },
      { __rid: 'r2', __primaryKey: '2', __apiName: 'Employee', id: '2', name: 'Bob', salary: null },
      { __rid: 'r3', __primaryKey: '3', __apiName: 'Employee', id: '3', name: 'Alice', salary: 200 },
    ];
    const sheet = computeAggregationSheet(rows, ['id', 'name', 'salary'], objectType);
    // Header row
    expect(sheet[0]).toEqual([
      'column',
      'count',
      'nonNull',
      'distinct',
      'min',
      'max',
      'sum',
      'avg',
    ]);
    const byCol = new Map(sheet.slice(1).map((r) => [r[0], r]));
    expect(byCol.get('id')?.slice(1, 4)).toEqual([3, 3, 3]);
    // string-typed name has count/nonNull/distinct only; numeric stats blank
    expect(byCol.get('name')).toEqual(['name', 3, 3, 2, '', '', '', '']);
    // numeric salary computes min/max/sum/avg over non-null values
    expect(byCol.get('salary')).toEqual(['salary', 3, 2, 2, 100, 200, 300, 150]);
  });

  it('emits empty stats when no rows are exported', () => {
    const sheet = computeAggregationSheet([], ['id', 'salary'], objectType);
    expect(sheet[0][0]).toBe('column');
    expect(sheet).toHaveLength(3);
    expect(sheet[1]).toEqual(['id', 0, 0, 0, '', '', '', '']);
    expect(sheet[2]).toEqual(['salary', 0, 0, 0, '', '', '', '']);
  });
});

describe('buildXlsxWorkbook', () => {
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
      salary: { dataType: { type: 'integer' }, rid: 'ri.salary' },
    },
  };

  it('produces a workbook with a Data sheet and a Summary sheet', () => {
    const rows: WireObject[] = [
      { __rid: 'r1', __primaryKey: '1', __apiName: 'Employee', id: '1', name: 'Alice', salary: 100 },
      { __rid: 'r2', __primaryKey: '2', __apiName: 'Employee', id: '2', name: 'Bob', salary: 200 },
    ];
    const wb = buildXlsxWorkbook(rows, ['id', 'name', 'salary'], objectType);
    expect(wb.SheetNames).toEqual(['Data', 'Summary']);

    const dataSheet = wb.Sheets['Data'];
    const dataRows = XLSX.utils.sheet_to_json(dataSheet, { header: 1 }) as unknown[][];
    expect(dataRows[0]).toEqual(['id', 'name', 'salary']);
    expect(dataRows).toHaveLength(3);
    expect(dataRows[1]).toEqual(['1', 'Alice', 100]);

    const summarySheet = wb.Sheets['Summary'];
    const summaryRows = XLSX.utils.sheet_to_json(summarySheet, { header: 1 }) as unknown[][];
    expect(summaryRows[0]).toEqual([
      'column',
      'count',
      'nonNull',
      'distinct',
      'min',
      'max',
      'sum',
      'avg',
    ]);
    const salaryRow = summaryRows.find((r) => r[0] === 'salary');
    expect(salaryRow).toEqual(['salary', 2, 2, 2, 100, 200, 300, 150]);
  });

  it('serialises object/array cells as JSON text in the Data sheet', () => {
    const rows: WireObject[] = [
      {
        __rid: 'r1',
        __primaryKey: '1',
        __apiName: 'Employee',
        id: '1',
        tags: ['a', 'b'],
        meta: { k: 1 },
      } as WireObject,
    ];
    const ot: ObjectType = {
      ...objectType,
      properties: {
        id: { dataType: { type: 'string' }, rid: 'ri.id' },
        tags: { dataType: { type: 'array', itemType: { type: 'string' } }, rid: 'ri.tags' },
        meta: { dataType: { type: 'struct' }, rid: 'ri.meta' },
      },
    };
    const wb = buildXlsxWorkbook(rows, ['id', 'tags', 'meta'], ot);
    const dataRows = XLSX.utils.sheet_to_json(wb.Sheets['Data'], { header: 1 }) as unknown[][];
    expect(dataRows[1]).toEqual(['1', '["a","b"]', '{"k":1}']);
  });
});

describe('exportObjects xlsx', () => {
  let createdUrls: string[] = [];
  const blobs: Blob[] = [];
  const createdObjectUrl = vi.fn((blob: Blob) => {
    const fakeUrl = `blob:fake-${createdUrls.length}`;
    createdUrls.push(fakeUrl);
    blobs.push(blob);
    return fakeUrl;
  });
  const revokeObjectUrl = vi.fn();
  const anchors: HTMLAnchorElement[] = [];

  beforeEach(() => {
    vi.mocked(listObjects).mockReset();
    vi.mocked(searchObjects).mockReset();
    createdUrls = [];
    blobs.length = 0;
    anchors.length = 0;

    vi.stubGlobal('URL', {
      createObjectURL: createdObjectUrl,
      revokeObjectURL: revokeObjectUrl,
    });

    const origCreate = document.createElement.bind(document);
    vi.spyOn(document, 'createElement').mockImplementation((tagName: string) => {
      const el = origCreate(tagName) as HTMLAnchorElement;
      if (tagName === 'a') {
        anchors.push(el);
        el.click = vi.fn();
      }
      return el;
    });
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

  it('produces a Data + Summary workbook for the xlsx format', async () => {
    vi.mocked(listObjects).mockResolvedValueOnce({
      data: [
        { __rid: 'r1', __primaryKey: '1', __apiName: 'Employee', id: '1', name: 'Alice' },
      ],
    });

    const result = await exportObjects(
      'xlsx',
      {
        ontologyApiName: 'ont',
        objectType: 'Employee',
        select: ['id', 'name'],
        hasActiveSearch: false,
      },
      objectType,
    );

    expect(result.filename).toBe('Employee-export.xlsx');
    expect(result.count).toBe(1);
    expect(anchors[0].download).toBe('Employee-export.xlsx');

    expect(blobs).toHaveLength(1);
    const buf = await blobs[0].arrayBuffer();
    const wb = XLSX.read(buf, { type: 'array' });
    expect(wb.SheetNames).toEqual(['Data', 'Summary']);
    const dataRows = XLSX.utils.sheet_to_json(wb.Sheets['Data'], {
      header: 1,
    }) as unknown[][];
    expect(dataRows[0]).toEqual(['id', 'name']);
    expect(dataRows[1]).toEqual(['1', 'Alice']);
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
