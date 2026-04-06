import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ObjectSetResultTable } from '../ObjectSetResultTable';

const sampleData = [
  { __rid: 'ri.1', __primaryKey: '1', __apiName: 'Employee', name: 'Alice', age: 30 },
  { __rid: 'ri.2', __primaryKey: '2', __apiName: 'Employee', name: 'Bob', age: 25 },
];

describe('ObjectSetResultTable', () => {
  it('renders column headers from data keys', () => {
    render(
      <ObjectSetResultTable
        data={sampleData}
        totalCount="2"
        hasNextPage={false}
        hasPrevPage={false}
        onNextPage={() => {}}
        onPrevPage={() => {}}
        currentPage={1}
      />,
    );
    expect(screen.getByText('name')).toBeInTheDocument();
    expect(screen.getByText('age')).toBeInTheDocument();
  });

  it('renders row values', () => {
    render(
      <ObjectSetResultTable
        data={sampleData}
        totalCount="2"
        hasNextPage={false}
        hasPrevPage={false}
        onNextPage={() => {}}
        onPrevPage={() => {}}
        currentPage={1}
      />,
    );
    expect(screen.getByText('Alice')).toBeInTheDocument();
    expect(screen.getByText('Bob')).toBeInTheDocument();
  });

  it('shows totalCount', () => {
    render(
      <ObjectSetResultTable
        data={sampleData}
        totalCount="42"
        hasNextPage={false}
        hasPrevPage={false}
        onNextPage={() => {}}
        onPrevPage={() => {}}
        currentPage={1}
      />,
    );
    expect(screen.getByText(/42/)).toBeInTheDocument();
  });

  it('calls onNextPage when next button is clicked', () => {
    const onNext = vi.fn();
    render(
      <ObjectSetResultTable
        data={sampleData}
        totalCount="50"
        hasNextPage={true}
        hasPrevPage={false}
        onNextPage={onNext}
        onPrevPage={() => {}}
        currentPage={1}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: /next/i }));
    expect(onNext).toHaveBeenCalled();
  });

  it('disables prev button on first page', () => {
    render(
      <ObjectSetResultTable
        data={sampleData}
        totalCount="50"
        hasNextPage={true}
        hasPrevPage={false}
        onNextPage={() => {}}
        onPrevPage={() => {}}
        currentPage={1}
      />,
    );
    expect(screen.getByRole('button', { name: /prev/i })).toBeDisabled();
  });
});
