import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';

// App publish / unpublish lifecycle UI.
//
// Gap: web/src/api/apps.ts exposes publishApp(rid) / unpublishApp(rid)
// but AppEditorPage only wired Save. This suite drives the publish
// lifecycle from the editor header:
//
//   - Publish is gated on a saved rid (no control for an unsaved draft).
//   - Clicking Publish POSTs publishApp(rid) and renders a published
//     badge "Published v{n} · {publishedAt}".
//   - Unpublish only appears once the App is published; clicking it
//     POSTs unpublishApp(rid) and clears the badge.
//   - In-flight + error states surface inline.

const apiMocks = vi.hoisted(() => ({
  listApps: vi.fn(),
  getApp: vi.fn(),
  createApp: vi.fn(),
  updateApp: vi.fn(),
  deleteApp: vi.fn(),
  listAppVersions: vi.fn(),
  rollbackApp: vi.fn(),
  publishApp: vi.fn(),
  unpublishApp: vi.fn(),
  viewApp: vi.fn(),
}));

vi.mock('../../../api/apps', () => apiMocks);

import { AppEditorPage } from '../AppEditorPage';

const liveLayout = {
  type: 'row',
  children: [
    {
      type: 'col',
      width: 12,
      child: { type: 'component', componentType: 'chart' },
    },
  ],
};

function baseApp(overrides: Record<string, unknown> = {}) {
  return {
    rid: 'ri.app.main.app.42',
    name: 'Console',
    ownerId: 'u1',
    layoutJson: liveLayout,
    version: 2,
    createdAt: '2026-05-03T00:00:00Z',
    updatedAt: '2026-05-04T00:00:00Z',
    ...overrides,
  };
}

beforeEach(() => {
  for (const m of Object.values(apiMocks)) m.mockReset();
  apiMocks.listApps.mockResolvedValue({ apps: [] });
  apiMocks.getApp.mockResolvedValue(baseApp());
  apiMocks.listAppVersions.mockResolvedValue({ versions: [] });
});

describe('App publish lifecycle — gating', () => {
  it('does not render the Publish button for an unsaved draft', () => {
    render(<AppEditorPage />);
    expect(screen.queryByTestId('app-publish')).toBeNull();
  });

  it('renders the Publish button once an rid is bound (saved App)', async () => {
    render(<AppEditorPage rid="ri.app.main.app.42" />);
    await waitFor(() => {
      expect(screen.getByTestId('app-publish')).toBeInTheDocument();
    });
  });

  it('does not render Unpublish until the App is published', async () => {
    render(<AppEditorPage rid="ri.app.main.app.42" />);
    await waitFor(() => {
      expect(screen.getByTestId('app-publish')).toBeInTheDocument();
    });
    expect(screen.queryByTestId('app-unpublish')).toBeNull();
    expect(screen.queryByTestId('app-published-badge')).toBeNull();
  });
});

describe('App publish lifecycle — publish', () => {
  it('clicking Publish calls publishApp with the rid and renders the published badge', async () => {
    apiMocks.publishApp.mockResolvedValue({
      rid: 'ri.app.main.app.42',
      name: 'Console',
      ownerId: 'u1',
      publishedVersion: 2,
      publishedAt: '2026-06-03T10:00:00Z',
      publishedBy: 'user:alice',
      layoutJson: liveLayout,
    });

    render(<AppEditorPage rid="ri.app.main.app.42" />);
    await waitFor(() => {
      expect(screen.getByTestId('app-publish')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('app-publish'));

    await waitFor(() => {
      expect(apiMocks.publishApp).toHaveBeenCalledWith('ri.app.main.app.42');
    });

    await waitFor(() => {
      const badge = screen.getByTestId('app-published-badge');
      expect(badge).toBeInTheDocument();
      expect(badge.textContent).toMatch(/Published v2/);
    });

    // Unpublish control now available; the published version is recorded
    // on the badge for E2E assertions.
    expect(screen.getByTestId('app-unpublish')).toBeInTheDocument();
    expect(
      screen.getByTestId('app-published-badge').getAttribute('data-version'),
    ).toBe('2');
  });

  it('surfaces a publish error inline and does not render the badge', async () => {
    apiMocks.publishApp.mockRejectedValue(new Error('publish denied'));

    render(<AppEditorPage rid="ri.app.main.app.42" />);
    await waitFor(() => {
      expect(screen.getByTestId('app-publish')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('app-publish'));

    await waitFor(() => {
      expect(screen.getByTestId('app-publish-error')).toBeInTheDocument();
    });
    expect(screen.getByTestId('app-publish-error').textContent).toMatch(
      /publish denied/i,
    );
    expect(screen.queryByTestId('app-published-badge')).toBeNull();
  });
});

describe('App publish lifecycle — already-published App', () => {
  it('shows the published badge + Unpublish for an App loaded already published', async () => {
    apiMocks.getApp.mockResolvedValue(
      baseApp({
        publishedVersion: 5,
        publishedAt: '2026-06-01T09:00:00Z',
        publishedBy: 'user:bob',
      }),
    );

    render(<AppEditorPage rid="ri.app.main.app.42" />);

    await waitFor(() => {
      const badge = screen.getByTestId('app-published-badge');
      expect(badge).toBeInTheDocument();
      expect(badge.textContent).toMatch(/Published v5/);
    });
    expect(screen.getByTestId('app-unpublish')).toBeInTheDocument();
  });

  it('clicking Unpublish calls unpublishApp and clears the badge', async () => {
    apiMocks.getApp.mockResolvedValue(
      baseApp({
        publishedVersion: 5,
        publishedAt: '2026-06-01T09:00:00Z',
        publishedBy: 'user:bob',
      }),
    );
    apiMocks.unpublishApp.mockResolvedValue(undefined);

    render(<AppEditorPage rid="ri.app.main.app.42" />);

    await waitFor(() => {
      expect(screen.getByTestId('app-unpublish')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('app-unpublish'));

    await waitFor(() => {
      expect(apiMocks.unpublishApp).toHaveBeenCalledWith('ri.app.main.app.42');
    });

    await waitFor(() => {
      expect(screen.queryByTestId('app-published-badge')).toBeNull();
    });
    // Back to the publishable (un-published) state.
    expect(screen.getByTestId('app-publish')).toBeInTheDocument();
    expect(screen.queryByTestId('app-unpublish')).toBeNull();
  });

  it('surfaces an unpublish error inline and keeps the badge', async () => {
    apiMocks.getApp.mockResolvedValue(
      baseApp({
        publishedVersion: 5,
        publishedAt: '2026-06-01T09:00:00Z',
        publishedBy: 'user:bob',
      }),
    );
    apiMocks.unpublishApp.mockRejectedValue(new Error('not the owner'));

    render(<AppEditorPage rid="ri.app.main.app.42" />);

    await waitFor(() => {
      expect(screen.getByTestId('app-unpublish')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId('app-unpublish'));

    await waitFor(() => {
      expect(screen.getByTestId('app-publish-error')).toBeInTheDocument();
    });
    expect(screen.getByTestId('app-publish-error').textContent).toMatch(
      /not the owner/i,
    );
    // Badge survives a failed unpublish.
    expect(screen.getByTestId('app-published-badge')).toBeInTheDocument();
  });
});
