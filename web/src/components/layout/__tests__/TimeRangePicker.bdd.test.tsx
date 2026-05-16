import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route, useLocation } from 'react-router';
import { TimeRangePicker } from '../TimeRangePicker';

// US-482 BDD — TopBar 时间范围 → query 参数双向绑定.
//
// PRD acceptance: "前端 TopBar 时间范围 → query 参数双向绑定".
//
// Scenario coverage:
//   1. Given the URL carries no time-range params, When the picker
//      mounts on /quiver, Then the 24h preset is highlighted as the
//      effective default.
//   2. Given the URL carries ?step=5m, When the picker mounts, Then
//      the 24h preset is highlighted (URL → picker direction).
//   3. Given the picker is mounted, When the user clicks 7d, Then the
//      URL gains ?range=7d&step=30m&from=…&to=… with from = now-7d
//      (picker → URL direction).
//   4. Negative control: Given the page is NOT under /quiver, When the
//      picker would otherwise mount, Then nothing renders. This stops an
//      "always render" regression from quietly passing the URL-binding
//      assertions on every page.

function UrlSpy({ onChange }: { onChange: (search: string) => void }) {
  const loc = useLocation();
  onChange(loc.search);
  return null;
}

function renderAt(path: string, options?: { now?: () => number }) {
  let lastSearch = '';
  render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route
          path="/quiver/*"
          element={
            <>
              <TimeRangePicker now={options?.now} />
              <UrlSpy onChange={(s) => (lastSearch = s)} />
            </>
          }
        />
        <Route
          path="/other/*"
          element={
            <>
              <TimeRangePicker now={options?.now} />
              <UrlSpy onChange={(s) => (lastSearch = s)} />
            </>
          }
        />
      </Routes>
    </MemoryRouter>,
  );
  return { getLastSearch: () => lastSearch };
}

describe('US-482 BDD: TimeRangePicker URL ↔ picker binding', () => {
  it('Scenario 1: empty URL on /quiver → 24h preset highlighted as default', () => {
    renderAt('/quiver/ont');
    expect(screen.getByTestId('time-range-picker')).toBeInTheDocument();
    expect(screen.getByTestId('time-range-24h')).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByTestId('time-range-1h')).toHaveAttribute('aria-pressed', 'false');
    expect(screen.getByTestId('time-range-7d')).toHaveAttribute('aria-pressed', 'false');
    expect(screen.getByTestId('time-range-30d')).toHaveAttribute('aria-pressed', 'false');
  });

  it('Scenario 2 (URL → picker): ?range=7d on /quiver highlights the 7d preset', () => {
    renderAt('/quiver/ont?range=7d');
    expect(screen.getByTestId('time-range-7d')).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByTestId('time-range-24h')).toHaveAttribute('aria-pressed', 'false');
  });

  it('Scenario 3 (picker → URL): clicking 7d writes ?range=7d&step=30m&from&to to the URL', async () => {
    const NOW = new Date('2026-05-16T12:00:00.000Z').getTime();
    const { getLastSearch } = renderAt('/quiver/ont', { now: () => NOW });

    await userEvent.click(screen.getByTestId('time-range-7d'));

    const params = new URLSearchParams(getLastSearch());
    expect(params.get('range')).toBe('7d');
    expect(params.get('step')).toBe('30m');
    expect(params.get('to')).toBe('2026-05-16T12:00:00.000Z');
    expect(params.get('from')).toBe('2026-05-09T12:00:00.000Z');
  });

  it('Scenario 4 (negative control): off-route /other does NOT render the picker', () => {
    renderAt('/other/page');
    expect(screen.queryByTestId('time-range-picker')).toBeNull();
  });

  it('Scenario 5 (URL → picker via ?step alone): ?step=1m highlights the 1h preset', () => {
    renderAt('/quiver/ont?from=2026-05-16T11%3A00%3A00.000Z&to=2026-05-16T12%3A00%3A00.000Z&step=1m');
    expect(screen.getByTestId('time-range-1h')).toHaveAttribute('aria-pressed', 'true');
  });
});

