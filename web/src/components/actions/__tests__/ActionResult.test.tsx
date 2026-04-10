import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { ActionResult } from '../ActionResult';

describe('ActionResult', () => {
  it('shows nothing when result is null', () => {
    const { container } = render(<ActionResult result={null} />);
    expect(container.firstChild).toBeNull();
  });

  it('displays edit counts', () => {
    render(
      <ActionResult
        result={{
          edits: {
            type: 'edits',
            addedObjectCount: 1,
            modifiedObjectCount: 1,
            deletedObjectCount: 0,
            addedLinksCount: 0,
            deletedLinksCount: 0,
          },
        }}
      />,
    );

    expect(screen.getByText('addObject')).toBeInTheDocument();
    expect(screen.getByText('modifyObject')).toBeInTheDocument();
    expect(screen.getByText('2 edits applied')).toBeInTheDocument();
  });

  it('displays empty message when no edits', () => {
    render(<ActionResult result={{}} />);
    expect(screen.getByText(/no edits/i)).toBeInTheDocument();
  });
});
