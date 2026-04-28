import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createElement } from 'react';
import {
  render,
  screen,
  fireEvent,
  waitFor,
  act,
} from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MediaUploadZone } from '../MediaUploadZone';
import * as mediaApi from '../../../api/media';

function makeWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  return ({ children }: { children: React.ReactNode }) =>
    createElement(QueryClientProvider, { client: queryClient }, children);
}

function renderWithProviders(ui: React.ReactElement) {
  const Wrapper = makeWrapper();
  return render(ui, { wrapper: Wrapper });
}

// react-dropzone triggers file dialogs on click; in tests we pipe files
// through the hidden input element's change event directly. This keeps the
// tests free of DataTransfer polyfills that jsdom lacks.
function dropFilesOnInput(input: HTMLInputElement, files: File[]) {
  Object.defineProperty(input, 'files', {
    value: files,
    configurable: true,
  });
  fireEvent.change(input);
}

describe('MediaUploadZone', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders the dropzone placeholder with byte-size hint', () => {
    renderWithProviders(
      <MediaUploadZone propertyName="avatar" values={[]} />,
    );
    expect(screen.getByText('avatar')).toBeInTheDocument();
    expect(
      screen.getByText(/将文件拖到此处或点击选择/),
    ).toBeInTheDocument();
  });

  it('uploads a dropped file and reports back via onChange', async () => {
    const uploadSpy = vi
      .spyOn(mediaApi, 'uploadMedia')
      .mockResolvedValue({
        rid: 'ri.media.main.asset.abc',
        realm: 'main',
        filename: 'hello.txt',
        mime: 'text/plain',
        sizeBytes: 5,
        sha256: 'deadbeef',
        path: 'main/2026/04/deadbeef',
        createdAt: '2026-04-18T00:00:00Z',
      });

    const onChange = vi.fn();
    renderWithProviders(
      <MediaUploadZone
        propertyName="avatar"
        values={[]}
        onChange={onChange}
      />,
    );

    const input = screen.getByTestId('dropzone-input') as HTMLInputElement;
    const file = new File(['hello'], 'hello.txt', { type: 'text/plain' });

    await act(async () => {
      dropFilesOnInput(input, [file]);
    });

    await waitFor(() =>
      expect(onChange).toHaveBeenCalledWith(['ri.media.main.asset.abc']),
    );
    expect(uploadSpy).toHaveBeenCalledTimes(1);
    expect(uploadSpy.mock.calls[0][0]).toBe(file);
  });

  it('shows an in-flight progress row while upload is pending', async () => {
    let progressCb: ((p: { loaded: number; total: number }) => void) | undefined;
    vi.spyOn(mediaApi, 'uploadMedia').mockImplementation(
      (_file, opts): Promise<mediaApi.MediaAsset> => {
        progressCb = opts?.onProgress;
        // Never resolve during this test — we just want to observe progress UI.
        return new Promise(() => {});
      },
    );

    renderWithProviders(
      <MediaUploadZone propertyName="avatar" values={[]} />,
    );

    const input = screen.getByTestId('dropzone-input') as HTMLInputElement;
    const file = new File(['x'.repeat(200)], 'big.bin', {
      type: 'application/octet-stream',
    });
    await act(async () => {
      dropFilesOnInput(input, [file]);
    });

    expect(screen.getByTestId('upload-list')).toBeInTheDocument();
    expect(screen.getByText('big.bin')).toBeInTheDocument();

    await act(async () => {
      progressCb?.({ loaded: 100, total: 200 });
    });
    expect(screen.getByText('50%')).toBeInTheDocument();
  });

  it('renders thumbnails for existing values', () => {
    renderWithProviders(
      <MediaUploadZone
        propertyName="images"
        multiple
        values={['ri.media.main.asset.one', 'ri.media.main.asset.two']}
        knownAssets={{
          'ri.media.main.asset.one': {
            rid: 'ri.media.main.asset.one',
            realm: 'main',
            filename: 'first.png',
            mime: 'image/png',
            sizeBytes: 1,
            sha256: 'a',
            path: 'p1',
            createdAt: '2026-04-18T00:00:00Z',
          },
          'ri.media.main.asset.two': {
            rid: 'ri.media.main.asset.two',
            realm: 'main',
            filename: 'notes.txt',
            mime: 'text/plain',
            sizeBytes: 1,
            sha256: 'b',
            path: 'p2',
            createdAt: '2026-04-18T00:00:00Z',
          },
        }}
      />,
    );
    const list = screen.getByTestId('media-thumbnails');
    expect(list.querySelectorAll('li').length).toBe(2);
    // Image mime renders <img>, non-image falls back to a generic file icon.
    const images = list.querySelectorAll('img');
    expect(images.length).toBe(1);
    expect(images[0]).toHaveAttribute('alt', 'first.png');
  });

  it('cancels an in-flight upload via the cancel button', async () => {
    let lastSignal: AbortSignal | undefined;
    let rejectFn: ((reason?: unknown) => void) | undefined;
    vi.spyOn(mediaApi, 'uploadMedia').mockImplementation(
      (_file, opts): Promise<mediaApi.MediaAsset> => {
        lastSignal = opts?.signal;
        return new Promise<mediaApi.MediaAsset>((_resolve, reject) => {
          rejectFn = reject;
          opts?.signal?.addEventListener('abort', () =>
            reject(new DOMException('Aborted', 'AbortError')),
          );
        });
      },
    );

    const onChange = vi.fn();
    renderWithProviders(
      <MediaUploadZone propertyName="avatar" values={[]} onChange={onChange} />,
    );

    const input = screen.getByTestId('dropzone-input') as HTMLInputElement;
    const file = new File(['x'.repeat(50)], 'cancel-me.bin', {
      type: 'application/octet-stream',
    });
    await act(async () => {
      dropFilesOnInput(input, [file]);
    });

    expect(screen.getByText('cancel-me.bin')).toBeInTheDocument();
    const cancelBtn = screen.getByRole('button', {
      name: /Cancel cancel-me\.bin/,
    });

    await act(async () => {
      fireEvent.click(cancelBtn);
    });

    expect(lastSignal?.aborted).toBe(true);
    // The aborted promise resolves the mock too so we don't leak a pending one.
    rejectFn?.(new DOMException('Aborted', 'AbortError'));

    await waitFor(() =>
      expect(screen.queryByText('cancel-me.bin')).not.toBeInTheDocument(),
    );
    expect(onChange).not.toHaveBeenCalled();
  });

  it('opens a confirmation dialog before deleting', async () => {
    const deleteSpy = vi.spyOn(mediaApi, 'deleteMedia').mockResolvedValue();
    const onChange = vi.fn();

    renderWithProviders(
      <MediaUploadZone
        propertyName="doc"
        values={['ri.media.main.asset.one']}
        onChange={onChange}
      />,
    );

    fireEvent.click(
      screen.getByRole('button', { name: /Delete ri\.media\.main\.asset\.one/ }),
    );
    expect(screen.getByText('删除媒体文件？')).toBeInTheDocument();

    // Cancel keeps the row.
    fireEvent.click(screen.getByRole('button', { name: '取消' }));
    expect(deleteSpy).not.toHaveBeenCalled();
    expect(onChange).not.toHaveBeenCalled();

    // Re-open and confirm.
    fireEvent.click(
      screen.getByRole('button', { name: /Delete ri\.media\.main\.asset\.one/ }),
    );
    fireEvent.click(screen.getByRole('button', { name: '确认删除' }));

    await waitFor(() =>
      expect(deleteSpy).toHaveBeenCalledWith('ri.media.main.asset.one'),
    );
    await waitFor(() => expect(onChange).toHaveBeenCalledWith([]));
  });
});
