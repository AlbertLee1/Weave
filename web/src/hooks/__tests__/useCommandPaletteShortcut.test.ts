import { describe, it, expect, vi } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { fireEvent } from '@testing-library/react';
import { useCommandPaletteShortcut } from '../useCommandPaletteShortcut';

describe('useCommandPaletteShortcut', () => {
  it('toggles when Cmd+K is pressed', () => {
    const onToggle = vi.fn();
    renderHook(() => useCommandPaletteShortcut(onToggle));
    act(() => {
      fireEvent.keyDown(document, { key: 'k', metaKey: true });
    });
    expect(onToggle).toHaveBeenCalledTimes(1);
  });

  it('toggles when Ctrl+K is pressed (non-Mac)', () => {
    const onToggle = vi.fn();
    renderHook(() => useCommandPaletteShortcut(onToggle));
    act(() => {
      fireEvent.keyDown(document, { key: 'k', ctrlKey: true });
    });
    expect(onToggle).toHaveBeenCalledTimes(1);
  });

  it('does not trigger on plain k', () => {
    const onToggle = vi.fn();
    renderHook(() => useCommandPaletteShortcut(onToggle));
    act(() => {
      fireEvent.keyDown(document, { key: 'k' });
    });
    expect(onToggle).not.toHaveBeenCalled();
  });

  it('does not trigger when meta+other is pressed', () => {
    const onToggle = vi.fn();
    renderHook(() => useCommandPaletteShortcut(onToggle));
    act(() => {
      fireEvent.keyDown(document, { key: 'p', metaKey: true });
    });
    expect(onToggle).not.toHaveBeenCalled();
  });

  it('cleans up listener on unmount', () => {
    const onToggle = vi.fn();
    const { unmount } = renderHook(() => useCommandPaletteShortcut(onToggle));
    unmount();
    act(() => {
      fireEvent.keyDown(document, { key: 'k', metaKey: true });
    });
    expect(onToggle).not.toHaveBeenCalled();
  });
});
