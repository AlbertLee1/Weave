import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  generateComplianceReport,
  generateGDPRExport,
} from '../compliance';

function installFetch(response: Response) {
  const fetchSpy = vi.fn(async () => response);
  vi.stubGlobal('fetch', fetchSpy);
  return fetchSpy;
}

describe('compliance API downloads', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('decodes RFC 5987 attachment filenames from compliance report responses', async () => {
    installFetch(
      new Response('pdf', {
        status: 200,
        headers: {
          'Content-Type': 'application/pdf',
          'Content-Disposition':
            "attachment; filename*=UTF-8''control%20evidence.pdf",
        },
      }),
    );

    const result = await generateComplianceReport({ format: 'pdf' });

    expect(result.filename).toBe('control evidence.pdf');
  });

  it('preserves quoted attachment filenames from GDPR export responses', async () => {
    installFetch(
      new Response('zip', {
        status: 200,
        headers: {
          'Content-Type': 'application/zip',
          'Content-Disposition': 'attachment; filename="gdpr-export-alice.zip"',
        },
      }),
    );

    const result = await generateGDPRExport('alice');

    expect(result.filename).toBe('gdpr-export-alice.zip');
  });

  it('falls back to deterministic names when attachment filenames are malformed', async () => {
    installFetch(
      new Response(JSON.stringify({ generatedAt: '2026-04-30T00:00:00Z' }), {
        status: 200,
        headers: {
          'Content-Type': 'application/json',
          'Content-Disposition': "attachment; filename*=UTF-8''%E0%A4%A",
        },
      }),
    );

    const result = await generateComplianceReport({ format: 'json' });

    expect(result.filename).toBe('weave-compliance-report.json');
  });
});
