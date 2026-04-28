import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent, act } from '@testing-library/react';
import { Toaster } from '../Toaster';
import { useToastStore } from '../../../stores/toastStore';

describe('Toaster (US-319)', () => {
  beforeEach(() => {
    useToastStore.getState().clear();
  });
  afterEach(() => {
    vi.useRealTimers();
    useToastStore.getState().clear();
  });

  it('renders nothing when the queue is empty', () => {
    render(<Toaster />);
    expect(screen.queryByTestId('toaster')).not.toBeInTheDocument();
  });

  it('renders queued toasts with their message and severity styling', () => {
    render(<Toaster />);
    act(() => {
      useToastStore.getState().push({ message: 'Hello', severity: 'success' });
    });
    expect(screen.getByTestId('toaster')).toBeInTheDocument();
    expect(screen.getByText('Hello')).toBeInTheDocument();
  });

  it('renders an inline action button when actionLabel + onAction are set, and clicking it does NOT auto-dismiss', () => {
    const onAction = vi.fn();
    render(<Toaster />);
    act(() => {
      useToastStore.getState().push({
        message: 'Action executed',
        actionLabel: 'Undo',
        onAction,
        ttlMs: 0,
      });
    });

    const btn = screen.getByTestId('toast-action');
    expect(btn).toHaveTextContent('Undo');
    fireEvent.click(btn);
    expect(onAction).toHaveBeenCalledTimes(1);
    // The toast remains until the caller dismisses it.
    expect(screen.getByTestId('toast')).toBeInTheDocument();
  });

  it('auto-dismisses after the configured ttlMs (default 5000ms)', () => {
    vi.useFakeTimers();
    render(<Toaster />);
    act(() => {
      useToastStore.getState().push({ message: 'Briefly' });
    });
    expect(screen.getByText('Briefly')).toBeInTheDocument();
    act(() => {
      vi.advanceTimersByTime(5000);
    });
    expect(screen.queryByText('Briefly')).not.toBeInTheDocument();
  });

  it('does NOT auto-dismiss when ttlMs is 0', () => {
    vi.useFakeTimers();
    render(<Toaster />);
    act(() => {
      useToastStore.getState().push({ message: 'Sticky', ttlMs: 0 });
    });
    act(() => {
      vi.advanceTimersByTime(60_000);
    });
    expect(screen.getByText('Sticky')).toBeInTheDocument();
  });

  it('dismisses immediately when the × button is clicked', () => {
    render(<Toaster />);
    act(() => {
      useToastStore.getState().push({ message: 'Bye', ttlMs: 0 });
    });
    fireEvent.click(screen.getByTestId('toast-dismiss'));
    expect(screen.queryByText('Bye')).not.toBeInTheDocument();
  });
});
