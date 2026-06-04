import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MetricSelector } from '../MetricSelector';

// BDD: the alias input must expose an accessible name (aria-label) so it can be
// located by role like its sibling inputs (percentile / maxItems / direction),
// rather than relying on a placeholder alone.
describe('BDD MetricSelector alias input accessible name', () => {
  it('Given a rendered metric, Then the alias input is reachable by accessible name', () => {
    render(
      <MetricSelector
        metrics={[{ type: 'count' }]}
        onChange={() => {}}
        availableFields={['name', 'age']}
      />,
    );

    // aria-label must be present for getByRole name matching to succeed.
    const aliasInput = screen.getByRole('textbox', { name: /alias/i });
    expect(aliasInput).toBeInTheDocument();
    // It is the same element addressed by the existing test id.
    expect(aliasInput).toBe(screen.getByTestId('metric-0-name'));
  });
});
