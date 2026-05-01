// Single source of truth for all global keyboard shortcuts.
//
// Each entry pairs a stable `id` (used by call sites and i18n) with a
// react-hotkeys-hook key pattern. `i18nKey` resolves to a human-readable
// description; the help panel (US-349) renders entries grouped by `group`.
//
// Sequences use `>` as the separator (e.g. `g>d` → press G then D).
// Comma-separated alternatives bind multiple key patterns to one handler
// (e.g. `meta+k, ctrl+k` covers Cmd on macOS and Ctrl elsewhere).

export type HotkeyGroup = 'global' | 'navigation';

export type HotkeyId =
  | 'commandPalette'
  | 'showHelp'
  | 'goDashboard'
  | 'goObjectsets'
  | 'goPipelines'
  | 'goApprovals';

export interface HotkeyDef {
  readonly id: HotkeyId;
  readonly keys: string;
  readonly i18nKey: string;
  readonly group: HotkeyGroup;
}

export const HOTKEYS: readonly HotkeyDef[] = [
  {
    id: 'commandPalette',
    keys: 'meta+k, ctrl+k',
    i18nKey: 'hotkeys.commandPalette',
    group: 'global',
  },
  {
    // `?` is Shift+/ on US/EN layouts. react-hotkeys-hook matches the
    // event's keyboard `code` rather than `key`, so bind to `shift+slash`
    // explicitly — `?` as a key string would only match if useKey:true
    // were set, which we don't expose on the shared hook.
    id: 'showHelp',
    keys: 'shift+slash',
    i18nKey: 'hotkeys.help',
    group: 'global',
  },
  {
    id: 'goDashboard',
    keys: 'g>d',
    i18nKey: 'hotkeys.goDashboard',
    group: 'navigation',
  },
  {
    id: 'goObjectsets',
    keys: 'g>o',
    i18nKey: 'hotkeys.goObjectsets',
    group: 'navigation',
  },
  {
    id: 'goPipelines',
    keys: 'g>p',
    i18nKey: 'hotkeys.goPipelines',
    group: 'navigation',
  },
  {
    id: 'goApprovals',
    keys: 'g>a',
    i18nKey: 'hotkeys.goApprovals',
    group: 'navigation',
  },
];

export function getHotkey(id: HotkeyId): HotkeyDef {
  const def = HOTKEYS.find((h) => h.id === id);
  if (!def) {
    throw new Error(`Unknown hotkey id: ${id}`);
  }
  return def;
}
