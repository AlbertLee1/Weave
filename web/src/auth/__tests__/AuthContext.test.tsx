import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { AuthProvider } from '../AuthContext';
import { useAuth } from '../useAuth';

const server = setupServer();

beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

function Probe() {
  const { user, loading, can } = useAuth();
  if (loading) return <div>loading</div>;
  if (!user) return <div>no-user</div>;
  return (
    <div>
      <span data-testid="user-id">{user.id}</span>
      <span data-testid="roles">{user.roles.join(',')}</span>
      <span data-testid="can-write">{can('ontology.write') ? 'yes' : 'no'}</span>
      <span data-testid="can-read">{can('ontology.read') ? 'yes' : 'no'}</span>
    </div>
  );
}

describe('AuthContext + useAuth', () => {
  it('fetches /api/v2/me on mount and exposes user', async () => {
    server.use(
      http.get('/api/v2/me', () =>
        HttpResponse.json({
          id: 'alice',
          email: 'alice@example.com',
          name: 'Alice',
          roles: ['editor'],
          ontologyRoles: {},
          permissions: ['ontology.read', 'action.execute'],
        }),
      ),
    );

    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>,
    );

    await waitFor(() => expect(screen.getByTestId('user-id')).toHaveTextContent('alice'));
    expect(screen.getByTestId('roles')).toHaveTextContent('editor');
    expect(screen.getByTestId('can-read')).toHaveTextContent('yes');
    expect(screen.getByTestId('can-write')).toHaveTextContent('no');
  });

  it('admin sees all permissions including write', async () => {
    server.use(
      http.get('/api/v2/me', () =>
        HttpResponse.json({
          id: 'root',
          email: 'root@example.com',
          name: 'Root',
          roles: ['admin'],
          ontologyRoles: {},
          permissions: ['ontology.read', 'ontology.write', 'securityPolicy.manage'],
        }),
      ),
    );

    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>,
    );

    await waitFor(() => expect(screen.getByTestId('user-id')).toHaveTextContent('root'));
    expect(screen.getByTestId('can-write')).toHaveTextContent('yes');
  });

  it('handles fetch failure gracefully (shows no-user)', async () => {
    server.use(http.get('/api/v2/me', () => new HttpResponse(null, { status: 401 })));

    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>,
    );

    await waitFor(() => expect(screen.getByText('no-user')).toBeInTheDocument());
  });

  it('canOnOntology checks scoped roles', async () => {
    server.use(
      http.get('/api/v2/me', () =>
        HttpResponse.json({
          id: 'carol',
          email: 'carol@example.com',
          name: 'Carol',
          roles: ['viewer'],
          ontologyRoles: { 'ri.ontology.main.ontology.northwind': 'ontology-owner' },
          permissions: [
            'ontology.read',
            'ontology.write',
            'objectType.write',
          ],
        }),
      ),
    );

    function ScopedProbe() {
      const { canOnOntology, loading } = useAuth();
      if (loading) return <div>loading</div>;
      return (
        <div>
          <span data-testid="northwind">
            {canOnOntology('ri.ontology.main.ontology.northwind', 'objectType.write') ? 'yes' : 'no'}
          </span>
          <span data-testid="other">
            {canOnOntology('ri.ontology.main.ontology.other', 'objectType.write') ? 'yes' : 'no'}
          </span>
        </div>
      );
    }

    render(
      <AuthProvider>
        <ScopedProbe />
      </AuthProvider>,
    );

    await waitFor(() => expect(screen.getByTestId('northwind')).toHaveTextContent('yes'));
    expect(screen.getByTestId('other')).toHaveTextContent('no');
  });
});
