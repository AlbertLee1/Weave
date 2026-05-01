import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';

import {
  Skeleton,
  SkeletonText,
  SkeletonCard,
  SkeletonTable,
  SkeletonList,
} from '../Skeleton';

describe('Skeleton', () => {
  it('renders an a11y status region with the default aria-label', () => {
    render(<Skeleton />);
    const region = screen.getByRole('status');
    expect(region).toHaveAttribute('aria-live', 'polite');
    expect(region).toHaveAttribute('aria-busy', 'true');
    expect(region).toHaveAccessibleName(/loading/i);
  });

  it('applies explicit width/height styles to the placeholder', () => {
    render(<Skeleton width={120} height={16} data-testid="sk" />);
    const placeholder = screen.getByTestId('sk');
    expect(placeholder.style.width).toBe('120px');
    expect(placeholder.style.height).toBe('16px');
  });

  it('accepts string width/height values', () => {
    render(<Skeleton width="50%" height="2rem" data-testid="sk" />);
    const placeholder = screen.getByTestId('sk');
    expect(placeholder.style.width).toBe('50%');
    expect(placeholder.style.height).toBe('2rem');
  });

  it('renders a circular placeholder for variant="circle"', () => {
    const { container } = render(<Skeleton variant="circle" />);
    expect(container.querySelector('.rounded-full')).not.toBeNull();
  });

  it('forwards a custom aria-label and merges className', () => {
    const { container } = render(
      <Skeleton aria-label="Fetching ontologies" className="ml-4" />,
    );
    const region = screen.getByRole('status');
    expect(region).toHaveAccessibleName('Fetching ontologies');
    // The className is applied to the outermost wrapper.
    expect(container.firstChild).toHaveClass('ml-4');
  });
});

describe('SkeletonText', () => {
  it('renders the requested number of text rows', () => {
    render(<SkeletonText lines={4} data-testid="rows" />);
    const rows = screen.getAllByTestId('skeleton-text-line');
    expect(rows).toHaveLength(4);
  });

  it('makes the last row narrower than the rest', () => {
    render(<SkeletonText lines={3} lastLineWidth="40%" />);
    const rows = screen.getAllByTestId('skeleton-text-line');
    expect(rows[rows.length - 1].style.width).toBe('40%');
    // Earlier rows fall back to a wider default.
    expect(rows[0].style.width).not.toBe('40%');
  });

  it('exposes a single status region for the whole block', () => {
    render(<SkeletonText lines={5} aria-label="Loading description" />);
    const regions = screen.getAllByRole('status');
    expect(regions).toHaveLength(1);
    expect(regions[0]).toHaveAccessibleName('Loading description');
  });
});

describe('SkeletonCard', () => {
  it('renders a card-shaped placeholder with title + body slots', () => {
    render(<SkeletonCard />);
    expect(screen.getByTestId('skeleton-card')).toBeInTheDocument();
    expect(screen.getByTestId('skeleton-card-title')).toBeInTheDocument();
    expect(
      screen.getAllByTestId('skeleton-card-body-line').length,
    ).toBeGreaterThan(0);
  });

  it('exposes a single status region for the card', () => {
    render(<SkeletonCard aria-label="Loading ontology" />);
    expect(screen.getByRole('status')).toHaveAccessibleName(
      'Loading ontology',
    );
  });
});

describe('SkeletonTable', () => {
  it('renders the requested rows × columns cells', () => {
    render(<SkeletonTable rows={3} columns={4} />);
    const rows = screen.getAllByTestId('skeleton-table-row');
    expect(rows).toHaveLength(3);
    for (const row of rows) {
      const cells = row.querySelectorAll('[data-testid="skeleton-table-cell"]');
      expect(cells.length).toBe(4);
    }
  });

  it('renders a header row separately when showHeader is true', () => {
    render(<SkeletonTable rows={2} columns={3} showHeader />);
    expect(screen.getByTestId('skeleton-table-header')).toBeInTheDocument();
    // Header has its own row of cells matching the column count.
    const headerCells = screen
      .getByTestId('skeleton-table-header')
      .querySelectorAll('[data-testid="skeleton-table-header-cell"]');
    expect(headerCells.length).toBe(3);
  });

  it('omits the header when showHeader is false', () => {
    render(<SkeletonTable rows={2} columns={3} showHeader={false} />);
    expect(
      screen.queryByTestId('skeleton-table-header'),
    ).not.toBeInTheDocument();
  });
});

describe('SkeletonList', () => {
  it('renders the requested number of list rows', () => {
    render(<SkeletonList items={5} />);
    expect(screen.getAllByTestId('skeleton-list-item')).toHaveLength(5);
  });

  it('exposes a single status region', () => {
    render(<SkeletonList items={3} aria-label="Loading list" />);
    const regions = screen.getAllByRole('status');
    expect(regions).toHaveLength(1);
    expect(regions[0]).toHaveAccessibleName('Loading list');
  });
});
