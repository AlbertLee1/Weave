import { useHotkeys } from 'react-hotkeys-hook';
import { getHotkey, type HotkeyId } from './registry';

export interface UseShortcutOptions {
  // Disable the binding without unmounting the caller. Useful when the
  // target navigation requires context that may be absent (e.g. an
  // ontology-scoped route before an ontology is selected).
  enabled?: boolean;
}

// Bind a global handler to the key pattern declared for `id` in the
// registry. Suppresses default behaviour and skips when typing into form
// fields. One useShortcut call per shortcut keeps the rules-of-hooks
// invariant trivial — no dynamic loops over a handler map.
//
// react-hotkeys-hook only re-subscribes when its key/options identity
// changes; toggling `options.enabled` after mount does NOT detach the
// listener. Gate inside the callback closure (via the dependency array)
// so a parent that flips `enabled` between renders is honoured.
export function useShortcut(
  id: HotkeyId,
  handler: () => void,
  options?: UseShortcutOptions,
): void {
  const def = getHotkey(id);
  const enabled = options?.enabled ?? true;
  useHotkeys(
    def.keys,
    (e) => {
      if (!enabled) return;
      e.preventDefault();
      handler();
    },
    {
      enableOnFormTags: false,
      preventDefault: true,
    },
    [handler, enabled],
  );
}
