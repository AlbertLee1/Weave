import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { FacetsPanel } from '../FacetsPanel';

describe('FacetsPanel', () => {
  it('renders nothing when there are no fields', () => {
    const { container } = render(
      <FacetsPanel
        fields={[]}
        facets={undefined}
        selected={{}}
        onToggle={() => {}}
      />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it('renders one group per field with bucket value + count', () => {
    render(
      <FacetsPanel
        fields={['owner', 'category']}
        facets={{
          owner: [
            { value: 'alice', count: 3 },
            { value: 'bob', count: 1 },
          ],
          category: [{ value: 'news', count: 2 }],
        }}
        selected={{}}
        onToggle={() => {}}
      />,
    );

    expect(screen.getByTestId('facet-group-owner')).toBeInTheDocument();
    expect(screen.getByTestId('facet-group-category')).toBeInTheDocument();
    // bucket value shown
    expect(screen.getByText('alice')).toBeInTheDocument();
    expect(screen.getByText('bob')).toBeInTheDocument();
    // counts visible alongside
    expect(screen.getByText('3')).toBeInTheDocument();
    expect(screen.getByText('2')).toBeInTheDocument();
  });

  it('shows "No values" placeholder for fields with empty buckets', () => {
    render(
      <FacetsPanel
        fields={['owner']}
        facets={{ owner: [] }}
        selected={{}}
        onToggle={() => {}}
      />,
    );
    expect(screen.getByText('No values')).toBeInTheDocument();
  });

  it('marks a checkbox as checked when its value is in `selected`', () => {
    render(
      <FacetsPanel
        fields={['owner']}
        facets={{
          owner: [
            { value: 'alice', count: 3 },
            { value: 'bob', count: 1 },
          ],
        }}
        selected={{ owner: ['alice'] }}
        onToggle={() => {}}
      />,
    );

    const aliceBox = screen.getByLabelText('owner: alice') as HTMLInputElement;
    const bobBox = screen.getByLabelText('owner: bob') as HTMLInputElement;
    expect(aliceBox.checked).toBe(true);
    expect(bobBox.checked).toBe(false);
  });

  it('fires onToggle(field, value) when a checkbox is clicked', () => {
    const onToggle = vi.fn();
    render(
      <FacetsPanel
        fields={['owner']}
        facets={{ owner: [{ value: 'alice', count: 3 }] }}
        selected={{}}
        onToggle={onToggle}
      />,
    );

    fireEvent.click(screen.getByLabelText('owner: alice'));
    expect(onToggle).toHaveBeenCalledWith('owner', 'alice');
  });

  it('renders a Clear button only when at least one bucket is selected', () => {
    const onClear = vi.fn();
    const { rerender } = render(
      <FacetsPanel
        fields={['owner']}
        facets={{ owner: [{ value: 'alice', count: 3 }] }}
        selected={{}}
        onToggle={() => {}}
        onClear={onClear}
      />,
    );
    expect(screen.queryByTestId('facets-clear')).not.toBeInTheDocument();

    rerender(
      <FacetsPanel
        fields={['owner']}
        facets={{ owner: [{ value: 'alice', count: 3 }] }}
        selected={{ owner: ['alice'] }}
        onToggle={() => {}}
        onClear={onClear}
      />,
    );
    fireEvent.click(screen.getByTestId('facets-clear'));
    expect(onClear).toHaveBeenCalled();
  });

  it('collapses long bucket lists behind a "Show more" toggle', () => {
    const buckets = Array.from({ length: 12 }, (_, i) => ({
      value: `v${i}`,
      count: 12 - i,
    }));
    render(
      <FacetsPanel
        fields={['owner']}
        facets={{ owner: buckets }}
        selected={{}}
        onToggle={() => {}}
      />,
    );

    // Initially only first 8 visible
    expect(screen.getByText('v0')).toBeInTheDocument();
    expect(screen.getByText('v7')).toBeInTheDocument();
    expect(screen.queryByText('v8')).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId('facet-toggle-owner'));
    expect(screen.getByText('v8')).toBeInTheDocument();
    expect(screen.getByText('v11')).toBeInTheDocument();
  });
});
