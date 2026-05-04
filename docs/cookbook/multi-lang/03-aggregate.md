# Chapter 3 — Aggregate

Run a typed aggregation against an ObjectType — sum / count / approxDistinct
/ approxPercentile / cube / rollup. The aggregation engine sits behind a
single endpoint and the SDKs marshal a uniform request envelope.

## Endpoint shape

```
POST /api/v2/ontologies/{ontology}/objects/{objectType}/aggregate
```

Body:

```json
{
  "aggregation": [
    {"name": "totalRevenue", "type": "sum", "field": "amount"},
    {"name": "uniqueCustomers", "type": "approximateDistinct", "field": "customerId"}
  ],
  "groupBy": [{"type": "exact", "field": "country"}],
  "accuracy": "ALLOW_APPROXIMATE"
}
```

Response (200) carries `data` (per-bucket rows), `accuracy` flag, and an
optional `computeUsage` envelope (US-382: `scannedRows`, `durationMs`,
`accuracy`).

---

## Python

```python
from weave_client import Client

client = Client("http://localhost:9117")
result = client.objects.aggregate(
    "northwind",
    "Order",
    aggregation=[
        {"name": "totalRevenue", "type": "sum", "field": "freight"},
        {"name": "approxCustomers", "type": "approximateDistinct", "field": "customerId"},
    ],
    group_by=[{"type": "exact", "field": "shipCountry"}],
    accuracy="ALLOW_APPROXIMATE",
)
for row in result["data"]:
    print(row["group"], row["metrics"])
print("accuracy:", result.get("accuracy"))
```

## TypeScript

```typescript
import { WeaveClient } from './client.js';

const client = new WeaveClient({ baseUrl: 'http://localhost:9117' });
const result = await client.objects.aggregate('northwind', 'Order', {
  aggregation: [
    { name: 'totalRevenue', type: 'sum', field: 'freight' },
    { name: 'approxCustomers', type: 'approximateDistinct', field: 'customerId' },
  ],
  groupBy: [{ type: 'exact', field: 'shipCountry' }],
  accuracy: 'ALLOW_APPROXIMATE',
});
for (const row of result.data) {
  console.log(row.group, row.metrics);
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

func aggregate(base, ontology, ot string, body any) ([]map[string]any, error) {
    raw, _ := json.Marshal(body)
    u := fmt.Sprintf("%s/api/v2/ontologies/%s/objects/%s/aggregate",
        base, url.PathEscape(ontology), url.PathEscape(ot))
    resp, err := http.Post(u, "application/json", bytes.NewReader(raw))
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    var out struct {
        Data []map[string]any `json:"data"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
        return nil, err
    }
    return out.Data, nil
}

func main() {
    body := map[string]any{
        "aggregation": []map[string]any{
            {"name": "totalRevenue", "type": "sum", "field": "freight"},
            {"name": "approxCustomers", "type": "approximateDistinct", "field": "customerId"},
        },
        "groupBy":  []map[string]any{{"type": "exact", "field": "shipCountry"}},
        "accuracy": "ALLOW_APPROXIMATE",
    }
    rows, err := aggregate("http://localhost:9117", "northwind", "Order", body)
    if err != nil {
        panic(err)
    }
    for _, r := range rows {
        fmt.Printf("%v\t%v\n", r["group"], r["metrics"])
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

public class Aggregate {
    public static void main(String[] args) throws Exception {
        String base = System.getenv().getOrDefault("WEAVE_BASE_URL", "http://localhost:9117");
        String body = "{"
            + "\"aggregation\":["
            + "{\"name\":\"totalRevenue\",\"type\":\"sum\",\"field\":\"freight\"},"
            + "{\"name\":\"approxCustomers\",\"type\":\"approximateDistinct\",\"field\":\"customerId\"}"
            + "],"
            + "\"groupBy\":[{\"type\":\"exact\",\"field\":\"shipCountry\"}],"
            + "\"accuracy\":\"ALLOW_APPROXIMATE\""
            + "}";
        String url = base
            + "/api/v2/ontologies/" + URLEncoder.encode("northwind", StandardCharsets.UTF_8)
            + "/objects/" + URLEncoder.encode("Order", StandardCharsets.UTF_8)
            + "/aggregate";
        HttpResponse<String> resp = HttpClient.newHttpClient().send(
            HttpRequest.newBuilder(URI.create(url))
                .header("Content-Type", "application/json")
                .POST(HttpRequest.BodyPublishers.ofString(body))
                .build(),
            HttpResponse.BodyHandlers.ofString());
        System.out.println(resp.body());
    }
}
```

## Pitfalls

- **Approximate vs accurate.** `accuracy="REQUIRE_ACCURATE"` forces exact
  `distinct` and `percentile` computation (US-367 / US-368). The response
  always echoes the actual mode in `accuracy` so you can spot a silent
  fallback.
- **Empty groupBy.** Yields a single bucket with `group: {}` rather than a
  flat object — keep your reader code uniform.
- **excludedItems.** Pass a list of primary keys to filter rows out before
  the aggregation runs (US-382). Duplicates and unknown PKs are ignored.
