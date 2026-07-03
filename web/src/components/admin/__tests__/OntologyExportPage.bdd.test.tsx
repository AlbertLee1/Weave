import {
  describe,
  it,
  expect,
  beforeAll,
  afterAll,
  afterEach,
  beforeEach,
  vi,
} from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { OntologyExportPage } from '../OntologyExportPage';
import { Toaster } from '../../common/Toaster';
import { useToastStore } from '../../../stores/toastStore';

// BDD: the Ontology Export & SDK page surfaces two per-ontology backend
// capabilities that previously had no UI trigger:
//   GET  /api/v2/ontologies/{o}/export          → ontology definition JSON
//   POST /api/v2/ontologies/{o}/sdkgen?lang=…    → generated client SDK (zip)
// Each scenario asserts externally observable behavior: the HTTP method/path/
// query the page emits, that a file download is actually triggered (we spy on
// the anchor click + object-URL plumbing jsdom doesn't implement), and that
// failures surface through the shared toast/describeApiError path.

interface RecordedCall {
  method: string;
  url: string;
}

const EXPORT_PAYLOAD = {
  ontology: { apiName: 'northwind', displayName: 'Northwind' },
  objectTypes: [{ apiName: 'Customer' }, { apiName: 'Order' }, { apiName: 'Product' }],
  linkTypes: [{ apiName: 'customerOrders' }],
  actionTypes: [{ apiName: 'createOrder' }, { apiName: 'shipOrder' }],
  interfaces: [],
  sharedProperties: [],
  valueTypes: [{ apiName: 'EmailAddress' }],
  typeGroups: [],
  functions: [],
  queryTypes: [],
};

let calls: RecordedCall[];
let failExport = false;
let failSdkgen = false;

const server = setupServer(
  http.get('/api/v2/ontologies/:ontology/export', ({ request, params }) => {
    calls.push({ method: 'GET', url: request.url });
    if (failExport) {
      return HttpResponse.json(
        {
          errorCode: 'NOT_FOUND',
          errorName: 'OntologyNotFound',
          errorInstanceId: 'x',
          parameters: { reason: `ontology "${String(params.ontology)}" not found` },
        },
        { status: 404 },
      );
    }
    return HttpResponse.json(EXPORT_PAYLOAD);
  }),

  http.post('/api/v2/ontologies/:ontology/sdkgen', ({ request }) => {
    calls.push({ method: 'POST', url: request.url });
    if (failSdkgen) {
      return HttpResponse.json(
        {
          errorCode: 'INVALID_ARGUMENT',
          errorName: 'UnsupportedSdkLanguage',
          errorInstanceId: 'x',
          parameters: { reason: 'lang not supported' },
        },
        { status: 400 },
      );
    }
    // Return the zip payload as a Uint8Array, not a jsdom Blob. undici's fetch
    // body extraction (Node 20 CI) calls `.stream()` on the body object, and a
    // jsdom Blob's stream interop throws `object.stream is not a function`
    // there — a TypedArray is a natively-supported BodyInit on every Node
    // version, and the app still reads it back via res.blob() unchanged.
    return new HttpResponse(new Uint8Array([0x50, 0x4b, 0x03, 0x04]), {
      status: 200,
      headers: {
        'Content-Type': 'application/zip',
        'Content-Disposition': 'attachment; filename="sdk.zip"',
      },
    });
  }),
);

// jsdom implements neither URL.createObjectURL nor a real anchor download, so
// we stub them and spy on the anchor click to prove a download was triggered.
let clickSpy: ReturnType<typeof vi.spyOn>;
let createdUrls: number;
let revokedUrls: number;

// We patch only the two static methods on the real URL constructor — replacing
// the whole global would drop the URL constructor itself and break fetch / MSW
// URL parsing for every relative request the page makes.
const origCreate = URL.createObjectURL;
const origRevoke = URL.revokeObjectURL;

