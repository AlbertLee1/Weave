# Chapter 10 — Batch

Apply many invocations of the same Action in one call. The server runs
them in a single transaction (atomic on the funnel side) and returns a
per-request result with edits and validation status.

## Endpoint shape

```
POST /api/v2/ontologies/{ontology}/actions/{actionType}/applyBatch
```

Body:

```json
{
  "requests": [
    {"parameters": {"customerId": "ALFKI"}},
    {"parameters": {"customerId": "ANATR"}},
    {"parameters": {"customerId": "ANTON"}}
  ],
  "options": { "returnEdits": "ALL" }
}
```

Response: `{ "results": [{...}, {...}, ...] }` — one entry per input
request, in the same order. Per-request errors surface inline as
`{"error": {...}}`; HTTP status stays 200 unless the entire batch is
rejected.

---

## Python

```python
from weave_client import Client

client = Client("http://localhost:9117")
resp = client.actions.apply_batch(
    "northwind", "createOrder",
    [
        {"parameters": {"customerId": "ALFKI"}},
        {"parameters": {"customerId": "ANATR"}},
        {"parameters": {"customerId": "ANTON"}},
    ],
    return_edits="ALL",
)
for i, item in enumerate(resp.results):
    print(i, item.validation, len(item.edits or []))
```

## TypeScript

```typescript
import { WeaveClient } from './client.js';

const client = new WeaveClient({ baseUrl: 'http://localhost:9117' });
const out = await client.actions.applyBatch('northwind', 'createOrder', [
  { parameters: { customerId: 'ALFKI' } },
  { parameters: { customerId: 'ANATR' } },
  { parameters: { customerId: 'ANTON' } },
], { returnEdits: 'ALL' });

for (const [i, r] of out.results.entries()) {
  console.log(i, r.validation, r.edits?.length ?? 0);
}
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

type batchRequest struct {
    Parameters map[string]any `json:"parameters"`
}

type batchResp struct {
    Results []map[string]any `json:"results"`
}

func applyBatch(base, ontology, action string, reqs []batchRequest) (*batchResp, error) {
    body, _ := json.Marshal(map[string]any{
        "requests": reqs,
        "options":  map[string]string{"returnEdits": "ALL"},
    })
    u := fmt.Sprintf("%s/api/v2/ontologies/%s/actions/%s/applyBatch",
        base, url.PathEscape(ontology), url.PathEscape(action))
    resp, err := http.Post(u, "application/json", bytes.NewReader(body))
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    var out batchResp
    if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
        return nil, err
    }
    return &out, nil
}

func main() {
    out, err := applyBatch("http://localhost:9117", "northwind", "createOrder",
        []batchRequest{
            {Parameters: map[string]any{"customerId": "ALFKI"}},
            {Parameters: map[string]any{"customerId": "ANATR"}},
            {Parameters: map[string]any{"customerId": "ANTON"}},
        })
    if err != nil {
        panic(err)
    }
    for i, r := range out.Results {
        fmt.Printf("%d: %v\n", i, r)
    }
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

public class Batch {
    public static void main(String[] args) throws Exception {
        String base = System.getenv().getOrDefault("WEAVE_BASE_URL", "http://localhost:9117");
        String body = "{"
            + "\"requests\":["
            + "  {\"parameters\":{\"customerId\":\"ALFKI\"}},"
            + "  {\"parameters\":{\"customerId\":\"ANATR\"}},"
            + "  {\"parameters\":{\"customerId\":\"ANTON\"}}"
            + "],"
            + "\"options\":{\"returnEdits\":\"ALL\"}"
            + "}";
        String url = base + "/api/v2/ontologies/"
            + URLEncoder.encode("northwind", StandardCharsets.UTF_8)
            + "/actions/" + URLEncoder.encode("createOrder", StandardCharsets.UTF_8)
            + "/applyBatch";
        HttpResponse<String> resp = HttpClient.newHttpClient().send(
            HttpRequest.newBuilder(URI.create(url))
                .header("Content-Type", "application/json")
                .POST(HttpRequest.BodyPublishers.ofString(body))
                .build(),
            HttpResponse.BodyHandlers.ofString());
        if (resp.statusCode() != 200) {
            throw new RuntimeException("batch failed: " + resp.statusCode());
        }
        System.out.println(resp.body());
    }
}
```

## Pitfalls

- **Per-request failure ≠ batch failure.** Validation failures on a
  subset of requests come back inline; the surrounding HTTP response
  is still 200. Walk `results[].validation` to find them.
- **`returnEdits=NONE` for throughput.** When you only need the success
  signal, skip edit serialisation — the server still emits NATS edits,
  it just doesn't echo them back.
- **Batch size cap.** Default upper bound is 1000 entries; over that
  the server returns `400 BatchTooLarge`. For million-row migrations,
  chunk the batch and stream — see chapter 3 of the Python-only
  cookbook (`docs/cookbook/03-batching.md`) for chunking heuristics.
