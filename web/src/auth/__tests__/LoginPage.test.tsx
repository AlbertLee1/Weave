import { describe, it, expect, beforeEach, beforeAll, afterAll, afterEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { MemoryRouter, Routes, Route } from 'react-router';
import { useAuthStore } from '../authStore';
import { LoginPage } from '../LoginPage';

const server = setupServer();
beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

beforeEach(() => {
  useAuthStore.getState().clear();
});

function renderLogin() {
  return render(
    <MemoryRouter initialEntries={['/login']}>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/" element={<div>home</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('LoginPage', () => {
  it('renders email and password fields and submit button', () => {
    renderLogin();
    expect(screen.getByLabelText(/email/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/password/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /sign in/i })).toBeInTheDocument();
  });

  it('successful login stores token and redirects home', async () => {
    server.use(
      http.post('/api/auth/login', async () =>
        HttpResponse.json({
          access_token: 'JWT.A',
          refresh_token: 'R',
          token_type: 'Bearer',
          expires_in: 900,
          user: { id: 'user:alice', email: 'alice@example.com', name: 'Alice', roles: ['editor'], ontologyRoles: {} },
        }),
      ),
    );

    renderLogin();
    await userEvent.type(screen.getByLabelText(/email/i), 'alice@example.com');
    await userEvent.type(screen.getByLabelText(/password/i), 'letmein');
    await userEvent.click(screen.getByRole('button', { name: /sign in/i }));

    await waitFor(() => {
      expect(screen.getByText('home')).toBeInTheDocument();
    });
    expect(useAuthStore.getState().accessToken).toBe('JWT.A');
  });

  it('shows generic error on 401', async () => {
    server.use(
      http.post('/api/auth/login', () =>
        new HttpResponse(
          JSON.stringify({ errorCode: 'UNAUTHORIZED', errorName: 'InvalidCredentials' }),
          { status: 401 },
        ),
      ),
    );

    renderLogin();
    await userEvent.type(screen.getByLabelText(/email/i), 'alice@example.com');
    await userEvent.type(screen.getByLabelText(/password/i), 'wrong');
    await userEvent.click(screen.getByRole('button', { name: /sign in/i }));

    await waitFor(() => {
      expect(screen.getByText(/invalid email or password/i)).toBeInTheDocument();
    });
    expect(useAuthStore.getState().accessToken).toBeNull();
  });

  it('disables submit while pending', async () => {
    let resolveFn: ((value: Response) => void) | null = null;
    server.use(
      http.post('/api/auth/login', () => {
        return new Promise<Response>((r) => {
          resolveFn = r as (v: Response) => void;
        });
      }),
    );

    renderLogin();
    await userEvent.type(screen.getByLabelText(/email/i), 'alice@example.com');
    await userEvent.type(screen.getByLabelText(/password/i), 'letmein');
    await userEvent.click(screen.getByRole('button', { name: /sign in/i }));

    const btn = screen.getByRole('button', { name: /signing in/i });
    expect(btn).toBeDisabled();
    if (resolveFn) {
      (resolveFn as (v: Response) => void)(
        HttpResponse.json({
          access_token: 'JWT.A',
          refresh_token: 'R',
          token_type: 'Bearer',
          expires_in: 900,
          user: { id: 'u', email: '', name: '', roles: [], ontologyRoles: {} },
        }),
      );
    }
  });

  it('does not submit if either field is empty', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch');
    renderLogin();
    await userEvent.click(screen.getByRole('button', { name: /sign in/i }));
    expect(fetchSpy).not.toHaveBeenCalled();
    fetchSpy.mockRestore();
  });
});
