import { useState, type FormEvent } from 'react';
import { useNavigate } from 'react-router';
import { login } from './api';

/**
 * LoginPage renders the email + password form and posts to /api/auth/login.
 * On success it stores the access token (in authStore via login()) and
 * navigates to "/". On failure it shows a generic error message.
 */
export function LoginPage() {
  const navigate = useNavigate();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  async function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (!email.trim() || !password) {
      setError('Email and password are required');
      return;
    }
    setError(null);
    setPending(true);
    try {
      await login(email.trim(), password);
      navigate('/', { replace: true });
    } catch {
      setError('Invalid email or password');
    } finally {
      setPending(false);
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-slate-950 px-4">
      <form
        onSubmit={onSubmit}
        className="w-full max-w-sm space-y-5 rounded-2xl border border-white/10 bg-slate-900/80 p-8 shadow-2xl backdrop-blur"
      >
        <div className="space-y-1">
          <h1 className="text-xl font-semibold text-white">Sign in to Weave</h1>
          <p className="text-sm text-slate-400">Enter your email and password to continue.</p>
        </div>

        <label className="block space-y-1">
          <span className="text-xs font-medium uppercase tracking-wide text-slate-400">Email</span>
          <input
            id="login-email"
            type="email"
            autoComplete="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            className="w-full rounded-md border border-white/10 bg-slate-950 px-3 py-2 text-sm text-white focus:border-cyan-400 focus:outline-none"
            aria-label="email"
          />
        </label>

        <label className="block space-y-1">
          <span className="text-xs font-medium uppercase tracking-wide text-slate-400">Password</span>
          <input
            id="login-password"
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="w-full rounded-md border border-white/10 bg-slate-950 px-3 py-2 text-sm text-white focus:border-cyan-400 focus:outline-none"
            aria-label="password"
          />
        </label>

        {error && (
          <div role="alert" className="rounded-md border border-red-400/40 bg-red-500/10 px-3 py-2 text-sm text-red-200">
            {error}
          </div>
        )}

        <button
          type="submit"
          disabled={pending}
          className="w-full rounded-md bg-cyan-500 px-4 py-2 text-sm font-semibold text-slate-950 hover:bg-cyan-400 disabled:cursor-not-allowed disabled:opacity-60"
        >
          {pending ? 'Signing in…' : 'Sign in'}
        </button>
      </form>
    </div>
  );
}
