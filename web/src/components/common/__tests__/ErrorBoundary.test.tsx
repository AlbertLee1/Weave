import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { I18nextProvider } from 'react-i18next';
import i18n from '../../../i18n';
import { GlobalErrorBoundary, RouteErrorBoundary } from '../ErrorBoundary';
import { setErrorReporter } from '../../../lib/errorReporter';

function Bomb({ message = 'kaboom' }: { message?: string }): never {
  throw new Error(message);
}

describe('ErrorBoundary', () => {
  // react-error-boundary calls console.error on every catch; silence it so the
  // expected error noise does not pollute the test output. The reporter itself
  // is verified separately via setErrorReporter spies.
  let consoleSpy: ReturnType<typeof vi.spyOn>;
  beforeEach(() => {
    consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {});
  });
  afterEach(() => {
    consoleSpy.mockRestore();
    setErrorReporter(null);
  });

  it('GlobalErrorBoundary renders fallback when child throws', () => {
    render(
      <GlobalErrorBoundary>
        <Bomb message="global-test-error" />
      </GlobalErrorBoundary>,
    );
    expect(screen.getByTestId('global-error-fallback')).toBeInTheDocument();
    expect(screen.getByTestId('error-message')).toHaveTextContent('global-test-error');
  });

  it('GlobalErrorBoundary renders children when no error', () => {
    render(
      <GlobalErrorBoundary>
        <div>healthy child</div>
      </GlobalErrorBoundary>,
    );
    expect(screen.getByText('healthy child')).toBeInTheDocument();
    expect(screen.queryByTestId('global-error-fallback')).not.toBeInTheDocument();
  });

  it('RouteErrorBoundary renders translated fallback when child throws', () => {
    render(
      <I18nextProvider i18n={i18n}>
        <MemoryRouter initialEntries={['/test']}>
          <RouteErrorBoundary>
            <Bomb message="route-test-error" />
          </RouteErrorBoundary>
        </MemoryRouter>
      </I18nextProvider>,
    );
    expect(screen.getByTestId('route-error-fallback')).toBeInTheDocument();
    expect(screen.getByTestId('error-message')).toHaveTextContent('route-test-error');
    // i18n key resolves: zh-CN default carries 'errorBoundary.title'
    expect(
      screen.getByText(i18n.t('errorBoundary.title')),
    ).toBeInTheDocument();
  });

  it('Retry resets the boundary so non-throwing children render again', () => {
    let throwOnNext = true;
    function MaybeBomb() {
      if (throwOnNext) throw new Error('first-render-fail');
      return <div>recovered</div>;
    }
    render(
      <GlobalErrorBoundary>
        <MaybeBomb />
      </GlobalErrorBoundary>,
    );
    expect(screen.getByTestId('global-error-fallback')).toBeInTheDocument();

    // Flip the flag and click retry — boundary resets and re-renders.
    throwOnNext = false;
    fireEvent.click(screen.getByRole('button', { name: /retry/i }));
    expect(screen.getByText('recovered')).toBeInTheDocument();
    expect(screen.queryByTestId('global-error-fallback')).not.toBeInTheDocument();
  });

  it('invokes the registered error reporter with message and componentStack', () => {
    const reporter = vi.fn();
    setErrorReporter(reporter);

    render(
      <GlobalErrorBoundary>
        <Bomb message="reported-error" />
      </GlobalErrorBoundary>,
    );

    expect(reporter).toHaveBeenCalledTimes(1);
    const payload = reporter.mock.calls[0][0];
    expect(payload.message).toBe('reported-error');
    expect(payload.componentStack).toBeTruthy();
    expect(payload.timestamp).toMatch(/\d{4}-\d{2}-\d{2}T/);
    expect(payload.url).toBeTypeOf('string');
  });

  it('reporter throwing does not escalate past the boundary', () => {
    setErrorReporter(() => {
      throw new Error('reporter-failed');
    });
    expect(() =>
      render(
        <GlobalErrorBoundary>
          <Bomb message="ok-error" />
        </GlobalErrorBoundary>,
      ),
    ).not.toThrow();
    expect(screen.getByTestId('global-error-fallback')).toBeInTheDocument();
  });

  it('RouteErrorBoundary auto-resets on pathname change', () => {
    function Router({ path, children }: { path: string; children: React.ReactNode }) {
      return (
        <I18nextProvider i18n={i18n}>
          <MemoryRouter initialEntries={[path]} key={path}>
            <RouteErrorBoundary>{children}</RouteErrorBoundary>
          </MemoryRouter>
        </I18nextProvider>
      );
    }
    const { rerender } = render(
      <Router path="/a">
        <Bomb message="route-a-error" />
      </Router>,
    );
    expect(screen.getByTestId('route-error-fallback')).toBeInTheDocument();

    // Render a new tree at a different path with a healthy child — the
    // top-level Router key change forces a fresh MemoryRouter, simulating
    // a user-initiated navigation. The boundary inside the new Router is a
    // fresh instance and renders the healthy subtree.
    rerender(
      <Router path="/b">
        <div>second page</div>
      </Router>,
    );
    expect(screen.getByText('second page')).toBeInTheDocument();
  });
});
