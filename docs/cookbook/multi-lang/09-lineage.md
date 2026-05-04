# Chapter 9 — Lineage

Walk column-level lineage (US-377) — see which upstream dataset columns
feed each Property on each ObjectType, and inversely which Properties
break when an upstream column is dropped.

## Endpoint shape

Forward lookup (per Property):

```
GET /api/v2/lineage/property/{propertyRid}
```

Returns `{ "data": [{"srcDataset":"...", "srcColumn":"..."}] }`.

Reverse impact (per upstream column):

```
GET /api/v2/lineage/dataset-columns/impact?dataset=<rid>&column=<name>
```

Returns the list of `(objectType, property)` pairs affected. Plus the
ObjectSet-level walker:

```
GET /api/v2/ontologies/{ontology}/objectSets/{objectSetRid}/lineage
```

---

## Python

```python
import os
import httpx

base = os.environ["WEAVE_BASE_URL"]
property_rid = "ri.oms.main.property.customer-revenue"

upstream = httpx.get(f"{base}/api/v2/lineage/property/{property_rid}").json()
for edge in upstream["data"]:
    print(edge["srcDataset"], "→", edge["srcColumn"])

# Reverse impact:
impact = httpx.get(
    f"{base}/api/v2/lineage/dataset-columns/impact",
    params={"dataset": "ri.datasets.main.dataset.crm", "column": "revenue"},
).json()
print("affected properties:", impact["data"])
```

## TypeScript

```typescript
const base = process.env['WEAVE_BASE_URL'] ?? 'http://localhost:9117';
const propertyRid = 'ri.oms.main.property.customer-revenue';

const upstream = await (await fetch(
  `${base}/api/v2/lineage/property/${encodeURIComponent(propertyRid)}`,
)).json() as { data: Array<{ srcDataset: string; srcColumn: string }> };
for (const edge of upstream.data) {
  console.log(`${edge.srcDataset} -> ${edge.srcColumn}`);
}

// Reverse impact:
const impact = await (await fetch(
  `${base}/api/v2/lineage/dataset-columns/impact?` + new URLSearchParams({
    dataset: 'ri.datasets.main.dataset.crm',
    column: 'revenue',
  }),
)).json();
console.log('affected:', impact);
```

## Go

```go
package main

import (
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "net/url"
)

type lineageEdge struct {
    SrcDataset string `json:"srcDataset"`
    SrcColumn  string `json:"srcColumn"`
}

type lineageResp struct {
    Data []lineageEdge `json:"data"`
}

func upstreamFor(base, propertyRid string) ([]lineageEdge, error) {
    u := fmt.Sprintf("%s/api/v2/lineage/property/%s", base, url.PathEscape(propertyRid))
    resp, err := http.Get(u)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    var out lineageResp
    if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
        return nil, err
    }
    return out.Data, nil
}

func impactOf(base, dataset, column string) ([]byte, error) {
    q := url.Values{}
    q.Set("dataset", dataset)
    q.Set("column", column)
    resp, err := http.Get(base + "/api/v2/lineage/dataset-columns/impact?" + q.Encode())
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    return io.ReadAll(resp.Body)
}

func main() {
    base := "http://localhost:9117"
    edges, _ := upstreamFor(base, "ri.oms.main.property.customer-revenue")
    for _, e := range edges {
        fmt.Println(e.SrcDataset, "→", e.SrcColumn)
    }
    raw, _ := impactOf(base, "ri.datasets.main.dataset.crm", "revenue")
    fmt.Println(string(raw))
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

public class Lineage {
    public static void main(String[] args) throws Exception {
        String base = System.getenv().getOrDefault("WEAVE_BASE_URL", "http://localhost:9117");
        HttpClient http = HttpClient.newHttpClient();

        String propertyRid = "ri.oms.main.property.customer-revenue";
        HttpResponse<String> upstream = http.send(
            HttpRequest.newBuilder(URI.create(
                base + "/api/v2/lineage/property/" + URLEncoder.encode(propertyRid, StandardCharsets.UTF_8)))
                .GET().build(),
            HttpResponse.BodyHandlers.ofString());
        System.out.println(upstream.body());

        String impactUrl = base + "/api/v2/lineage/dataset-columns/impact"
            + "?dataset=" + URLEncoder.encode("ri.datasets.main.dataset.crm", StandardCharsets.UTF_8)
            + "&column=" + URLEncoder.encode("revenue", StandardCharsets.UTF_8);
        HttpResponse<String> impact = http.send(
            HttpRequest.newBuilder(URI.create(impactUrl)).GET().build(),
            HttpResponse.BodyHandlers.ofString());
        System.out.println(impact.body());
    }
}
```

## Pitfalls

- **Degraded-mode 404.** When the column-lineage store isn't wired
  (degraded test routers do this) the endpoint returns
  `404 ColumnLineageNotConfigured`, NOT 503 — handle it as "no edges
  available" in your client, the same as a property with no upstream.
- **Edge replacement semantics.** Lineage is rewritten on every
  datasource-binding write (US-377): a binding update that DROPS a
  column → property mapping immediately yields zero upstream edges.
- **Sort order.** Edges are returned sorted lexicographically by
  `(api_name, src_column)` so identical mapping payloads produce
  identical edge sets — useful as a content hash.
