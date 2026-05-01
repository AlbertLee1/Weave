import { describe, it, expect, beforeEach } from 'vitest';
import {
  getItem,
  setItem,
  removeItem,
  clear,
  keys,
  __resetForTests,
} from '../offlineCache';

// jsdom does not implement IndexedDB, so these tests exercise the
// in-memory fallback path. The same surface contract applies to the
// IDB-backed path in a real browser.
describe('offlineCache (US-354)', () => {
  beforeEach(async () => {
    __resetForTests();
    await clear();
  });

  it('round-trips primitive values via setItem / getItem', async () => {
    await setItem('foo', 'bar');
    expect(await getItem<string>('foo')).toBe('bar');
  });

  it('round-trips object values structurally', async () => {
    await setItem('payload', { count: 3, items: ['a', 'b'] });
    const got = await getItem<{ count: number; items: string[] }>('payload');
    expect(got).toEqual({ count: 3, items: ['a', 'b'] });
  });

  it('returns null for missing keys', async () => {
    expect(await getItem('nope')).toBeNull();
  });

  it('removeItem deletes a single key without touching others', async () => {
    await setItem('a', 1);
    await setItem('b', 2);
    await removeItem('a');
    expect(await getItem('a')).toBeNull();
    expect(await getItem<number>('b')).toBe(2);
  });

  it('clear empties the store', async () => {
    await setItem('a', 1);
    await setItem('b', 2);
    await clear();
    expect(await keys()).toEqual([]);
  });

  it('keys returns every set key', async () => {
    await setItem('one', 1);
    await setItem('two', 2);
    const k = await keys();
    expect(k.sort()).toEqual(['one', 'two']);
  });
});
