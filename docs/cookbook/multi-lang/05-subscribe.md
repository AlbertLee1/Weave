# Chapter 5 — Subscribe

Stream object change events over WebSocket with cursor + replay (US-380).
The connection survives reconnects: every reconnect sends `?since=<cursor>`
so the server replays the events your client missed during the outage.

## Endpoint shape

```
GET wss://host/api/v2/subscriptions/objects?token=<bearer>&since=<cursor>
```

Once connected, the client sends a subscribe envelope:

```json
{
  "type": "subscribe",
  "data": {
    "objectType": "Customer",
    "where": null
  }
}
```

The server streams `objectChange` envelopes carrying `cursor`, `state`,
and `object`. On reconnect, supply the last seen cursor.

---

## Python

```python
import asyncio
from weave_client import WeaveAsyncClient, WeaveOutOfDate

async def main():
    async with WeaveAsyncClient("http://localhost:9117") as client:
        sub = await client.objects.subscribe("northwind", object_type="Customer")
        async for event in sub:
            print(event.cursor, event.state, event.object.get("__primaryKey"))

asyncio.run(main())
```

`WeaveAsyncClient.objects.subscribe` auto-reconnects on transient socket
errors, threading the most recently seen cursor onto the next handshake.
Catch `WeaveOutOfDate` for connection-level resync (full reload), and rely
on the iterator for subscription-level replay.

## TypeScript

```typescript
import { WeaveClient } from './client.js';

const client = new WeaveClient({ baseUrl: 'http://localhost:9117' });
const sub = await client.subscribe.objects('northwind', { objectType: 'Customer' });
for await (const evt of sub) {
  console.log(evt.cursor, evt.state, evt.object['__primaryKey']);
}
```

The OSDK ships its own pluggable `SubscribeTransport` ABC (see
`examples/ts-quickstart/src/subscribe.ts`) so unit tests can script
inbound frames without a real socket.

## Go

```go
package main

import (
    "encoding/json"
    "fmt"
    "log"
    "net/url"

    "github.com/coder/websocket"
    "context"
)

func main() {
    ctx := context.Background()
    u := url.URL{Scheme: "ws", Host: "localhost:9117", Path: "/api/v2/subscriptions/objects"}
    c, _, err := websocket.Dial(ctx, u.String(), nil)
    if err != nil {
        log.Fatal(err)
    }
    defer c.Close(websocket.StatusNormalClosure, "bye")

    sub := map[string]any{
        "type": "subscribe",
        "data": map[string]any{"objectType": "Customer"},
    }
    raw, _ := json.Marshal(sub)
    if err := c.Write(ctx, websocket.MessageText, raw); err != nil {
        log.Fatal(err)
    }

    for {
        _, data, err := c.Read(ctx)
        if err != nil {
            log.Fatal(err)
        }
        var evt struct {
            Cursor int64           `json:"cursor"`
            State  string          `json:"state"`
            Object json.RawMessage `json:"object"`
        }
        if err := json.Unmarshal(data, &evt); err == nil {
            fmt.Printf("[%d] %s %s\n", evt.Cursor, evt.State, string(evt.Object))
        }
    }
}
```

## Java

```java
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.WebSocket;
import java.util.concurrent.CompletionStage;

public class Subscribe {
    public static void main(String[] args) throws Exception {
        String base = System.getenv().getOrDefault("WEAVE_BASE_URL", "http://localhost:9117");
        URI ws = URI.create(base.replaceFirst("^http", "ws") + "/api/v2/subscriptions/objects");
        WebSocket socket = HttpClient.newHttpClient().newWebSocketBuilder()
            .buildAsync(ws, new WebSocket.Listener() {
                @Override public void onOpen(WebSocket s) {
                    s.sendText(
                        "{\"type\":\"subscribe\",\"data\":{\"objectType\":\"Customer\"}}",
                        true);
                    WebSocket.Listener.super.onOpen(s);
                }
                @Override public CompletionStage<?> onText(WebSocket s, CharSequence data, boolean last) {
                    System.out.println(data);
                    return WebSocket.Listener.super.onText(s, data, last);
                }
            }).join();
        // Block until JVM exits — production code keeps a Thread parking here.
        Thread.sleep(Long.MAX_VALUE);
    }
}
```

## Pitfalls

- **Connection-level `onOutOfDate`.** When `?since=<cursor>` is older
  than the replay window (default 5 min), the server emits a connection-level
  out-of-date signal — your client must drop cached state and re-list.
- **Subscription-level `onOutOfDate`.** Carries a `subscriptionId`; treat
  this as transient (close + reconnect with the same cursor) rather than
  a full resync.
- **WS auth.** The `Authorization: Bearer ...` header isn't available on
  browser WS upgrades, so the server reads `?token=` from the query
  string instead.
- **Aggregations don't replay.** Only `objectChange` and
  `actionJobProgress` go into the replay log; aggregation subscriptions
  re-seed from Bleve at subscribe time.
