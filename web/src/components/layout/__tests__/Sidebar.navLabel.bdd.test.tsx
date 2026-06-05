import { describe, it, expect, beforeAll, afterAll, afterEach, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { MemoryRouter } from 'react-router';
import { AuthProvider } from '../../../auth/AuthContext';
import { useOntologyStore } from '../../../stores/ontologyStore';
import { Sidebar } from '../Sidebar';

// BDD: the primary navigation landmark must carry an accessible name so that
// screen-reader landmark navigation can disambiguate it from the other
// <nav> landmarks present elsewhere in the app shell (Marketplace, per-page
// secondary navs, etc.). Multiple unnamed same-role landmarks are ambiguous.
const server = setupServer();
beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

beforeEach(() => {
  useOntologyStore.setState({
    selectedOntology: 'northwind',
    selectedObjectType: null,
    sidebarCollapsed: false,
    recentlyViewed: [],
  });
});

function renderSidebar() {
  server.use(http.get('/api/v2/me', () => new HttpResponse(null, { status: 401 })));
  return render(
    <MemoryRouter>
      <AuthProvider>
        <Sidebar />
      </AuthProvider>
    </MemoryRouter>,
  );
}

describe('BDD: Sidebar main navigation landmark', () => {
  it('exposes the primary nav as a named navigation landmark', async () => {
    // Given the app shell renders the Sidebar
    renderSidebar();
    await waitFor(() => {
      expect(screen.getByText('Dashboard')).toBeInTheDocument();
    });

    // Then the primary navigation landmark has an accessible name so screen
    // readers can distinguish it from other <nav> landmarks on the page.
    expect(
      screen.getByRole('navigation', { name: /main navigation/i }),
    ).toBeInTheDocument();
  });
});
