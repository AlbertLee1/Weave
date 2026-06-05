import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { describe, it, expect } from 'vitest';
import { NotFoundPage } from '../NotFoundPage';

// BDD: the catch-all 404 page must expose a page-level <h1> so it has the
// same heading structure as every other page in the app (a11y: each page
// should have exactly one top-level heading).
describe('NotFoundPage a11y heading structure', () => {
  function renderAt(path: string) {
    return render(
      <MemoryRouter initialEntries={[path]}>
        <NotFoundPage />
      </MemoryRouter>,
    );
  }

  // Given an unknown URL rendering the catch-all 404 page
  // When the page renders
  // Then there is exactly one level-1 heading naming the not-found state
  it('renders exactly one h1 naming the not-found state', () => {
    renderAt('/some/unknown/path');

    const h1s = screen.getAllByRole('heading', { level: 1 });
    expect(h1s).toHaveLength(1);
    expect(h1s[0]).toHaveAccessibleName(/not found|404/i);
  });

  // Guard: the existing test contract (the page wrapper) is preserved.
  it('still renders the not-found page wrapper', () => {
    renderAt('/another/missing/route');
    expect(screen.getByTestId('not-found-page')).toBeInTheDocument();
  });
});
