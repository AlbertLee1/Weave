import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { BulkActionToolbar } from '../BulkActionToolbar';
import type { ActionType, ObjectType, WireObject } from '../../../api/types';

vi.mock('../../../lib/exportObjects', async () => {
  const actual = await vi.importActual<
    typeof import('../../../lib/exportObjects')
  >('../../../lib/exportObjects');
  return {
    ...actual,
    triggerDownload: vi.fn(),
  };
});

import { triggerDownload } from '../../../lib/exportObjects';

const objectType: ObjectType = {
  rid: 'ri.ot',
  apiName: 'Employee',
  displayName: 'Employee',
  primaryKey: 'id',
  status: 'ACTIVE',
  visibility: 'NORMAL',
  properties: {
    id: { dataType: { type: 'string' }, rid: 'ri.p.id' },
    name: { dataType: { type: 'string' }, rid: 'ri.p.name' },
  },
};

const deleteAction: ActionType = {
  rid: 'ri.at.delete',
  apiName: 'deleteEmployee',
  displayName: 'Delete Employee',
  status: 'ACTIVE',
  parameters: {
    primaryKey: { dataType: { type: 'string' }, required: true },
  },
  rules: [{ type: 'deleteObject', objectType: 'Employee', primaryKey: 'primaryKey' }],
};

const rows: WireObject[] = [
  { __primaryKey: '1', __apiName: 'Employee', __rid: 'ri.o.1', id: '1', name: 'Alice' },
  { __primaryKey: '2', __apiName: 'Employee', __rid: 'ri.o.2', id: '2', name: 'Bob' },
];

let applyBatchSpy: ReturnType<typeof vi.fn<(body: unknown) => Promise<unknown>>>;

const server = setupServer();

function renderToolbar(opts?: {
  selectedRows?: WireObject[];
  actions?: ActionType[];
  onClear?: () => void;
  onDeleted?: () => void;
}) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const actions = opts?.actions ?? [deleteAction];
  server.use(
    http.get('/api/v2/ontologies/:ontology/actionTypes', () =>
      HttpResponse.json({ data: actions }),
    ),
  );
  return render(
    <QueryClientProvider client={client}>
      <BulkActionToolbar
        ontologyApiName="ont"
        objectType={objectType}
        selectedRows={opts?.selectedRows ?? rows}
        onClear={opts?.onClear ?? vi.fn()}
        onDeleted={opts?.onDeleted}
      />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  applyBatchSpy = vi.fn(async () => ({}));
  server.listen();
  server.use(
    http.post(
      '/api/v2/ontologies/:ontology/actions/:action/applyBatch',
      async ({ request }) => {
        const body = (await request.json()) as unknown;
        applyBatchSpy(body);
        return HttpResponse.json({});
      },
    ),
  );
});

afterEach(() => {
  server.resetHandlers();
  server.close();
  vi.clearAllMocks();
});

describe('BulkActionToolbar', () => {
  it('renders nothing when no rows are selected', () => {
    renderToolbar({ selectedRows: [] });
    expect(screen.queryByTestId('bulk-action-toolbar')).not.toBeInTheDocument();
  });

  it('shows the selected count', () => {
    renderToolbar();
    expect(screen.getByTestId('selected-count')).toHaveTextContent('2 selected');
  });

  it('opens confirmation modal and calls applyBatch with delete requests', async () => {
    const onClear = vi.fn();
    renderToolbar({ onClear });

    fireEvent.click(screen.getByTestId('bulk-delete'));
    expect(screen.getByText(/Delete selected objects/i)).toBeInTheDocument();

    // Wait for useActionTypes to resolve so the confirm button becomes enabled.
    await waitFor(() => {
      expect(screen.getByTestId('bulk-delete-confirm')).not.toBeDisabled();
    });

    fireEvent.click(screen.getByTestId('bulk-delete-confirm'));

    await waitFor(() => {
      expect(applyBatchSpy).toHaveBeenCalled();
    });
    const body = applyBatchSpy.mock.calls[0][0] as {
      requests: Array<{ parameters: Record<string, unknown> }>;
    };
    expect(body.requests).toHaveLength(2);
    expect(body.requests[0].parameters).toEqual({ primaryKey: '1' });
    expect(body.requests[1].parameters).toEqual({ primaryKey: '2' });

    await waitFor(() => {
      expect(onClear).toHaveBeenCalled();
    });
  });

  it('shows "no delete action" message when no deleteObject action exists', async () => {
    renderToolbar({ actions: [] });

    fireEvent.click(screen.getByTestId('bulk-delete'));

    await waitFor(() => {
      expect(screen.getByTestId('no-delete-action')).toBeInTheDocument();
    });
    // Confirm stays disabled so the user cannot submit.
    expect(screen.getByTestId('bulk-delete-confirm')).toBeDisabled();
  });

  it('Export Selected as CSV downloads only the selected rows', async () => {
    renderToolbar();
    fireEvent.click(screen.getByTestId('bulk-export'));
    fireEvent.click(screen.getByTestId('bulk-export-csv'));

    expect(triggerDownload).toHaveBeenCalledTimes(1);
    const [content, filename, mime] = (triggerDownload as unknown as {
      mock: { calls: [string, string, string][] };
    }).mock.calls[0];
    expect(filename).toBe('Employee-selected.csv');
    expect(mime).toContain('text/csv');
    expect(content).toContain('id,name');
    expect(content).toContain('1,Alice');
    expect(content).toContain('2,Bob');
  });

  it('Export Selected as JSON downloads only the selected rows in envelope', () => {
    renderToolbar();
    fireEvent.click(screen.getByTestId('bulk-export'));
    fireEvent.click(screen.getByTestId('bulk-export-json'));

    expect(triggerDownload).toHaveBeenCalledTimes(1);
    const [content, filename] = (triggerDownload as unknown as {
      mock: { calls: [string, string, string][] };
    }).mock.calls[0];
    expect(filename).toBe('Employee-selected.json');
    const parsed = JSON.parse(content);
    expect(parsed.metadata.objectType).toBe('Employee');
    expect(parsed.metadata.count).toBe(2);
    expect(parsed.data).toHaveLength(2);
    expect(parsed.data[0].id).toBe('1');
  });

  it('Clear button calls onClear', () => {
    const onClear = vi.fn();
    renderToolbar({ onClear });
    fireEvent.click(screen.getByTestId('bulk-clear'));
    expect(onClear).toHaveBeenCalled();
  });
});
