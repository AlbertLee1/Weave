import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { AuthProvider } from '../../../auth/AuthContext';
import { PermissionGate } from '../PermissionGate';

const server = setupServer();
beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

function renderWithMe(me: object) {
  server.use(http.get('/api/v2/me', () => HttpResponse.json(me)));
  return render(
    <AuthProvider>
      <PermissionGate permission="ontology.write">
        <button>Create Ontology</button>
      </PermissionGate>
    </AuthProvider>,
  );
}

describe('PermissionGate', () => {
  it('renders children enabled when user has permission', async () => {
    renderWithMe({
      id: 'admin',
      email: '',
      name: '',
      roles: ['admin'],
      ontologyRoles: {},
      permissions: ['ontology.write'],
    });
    await waitFor(() => {
      const btn = screen.getByRole('button', { name: 'Create Ontology' });
      expect(btn).not.toBeDisabled();
    });
  });

  it('disables children when user lacks permission', async () => {
    renderWithMe({
      id: 'viewer',
      email: '',
      name: '',
      roles: ['viewer'],
      ontologyRoles: {},
      permissions: ['ontology.read'],
    });
    await waitFor(() => {
      const btn = screen.getByRole('button', { name: 'Create Ontology' });
      expect(btn).toBeDisabled();
    });
    // Tooltip is exposed via title attribute on the wrapper for accessibility.
    const wrapper = screen.getByTestId('permission-gate');
    expect(wrapper).toHaveAttribute('title');
    expect(wrapper.getAttribute('title')).toMatch(/permission/i);
  });

  it('disables when user is null (loading or unauthenticated)', async () => {
    server.use(http.get('/api/v2/me', () => new HttpResponse(null, { status: 401 })));
    render(
      <AuthProvider>
        <PermissionGate permission="ontology.write">
          <button>Create Ontology</button>
        </PermissionGate>
      </AuthProvider>,
    );
    await waitFor(() => {
      const btn = screen.getByRole('button', { name: 'Create Ontology' });
      expect(btn).toBeDisabled();
    });
  });

  it('respects ontologyRid for scoped checks', async () => {
    server.use(
      http.get('/api/v2/me', () =>
        HttpResponse.json({
          id: 'carol',
          email: '',
          name: '',
          roles: ['viewer'],
          ontologyRoles: { 'ri.ontology.main.ontology.northwind': 'ontology-owner' },
          permissions: ['ontology.read', 'objectType.write'],
        }),
      ),
    );
    render(
      <AuthProvider>
        <PermissionGate permission="objectType.write" ontologyRid="ri.ontology.main.ontology.northwind">
          <button>Create Type</button>
        </PermissionGate>
        <PermissionGate permission="objectType.write" ontologyRid="ri.ontology.main.ontology.other">
          <button>Create Other Type</button>
        </PermissionGate>
      </AuthProvider>,
    );
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Create Type' })).not.toBeDisabled();
    });
    expect(screen.getByRole('button', { name: 'Create Other Type' })).toBeDisabled();
  });
});
