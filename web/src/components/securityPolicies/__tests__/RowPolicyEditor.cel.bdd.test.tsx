import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { SecurityPoliciesPage } from '../SecurityPoliciesPage';

// US-487 (Unit 7): Row policy CEL expression editor.
//
// Backend pkg/rls accepts `celExpression` as an alternative to the legacy
// JSON `predicate` (RowPolicy.Validate requires at least one of the two).
// These BDD scenarios drive the editor from the UI: toggling to CEL mode
// and submitting must POST `celExpression` (not `predicate`); a malformed
// CEL expression must surface a lint error via the shared
// lintCellExpression validator before any request is made.

let capturedCreateBody: Record<string, unknown> | null = null;

const OBJECT_TYPE_RID = 'ri.ontology.main.object-type.order';

const server = setupServer(
  http.get('/api/v2/ontologies/:ontology/objectTypes', () =>
    HttpResponse.json({
      data: [
        {
          rid: OBJECT_TYPE_RID,
          apiName: 'order',
          displayName: 'Order',
          status: 'ACTIVE',
          visibility: 'PROMINENT',
        },
      ],
    }),
  ),
  http.get('/api/admin/row-policies', () =>
    HttpResponse.json({ policies: [] }),
  ),
  http.post('/api/admin/row-policies', async ({ request }) => {
    capturedCreateBody = (await request.json()) as Record<string, unknown>;
    return HttpResponse.json({
      rid: 'ri.rls.row-policy.cel-1',
      objectTypeRid: OBJECT_TYPE_RID,
      celExpression: capturedCreateBody.celExpression ?? '',
      predicate: capturedCreateBody.predicate ?? undefined,
      appliesTo: capturedCreateBody.appliesTo ?? {},
      description: capturedCreateBody.description ?? '',
    });
  }),
);

beforeAll(() => server.listen());
afterEach(() => {
  capturedCreateBody = null;
  server.resetHandlers();
});
afterAll(() => server.close());

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/admin/northwind/security']}>
        <Routes>
          <Route
            path="/admin/:ontology/security"
            element={<SecurityPoliciesPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('BDD: RowPolicyEditor CEL expression mode (US-487)', () => {
  it('Given CEL mode, When a valid expression is submitted, Then the POST body carries celExpression and omits predicate', async () => {
    renderPage();

    fireEvent.click(await screen.findByTestId('row-policies-create-btn'));
    const editor = await screen.findByTestId('row-policy-editor');
    expect(editor).toBeTruthy();

    // Switch from the default JSON predicate mode into CEL mode.
    fireEvent.click(screen.getByTestId('row-policy-editor-mode-cel'));

    fireEvent.change(screen.getByTestId('row-policy-editor-cel'), {
      target: { value: 'user.roles.exists(r, r == "finance")' },
    });

    fireEvent.click(screen.getByTestId('row-policy-editor-submit-btn'));

    await waitFor(() => {
      expect(capturedCreateBody).not.toBeNull();
    });

    expect(capturedCreateBody).toMatchObject({
      objectTypeRid: OBJECT_TYPE_RID,
      celExpression: 'user.roles.exists(r, r == "finance")',
    });
    // predicate must NOT be present when CEL mode is active.
    expect(
      Object.prototype.hasOwnProperty.call(capturedCreateBody, 'predicate'),
    ).toBe(false);
  });

  it('Given JSON mode (default), When a valid predicate is submitted, Then the POST body carries predicate and omits celExpression', async () => {
    renderPage();

    fireEvent.click(await screen.findByTestId('row-policies-create-btn'));
    await screen.findByTestId('row-policy-editor');

    // Default mode is JSON predicate; the textarea is prefilled with a
    // valid where-clause sample, so just submit.
    fireEvent.click(screen.getByTestId('row-policy-editor-submit-btn'));

    await waitFor(() => {
      expect(capturedCreateBody).not.toBeNull();
    });

    expect(
      Object.prototype.hasOwnProperty.call(capturedCreateBody, 'predicate'),
    ).toBe(true);
    expect(
      Object.prototype.hasOwnProperty.call(capturedCreateBody, 'celExpression'),
    ).toBe(false);
  });

  it('Given CEL mode, When the expression is malformed, Then a lint error is shown and no request is made', async () => {
    renderPage();

    fireEvent.click(await screen.findByTestId('row-policies-create-btn'));
    await screen.findByTestId('row-policy-editor');

    fireEvent.click(screen.getByTestId('row-policy-editor-mode-cel'));

    // Unbalanced parentheses — lintCellExpression must reject this.
    fireEvent.change(screen.getByTestId('row-policy-editor-cel'), {
      target: { value: 'user.roles.exists(r, r == "finance"' },
    });

    // Lint error surfaces on change.
    const err = await screen.findByTestId('row-policy-editor-cel-error');
    expect(err.textContent).toMatch(/parenthes/i);

    // Submit must be blocked — no network call.
    fireEvent.click(screen.getByTestId('row-policy-editor-submit-btn'));

    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(capturedCreateBody).toBeNull();
  });

  it('Given CEL mode, When the expression is empty, Then submit is blocked (mirrors backend Validate)', async () => {
    renderPage();

    fireEvent.click(await screen.findByTestId('row-policies-create-btn'));
    await screen.findByTestId('row-policy-editor');

    fireEvent.click(screen.getByTestId('row-policy-editor-mode-cel'));
    fireEvent.change(screen.getByTestId('row-policy-editor-cel'), {
      target: { value: '   ' },
    });

    fireEvent.click(screen.getByTestId('row-policy-editor-submit-btn'));

    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(capturedCreateBody).toBeNull();
    expect(screen.getByTestId('row-policy-editor-cel-error')).toBeTruthy();
  });
});
