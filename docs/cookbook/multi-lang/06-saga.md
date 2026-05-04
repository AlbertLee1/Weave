# Chapter 6 — Saga

Run a multi-step Action saga with idempotency and inverse-edit
compensation (US-369). On any step's failure, the executor reverses
applied edits in reverse order; on retry with the same idempotency key,
the previous result is replayed instead of re-executed.

## Endpoint shape

```
POST /api/v2/ontologies/{ontology}/actions/applySaga
```

Body:

```json
{
  "idempotencyKey": "deploy-2026-05-05-abc123",
  "steps": [
    {"actionType": "createOrder", "parameters": {"customerId": "ALFKI"}},
    {"actionType": "decrementInventory", "parameters": {"sku": "WIDGET-1", "qty": 5}},
    {"actionType": "sendNotification", "parameters": {"to": "ops@example.com"}}
  ]
}
```

Response on success: `{ "status": "SUCCESS", "stepResults": [...] }`.
Failure: `{ "status": "COMPENSATED", "failedStep": 1, "compensation": "OK" }`.
Hard-fail (compensation also failed) lands in the saga DLQ — see chapter 9
of the AdminOps guide for retry UI.

---

## Python

```python
import os
import httpx

base = os.environ["WEAVE_BASE_URL"]
resp = httpx.post(f"{base}/api/v2/ontologies/northwind/actions/applySaga", json={
    "idempotencyKey": "deploy-2026-05-05-abc123",
    "steps": [
        {"actionType": "createOrder", "parameters": {"customerId": "ALFKI"}},
        {"actionType": "decrementInventory", "parameters": {"sku": "WIDGET-1", "qty": 5}},
        {"actionType": "sendNotification", "parameters": {"to": "ops@example.com"}},
    ],
})
resp.raise_for_status()
out = resp.json()
print(out["status"], out.get("failedStep"))
```

Re-POSTing with the same `idempotencyKey` returns the prior result; a
DIFFERENT body under the same key returns `409 SagaIdempotencyConflict`.

## TypeScript

```typescript
const base = process.env['WEAVE_BASE_URL'] ?? 'http://localhost:9117';
const resp = await fetch(`${base}/api/v2/ontologies/northwind/actions/applySaga`, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    idempotencyKey: 'deploy-2026-05-05-abc123',
    steps: [
      { actionType: 'createOrder', parameters: { customerId: 'ALFKI' } },
      { actionType: 'decrementInventory', parameters: { sku: 'WIDGET-1', qty: 5 } },
      { actionType: 'sendNotification', parameters: { to: 'ops@example.com' } },
    ],
  }),
});
if (!resp.ok) throw new Error(`saga failed: ${resp.status}`);
const out = (await resp.json()) as { status: string; failedStep?: number };
console.log(out.status, out.failedStep);
```

## Go

```go
package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "net/http"
    "os"
)

type sagaStep struct {
    ActionType string         `json:"actionType"`
    Parameters map[string]any `json:"parameters"`
}

type sagaResp struct {
    Status     string `json:"status"`
    FailedStep *int   `json:"failedStep,omitempty"`
}

func main() {
    body, _ := json.Marshal(map[string]any{
        "idempotencyKey": "deploy-2026-05-05-abc123",
        "steps": []sagaStep{
            {ActionType: "createOrder", Parameters: map[string]any{"customerId": "ALFKI"}},
            {ActionType: "decrementInventory", Parameters: map[string]any{"sku": "WIDGET-1", "qty": 5}},
            {ActionType: "sendNotification", Parameters: map[string]any{"to": "ops@example.com"}},
        },
    })
    base := os.Getenv("WEAVE_BASE_URL")
    resp, err := http.Post(base+"/api/v2/ontologies/northwind/actions/applySaga",
        "application/json", bytes.NewReader(body))
    if err != nil {
        panic(err)
    }
    defer resp.Body.Close()
    var out sagaResp
    if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
        panic(err)
    }
    fmt.Println("status:", out.Status, "failedStep:", out.FailedStep)
}
```

## Java

```java
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;

public class Saga {
    public static void main(String[] args) throws Exception {
        String base = System.getenv().getOrDefault("WEAVE_BASE_URL", "http://localhost:9117");
        String body = "{"
            + "\"idempotencyKey\":\"deploy-2026-05-05-abc123\","
            + "\"steps\":["
            + "  {\"actionType\":\"createOrder\",\"parameters\":{\"customerId\":\"ALFKI\"}},"
            + "  {\"actionType\":\"decrementInventory\",\"parameters\":{\"sku\":\"WIDGET-1\",\"qty\":5}},"
            + "  {\"actionType\":\"sendNotification\",\"parameters\":{\"to\":\"ops@example.com\"}}"
            + "]"
            + "}";
        HttpResponse<String> resp = HttpClient.newHttpClient().send(
            HttpRequest.newBuilder(URI.create(base + "/api/v2/ontologies/northwind/actions/applySaga"))
                .header("Content-Type", "application/json")
                .POST(HttpRequest.BodyPublishers.ofString(body))
                .build(),
            HttpResponse.BodyHandlers.ofString());
        System.out.println(resp.statusCode() + " " + resp.body());
    }
}
```

## Pitfalls

- **Idempotency keys are global per ontology.** Pick collision-free
  identifiers (deploy id + nonce). A re-used key with a *different* body
  is a server-side error, not a silent overwrite.
- **Compensation budget.** Each inverse edit gets its own retry budget;
  if compensation also fails the saga lands in `action_saga_dlq` and the
  response surfaces `status: COMPENSATION_FAILED`.
- **Step output → next step input.** Each step's edits are visible to
  later steps within the same saga (read-your-writes within the saga
  scope), but outside callers don't see uncommitted edits until the
  saga resolves.
