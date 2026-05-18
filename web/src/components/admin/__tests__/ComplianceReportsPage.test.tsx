// US-453: SPA-side audit report console — exercise the report-type selector,
// time-window inputs, format choice, and download trigger against a mocked
// authedFetch. The page wires POST /api/admin/compliance/report (SOC2 / control
// evidence — JSON / HTML / PDF) and POST /api/admin/gdpr/export (GDPR data
// portability — ZIP).
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ComplianceReportsPage } from '../ComplianceReportsPage';

interface FetchCall {
  url: string;
  method: string;
  body: string | null;
  headers: Record<string, string>;
}

function installFetch(
  responder: (call: FetchCall) => Response | Promise<Response>,
) {
  const calls: FetchCall[] = [];
  const fetchSpy = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const url = typeof input === 'string' ? input : input.toString();
      const method = (init?.method ?? 'GET').toUpperCase();
      const body =
        typeof init?.body === 'string'
          ? init.body
          : init?.body == null
            ? null
            : String(init.body);
      const headersOut: Record<string, string> = {};
      if (init?.headers) {
        const h = new Headers(init.headers);
        h.forEach((v, k) => {
          headersOut[k.toLowerCase()] = v;
        });
      }
      const call: FetchCall = { url, method, body, headers: headersOut };
      calls.push(call);
      return responder(call);
    },
  );
  vi.stubGlobal('fetch', fetchSpy);
  return calls;
}

function streamlessBinaryResponse(
  bytes: Uint8Array,
  contentType: string,
  disposition: string,
): Response {
  const body = new ArrayBuffer(bytes.byteLength);
  new Uint8Array(body).set(bytes);
  const response = new Response(body, {
    status: 200,
    headers: {
      'Content-Type': contentType,
      'Content-Disposition': disposition,
    },
  });
  vi.spyOn(response, 'blob').mockRejectedValueOnce(
    new TypeError('object.stream is not a function'),
  );
  return response;
}

interface DownloadRecord {
  filename: string;
  size: number;
  type: string;
}

function captureDownloads(): DownloadRecord[] {
  const records: DownloadRecord[] = [];
  // Override URL.createObjectURL/revokeObjectURL — jsdom doesn't implement
  // them. We simultaneously sniff the synthesised <a> click to capture the
  // download filename + body.
  let nextId = 0;
  const objectUrls = new Map<string, Blob>();
  vi.spyOn(URL, 'createObjectURL').mockImplementation(
    (obj: Blob | MediaSource) => {
      nextId += 1;
      const url = `blob:test-${nextId}`;
      if (obj instanceof Blob) {
        objectUrls.set(url, obj);
      }
      return url;
    },
  );
  vi.spyOn(URL, 'revokeObjectURL').mockImplementation((url: string) => {
    objectUrls.delete(url);
  });
  const origCreate = document.createElement.bind(document);
  vi.spyOn(document, 'createElement').mockImplementation((tag: string) => {
    const el = origCreate(tag) as HTMLElement;
    if (tag.toLowerCase() === 'a') {
      const a = el as HTMLAnchorElement;
      a.click = () => {
        const url = a.href;
        const blob = objectUrls.get(url);
        if (blob) {
          records.push({
            filename: a.download,
            size: blob.size,
            type: blob.type,
          });
        }
      };
    }
    return el;
  });
  return records;
}

