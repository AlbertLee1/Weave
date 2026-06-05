import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route, Link } from 'react-router';
import { RouteTitle } from '../components/common/RouteTitle';

// Render RouteTitle inside a MemoryRouter so it can read the current
// pathname via useLocation(). RouteTitle renders nothing visible — its
// only effect is `document.title`, which we assert directly. This gives
// us the WCAG 2.4.2 (Page Titled) contract: every primary route updates
// the browser tab / screen-reader page title rather than leaving the
// static index.html "Weave".
function renderAt(pathname: string) {
  return render(
    <MemoryRouter initialEntries={[pathname]}>
      <RouteTitle />
      <Routes>
        <Route path="*" element={null} />
      </Routes>
    </MemoryRouter>,
  );
}

afterEach(() => {
  cleanup();
  document.title = '';
});

describe('BDD: RouteTitle keeps document.title in sync with the route', () => {
  it.each([
    ['/', 'Dashboard · Weave'],
    ['/explorer/iotDemo', 'Explorer · Weave'],
    ['/explorer/iotDemo/Sensor', 'Explorer · Weave'],
    ['/browser/iotDemo/Sensor', 'Browser · Weave'],
    ['/actions/iotDemo', 'Actions · Weave'],
    ['/actions/iotDemo/history', 'Actions · Weave'],
    ['/aggregation/iotDemo/Sensor', 'Aggregation · Weave'],
    ['/admin/iotDemo/objectTypes', 'Admin · Weave'],
    ['/marketplace', 'Marketplace · Weave'],
    ['/quiver/iotDemo', 'Quiver · Weave'],
    ['/notifications', 'Notifications · Weave'],
    ['/settings', 'Settings · Weave'],
    ['/login', 'Sign in · Weave'],
    ['/vertex/some-rid', 'Vertex · Weave'],
  ])(
    'Given the user navigates to %s, Then document.title becomes "%s"',
    (pathname, expectedTitle) => {
      renderAt(pathname);
      expect(document.title).toBe(expectedTitle);
    },
  );

  it('Given an unmapped route, Then document.title falls back to plain "Weave"', () => {
    renderAt('/some/totally/unknown/path');
    expect(document.title).toBe('Weave');
  });

  it('Given the user navigates between routes, Then document.title updates on each navigation', async () => {
    const user = userEvent.setup();
    render(
      <MemoryRouter initialEntries={['/explorer/iotDemo']}>
        <RouteTitle />
        <Link to="/settings">Go to settings</Link>
      </MemoryRouter>,
    );
    expect(document.title).toBe('Explorer · Weave');

    await user.click(screen.getByRole('link', { name: /go to settings/i }));

    expect(document.title).toBe('Settings · Weave');
  });
});
