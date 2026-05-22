# SSE Subscriptions

Weave supports real-time object change notifications through Server-Sent Events (SSE). Clients subscribe to an ObjectSet and receive a stream of events as objects are created, modified, or deleted.

## Endpoint

```
GET /api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}/subscribe
```

### Path Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `ontologyApiName` | string | The ontology API name |
| `objectSetRid` | string | RID of a previously created temporary ObjectSet |

### Query Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `lastEventId` | string | Optional. NATS sequence number for replay (fallback for `Last-Event-ID` header) |

### Response Headers

```
Content-Type: text/event-stream
Cache-Control: no-cache
Connection: keep-alive
X-Accel-Buffering: no
```

## Event Payload Schema

Each SSE event carries a JSON payload with the following structure:

```json
{
  "eventType": "ADDED_OR_UPDATED",
  "object": {
    "__primaryKey": "ORD-001",
    "__apiName": "Order",
    "customerName": "Acme Corp",
    "total": 1500.00
  }
}
```

### Fields

| Field | Type | Description |
|-------|------|-------------|
| `eventType` | string | `"ADDED_OR_UPDATED"` or `"DELETED"` |
| `object` | object | The affected object in WireObject format |
| `object.__primaryKey` | string | The object's primary key |
| `object.__apiName` | string | The ObjectType API name |

### Event Type Mapping

| Edit Operation | SSE `eventType` |
|----------------|-----------------|
| `CREATE` | `ADDED_OR_UPDATED` |
| `MODIFY` | `ADDED_OR_UPDATED` |
| `DELETE` | `DELETED` |

### SSE Wire Format

Events are delivered as standard SSE frames:

```
id: 42
data: {"eventType":"ADDED_OR_UPDATED","object":{"__primaryKey":"ORD-001","__apiName":"Order","total":1500}}

```

- **`id`**: The NATS stream sequence number. Clients should persist this for reconnection.
- **`data`**: JSON-encoded event payload.
- Each frame is terminated by a blank line.

## Last-Event-ID and Reconnection

### Tracking Position

Every SSE event includes an `id` field containing the NATS stream sequence number. Clients should track this value to resume from the correct position after a disconnect.

### Reconnection Flow

```
1. Client connects to /subscribe
2. Server streams events, each with id: <sequence>
3. Client disconnects (network error, browser tab sleep, etc.)
4. Client reconnects with Last-Event-ID header or ?lastEventId= query param
5. Server replays missed events from ring buffer, then resumes live stream
```

### Header vs Query Parameter

The standard SSE reconnection mechanism uses the `Last-Event-ID` HTTP header. However, the browser `EventSource` API does not support custom headers, so Weave also accepts the `lastEventId` query parameter as a fallback:

```
GET /api/v2/ontologies/default/objectSets/ri.os.123/subscribe?lastEventId=42
```

Both are checked; the header takes priority.

### Ring Buffer

The server maintains an in-memory ring buffer of recent events (default capacity: 1024 events). On reconnection:

1. The server parses the `Last-Event-ID` / `lastEventId` value as a `uint64` sequence number.
2. Events in the ring buffer with sequence > `fromSeq` are replayed in order.
3. After replay completes, the connection transitions to live streaming.
4. Replay and live subscription are atomic (no event gaps during the transition).

If the requested sequence is older than the ring buffer's oldest entry, replay starts from the oldest available event. There is no error -- the client may miss events that have rotated out.

UI clients should treat the SSE sequence number as an idempotency key. If a reconnect path or intermediate retry delivers a sequence number that is already rendered, ignore the
duplicate instead of adding another visible event row.

### NATS Durable Storage

Beyond the ring buffer, NATS JetStream retains edit events for up to 24 hours. The ring buffer is tuned for brief disconnects (seconds); longer outages may require application-level reconciliation.

## Heartbeat / Keepalive

The server sends periodic heartbeat comments to keep the connection alive and detect stale connections:

```
:ping

```

### Configuration

| Setting | Default | Description |
|---------|---------|-------------|
| Heartbeat interval | 30 seconds | Time between `:ping` comments |

Heartbeats are SSE comment lines (prefixed with `:`). The browser `EventSource` API silently ignores them -- they do not trigger `onmessage` callbacks.

**Purpose:**
- Prevents intermediate proxies and firewalls from closing idle connections.
- Allows the server to detect broken connections (write failure triggers cleanup).

## Server-Side Filtering

### Where-Clause Filtering

When the subscribed ObjectSet includes `filter` definitions with Where clauses, the server evaluates each event against those clauses before delivery. Events that do not match are silently dropped.

**Supported operators:**

| Category | Operators |
|----------|-----------|
| Comparison | `eq`, `gt`, `gte`, `lt`, `lte` |
| String | `contains`, `startsWith` |
| Null check | `isNull` |
| Logic | `and`, `or`, `not` |

