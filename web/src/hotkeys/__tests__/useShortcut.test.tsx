import { describe, expect, it, vi } from 'vitest';
import { act, fireEvent, renderHook } from '@testing-library/react';
import { useShortcut } from '../useShortcut';

// Sanity-check the wiring between the registry and react-hotkeys-hook by
// firing keyboard events on document and asserting the registered handler
// runs. Sequence keys (g>d) need two consecutive presses; the library
// listens for them with a short timeout.

describe('useShortcut', () => {
  it('fires on meta+k for the command palette shortcut', () => {
    const onTrigger = vi.fn();
    renderHook(() => useShortcut('commandPalette', onTrigger));
    act(() => {
      fireEvent.keyDown(document, { key: 'k', code: 'KeyK', metaKey: true });
    });
    expect(onTrigger).toHaveBeenCalledTimes(1);
  });

  it('fires on ctrl+k for non-Mac users', () => {
    const onTrigger = vi.fn();
    renderHook(() => useShortcut('commandPalette', onTrigger));
    act(() => {
      fireEvent.keyDown(document, { key: 'k', code: 'KeyK', ctrlKey: true });
    });
    expect(onTrigger).toHaveBeenCalledTimes(1);
  });

  it('does not fire on bare k', () => {
    const onTrigger = vi.fn();
    renderHook(() => useShortcut('commandPalette', onTrigger));
    act(() => {
      fireEvent.keyDown(document, { key: 'k', code: 'KeyK' });
    });
    expect(onTrigger).not.toHaveBeenCalled();
  });

  it('respects the enabled flag', () => {
    const onTrigger = vi.fn();
    const { rerender } = renderHook(
      ({ enabled }: { enabled: boolean }) =>
        useShortcut('commandPalette', onTrigger, { enabled }),
      { initialProps: { enabled: false } },
    );
    act(() => {
      fireEvent.keyDown(document, { key: 'k', code: 'KeyK', metaKey: true });
    });
    expect(onTrigger).not.toHaveBeenCalled();

    rerender({ enabled: true });
    act(() => {
      fireEvent.keyDown(document, { key: 'k', code: 'KeyK', metaKey: true });
    });
    expect(onTrigger).toHaveBeenCalledTimes(1);
  });

  it('cleans up on unmount', () => {
    const onTrigger = vi.fn();
    const { unmount } = renderHook(() =>
      useShortcut('commandPalette', onTrigger),
    );
    unmount();
    act(() => {
      fireEvent.keyDown(document, { key: 'k', code: 'KeyK', metaKey: true });
    });
    expect(onTrigger).not.toHaveBeenCalled();
  });

  it('fires for the g>d navigation sequence', () => {
    const onTrigger = vi.fn();
    renderHook(() => useShortcut('goDashboard', onTrigger));
    act(() => {
      fireEvent.keyDown(document, { key: 'g', code: 'KeyG' });
      fireEvent.keyDown(document, { key: 'd', code: 'KeyD' });
    });
    expect(onTrigger).toHaveBeenCalledTimes(1);
  });
});
