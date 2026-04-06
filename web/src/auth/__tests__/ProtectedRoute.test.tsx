import { describe, it, expect, beforeEach, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { MemoryRouter, Routes, Route } from 'react-router';
import { useAuthStore } from '../authStore';
import { AuthProvider } from '../AuthContext';
import { ProtectedRoute } from '../ProtectedRoute';

const server = setupServer();
beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

beforeEach(() => {
  useAuthStore.getState().clear();
});

function renderWithRoutes(initial: string) {
  return render(
    <MemoryRouter initialEntries={[initial]}>
      <AuthProvider>
        <Routes>
          <Route path="/login" element={<div>login-page</div>} />
          <Route
            path="/"
            element={
              <ProtectedRoute>
                <div>protected-content</div>
              </ProtectedRoute>
            }
          />
        </Routes>
      </AuthProvider>
    </MemoryRouter>,
  );
}

describe('ProtectedRoute', () => {
  it('redirects to /login when not authenticated', async () => {
    server.use(http.get('/api/v2/me', () => new HttpResponse(null, { status: 401 })));
    renderWithRoutes('/');
    await waitFor(() => {
      expect(screen.getByText('login-page')).toBeInTheDocument();
    });
  });

  it('renders children when authenticated', async () => {
    server.use(
      http.get('/api/v2/me', () =>
        HttpResponse.json({
          id: 'user:alice',
          email: 'alice@example.com',
          name: 'Alice',
          roles: ['editor'],
          ontologyRoles: {},
          permissions: ['ontology.read'],
        }),
      ),
    );
    renderWithRoutes('/');
    await waitFor(() => {
      expect(screen.getByText('protected-content')).toBeInTheDocument();
    });
  });
});