**Conservative behavior:** Unsupported operators or malformed values cause the event to be dropped (not delivered), matching the fail-closed security posture.

### Policy Filtering

If a security policy engine is configured, each event is checked against the subscribing user's permissions before delivery. Users only receive events for objects they are authorized to see.

The policy filter uses the same marking AND semantics as the query-time policy engine: object markings must be a subset of the user's markings. See [Security Policy Model](../security/policy-model.md) for details.

## Per-User Connection Limits

The server enforces a maximum number of concurrent SSE connections per user to prevent resource exhaustion.

| Setting | Default | Description |
|---------|---------|-------------|
| Max connections per user | 10 | Concurrent SSE streams per user identity |

### Connection Key

- **Authenticated users:** `user:<userID>`
- **Unauthenticated requests:** `ip:<remoteAddress>` (fallback)

When the limit is exceeded, the server responds with HTTP 429:

```json
{
  "errorCode": "SSEConnectionLimitExceeded",
  "parameters": {
    "maxPerUser": "10",
    "connectionKey": "user:alice"
  }
}
```

The limit is checked before streaming headers are sent, so clients receive a clean HTTP error response.

## Error Responses

| HTTP Status | Error Code | Cause |
|-------------|------------|-------|
| 200 | -- | Success, SSE stream begins |
| 400 | `InvalidObjectSetRid` | Malformed ObjectSet RID |
| 400 | `SSEUnsupportedObjectSet` | ObjectSet shape not supported for subscription |
| 404 | `ObjectSetNotFound` | ObjectSet RID not found in store |
| 429 | `SSEConnectionLimitExceeded` | Per-user connection limit reached |
| 500 | `SSESubscribeNotConfigured` | Server not configured for SSE (NATS unavailable) |

All error responses are JSON with `errorCode` and `parameters` fields.

## Architecture

### Event Pipeline

```
Action Executor
    |
    v
NATS JetStream (subject: edits.<ontology>.<objectType>)
    |
    v
Funnel Consumer (applies edits to Bleve indexes)
    |
    v
In-Process Broadcast Hub (fan-out to SSE subscribers)
    |
    v
SSE Handler (per-connection goroutine)
    |-- Where-clause filter
    |-- Policy filter
    |-- Write SSE frame to HTTP response
    v
Client (EventSource)
```

### NATS JetStream Configuration

| Setting | Value |
|---------|-------|
| Stream name | `OBJECT_EDITS` |
| Subject pattern | `edits.<ontologyApiName>.<objectType>` |
| Retention | `WorkQueuePolicy` (consumed = discarded) |
| Max age | 24 hours |
| Storage | File (persistent) |
| Consumer | Durable, manual ack, 30s ack wait |

### Broadcast Hub

The Broadcast hub is an in-process fan-out mechanism:

- **Non-blocking publish**: Slow subscribers do not block the publisher or other subscribers.
- **Per-subscriber channel**: Each SSE connection gets a buffered channel (capacity 16).
- **Overflow handling**: If a subscriber's channel is full, the event is dropped for that subscriber only.
- **Ring buffer**: Holds the most recent 1024 events for replay on reconnection.

## Client Implementation

### Browser (EventSource)

```javascript
const url = `/api/v2/ontologies/${ontology}/objectSets/${objectSetRid}/subscribe`;
const eventSource = new EventSource(url);

let lastEventId = null;

eventSource.onmessage = (event) => {
  lastEventId = event.lastEventId;
  const payload = JSON.parse(event.data);
  console.log(payload.eventType, payload.object.__primaryKey);
};

eventSource.onerror = () => {
  eventSource.close();
  // Reconnect with backoff, passing lastEventId
  setTimeout(() => {
    reconnect(lastEventId);
  }, backoffMs);
};
```

### Reconnection with Backoff

The recommended client reconnection strategy uses exponential backoff:

| Attempt | Delay |
|---------|-------|
| 1 | 1 second |
| 2 | 2 seconds |
| 3 | 4 seconds |
| 4 | 8 seconds |
| 5 | 16 seconds |
| 6+ | 30 seconds (cap) |

Reset the backoff delay to 1 second on successful connection (`onopen` event).

When reconnecting, include the last received event ID:

```
GET /subscribe?lastEventId=42
```

### React Hook

Weave's frontend provides a `useObjectSetSubscription` hook:

```typescript
useObjectSetSubscription(ontology, objectSetRid, {
  enabled: true,
  onEvent: (event) => {
    // Handle ADDED_OR_UPDATED or DELETED
  },
});
```

The hook manages the full lifecycle: connection, reconnection with exponential backoff, `lastEventId` tracking, and cleanup on unmount.

## Conditional Availability

SSE subscriptions require NATS JetStream to be running. If NATS is unavailable at server startup, the subscribe endpoint is not registered and returns 500 with `SSESubscribeNotConfigured`. All other Weave functionality remains operational -- SSE is an additive feature.
