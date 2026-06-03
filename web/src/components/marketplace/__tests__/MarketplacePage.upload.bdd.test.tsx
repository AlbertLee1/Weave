import {
  describe,
  it,
  expect,
  vi,
  beforeAll,
  afterAll,
  beforeEach,
  afterEach,
} from 'vitest';
import { createElement } from 'react';
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { MarketplacePage } from '../MarketplacePage';
import { useToastStore } from '../../../stores/toastStore';

// US-412 (UI surface): BDD coverage for installing a .weavepkg envelope by
// uploading it through the Installed tab's file picker. The handler at
// POST /api/v2/pkg/install accepts a JSON PackageInstallRequest
// ({manifest, ontology, migrations, onConflict}); the picker parses the
// selected file as JSON, extracts manifest + ontology, and forwards the
// operator's onConflict selection.

const server = setupServer();
beforeAll(() => server.listen({ onUnhandledRequest: 'bypass' }));
afterEach(() => {
  server.resetHandlers();
  vi.clearAllMocks();
  useToastStore.getState().clear();
});
afterAll(() => server.close());

function renderPage() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return render(
    createElement(
      QueryClientProvider,
      { client: qc },
      createElement(
        MemoryRouter,
        { initialEntries: ['/marketplace'] },
        createElement(MarketplacePage, null),
      ),
    ),
  );
}

// A valid .weavepkg.json envelope mirroring the JSON body the CLI POSTs.
const VALID_ENVELOPE = {
  manifest: {
    name: 'acme-crm',
    version: '2.3.0',
    author: 'ACME',
    license: 'Apache-2.0',
    description: 'ACME CRM ontology',
  },
  ontology: {
    ontology: { apiName: 'acmeCrm', displayName: 'ACME CRM' },
    objectTypes: [{ apiName: 'Account' }],
  },
  migrations: [{ filename: '000001_init.up.sql', content: 'U0VMRUNUIDE7' }],
};

function fileFromObject(name: string, obj: unknown): File {
  return new File([JSON.stringify(obj)], name, { type: 'application/json' });
}

