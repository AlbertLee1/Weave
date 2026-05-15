import { describe, expect, it } from 'vitest';
import { HOTKEYS, getHotkey } from '../registry';

describe('hotkey registry', () => {
  it('every entry has an id, key pattern, i18nKey and group', () => {
    for (const def of HOTKEYS) {
      expect(def.id).toBeTruthy();
      expect(def.keys).toBeTruthy();
      expect(def.i18nKey).toMatch(/^hotkeys\./);
      // US-458: groups normalised to PRD's Navigation / Editing / Search.
      expect(['navigation', 'editing', 'search']).toContain(def.group);
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
    expect(def.group).toBe('search');
  });

  it('US-458: each PRD group has at least one entry', () => {
    const groups = new Set(HOTKEYS.map((h) => h.group));
    expect(groups.has('navigation')).toBe(true);
    expect(groups.has('editing')).toBe(true);
    expect(groups.has('search')).toBe(true);
  });

  it('VTX-120: saveGraph is bound to meta+s and ctrl+s', () => {
    const def = getHotkey('saveGraph');
    expect(def.keys).toContain('meta+s');
    expect(def.keys).toContain('ctrl+s');
    expect(def.group).toBe('editing');
  });

  it('VTX-120: runScenario is bound to meta+enter and ctrl+enter', () => {
    const def = getHotkey('runScenario');
    expect(def.keys).toContain('meta+enter');
    expect(def.keys).toContain('ctrl+enter');
    expect(def.group).toBe('editing');
  });

  it('getHotkey throws on unknown id', () => {
    expect(() =>
      // @ts-expect-error — exercising the runtime guard with an invalid id
      getHotkey('definitelyNotAShortcut'),
    ).toThrow(/Unknown hotkey id/);
  });
});