function renderPage() {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchInterval: false },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/admin/compliance']}>
        <Routes>
          <Route path="/admin/compliance" element={<ComplianceReportsPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('ComplianceReportsPage (US-453)', () => {
  beforeEach(() => {});
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('renders heading, report-type selector, time-window inputs, and the download CTA', () => {
    installFetch(() => new Response('{}', { status: 200 }));
    renderPage();
    expect(
      screen.getByRole('heading', { name: /compliance reports/i }),
    ).toBeInTheDocument();
    expect(screen.getByLabelText(/report type/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/format/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/^from/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/^to/i)).toBeInTheDocument();
    expect(
      screen.getByRole('button', { name: /generate.*download/i }),
    ).toBeInTheDocument();
  });

  it('SOC2 PDF: posts the report with format=pdf and downloads the response body', async () => {
    const calls = installFetch(async (call) => {
      if (call.url.endsWith('/api/admin/compliance/report')) {
        return streamlessBinaryResponse(
          new Uint8Array([0x25, 0x50, 0x44, 0x46]),
          'application/pdf',
          'attachment; filename="weave-compliance-report.pdf"',
        );
      }
      return new Response('{}', { status: 200 });
    });
    const downloads = captureDownloads();
    const user = userEvent.setup();
    renderPage();

    await user.selectOptions(screen.getByLabelText(/format/i), 'pdf');
    await user.type(screen.getByLabelText(/^from/i), '2026-01-01');
    await user.type(screen.getByLabelText(/^to/i), '2026-04-30');
    await user.click(
      screen.getByRole('button', { name: /generate.*download/i }),
    );

    await waitFor(() => {
      expect(downloads.length).toBe(1);
    });
    expect(downloads[0].filename).toMatch(/\.pdf$/);
    expect(downloads[0].type).toBe('application/pdf');
    expect(screen.queryByText(/object\.stream/i)).not.toBeInTheDocument();
    const reportCall = calls.find((c) =>
      c.url.endsWith('/api/admin/compliance/report'),
    );
    expect(reportCall).toBeDefined();
    expect(reportCall!.method).toBe('POST');
    expect(reportCall!.headers['content-type']).toContain('application/json');
    const body = JSON.parse(reportCall!.body ?? '{}');
    expect(body.format).toBe('pdf');
    // The form's `<input type=date>` value `2026-01-01` should be widened to
    // an RFC3339 instant when shipped to the server (the backend rejects
    // bare YYYY-MM-DD).
    expect(body.from).toMatch(/^2026-01-01T/);
    expect(body.to).toMatch(/^2026-04-30T/);
  });

  it('SOC2 JSON: parses the JSON body as a Blob and downloads .json', async () => {
    installFetch(
      () =>
        new Response(JSON.stringify({ generatedAt: '2026-04-30T00:00:00Z' }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
    );
    const downloads = captureDownloads();
    const user = userEvent.setup();
    renderPage();
    await user.selectOptions(screen.getByLabelText(/format/i), 'json');
    await user.click(
      screen.getByRole('button', { name: /generate.*download/i }),
    );
    await waitFor(() => {
      expect(downloads.length).toBe(1);
    });
    expect(downloads[0].filename).toMatch(/\.json$/);
  });

  it('GDPR: switching report type to gdpr replaces the time window with a userId input and submits a ZIP request', async () => {
    const calls = installFetch(async (call) => {
      if (call.url.endsWith('/api/admin/gdpr/export')) {
        return streamlessBinaryResponse(
          new Uint8Array([0x50, 0x4b, 0x03, 0x04]),
          'application/zip',
          'attachment; filename="gdpr-export-alice.zip"',
        );
      }
      return new Response('{}', { status: 200 });
    });
    const downloads = captureDownloads();
    const user = userEvent.setup();
    renderPage();

    await user.selectOptions(screen.getByLabelText(/report type/i), 'gdpr');

    // Time-window inputs must hide; userId must surface.
    expect(screen.queryByLabelText(/^from/i)).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/^to/i)).not.toBeInTheDocument();
    expect(screen.getByLabelText(/user id/i)).toBeInTheDocument();

    await user.type(screen.getByLabelText(/user id/i), 'alice');
    await user.click(
      screen.getByRole('button', { name: /generate.*download/i }),
    );

    await waitFor(() => {
      expect(downloads.length).toBe(1);
    });
    expect(downloads[0].filename).toMatch(/\.zip$/);
    expect(downloads[0].type).toBe('application/zip');
    expect(screen.queryByText(/object\.stream/i)).not.toBeInTheDocument();
    const exportCall = calls.find((c) =>
      c.url.endsWith('/api/admin/gdpr/export'),
    );
    expect(exportCall).toBeDefined();
    expect(exportCall!.method).toBe('POST');
    expect(JSON.parse(exportCall!.body ?? '{}').userId).toBe('alice');
  });

  it('renders an inline error when the API responds with a structured 4xx', async () => {
    installFetch(
      () =>
        new Response(
          JSON.stringify({
            errorCode: 'INVALID_PARAMETER',
            errorName: 'InvalidWindow',
            errorInstanceId: 'x',
            parameters: { reason: 'to must be greater than or equal to from' },
          }),
          {
            status: 400,
            headers: { 'Content-Type': 'application/json' },
          },
        ),
    );
    captureDownloads();
    const user = userEvent.setup();
    renderPage();
    await user.click(
      screen.getByRole('button', { name: /generate.*download/i }),
    );
    expect(
      await screen.findByText(/InvalidWindow|to must be greater/i),
    ).toBeInTheDocument();
  });

  it('rejects "to before from" without firing a request', async () => {
    const calls = installFetch(() => new Response('{}', { status: 200 }));
    captureDownloads();
    const user = userEvent.setup();
    renderPage();
    await user.type(screen.getByLabelText(/^from/i), '2026-04-30');
    await user.type(screen.getByLabelText(/^to/i), '2026-01-01');
    await user.click(
      screen.getByRole('button', { name: /generate.*download/i }),
    );
    expect(
      await screen.findByText(/must be on or after/i),
    ).toBeInTheDocument();
    const reportCalls = calls.filter((c) =>
      c.url.endsWith('/api/admin/compliance/report'),
    );
    expect(reportCalls.length).toBe(0);
  });
});
