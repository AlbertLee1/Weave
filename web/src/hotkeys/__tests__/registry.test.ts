import { describe, expect, it } from 'vitest';
import { HOTKEYS, getHotkey } from '../registry';

describe('hotkey registry', () => {
  it('every entry has an id, key pattern, i18nKey and group', () => {
    for (const def of HOTKEYS) {
      expect(def.id).toBeTruthy();
      expect(def.keys).toBeTruthy();
      expect(def.i18nKey).toMatch(/^hotkeys\./);
      expect(['global', 'navigation']).toContain(def.group);
    }
  });

  it('ids are unique', () => {
    const ids = HOTKEYS.map((h) => h.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  it('command palette is bound to meta+k and ctrl+k', () => {
    const def = getHotkey('commandPalette');
    expect(def.keys).toContain('meta+k');
    expect(def.keys).toContain('ctrl+k');
    expect(def.group).toBe('global');
  });

  it('getHotkey throws on unknown id', () => {
    expect(() =>
      // @ts-expect-error — exercising the runtime guard with an invalid id
      getHotkey('definitelyNotAShortcut'),
    ).toThrow(/Unknown hotkey id/);
  });
});