beforeAll(() => {
  server.listen({ onUnhandledRequest: 'error' });
  URL.createObjectURL = vi.fn(() => {
    createdUrls += 1;
    return `blob:mock-${createdUrls}`;
  });
  URL.revokeObjectURL = vi.fn(() => {
    revokedUrls += 1;
  });
});
afterEach(() => {
  server.resetHandlers();
  useToastStore.getState().clear();
  clickSpy.mockRestore();
});
afterAll(() => {
  server.close();
  URL.createObjectURL = origCreate;
  URL.revokeObjectURL = origRevoke;
});

beforeEach(() => {
  calls = [];
  failExport = false;
  failSdkgen = false;
  createdUrls = 0;
  revokedUrls = 0;
  // Spy on the anchor click so the jsdom navigation no-op still records intent.
  clickSpy = vi
    .spyOn(HTMLAnchorElement.prototype, 'click')
    .mockImplementation(() => {});
});

function renderPage(ontology = 'northwind') {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchInterval: false },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[`/admin/${ontology}/export`]}>
        <Routes>
          <Route
            path="/admin/:ontology/export"
            element={<OntologyExportPage />}
          />
        </Routes>
        <Toaster />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('BDD: OntologyExportPage', () => {
  it('renders the heading and both capability sections', () => {
    renderPage();
    expect(
      screen.getByRole('heading', { level: 1, name: /Export & SDK/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByTestId('ontology-export-section'),
    ).toBeInTheDocument();
    expect(screen.getByTestId('ontology-sdk-section')).toBeInTheDocument();
  });

  it('clicking Export GETs the export endpoint and triggers a JSON download', async () => {
    const user = userEvent.setup();
    renderPage();

    await user.click(screen.getByTestId('ontology-export-btn'));

    await waitFor(() => {
      expect(calls.find((c) => c.method === 'GET')).toBeTruthy();
    });
    const get = calls.find((c) => c.method === 'GET')!;
    expect(get.url).toContain('/api/v2/ontologies/northwind/export');

    // A download was actually triggered.
    await waitFor(() => {
      expect(clickSpy).toHaveBeenCalled();
    });
    expect(createdUrls).toBeGreaterThanOrEqual(1);
    expect(revokedUrls).toBeGreaterThanOrEqual(1);

    // The success summary surfaces the per-collection counts.
    const summary = await screen.findByTestId('ontology-export-summary');
    expect(within(summary).getByText(/objectTypes/i)).toBeInTheDocument();
    expect(summary.textContent).toContain('3'); // 3 object types
  });

  it('surfaces an error toast when the export fails', async () => {
    failExport = true;
    const user = userEvent.setup();
    renderPage();

    await user.click(screen.getByTestId('ontology-export-btn'));

    const toaster = await screen.findByTestId('toaster');
    await within(toaster).findByText(/OntologyNotFound/i);
    // No download happened.
    expect(clickSpy).not.toHaveBeenCalled();
  });

  it('selecting a language and clicking Generate POSTs sdkgen with ?lang= and downloads the zip', async () => {
    const user = userEvent.setup();
    renderPage();

    const select = screen.getByTestId('ontology-sdk-lang-select') as HTMLSelectElement;
    await user.selectOptions(select, 'python');

    await user.click(screen.getByTestId('ontology-sdk-generate-btn'));

    await waitFor(() => {
      expect(calls.find((c) => c.method === 'POST')).toBeTruthy();
    });
    const post = calls.find((c) => c.method === 'POST')!;
    expect(post.url).toContain('/api/v2/ontologies/northwind/sdkgen');
    expect(post.url).toContain('lang=python');

    await waitFor(() => {
      expect(clickSpy).toHaveBeenCalled();
    });
    expect(createdUrls).toBeGreaterThanOrEqual(1);
  });

  it('surfaces an error toast when sdkgen fails', async () => {
    failSdkgen = true;
    const user = userEvent.setup();
    renderPage();

    await user.click(screen.getByTestId('ontology-sdk-generate-btn'));

    const toaster = await screen.findByTestId('toaster');
    await within(toaster).findByText(/UnsupportedSdkLanguage/i);
    expect(clickSpy).not.toHaveBeenCalled();
  });
});
