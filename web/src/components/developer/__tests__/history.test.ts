import { describe, it, expect, beforeEach } from 'vitest';
import { loadHistory, pushHistory, clearHistory, HISTORY_LIMIT } from '../history';

describe('playground history', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('round-trips an entry through localStorage', () => {
    pushHistory({
      id: '1',
      method: 'GET',
      url: '/foo',
      status: 200,
      durationMs: 12,
      timestamp: 1000,
    });
    expect(loadHistory()).toHaveLength(1);
    expect(loadHistory()[0].url).toBe('/foo');
  });

  it('keeps most-recent first and caps at HISTORY_LIMIT', () => {
    for (let i = 0; i < HISTORY_LIMIT + 10; i++) {
      pushHistory({
        id: String(i),
        method: 'GET',
        url: `/p/${i}`,
        status: 200,
        durationMs: 1,
        timestamp: i,
      });
    }
    const h = loadHistory();
    expect(h).toHaveLength(HISTORY_LIMIT);
    expect(h[0].url).toBe(`/p/${HISTORY_LIMIT + 9}`);
    expect(h[HISTORY_LIMIT - 1].url).toBe(`/p/${10}`);
  });

  it('returns [] when storage is empty or corrupt', () => {
    expect(loadHistory()).toEqual([]);
    localStorage.setItem('weave.playground.history', 'not json');
    expect(loadHistory()).toEqual([]);
  });

  it('clearHistory wipes stored entries', () => {
    pushHistory({ id: '1', method: 'GET', url: '/x', status: 200, durationMs: 0, timestamp: 0 });
    clearHistory();
    expect(loadHistory()).toEqual([]);
  });
});
