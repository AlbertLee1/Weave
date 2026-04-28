import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { ExportButton } from '../ExportButton';
import type { ObjectType } from '../../../api/types';

vi.mock('../../../lib/exportObjects', async () => {
  const actual = await vi.importActual<typeof import('../../../lib/exportObjects')>(
    '../../../lib/exportObjects',
  );
  return {
    ...actual,
    exportObjects: vi.fn(),
  };
});

import { exportObjects } from '../../../lib/exportObjects';

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

function renderButton() {
  return render(
    <ExportButton
      objectType={objectType}
      query={{
        ontologyApiName: 'ont',
        objectType: 'Employee',
        select: ['id', 'name'],
        hasActiveSearch: false,
      }}
    />,
  );
}

describe('ExportButton', () => {
  beforeEach(() => {
    vi.mocked(exportObjects).mockReset();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('shows dropdown with CSV, JSON, and Excel options when clicked', () => {
    renderButton();
    expect(screen.queryByTestId('export-csv')).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId('export-button'));
    expect(screen.getByTestId('export-csv')).toBeInTheDocument();
    expect(screen.getByTestId('export-json')).toBeInTheDocument();
    expect(screen.getByTestId('export-xlsx')).toBeInTheDocument();
  });

  it('invokes exportObjects with "csv" when CSV option clicked', async () => {
    vi.mocked(exportObjects).mockResolvedValue({
      filename: 'Employee-export.csv',
      count: 0,
    });
    renderButton();
    fireEvent.click(screen.getByTestId('export-button'));
    fireEvent.click(screen.getByTestId('export-csv'));

    await waitFor(() => {
      expect(exportObjects).toHaveBeenCalledWith(
        'csv',
        expect.objectContaining({
          ontologyApiName: 'ont',
          objectType: 'Employee',
          hasActiveSearch: false,
        }),
        objectType,
        expect.any(Function),
      );
    });
  });

  it('invokes exportObjects with "json" when JSON option clicked', async () => {
    vi.mocked(exportObjects).mockResolvedValue({
      filename: 'Employee-export.json',
      count: 0,
    });
    renderButton();
    fireEvent.click(screen.getByTestId('export-button'));
    fireEvent.click(screen.getByTestId('export-json'));

    await waitFor(() => {
      expect(exportObjects).toHaveBeenCalledWith(
        'json',
        expect.anything(),
        objectType,
        expect.any(Function),
      );
    });
  });

  it('invokes exportObjects with "xlsx" when Excel option clicked', async () => {
    vi.mocked(exportObjects).mockResolvedValue({
      filename: 'Employee-export.xlsx',
      count: 0,
    });
    renderButton();
    fireEvent.click(screen.getByTestId('export-button'));
    fireEvent.click(screen.getByTestId('export-xlsx'));

    await waitFor(() => {
      expect(exportObjects).toHaveBeenCalledWith(
        'xlsx',
        expect.anything(),
        objectType,
        expect.any(Function),
      );
    });
  });

  it('shows error banner when export fails', async () => {
    vi.mocked(exportObjects).mockRejectedValue(new Error('boom'));
    renderButton();
    fireEvent.click(screen.getByTestId('export-button'));
    fireEvent.click(screen.getByTestId('export-csv'));

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('boom');
    });
  });
});
