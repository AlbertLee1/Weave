import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import { Toaster } from '../Toaster';
import { useToastStore } from '../../../stores/toastStore';

// BDD: screen-reader users must be able to perceive async operation outcomes
// (save succeeded/failed, install complete, ...) that surface as toasts. The
// `role="status"` tile therefore carries an `aria-live` region whose urgency is
// derived from the toast severity: errors interrupt ("assertive"), everything
// else waits politely ("polite"). `aria-atomic="true"` guarantees the whole
// message is read out as a single unit.
describe('BDD: Toaster announces toasts via aria-live', () => {
  beforeEach(() => {
    useToastStore.getState().clear();
  });
  afterEach(() => {
    vi.useRealTimers();
    useToastStore.getState().clear();
  });

  it('Given an info toast, Then its role="status" region is announced politely', () => {
    render(<Toaster />);
    act(() => {
      useToastStore.getState().push({ message: 'Heads up', severity: 'info' });
    });

    const tile = screen.getByTestId('toast');
    expect(tile).toHaveAttribute('role', 'status');
    expect(tile).toHaveAttribute('aria-live', 'polite');
    expect(tile).toHaveAttribute('aria-atomic', 'true');
  });

  it('Given a success toast, Then it is announced politely', () => {
    render(<Toaster />);
    act(() => {
      useToastStore.getState().push({ message: 'Saved', severity: 'success' });
    });

    const tile = screen.getByTestId('toast');
    expect(tile).toHaveAttribute('aria-live', 'polite');
    expect(tile).toHaveAttribute('aria-atomic', 'true');
  });

  it('Given an error toast, Then it interrupts with aria-live="assertive"', () => {
    render(<Toaster />);
    act(() => {
      useToastStore.getState().push({ message: 'Boom', severity: 'error' });
    });

    const tile = screen.getByTestId('toast');
    expect(tile).toHaveAttribute('role', 'status');
    expect(tile).toHaveAttribute('aria-live', 'assertive');
    expect(tile).toHaveAttribute('aria-atomic', 'true');
  });

  it('Given a toast with no severity, Then it defaults to polite', () => {
    render(<Toaster />);
    act(() => {
      useToastStore.getState().push({ message: 'Plain' });
    });

    const tile = screen.getByTestId('toast');
    expect(tile).toHaveAttribute('aria-live', 'polite');
  });
});
