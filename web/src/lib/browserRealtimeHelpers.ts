type JsonRecord = Record<string, unknown>;

function isRecord(value: unknown): value is JsonRecord {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function payloadToText(payload: string | Uint8Array): string {
  if (typeof payload === 'string') return payload;
  return new TextDecoder().decode(payload);
}

function containsExactString(value: unknown, expected: string): boolean {
  if (typeof value === 'string') return value === expected;
  if (Array.isArray(value)) {
    return value.some((item) => containsExactString(item, expected));
  }
  if (!isRecord(value)) return false;
  return Object.values(value).some((item) => containsExactString(item, expected));
}

function changePayloads(envelope: JsonRecord): unknown[] {
  const data = envelope.data;

  if (envelope.type === 'objectChanged') {
    if (isRecord(data) && 'object' in data) return [data.object];
    return [data];
  }

  if (
    envelope.eventType === 'ADDED_OR_UPDATED' ||
    envelope.eventType === 'DELETED' ||
    envelope.type === 'created' ||
    envelope.type === 'modified' ||
    envelope.type === 'deleted'
  ) {
    return [envelope.object, envelope.properties];
  }

  return [];
}

export function realtimePayloadMatchesPrimaryKey(
  payload: string | Uint8Array,
  primaryKey: string,
): boolean {
  const expected = primaryKey.trim();
  if (expected.length === 0) return false;

  let envelope: unknown;
  try {
    envelope = JSON.parse(payloadToText(payload));
  } catch {
    return false;
  }

  if (!isRecord(envelope)) return false;
  return changePayloads(envelope).some((candidate) =>
    containsExactString(candidate, expected),
  );
}
