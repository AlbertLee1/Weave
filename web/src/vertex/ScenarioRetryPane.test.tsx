import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ScenarioRetryPane, type ScenarioRetryEvent } from './ScenarioRetryPane';

const events: ScenarioRetryEvent[] = [
  {
    activityId: 'delayPropagate',
    attempt: 1,
    error: 'connection reset',
    stack: 'at fn delayPropagate (line 4)\nat run (line 12)',
    occurredAt: '2026-05-15T10:00:00Z',
  },
  {
    activityId: 'delayPropagate',
    attempt: 2,
    error: 'connection reset',
    stack: 'at fn delayPropagate (line 4)\nat run (line 12)',
    occurredAt: '2026-05-15T10:00:05Z',
  },
];

describe('ScenarioRetryPane (VTX-101)', () => {
  it('shows an empty placeholder when no retries arrived yet', () => {
    render(<ScenarioRetryPane events={[]} />);
    expect(screen.getByTestId('scenario-retry-empty')).toBeInTheDocument();
  });

  it('shows a retry counter for the failing activity', () => {
    render(<ScenarioRetryPane events={events} />);
    const counter = screen.getByTestId('scenario-retry-counter-delayPropagate');
    expect(counter.textContent).toContain('retries: 2');
  });

  it('keeps error stacks hidden until the row is expanded', () => {
    render(<ScenarioRetryPane events={events} />);
    expect(screen.queryByTestId('scenario-retry-stacks-delayPropagate')).toBeNull();
    fireEvent.click(screen.getByTestId('scenario-retry-toggle-delayPropagate'));
    const stacks = screen.getByTestId('scenario-retry-stacks-delayPropagate');
    expect(stacks).toBeInTheDocument();
    expect(stacks.textContent).toContain('attempt #1');
    expect(stacks.textContent).toContain('attempt #2');
    expect(stacks.textContent).toContain('connection reset');
  });

  it('orders retries by attempt ascending even if events arrive out of order', () => {
    const reversed = [...events].reverse();
    render(<ScenarioRetryPane events={reversed} />);
    fireEvent.click(screen.getByTestId('scenario-retry-toggle-delayPropagate'));
    const items = screen.getByTestId('scenario-retry-stacks-delayPropagate').querySelectorAll('li');
    expect(items[0].textContent).toContain('#1');
    expect(items[1].textContent).toContain('#2');
  });
});
