# Chapter 2 — Load

List objects from an ObjectType with cursor-based pagination. The same
endpoint backs the SPA's Browser, the SDKs' `objects.list`, and the
contract test fixtures.

## Endpoint shape

```
GET /api/v2/ontologies/{ontology}/objects/{objectType}?pageSize=25&pageToken=...
```

Response:

```json
{
  "data": [{"__primaryKey": "ALFKI", "companyName": "Alfreds"}, ...],
  "nextPageToken": "eyJvZmZzZXQiOjI1fQ=="
}
```

To stream every page, pass the previous response's `nextPageToken` as
`pageToken` on the next request and stop when it's empty.

---

## Python

```python
from weave_client import Client

client = Client("http://localhost:9117")
page = client.objects.list("northwind", "Customer", page_size=25)
for row in page.data:
    print(row["__primaryKey"], row.get("companyName"))

# Iterate every page:
for row in client.objects.iter_all("northwind", "Customer", page_size=200):
    handle(row)
```

## TypeScript

```typescript
import { WeaveClient } from './client.js';

const client = new WeaveClient({ baseUrl: 'http://localhost:9117' });
const customers = client.objects.of<{ __primaryKey: string; companyName?: string }>(
  'northwind', 'Customer',
);

let pageToken: string | undefined;
do {
  const page = await customers.list({ pageSize: 200, ...(pageToken ? { pageToken } : {}) });
  for (const row of page.data) {
    console.log(row.__primaryKey, row.companyName);
  }
  pageToken = page.nextPageToken;
} while (pageToken);
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

type page struct {
    Data          []map[string]any `json:"data"`
    NextPageToken string           `json:"nextPageToken,omitempty"`
}

func loadAll(base, ontology, ot string) ([]map[string]any, error) {
    var all []map[string]any
    var token string
    for {
        u := fmt.Sprintf("%s/api/v2/ontologies/%s/objects/%s?pageSize=200",
            base, url.PathEscape(ontology), url.PathEscape(ot))
        if token != "" {
            u += "&pageToken=" + url.QueryEscape(token)
        }
        resp, err := http.Get(u)
        if err != nil {
            return nil, err
        }
        body, _ := io.ReadAll(resp.Body)
        resp.Body.Close()
        var p page
        if err := json.Unmarshal(body, &p); err != nil {
            return nil, err
        }
        all = append(all, p.Data...)
        if p.NextPageToken == "" {
            return all, nil
        }
        token = p.NextPageToken
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

public class Load {
    static final HttpClient HTTP = HttpClient.newHttpClient();

    public static String loadPage(String base, String ontology, String ot, String token) throws Exception {
        StringBuilder url = new StringBuilder(base)
            .append("/api/v2/ontologies/").append(URLEncoder.encode(ontology, StandardCharsets.UTF_8))
            .append("/objects/").append(URLEncoder.encode(ot, StandardCharsets.UTF_8))
            .append("?pageSize=200");
        if (token != null && !token.isEmpty()) {
            url.append("&pageToken=").append(URLEncoder.encode(token, StandardCharsets.UTF_8));
        }
        HttpResponse<String> resp = HTTP.send(
            HttpRequest.newBuilder(URI.create(url.toString())).GET().build(),
            HttpResponse.BodyHandlers.ofString());
        if (resp.statusCode() != 200) {
            throw new RuntimeException("load failed: " + resp.statusCode());
        }
        return resp.body();
    }

    public static void main(String[] args) throws Exception {
        String base = System.getenv().getOrDefault("WEAVE_BASE_URL", "http://localhost:9117");
        System.out.println(loadPage(base, "northwind", "Customer", null));
    }
}
```

## Pitfalls

- **`pageSize` cap.** Server-side max is 1000. Above that the request
  returns `400 InvalidPageSize`.
- **Page-token opacity.** The token is base64-encoded server state; do
  NOT decode or persist it across server restarts — the offset format
  is internal.
- **Filtering.** Pass `where=` for full-text search; the SDKs expose
  the same parameter via typed `where` clauses (`pkg/oss/where`).
