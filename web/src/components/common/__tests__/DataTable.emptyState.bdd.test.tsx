import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
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

const rows: TestRow[] = [
  { id: 1, name: 'Alice', status: 'active' },
  { id: 2, name: 'Bob', status: 'inactive' },
];

describe('BDD: DataTable empty-state', () => {
  it('Given empty data, Then renders the default "No data" empty-state message', () => {
    render(<DataTable columns={columns} data={[]} />);
    expect(screen.getByText('No data')).toBeInTheDocument();
  });

  it('Given empty data, Then the empty-state cell spans all columns via colSpan', () => {
    render(<DataTable columns={columns} data={[]} />);
    const cell = screen.getByText('No data').closest('td');
    expect(cell).not.toBeNull();
    expect(cell).toHaveAttribute('colspan', String(columns.length));
  });

  it('Given empty data, Then exactly one tbody row is rendered (the empty-state row)', () => {
    const { container } = render(<DataTable columns={columns} data={[]} />);
    const bodyRows = container.querySelectorAll('tbody tr');
    expect(bodyRows).toHaveLength(1);
  });

  it('Given a custom emptyMessage with empty data, Then it is displayed instead of the default', () => {
    render(
      <DataTable columns={columns} data={[]} emptyMessage="Nothing to show here" />,
    );
    expect(screen.getByText('Nothing to show here')).toBeInTheDocument();
    expect(screen.queryByText('No data')).not.toBeInTheDocument();
  });

  it('Given a ReactNode emptyMessage with empty data, Then the node is rendered', () => {
    render(
      <DataTable
        columns={columns}
        data={[]}
        emptyMessage={<span data-testid="custom-empty">Empty!</span>}
      />,
    );
    expect(screen.getByTestId('custom-empty')).toBeInTheDocument();
  });

  it('Given non-empty data, Then the empty-state message does NOT appear and all rows render', () => {
    const { container } = render(<DataTable columns={columns} data={rows} />);
    expect(screen.queryByText('No data')).not.toBeInTheDocument();
    expect(screen.getByText('Alice')).toBeInTheDocument();
    expect(screen.getByText('Bob')).toBeInTheDocument();
    const bodyRows = container.querySelectorAll('tbody tr');
    expect(bodyRows).toHaveLength(rows.length);
  });

  it('Given empty data with pagination footer, Then footer reports 0 rows', () => {
    render(
      <DataTable
        columns={columns}
        data={[]}
        onNextPage={() => {}}
        currentPage={1}
      />,
    );
    const pagination = screen.getByTestId('pagination');
    expect(pagination).toHaveTextContent('0 rows');
  });
});
