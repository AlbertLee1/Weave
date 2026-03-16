import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { ActionResult } from '../ActionResult';

describe('ActionResult', () => {
  it('shows nothing when result is null', () => {
    const { container } = render(<ActionResult result={null} />);
    expect(container.firstChild).toBeNull();
  });

  it('displays edit entries', () => {
    render(
      <ActionResult
        result={{
          edits: [
            {
              type: 'addObject',
              objectType: 'Employee',
              primaryKey: '1',
              properties: { name: 'Alice' },
            },
            {
              type: 'modifyObject',
              objectType: 'Employee',
              primaryKey: '2',
              properties: { name: 'Bob' },
            },
          ],
        }}
      />,
    );

    expect(screen.getByText('addObject')).toBeInTheDocument();
    expect(screen.getByText('modifyObject')).toBeInTheDocument();
    expect(screen.getAllByText('Employee')).toHaveLength(2);
    expect(screen.getByText('2 edits applied')).toBeInTheDocument();
  });

  it('displays empty message when no edits', () => {
    render(<ActionResult result={{ edits: [] }} />);
    expect(screen.getByText(/no edits/i)).toBeInTheDocument();
  });
});
