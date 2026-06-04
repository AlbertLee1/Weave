import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { BulkActionToolbar } from '../BulkActionToolbar';
import type { ActionType, ObjectType, WireObject } from '../../../api/types';

// Stub the actual download side-effect so clicking a format item in these
// keyboard tests does not try to create blobs / object URLs in jsdom.
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

const rows: WireObject[] = [
  { __primaryKey: '1', __apiName: 'Employee', __rid: 'ri.o.1', id: '1', name: 'Alice' },
  { __primaryKey: '2', __apiName: 'Employee', __rid: 'ri.o.2', id: '2', name: 'Bob' },
];

const server = setupServer();

function renderToolbar(opts?: { actions?: ActionType[] }) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const actions = opts?.actions ?? [];
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
        selectedRows={rows}
        onClear={vi.fn()}
      />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  server.listen();
});

afterEach(() => {
  server.resetHandlers();
  server.close();
  vi.clearAllMocks();
});

describe('BDD: BulkActionToolbar export menu keyboard navigation (WAI-ARIA)', () => {
  it('Given the export menu is opened, Then focus lands on the first format item', async () => {
    const user = userEvent.setup();
    renderToolbar();

    await user.click(screen.getByTestId('bulk-export'));

    await waitFor(() => {
      expect(screen.getByTestId('bulk-export-csv')).toHaveFocus();
    });
  });

  it('Given the open menu, When ArrowDown is pressed, Then focus moves through items and wraps to the first', async () => {
    const user = userEvent.setup();
    renderToolbar();

    await user.click(screen.getByTestId('bulk-export'));
    await waitFor(() => {
      expect(screen.getByTestId('bulk-export-csv')).toHaveFocus();
    });

    await user.keyboard('{ArrowDown}');
    expect(screen.getByTestId('bulk-export-json')).toHaveFocus();

    // wraps around back to the first item
    await user.keyboard('{ArrowDown}');
    expect(screen.getByTestId('bulk-export-csv')).toHaveFocus();
  });

  it('Given the open menu, When ArrowUp is pressed on the first item, Then focus wraps to the last item', async () => {
    const user = userEvent.setup();
    renderToolbar();

    await user.click(screen.getByTestId('bulk-export'));
    await waitFor(() => {
      expect(screen.getByTestId('bulk-export-csv')).toHaveFocus();
    });

    await user.keyboard('{ArrowUp}');
    expect(screen.getByTestId('bulk-export-json')).toHaveFocus();

    await user.keyboard('{ArrowUp}');
    expect(screen.getByTestId('bulk-export-csv')).toHaveFocus();
  });

  it('Given the open menu, When Home/End are pressed, Then focus jumps to first/last item', async () => {
    const user = userEvent.setup();
    renderToolbar();

    await user.click(screen.getByTestId('bulk-export'));
    await waitFor(() => {
      expect(screen.getByTestId('bulk-export-csv')).toHaveFocus();
    });

    await user.keyboard('{End}');
    expect(screen.getByTestId('bulk-export-json')).toHaveFocus();

    await user.keyboard('{Home}');
    expect(screen.getByTestId('bulk-export-csv')).toHaveFocus();
  });

  it('Given the open menu, When Escape is pressed, Then the menu closes and focus returns to the trigger', async () => {
    const user = userEvent.setup();
    renderToolbar();

    const trigger = screen.getByTestId('bulk-export');
    await user.click(trigger);
    await waitFor(() => {
      expect(screen.getByTestId('bulk-export-csv')).toHaveFocus();
    });

    await user.keyboard('{Escape}');

    await waitFor(() => {
      expect(screen.queryByTestId('bulk-export-csv')).not.toBeInTheDocument();
    });
    expect(trigger).toHaveFocus();
  });

  it('Given the open menu, When a format is clicked, Then the existing export logic still fires', async () => {
    const user = userEvent.setup();
    renderToolbar();

    await user.click(screen.getByTestId('bulk-export'));
    await user.click(screen.getByTestId('bulk-export-csv'));

    expect(triggerDownload).toHaveBeenCalledTimes(1);
    const [, filename] = (triggerDownload as unknown as {
      mock: { calls: [string, string, string][] };
    }).mock.calls[0];
    expect(filename).toBe('Employee-selected.csv');
  });
});
