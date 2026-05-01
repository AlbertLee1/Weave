import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import { OfflineIndicator } from '../OfflineIndicator';
import '../../../i18n';

function setOnLine(value: boolean) {
  Object.defineProperty(navigator, 'onLine', {
    configurable: true,
    get: () => value,
  });
}

describe('OfflineIndicator (US-354)', () => {
  beforeEach(() => {
    setOnLine(true);
  });
  afterEach(() => {
    vi.useRealTimers();
    setOnLine(true);
  });

  it('renders nothing while online', () => {
    setOnLine(true);
    render(<OfflineIndicator />);
    expect(screen.queryByTestId('offline-indicator')).not.toBeInTheDocument();
  });

  it('shows the offline banner when navigator.onLine is false', () => {
    setOnLine(false);
    render(<OfflineIndicator />);
    const banner = screen.getByTestId('offline-indicator');
    expect(banner).toBeInTheDocument();
    expect(banner).toHaveAttribute('data-state', 'offline');
    expect(banner).toHaveAttribute('role', 'status');
  });

  it('flips to a "reconnected" pulse when going offline → online, then unmounts', () => {
    vi.useFakeTimers();
    setOnLine(false);
    render(<OfflineIndicator />);
    expect(screen.getByTestId('offline-indicator')).toHaveAttribute(
      'data-state',
      'offline',
    );

    act(() => {
      setOnLine(true);
      window.dispatchEvent(new Event('online'));
    });
    expect(screen.getByTestId('offline-indicator')).toHaveAttribute(
      'data-state',
      'reconnected',
    );

    act(() => {
      vi.advanceTimersByTime(2500);
    });
    expect(screen.queryByTestId('offline-indicator')).not.toBeInTheDocument();
  });

  it('toggles back to offline when the network drops mid-session', () => {
    setOnLine(true);
    render(<OfflineIndicator />);
    expect(screen.queryByTestId('offline-indicator')).not.toBeInTheDocument();

    act(() => {
      setOnLine(false);
      window.dispatchEvent(new Event('offline'));
    });
    expect(screen.getByTestId('offline-indicator')).toHaveAttribute(
      'data-state',
      'offline',
    );
  });
});
