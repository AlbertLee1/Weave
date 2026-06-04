import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { ComponentProps } from 'react';
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

function renderButton(props: Partial<ComponentProps<typeof ExportButton>> = {}) {
  return render(
    <ExportButton
      objectType={objectType}
      query={{
        ontologyApiName: 'ont',
        objectType: 'Employee',
        select: ['id', 'name'],
        hasActiveSearch: false,
      }}
      {...props}
    />,
  );
}

describe('BDD: ExportButton menu keyboard navigation (WAI-ARIA)', () => {
  beforeEach(() => {
    vi.mocked(exportObjects).mockReset();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('Given the menu is opened, Then focus lands on the first menu item', async () => {
    const user = userEvent.setup();
    renderButton();

    await user.click(screen.getByTestId('export-button'));

    await waitFor(() => {
      expect(screen.getByTestId('export-csv')).toHaveFocus();
    });
  });

  it('Given the open menu, When ArrowDown is pressed, Then focus moves through items and wraps to the first', async () => {
    const user = userEvent.setup();
    renderButton();

    await user.click(screen.getByTestId('export-button'));
    await waitFor(() => {
      expect(screen.getByTestId('export-csv')).toHaveFocus();
    });

    await user.keyboard('{ArrowDown}');
    expect(screen.getByTestId('export-json')).toHaveFocus();

    await user.keyboard('{ArrowDown}');
    expect(screen.getByTestId('export-xlsx')).toHaveFocus();

    // wraps around back to the first item
    await user.keyboard('{ArrowDown}');
    expect(screen.getByTestId('export-csv')).toHaveFocus();
  });

  it('Given the open menu, When ArrowUp is pressed on the first item, Then focus wraps to the last item', async () => {
    const user = userEvent.setup();
    renderButton();

    await user.click(screen.getByTestId('export-button'));
    await waitFor(() => {
      expect(screen.getByTestId('export-csv')).toHaveFocus();
    });

    await user.keyboard('{ArrowUp}');
    expect(screen.getByTestId('export-xlsx')).toHaveFocus();

    await user.keyboard('{ArrowUp}');
    expect(screen.getByTestId('export-json')).toHaveFocus();
  });

  it('Given the open menu, When Home/End are pressed, Then focus jumps to first/last item', async () => {
    const user = userEvent.setup();
    renderButton();

    await user.click(screen.getByTestId('export-button'));
    await waitFor(() => {
      expect(screen.getByTestId('export-csv')).toHaveFocus();
    });

    await user.keyboard('{End}');
    expect(screen.getByTestId('export-xlsx')).toHaveFocus();

    await user.keyboard('{Home}');
    expect(screen.getByTestId('export-csv')).toHaveFocus();
  });

  it('Given the open menu, When Escape is pressed, Then the menu closes and focus returns to the trigger', async () => {
    const user = userEvent.setup();
    renderButton();

    const trigger = screen.getByTestId('export-button');
    await user.click(trigger);
    await waitFor(() => {
      expect(screen.getByTestId('export-csv')).toHaveFocus();
    });

    await user.keyboard('{Escape}');

    await waitFor(() => {
      expect(screen.queryByTestId('export-csv')).not.toBeInTheDocument();
    });
    expect(trigger).toHaveFocus();
  });

  it('Given the open menu, When a format is clicked, Then the existing export logic still fires', async () => {
    vi.mocked(exportObjects).mockResolvedValue({
      filename: 'Employee-export.csv',
      count: 0,
    });
    const user = userEvent.setup();
    renderButton();

    await user.click(screen.getByTestId('export-button'));
    await user.click(screen.getByTestId('export-csv'));

    await waitFor(() => {
      expect(exportObjects).toHaveBeenCalledWith(
        'csv',
        expect.objectContaining({ objectType: 'Employee' }),
        objectType,
        expect.any(Function),
      );
    });
  });
});
