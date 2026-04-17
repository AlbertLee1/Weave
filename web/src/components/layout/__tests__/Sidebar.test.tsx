import { describe, it, expect, beforeAll, afterAll, afterEach, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { MemoryRouter } from 'react-router';
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
