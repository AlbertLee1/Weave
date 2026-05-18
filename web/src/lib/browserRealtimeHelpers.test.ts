import { describe, expect, it } from 'vitest';
import { realtimePayloadMatchesPrimaryKey } from './browserRealtimeHelpers';

describe('browser realtime subscription payload matching', () => {
  it('matches a WebSocket objectChanged frame carrying the inserted primary key', () => {
    const payload = JSON.stringify({
      type: 'objectChanged',
      data: {
        state: 'ADDED_OR_UPDATED',
        object: {
          __primaryKey: 'RT-123',
          customerID: 'RT-123',
          companyName: 'Realtime Co RT-123',
        },
      },
    });

    expect(realtimePayloadMatchesPrimaryKey(payload, 'RT-123')).toBe(true);
  });

  it('matches an ObjectSet SSE frame carrying the inserted primary key', () => {
    const payload = JSON.stringify({
      eventType: 'ADDED_OR_UPDATED',
      object: {
        __primaryKey: 'RT-456',
        customerID: 'RT-456',
      },
      type: 'created',
      properties: {
        customerID: 'RT-456',
      },
    });

    expect(realtimePayloadMatchesPrimaryKey(payload, 'RT-456')).toBe(true);
  });

  it('ignores handshake frames and unrelated object changes', () => {
    expect(
      realtimePayloadMatchesPrimaryKey(
        JSON.stringify({ type: 'welcome', connectionId: 'conn-1' }),
        'RT-789',
      ),
    ).toBe(false);

    expect(
      realtimePayloadMatchesPrimaryKey(
        JSON.stringify({
          type: 'objectChanged',
          data: {
            state: 'ADDED_OR_UPDATED',
            object: { customerID: 'OTHER' },
          },
        }),
        'RT-789',
      ),
    ).toBe(false);
  });
});
