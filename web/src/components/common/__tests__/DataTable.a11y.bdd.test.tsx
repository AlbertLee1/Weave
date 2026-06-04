import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { DataTable, type Column } from '../DataTable';

interface TestRow {
  id: number;
  name: string;
  status: string;
  [key: string]: unknown;
}

const columns: Column<TestRow>[] = [
  { key: 'id', header: 'ID', sortable: true },
  { key: 'name', header: 'Name', sortable: true },
  { key: 'status', header: 'Status' },
];

const data: TestRow[] = [
  { id: 1, name: 'Alice', status: 'active' },
  { id: 2, name: 'Bob', status: 'inactive' },
  { id: 3, name: 'Charlie', status: 'active' },
];

describe('BDD: DataTable keyboard-operable sort headers (WCAG 2.1.1)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  // Given a DataTable with a sortable column,
  // When a keyboard user Tabs to the header and presses Enter,
  // Then onSort fires for that column.
  it('triggers onSort when a sortable header receives Enter via keyboard', async () => {
    const user = userEvent.setup();
    const onSort = vi.fn();
    render(<DataTable columns={columns} data={data} onSort={onSort} />);

    const nameHeader = screen.getByRole('button', { name: 'Name' });
    nameHeader.focus();
    expect(nameHeader).toHaveFocus();

    await user.keyboard('{Enter}');
    expect(onSort).toHaveBeenCalledWith('name', 'asc');
  });

  // Given a DataTable with a sortable column,
  // When a keyboard user presses Space on the header,
  // Then onSort fires (and the page does not scroll — preventDefault).
  it('triggers onSort when a sortable header receives Space via keyboard', async () => {
    const user = userEvent.setup();
    const onSort = vi.fn();
    render(<DataTable columns={columns} data={data} onSort={onSort} />);

    const nameHeader = screen.getByRole('button', { name: 'Name' });
    nameHeader.focus();

    await user.keyboard('{ }');
    expect(onSort).toHaveBeenCalledWith('name', 'asc');
  });

  // Sortable headers must be keyboard-focusable controls (button role) and
  // must live inside a columnheader carrying aria-sort.
  it('exposes sortable headers as focusable buttons within a columnheader', () => {
    render(<DataTable columns={columns} data={data} />);

    const nameButton = screen.getByRole('button', { name: 'Name' });
    expect(nameButton.tagName).toBe('BUTTON');
    // A native button is keyboard-focusable; it must sit inside the <th>
    // (columnheader) which is the element allowed to carry aria-sort.
    const th = nameButton.closest('th');
    expect(th).not.toBeNull();
    expect(th).toHaveAttribute('aria-sort');
  });

  // Non-sortable columns must NOT be keyboard-operable buttons.
  it('does not expose non-sortable headers as buttons', () => {
    render(<DataTable columns={columns} data={data} />);
    expect(screen.queryByRole('button', { name: 'Status' })).toBeNull();
  });

  // aria-sort (on the columnheader) reflects the current sort state.
  it('sets aria-sort according to current sort column and direction', async () => {
    const user = userEvent.setup();
    render(<DataTable columns={columns} data={data} onSort={vi.fn()} />);

    const nameButton = screen.getByRole('button', { name: 'Name' });
    const idButton = screen.getByRole('button', { name: 'ID' });
    const nameHeader = nameButton.closest('th')!;
    const idHeader = idButton.closest('th')!;

    // Initially nothing is sorted.
    expect(nameHeader).toHaveAttribute('aria-sort', 'none');
    expect(idHeader).toHaveAttribute('aria-sort', 'none');

    // After first activation -> ascending.
    nameButton.focus();
    await user.keyboard('{Enter}');
    expect(nameHeader).toHaveAttribute('aria-sort', 'ascending');
    expect(idHeader).toHaveAttribute('aria-sort', 'none');

    // After second activation -> descending.
    await user.keyboard('{Enter}');
    expect(nameHeader).toHaveAttribute('aria-sort', 'descending');
  });
});

describe('BDD: DataTable pagination has accessible names', () => {
  // Given pagination controls,
  // When a screen-reader user enumerates buttons,
  // Then Prev/Next expose an accessible "page" context.
  it('labels Prev and Next buttons with page context', () => {
    render(
      <DataTable
        columns={columns}
        data={data}
        totalCount="100"
        hasNextPage={true}
        hasPrevPage={true}
        onNextPage={vi.fn()}
        onPrevPage={vi.fn()}
        currentPage={2}
      />,
    );

    expect(
      screen.getByRole('button', { name: /previous page/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole('button', { name: /next page/i }),
    ).toBeInTheDocument();
  });
});
