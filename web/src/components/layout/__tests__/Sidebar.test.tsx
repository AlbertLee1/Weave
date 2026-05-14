import { describe, it, expect, beforeAll, afterAll, afterEach, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { MemoryRouter, Route, Routes } from 'react-router';
import { AuthProvider } from '../../../auth/AuthContext';
import { useOntologyStore } from '../../../stores/ontologyStore';
import { Sidebar } from '../Sidebar';

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

function renderSidebar(me: object | null) {
  if (me === null) {
    server.use(http.get('/api/v2/me', () => new HttpResponse(null, { status: 401 })));
  } else {
    server.use(http.get('/api/v2/me', () => HttpResponse.json(me)));
  }
  return render(
    <MemoryRouter>
      <AuthProvider>
        <Sidebar />
      </AuthProvider>
    </MemoryRouter>,
  );
}

describe('Sidebar admin section', () => {
  it('shows the admin section for admin users', async () => {
    renderSidebar({
      id: 'admin',
      email: '',
      name: '',
      roles: ['admin'],
      ontologyRoles: {},
      permissions: ['ontology.write'],
    });
    await waitFor(() => {
      expect(screen.getByTestId('sidebar-admin-section')).toBeInTheDocument();
    });
    expect(screen.getByText('Object Types')).toBeInTheDocument();
    expect(screen.getByText('Link Types')).toBeInTheDocument();
    expect(screen.getByText('Action Types')).toBeInTheDocument();
    expect(screen.getByText('Interfaces')).toBeInTheDocument();
    expect(screen.getByText('Schema Graph')).toBeInTheDocument();
    expect(screen.getByText('History')).toBeInTheDocument();
  });

  it('shows the admin section for ontology-owner users', async () => {
    renderSidebar({
      id: 'owner',
      email: '',
      name: '',
      roles: ['ontology-owner'],
      ontologyRoles: {},
      permissions: ['ontology.write'],
    });
    await waitFor(() => {
      expect(screen.getByTestId('sidebar-admin-section')).toBeInTheDocument();
    });
  });

  it('shows the admin section for scoped ontology-owner via ontologyRoles', async () => {
    renderSidebar({
      id: 'carol',
      email: '',
      name: '',
      roles: ['viewer'],
      ontologyRoles: { 'ri.ontology.main.ontology.northwind': 'ontology-owner' },
      permissions: ['ontology.read', 'ontology.write'],
    });
    await waitFor(() => {
      expect(screen.getByTestId('sidebar-admin-section')).toBeInTheDocument();
    });
  });

  it('hides the admin section for viewers/editors', async () => {
    renderSidebar({
      id: 'editor',
      email: '',
      name: '',
      roles: ['editor'],
      ontologyRoles: {},
      permissions: ['ontology.read', 'object.write'],
    });
    await waitFor(() => {
      expect(screen.getByText('Dashboard')).toBeInTheDocument();
    });
    expect(screen.queryByTestId('sidebar-admin-section')).not.toBeInTheDocument();
  });

  it('hides the admin section when unauthenticated', async () => {
    renderSidebar(null);
    await waitFor(() => {
      expect(screen.getByText('Dashboard')).toBeInTheDocument();
    });
    expect(screen.queryByTestId('sidebar-admin-section')).not.toBeInTheDocument();
  });

  // Dogfood report #3 + #5: when no ontology is selected, Query Builder
  // and Quiver TS were falling back to "/" (Dashboard), producing dead links
  // and duplicate React keys. They should only appear once an ontology is
  // active.
  it('hides Query Builder and Quiver TS when no ontology is selected', async () => {
    useOntologyStore.setState({
      selectedOntology: null,
      selectedObjectType: null,
      sidebarCollapsed: false,
      recentlyViewed: [],
    });
    renderSidebar({
      id: 'viewer',
      email: '',
      name: '',
      roles: ['viewer'],
      ontologyRoles: {},
      permissions: ['ontology.read'],
    });
    await waitFor(() => {
      expect(screen.getByText('Dashboard')).toBeInTheDocument();
    });
    expect(screen.queryByText('Query Builder')).not.toBeInTheDocument();
    expect(screen.queryByText('Quiver TS')).not.toBeInTheDocument();
  });

  it('shows Query Builder and Quiver TS scoped to the active ontology', async () => {
    renderSidebar({
      id: 'viewer',
      email: '',
      name: '',
      roles: ['viewer'],
      ontologyRoles: {},
      permissions: ['ontology.read'],
    });
    await waitFor(() => {
      expect(screen.getByText('Query Builder')).toBeInTheDocument();
    });
    expect(screen.getByText('Query Builder').closest('a')).toHaveAttribute(
      'href',
      '/objectsets/northwind',
    );
    expect(screen.getByText('Quiver TS').closest('a')).toHaveAttribute(
      'href',
      '/quiver/northwind',
    );
  });

  it('uses unique React keys even when sidebar items share routes', async () => {
    // Spy on console.error to catch the "two children with the same key"
    // warning that the previous implementation emitted when activeOntology
    // was null.
    const errors: string[] = [];
    const original = console.error;
    console.error = (...args: unknown[]) => {
      errors.push(args.map(String).join(' '));
      original(...(args as Parameters<typeof console.error>));
    };
    useOntologyStore.setState({
      selectedOntology: null,
      selectedObjectType: null,
      sidebarCollapsed: false,
      recentlyViewed: [],
    });
    try {
      renderSidebar({
        id: 'viewer',
        email: '',
        name: '',
        roles: ['viewer'],
        ontologyRoles: {},
        permissions: ['ontology.read'],
      });
      await waitFor(() => {
        expect(screen.getByText('Dashboard')).toBeInTheDocument();
      });
    } finally {
      console.error = original;
    }
    const duplicateKeyWarnings = errors.filter((m) =>
      m.includes('two children with the same key'),
    );
    expect(duplicateKeyWarnings).toEqual([]);
  });

  // Dogfood Round 3 #2: /admin/datasets/:dataset/rollback uses :dataset
  // (not :ontology), so the sidebar fell back to global mode and dropped
  // Query Builder / Quiver TS / Object Types. The sidebar must accept
  // :dataset as an alias for the active ontology.
  it('treats :dataset route param as active ontology on dataset routes', async () => {
    // Wipe the store so selectedOntology cannot mask the bug.
    useOntologyStore.setState({
      selectedOntology: null,
      selectedObjectType: null,
      sidebarCollapsed: false,
      recentlyViewed: [],
    });
    server.use(
      http.get('/api/v2/me', () =>
        HttpResponse.json({
          id: 'admin',
          email: '',
          name: '',
          roles: ['admin'],
          ontologyRoles: {},
          permissions: ['ontology.write'],
        }),
      ),
    );
    render(
      <MemoryRouter initialEntries={['/admin/datasets/iotDemo/rollback']}>
        <AuthProvider>
          <Routes>
            <Route
              path="/admin/datasets/:dataset/rollback"
              element={<Sidebar />}
            />
          </Routes>
        </AuthProvider>
      </MemoryRouter>,
    );
    await waitFor(() => {
      expect(screen.getByText('Query Builder')).toBeInTheDocument();
    });
    expect(screen.getByText('Query Builder').closest('a')).toHaveAttribute(
      'href',
      '/objectsets/iotDemo',
    );
    expect(screen.getByText('Quiver TS').closest('a')).toHaveAttribute(
      'href',
      '/quiver/iotDemo',
    );
    expect(screen.getByText('Object Types').closest('a')).toHaveAttribute(
      'href',
      '/admin/iotDemo/objectTypes',
    );
  });

  it('admin links point to /admin/:ontology/{section}', async () => {
    renderSidebar({
      id: 'admin',
      email: '',
      name: '',
      roles: ['admin'],
      ontologyRoles: {},
      permissions: ['ontology.write'],
    });
    await waitFor(() => {
      expect(screen.getByTestId('sidebar-admin-section')).toBeInTheDocument();
    });
    expect(
      screen.getByText('Object Types').closest('a'),
    ).toHaveAttribute('href', '/admin/northwind/objectTypes');
    expect(screen.getByText('Link Types').closest('a')).toHaveAttribute(
      'href',
      '/admin/northwind/linkTypes',
    );
    expect(screen.getByText('Action Types').closest('a')).toHaveAttribute(
      'href',
      '/admin/northwind/actionTypes',
    );
    expect(screen.getByText('Interfaces').closest('a')).toHaveAttribute(
      'href',
      '/admin/northwind/interfaces',
    );
    expect(screen.getByText('Schema Graph').closest('a')).toHaveAttribute(
      'href',
      '/admin/northwind/graph',
    );
    expect(screen.getByText('History').closest('a')).toHaveAttribute(
      'href',
      '/admin/northwind/history',
    );
  });
});
