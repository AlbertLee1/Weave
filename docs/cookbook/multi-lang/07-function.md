# Chapter 7 — Function

Execute a server-side Function (Goja-sandboxed JavaScript) with typed
input and output. Functions support deterministic replay (US-370),
function-to-function dispatch with cycle detection (US-371), and per-branch
versioning (US-389).

## Endpoint shape

```
POST /api/v2/ontologies/{ontology}/functions/{functionRid}/execute
```

`functionRid` accepts the canonical RID (`ri.functions.main.fn.<uuid>`),
the human name (`topProducts`), or the name-with-version (`topProducts@2`).
Body:

```json
{ "parameters": { "limit": 10, "country": "USA" } }
```

Response: `{ "result": <typed value> }` or `{ "error": {...} }`.

---

## Python

```python
from weave_client import Client

client = Client("http://localhost:9117")
out = client.functions.execute(
    "northwind", "topProducts",
    parameters={"limit": 10, "country": "USA"},
)
print(out["result"])

# Streaming Functions emit NDJSON:
for item in client.functions.execute_stream("northwind", "scanCustomers", parameters={}):
    print(item)
```

## TypeScript

```typescript
import { WeaveClient } from './client.js';

const client = new WeaveClient({ baseUrl: 'http://localhost:9117' });
const out = await client.functions.execute<{ items: unknown[] }>(
  'northwind', 'topProducts',
  { limit: 10, country: 'USA' },
);
console.log(out.items.length);
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

func executeFunction(base, ontology, ref string, params map[string]any) (any, error) {
    body, _ := json.Marshal(map[string]any{"parameters": params})
    u := fmt.Sprintf("%s/api/v2/ontologies/%s/functions/%s/execute",
        base, url.PathEscape(ontology), url.PathEscape(ref))
    resp, err := http.Post(u, "application/json", bytes.NewReader(body))
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    if resp.StatusCode/100 != 2 {
        return nil, fmt.Errorf("execute failed: %d", resp.StatusCode)
    }
    var out struct {
        Result any `json:"result"`
    }
    return out.Result, json.NewDecoder(resp.Body).Decode(&out)
}

func main() {
    out, err := executeFunction(
        "http://localhost:9117", "northwind", "topProducts",
        map[string]any{"limit": 10, "country": "USA"})
    if err != nil {
        panic(err)
    }
    fmt.Printf("%+v\n", out)
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

public class Function {
    public static void main(String[] args) throws Exception {
        String base = System.getenv().getOrDefault("WEAVE_BASE_URL", "http://localhost:9117");
        String body = "{\"parameters\":{\"limit\":10,\"country\":\"USA\"}}";
        String url = base + "/api/v2/ontologies/"
            + URLEncoder.encode("northwind", StandardCharsets.UTF_8)
            + "/functions/" + URLEncoder.encode("topProducts", StandardCharsets.UTF_8)
            + "/execute";
        HttpResponse<String> resp = HttpClient.newHttpClient().send(
            HttpRequest.newBuilder(URI.create(url))
                .header("Content-Type", "application/json")
                .POST(HttpRequest.BodyPublishers.ofString(body))
                .build(),
            HttpResponse.BodyHandlers.ofString());
        if (resp.statusCode() != 200) {
            throw new RuntimeException("execute failed: " + resp.statusCode());
        }
        System.out.println(resp.body());
    }
}
```

## Pitfalls

- **Recursion limit.** Function-to-function calls go through
  `runtime.callFunction(ref, args)` (US-371) with `MaxDepth=8`. Deeper
  stacks raise `FUNCTION_RECURSION_DEPTH_EXCEEDED`. Static cycle
  detection runs at publish time — `A → B → A` is rejected before it
  ships.
- **Branches.** Add `?branch=feature-x` to route to a branch's published
  version (US-389). Default routes to `main`.
- **Streaming.** `execute_stream` (Python) and `executeStream` (TS)
  consume the NDJSON body line-by-line. The Go and Java HTTP shapes need
  a manual `bufio.Scanner` / `BufferedReader` over the streaming body.
