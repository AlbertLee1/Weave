import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { listDatasetHistory } from '../datasets';

const server = setupServer();

beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe('datasets API', () => {
  it('listDatasetHistory() preserves the truncated flag from the server', async () => {
    server.use(
      http.get('/api/v2/datasets/iotDemo/history', () =>
        HttpResponse.json({
          transactions: [
            {
              txId: 'tx-1001',
              ontologyApiName: 'iotDemo',
              committedAt: '2026-05-19T08:00:00Z',
              editsCount: 2,
            },
          ],
          truncated: true,
        }),
      ),
    );

    const result = await listDatasetHistory('iotDemo');
    const truncated: boolean = result.truncated;
    expect(truncated).toBe(true);
  });
});
