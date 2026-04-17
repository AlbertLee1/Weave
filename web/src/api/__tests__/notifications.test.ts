import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { listNotifications, markNotificationRead } from '../notifications';

const server = setupServer();
beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

const SAMPLE = [
  {
    id: 'ri.notification.main.notification.1',
    userId: 'dev-user',
    title: 'Inventory low',
    body: 'Widget stock below 10',
    type: 'automate.alert',
    link: '/browser/northwind/Product',
    read: false,
    createdAt: '2026-04-18T11:55:00Z',
  },
];

describe('notifications API', () => {
  it('listNotifications() GETs all notifications by default', async () => {
    let capturedUrl = '';
    server.use(
      http.get('/api/v2/notifications', ({ request: req }) => {
        capturedUrl = req.url;
        return HttpResponse.json({ data: SAMPLE });
      }),
    );
    const res = await listNotifications();
    expect(res.data).toHaveLength(1);
    expect(res.data[0].title).toBe('Inventory low');
    expect(capturedUrl).not.toContain('unread=');
  });

  it('listNotifications({ unreadOnly: true }) adds ?unread=true', async () => {
    let capturedUrl = '';
    server.use(
      http.get('/api/v2/notifications', ({ request: req }) => {
        capturedUrl = req.url;
        return HttpResponse.json({ data: SAMPLE });
      }),
    );
    await listNotifications({ unreadOnly: true });
    expect(capturedUrl).toContain('unread=true');
  });

  it('listNotifications({ unreadOnly: false }) omits the unread param', async () => {
    let capturedUrl = '';
    server.use(
      http.get('/api/v2/notifications', ({ request: req }) => {
        capturedUrl = req.url;
        return HttpResponse.json({ data: [] });
      }),
    );
    await listNotifications({ unreadOnly: false });
    expect(capturedUrl).not.toContain('unread=');
  });

  it('markNotificationRead() POSTs to the read endpoint and resolves on 204', async () => {
    let capturedUrl = '';
    server.use(
      http.post('/api/v2/notifications/:id/read', ({ request: req, params }) => {
        capturedUrl = new URL(req.url).pathname;
        expect(params.id).toBe('notif-1');
        return new HttpResponse(null, { status: 204 });
      }),
    );
    await expect(markNotificationRead('notif-1')).resolves.toBeUndefined();
    expect(capturedUrl).toBe('/api/v2/notifications/notif-1/read');
  });

  it('markNotificationRead() encodes the id path segment', async () => {
    let capturedPath = '';
    server.use(
      http.post('/api/v2/notifications/:id/read', ({ request: req }) => {
        capturedPath = new URL(req.url).pathname;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    await markNotificationRead('ri.notification.main.notification.abc/def');
    expect(capturedPath).toContain('abc%2Fdef');
  });
});
