import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { MemoryRouter, Routes, Route } from 'react-router';
import { AuthProvider } from '../AuthContext';
import { PermissionRoute } from '../PermissionRoute';

const server = setupServer();
beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

function renderRoute(me: object | null, path = '/admin/northwind/objectTypes') {
  if (me === null) {
    server.use(http.get('/api/v2/me', () => new HttpResponse(null, { status: 401 })));
  } else {
    server.use(http.get('/api/v2/me', () => HttpResponse.json(me)));
  }
  return render(
    <MemoryRouter initialEntries={[path]}>
      <AuthProvider>
        <Routes>
          <Route
            path="/admin/:ontology/objectTypes"
            element={
              <PermissionRoute permission="ontology.write" scopeToOntologyParam>
                <div>admin-content</div>
              </PermissionRoute>
            }
          />
        </Routes>
      </AuthProvider>
    </MemoryRouter>,
  );
}

describe('PermissionRoute', () => {
  it('renders the child page when the user has the global permission', async () => {
    renderRoute({
      id: 'admin',
      email: '',
      name: '',
      roles: ['admin'],
      ontologyRoles: {},
      permissions: ['ontology.write'],
    });
    await waitFor(() => {
      expect(screen.getByText('admin-content')).toBeInTheDocument();
    });
  });

  it('renders access-denied when the user lacks the permission', async () => {
    renderRoute({
      id: 'viewer',
      email: '',
      name: '',
      roles: ['viewer'],
      ontologyRoles: {},
      permissions: ['ontology.read'],
    });
    await waitFor(() => {
      expect(screen.getByTestId('permission-route-denied')).toBeInTheDocument();
    });
    expect(screen.queryByText('admin-content')).not.toBeInTheDocument();
  });

  it('renders access-denied when the user is unauthenticated', async () => {
    renderRoute(null);
    await waitFor(() => {
      expect(screen.getByTestId('permission-route-denied')).toBeInTheDocument();
    });
  });

  it('allows a scoped ontology-owner for the matching ontology param', async () => {
    renderRoute({
      id: 'carol',
      email: '',
      name: '',
      roles: ['viewer'],
      ontologyRoles: { 'ri.ontology.main.ontology.northwind': 'ontology-owner' },
      permissions: ['ontology.read', 'ontology.write'],
    });
    await waitFor(() => {
      expect(screen.getByText('admin-content')).toBeInTheDocument();
    });
  });

  it('denies a scoped ontology-owner on a different ontology param', async () => {
    renderRoute(
      {
        id: 'carol',
        email: '',
        name: '',
        roles: ['viewer'],
        ontologyRoles: { 'ri.ontology.main.ontology.chinook': 'ontology-owner' },
        permissions: ['ontology.read', 'ontology.write'],
      },
      '/admin/northwind/objectTypes',
    );
    await waitFor(() => {
      expect(screen.getByTestId('permission-route-denied')).toBeInTheDocument();
    });
  });
});
