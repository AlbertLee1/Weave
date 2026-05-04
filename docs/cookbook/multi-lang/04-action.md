# Chapter 4 — Action

Apply a single Action — the canonical mutating call. The action API name
sits in the URL; the request body carries only `parameters`. Server-side
validation, rule execution, edit generation, and NATS publication all
happen before the response returns.

## Endpoint shape

```
POST /api/v2/ontologies/{ontology}/actions/{actionType}/apply
```

Body:

```json
{ "parameters": { "customerId": "ALFKI", "amount": 99.50 } }
```

Response (200):

```json
{
  "edits": [{"type": "MODIFY", "primaryKey": "ALFKI", "objectType": "Customer"}],
  "validation": "VALID"
}
```

Errors: `400 InvalidParameter`, `409 PrimaryKeyConflict`, `403 ActionDenied`.

---

## Python

```python
from weave_client import Client

client = Client("http://localhost:9117")
resp = client.actions.apply(
    "northwind", "createOrder",
    parameters={"customerId": "ALFKI", "amount": 99.50},
)
print("edits:", len(resp.edits))
```

Use `apply_with_options` to pass `mode=VALIDATE_ONLY` for a dry run, or
`return_edits=NONE` to skip the edit list when you only need success.

## TypeScript

```typescript
import { WeaveClient } from './client.js';

const client = new WeaveClient({ baseUrl: 'http://localhost:9117' });
const resp = await client.actions.apply('northwind', 'createOrder', {
  customerId: 'ALFKI',
  amount: 99.50,
});
console.log('edits:', resp.edits?.length ?? 0);
```

## Go

```go
package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "net/http"
    "net/url"
)

func applyAction(base, ontology, action string, params map[string]any) (map[string]any, error) {
    body, _ := json.Marshal(map[string]any{"parameters": params})
    u := fmt.Sprintf("%s/api/v2/ontologies/%s/actions/%s/apply",
        base, url.PathEscape(ontology), url.PathEscape(action))
    resp, err := http.Post(u, "application/json", bytes.NewReader(body))
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    if resp.StatusCode/100 != 2 {
        return nil, fmt.Errorf("apply failed: %d", resp.StatusCode)
    }
    var out map[string]any
    return out, json.NewDecoder(resp.Body).Decode(&out)
}

func main() {
    out, err := applyAction("http://localhost:9117", "northwind", "createOrder",
        map[string]any{"customerId": "ALFKI", "amount": 99.50})
    if err != nil {
        panic(err)
    }
    fmt.Printf("response: %+v\n", out)
}
```

## Java

```java
import java.net.URI;
import java.net.URLEncoder;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;

public class Action {
    public static void main(String[] args) throws Exception {
        String base = System.getenv().getOrDefault("WEAVE_BASE_URL", "http://localhost:9117");
        String body = "{\"parameters\":{\"customerId\":\"ALFKI\",\"amount\":99.50}}";
        String url = base + "/api/v2/ontologies/"
            + URLEncoder.encode("northwind", StandardCharsets.UTF_8)
            + "/actions/" + URLEncoder.encode("createOrder", StandardCharsets.UTF_8)
            + "/apply";
        HttpResponse<String> resp = HttpClient.newHttpClient().send(
            HttpRequest.newBuilder(URI.create(url))
                .header("Content-Type", "application/json")
                .POST(HttpRequest.BodyPublishers.ofString(body))
                .build(),
            HttpResponse.BodyHandlers.ofString());
        if (resp.statusCode() != 200) {
            throw new RuntimeException("apply failed: " + resp.statusCode() + " body=" + resp.body());
        }
        System.out.println(resp.body());
    }
}
```

## Pitfalls

- **`returnEdits`** defaults to `ALL` — for high-throughput callers that
  don't need the edit list, set it to `NONE` so the server skips
  serialising thousands of edits per response.
- **Idempotency.** Single-action `apply` is NOT idempotent. Use a saga
  (chapter 6) or `applyBatch` with idempotency keys for retry-safe
  semantics.
- **Validation-only.** `mode=VALIDATE_ONLY` runs the rules without
  publishing edits — useful for form preview before commit.
