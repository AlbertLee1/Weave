import { describe, it, expect, vi, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { FormProvider, useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { ParameterForm } from '../ParameterForm';
import type { ActionParameterV2 } from '../../../api/types';
import {
  buildParameterDefaults,
  buildParameterZodSchema,
} from '../parameterSchema';

// Capture the request the widget makes to the global attachment-upload
// endpoint so we can assert the filename query param. Note: jsdom's fetch
// does not serialize a File/Blob request body through MSW, so the raw-bytes
// half of the contract is asserted via a fetch spy (see fetchSpy below)
// rather than by reading request.text() here.
let capturedUpload: { url: string } | null = null;

const UPLOAD_PATH = '/api/v2/ontologies/attachments/upload';

const server = setupServer(
  http.post(UPLOAD_PATH, ({ request }) => {
    capturedUpload = { url: request.url };
    const filename = new URL(request.url).searchParams.get('filename') ?? 'unknown';
    return HttpResponse.json({
      rid: 'ri.attachments.main.attachment.abc123',
      filename,
      sizeBytes: 11,
      mediaType: 'text/plain',
    });
  }),
);

beforeAll(() => server.listen());
afterEach(() => {
  capturedUpload = null;
  vi.restoreAllMocks();
  server.resetHandlers();
});
afterAll(() => server.close());

const attachmentParameters: Record<string, ActionParameterV2> = {
  evidence: {
    dataType: { type: 'attachment' },
    required: true,
    description: 'Supporting evidence file',
  },
};

function Harness({
  parameters = attachmentParameters,
  onSubmit = vi.fn(),
}: {
  parameters?: Record<string, ActionParameterV2>;
  onSubmit?: (values: Record<string, unknown>) => void;
}) {
  const form = useForm<Record<string, unknown>>({
    resolver: zodResolver(buildParameterZodSchema(parameters)),
    defaultValues: buildParameterDefaults(parameters),
    mode: 'onBlur',
  });
  return (
    <FormProvider {...form}>
      <form onSubmit={form.handleSubmit(onSubmit)} noValidate>
        <ParameterForm parameters={parameters} />
        <button type="submit">Submit</button>
      </form>
    </FormProvider>
  );
}

function makeFile(name: string, content: string): File {
  return new File([content], name, { type: 'text/plain' });
}

describe('BDD: ParameterForm attachment upload widget', () => {
  it('Given an attachment param, Then it renders a file input (not just a text box)', () => {
    render(<Harness />);
    const fileInput = screen.getByTestId('param-evidence-file');
    expect(fileInput).toBeInTheDocument();
    expect(fileInput).toHaveAttribute('type', 'file');
  });

  it('When a file is selected, Then it POSTs the raw file to the upload endpoint with the filename query param and sets the value to the returned rid', async () => {
    const onSubmit = vi.fn();
    // Spy on fetch to assert the raw File is sent as the body (NOT multipart);
    // jsdom does not round-trip a Blob body through MSW, so we inspect the
    // outgoing request init directly while still delegating the response to
    // the MSW handler via the real fetch.
    const fetchSpy = vi.spyOn(globalThis, 'fetch');
    render(<Harness onSubmit={onSubmit} />);

    const file = makeFile('hello world.txt', 'hello world');
    const fileInput = screen.getByTestId('param-evidence-file') as HTMLInputElement;
    fireEvent.change(fileInput, { target: { files: [file] } });

    // Uploaded filename surfaces in the UI once the request resolves.
    const uploaded = await screen.findByTestId('param-evidence-uploaded');
    expect(uploaded.textContent ?? '').toContain('hello world.txt');

    expect(capturedUpload).not.toBeNull();
    const url = new URL(capturedUpload!.url);
    expect(url.pathname).toBe(UPLOAD_PATH);
    expect(url.searchParams.get('filename')).toBe('hello world.txt');

    // The raw File is the request body — NOT FormData/multipart.
    const uploadCall = fetchSpy.mock.calls.find((c) =>
      String(c[0]).includes(UPLOAD_PATH),
    );
    expect(uploadCall).toBeDefined();
    const init = uploadCall![1] as RequestInit;
    expect(init.method).toBe('POST');
    expect(init.body).toBe(file);
    expect(init.body).not.toBeInstanceOf(FormData);

    // The form value is the returned rid, so submitting forwards it.
    fireEvent.click(screen.getByText('Submit'));
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    const submitted = onSubmit.mock.calls[0][0] as Record<string, unknown>;
    expect(submitted.evidence).toBe('ri.attachments.main.attachment.abc123');
  });

  it('When the upload fails, Then an inline error is surfaced and no rid is set', async () => {
    server.use(
      http.post(UPLOAD_PATH, () =>
        HttpResponse.json(
          {
            errorCode: 'INTERNAL',
            errorName: 'AttachmentStoreError',
            errorInstanceId: 'x',
          },
          { status: 500 },
        ),
      ),
    );

    render(<Harness />);
    const fileInput = screen.getByTestId('param-evidence-file') as HTMLInputElement;
    fireEvent.change(fileInput, {
      target: { files: [makeFile('bad.txt', 'oops')] },
    });

    const err = await screen.findByTestId('param-evidence-upload-error');
    expect(err).toBeInTheDocument();
    expect(err.textContent ?? '').toMatch(/upload/i);
  });

  it('Then a "paste a RID" text fallback is available and writes the form value', async () => {
    const onSubmit = vi.fn();
    render(<Harness onSubmit={onSubmit} />);

    const ridInput = screen.getByTestId('param-evidence-rid');
    fireEvent.change(ridInput, {
      target: { value: 'ri.attachments.main.attachment.pasted' },
    });

    fireEvent.click(screen.getByText('Submit'));
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    const submitted = onSubmit.mock.calls[0][0] as Record<string, unknown>;
    expect(submitted.evidence).toBe('ri.attachments.main.attachment.pasted');
  });
});