describe('MarketplacePage upload install (US-412)', () => {
  beforeEach(() => {
    useToastStore.getState().clear();
  });

  it('Given a valid .weavepkg.json and onConflict=skip, When the operator picks the file and installs, Then POST /api/v2/pkg/install carries the parsed manifest/ontology/migrations + skip', async () => {
    const installBodies: unknown[] = [];
    let listCalls = 0;
    server.use(
      http.get('/api/v2/pkg', () => {
        listCalls++;
        if (listCalls === 1) return HttpResponse.json({ data: [] });
        return HttpResponse.json({
          data: [
            {
              id: 1,
              name: 'acme-crm',
              version: '2.3.0',
              ontology: 'acmeCrm',
              manifest: VALID_ENVELOPE.manifest,
              migrations: ['000001_init.up.sql'],
              enabled: true,
              installedAt: '2026-06-03T00:00:00Z',
              updatedAt: '2026-06-03T00:00:00Z',
            },
          ],
        });
      }),
      http.get('/api/v2/pkg/builtin', () => HttpResponse.json({ data: [] })),
      http.post('/api/v2/pkg/install', async ({ request }) => {
        installBodies.push(await request.json());
        return HttpResponse.json(
          {
            name: 'acme-crm',
            version: '2.3.0',
            ontology: 'acmeCrm',
            imported: { objectTypes: 1 },
            migrationsRan: 1,
            migrationsTotal: 1,
            message: 'package installed',
          },
          { status: 201 },
        );
      }),
    );

    renderPage();

    // The Installed tab header surfaces the upload affordance.
    await waitFor(() => {
      expect(screen.getByTestId('marketplace-upload-input')).toBeInTheDocument();
    });

    // Choose onConflict=skip.
    const select = screen.getByTestId(
      'marketplace-upload-onconflict',
    ) as HTMLSelectElement;
    await act(async () => {
      fireEvent.change(select, { target: { value: 'skip' } });
    });

    // Pick the .weavepkg.json file.
    const input = screen.getByTestId(
      'marketplace-upload-input',
    ) as HTMLInputElement;
    const file = fileFromObject('acme-crm.weavepkg.json', VALID_ENVELOPE);
    await act(async () => {
      fireEvent.change(input, { target: { files: [file] } });
    });

    // The selected filename is surfaced and the install button enables.
    await waitFor(() => {
      expect(
        screen.getByTestId('marketplace-upload-filename').textContent,
      ).toContain('acme-crm.weavepkg.json');
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('marketplace-upload-install'));
    });

    await waitFor(() => {
      expect(installBodies).toHaveLength(1);
    });
    expect(installBodies[0]).toEqual({
      manifest: VALID_ENVELOPE.manifest,
      ontology: VALID_ENVELOPE.ontology,
      migrations: VALID_ENVELOPE.migrations,
      onConflict: 'skip',
    });

    // Success toast surfaces and the catalog is refetched.
    await waitFor(() => {
      const toasts = useToastStore.getState().toasts;
      expect(
        toasts.some(
          (t) =>
            t.severity === 'success' &&
            t.message.toLowerCase().includes('installed'),
        ),
      ).toBe(true);
    });
  });

  it('Given a server 409 conflict, When installing the uploaded package, Then an error toast surfaces', async () => {
    server.use(
      http.get('/api/v2/pkg', () => HttpResponse.json({ data: [] })),
      http.get('/api/v2/pkg/builtin', () => HttpResponse.json({ data: [] })),
      http.post('/api/v2/pkg/install', () =>
        HttpResponse.json(
          {
            errorCode: 'CONFLICT',
            errorName: 'PackageConflict',
            errorInstanceId: 'x',
            parameters: { package: 'acme-crm', version: '2.3.0', conflicts: '[]' },
          },
          { status: 409 },
        ),
      ),
    );

    renderPage();
    await waitFor(() => {
      expect(screen.getByTestId('marketplace-upload-input')).toBeInTheDocument();
    });

    const input = screen.getByTestId(
      'marketplace-upload-input',
    ) as HTMLInputElement;
    await act(async () => {
      fireEvent.change(input, {
        target: { files: [fileFromObject('acme.weavepkg.json', VALID_ENVELOPE)] },
      });
    });
    await waitFor(() => {
      expect(
        screen.getByTestId('marketplace-upload-install'),
      ).not.toBeDisabled();
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId('marketplace-upload-install'));
    });

    await waitFor(() => {
      const toasts = useToastStore.getState().toasts;
      expect(
        toasts.some(
          (t) => t.severity === 'error' && t.message.includes('PackageConflict'),
        ),
      ).toBe(true);
    });
  });

  it('Given a binary .weavepkg ZIP archive, When the operator selects it, Then a parse error surfaces and no install is attempted', async () => {
    const installCalls: number[] = [];
    server.use(
      http.get('/api/v2/pkg', () => HttpResponse.json({ data: [] })),
      http.get('/api/v2/pkg/builtin', () => HttpResponse.json({ data: [] })),
      http.post('/api/v2/pkg/install', () => {
        installCalls.push(1);
        return HttpResponse.json({}, { status: 201 });
      }),
    );

    renderPage();
    await waitFor(() => {
      expect(screen.getByTestId('marketplace-upload-input')).toBeInTheDocument();
    });

    // A ZIP local-file-header magic (PK\x03\x04) — not client-parseable JSON.
    const zipBytes = new Uint8Array([0x50, 0x4b, 0x03, 0x04, 0x14, 0x00]);
    const zipFile = new File([zipBytes], 'acme.weavepkg', {
      type: 'application/octet-stream',
    });
    const input = screen.getByTestId(
      'marketplace-upload-input',
    ) as HTMLInputElement;
    await act(async () => {
      fireEvent.change(input, { target: { files: [zipFile] } });
    });

    await waitFor(() => {
      expect(
        screen.getByTestId('marketplace-upload-error'),
      ).toBeInTheDocument();
    });
    // Install button stays disabled because no envelope was parsed.
    expect(screen.getByTestId('marketplace-upload-install')).toBeDisabled();
    expect(installCalls).toHaveLength(0);
  });

  it('Given a JSON file missing manifest.version, When selected, Then a validation error surfaces and install stays disabled', async () => {
    server.use(
      http.get('/api/v2/pkg', () => HttpResponse.json({ data: [] })),
      http.get('/api/v2/pkg/builtin', () => HttpResponse.json({ data: [] })),
    );
    renderPage();
    await waitFor(() => {
      expect(screen.getByTestId('marketplace-upload-input')).toBeInTheDocument();
    });

    const bad = {
      manifest: { name: 'acme-crm' },
      ontology: { ontology: { apiName: 'acmeCrm' } },
    };
    const input = screen.getByTestId(
      'marketplace-upload-input',
    ) as HTMLInputElement;
    await act(async () => {
      fireEvent.change(input, {
        target: { files: [fileFromObject('bad.weavepkg.json', bad)] },
      });
    });

    await waitFor(() => {
      expect(screen.getByTestId('marketplace-upload-error')).toBeInTheDocument();
    });
    expect(screen.getByTestId('marketplace-upload-install')).toBeDisabled();
  });
});
