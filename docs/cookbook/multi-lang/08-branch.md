# Chapter 8 — Branch

Create and merge ontology branches (US-383 through US-385). Branches let
you stage ObjectType / ActionType / Function changes in isolation, diff
them against `main`, then merge with conflict resolution.

## Endpoint shape

Create:

```
POST /api/v2/ontologies/{ontology}/branches
```

Body: `{ "name": "feature-x", "parentBranch": "main" }`.

List:

```
GET /api/v2/ontologies/{ontology}/branches
```

Diff:

```
GET  /api/v2/ontologies/{ontology}/branches/{branchId}/diff
POST /api/v2/ontologies/{ontology}/branches/{branchId}/diff
```

Merge:

```
POST /api/v2/ontologies/{ontology}/branches/{branchId}/merge
```

To target a branch on subsequent reads, append `?branch=<name>` — the
SDK clients pick this up via the per-ontology branch store (US-386 in
the SPA).

---

## Python

```python
import os
import httpx

base = os.environ["WEAVE_BASE_URL"]

# Create the branch.
resp = httpx.post(f"{base}/api/v2/ontologies/northwind/branches", json={
    "name": "feature-x",
    "parentBranch": "main",
})
resp.raise_for_status()

# Read on the branch — same SDK methods, ?branch= appended.
import httpx
page = httpx.get(
    f"{base}/api/v2/ontologies/northwind/objects/Customer?pageSize=10&branch=feature-x",
).json()
print(len(page["data"]), "rows on feature-x")

# Diff against main.
diff = httpx.get(
    f"{base}/api/v2/ontologies/northwind/branches/feature-x/diff",
).json()
print("changes:", diff)
```

## TypeScript

```typescript
const base = process.env['WEAVE_BASE_URL'] ?? 'http://localhost:9117';

await fetch(`${base}/api/v2/ontologies/northwind/branches`, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ name: 'feature-x', parentBranch: 'main' }),
});

const diff = await (await fetch(
  `${base}/api/v2/ontologies/northwind/branches/feature-x/diff`,
)).json();
console.log('changes:', diff);

// Merge with explicit conflict resolution:
await fetch(`${base}/api/v2/ontologies/northwind/branches/feature-x/merge`, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    conflictResolution: { 'Customer.creditLimit': 'use-branch' },
  }),
});
```

## Go

```go
package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "net/http"
)

func createBranch(base, ontology, name, parent string) error {
    body, _ := json.Marshal(map[string]string{"name": name, "parentBranch": parent})
    resp, err := http.Post(
        fmt.Sprintf("%s/api/v2/ontologies/%s/branches", base, ontology),
        "application/json", bytes.NewReader(body),
    )
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    if resp.StatusCode/100 != 2 {
        return fmt.Errorf("create branch failed: %d", resp.StatusCode)
    }
    return nil
}

func mergeBranch(base, ontology, branch string, resolutions map[string]string) error {
    body, _ := json.Marshal(map[string]any{"conflictResolution": resolutions})
    resp, err := http.Post(
        fmt.Sprintf("%s/api/v2/ontologies/%s/branches/%s/merge", base, ontology, branch),
        "application/json", bytes.NewReader(body),
    )
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    if resp.StatusCode/100 != 2 {
        return fmt.Errorf("merge failed: %d", resp.StatusCode)
    }
    return nil
}

func main() {
    base := "http://localhost:9117"
    if err := createBranch(base, "northwind", "feature-x", "main"); err != nil {
        panic(err)
    }
    if err := mergeBranch(base, "northwind", "feature-x", map[string]string{
        "Customer.creditLimit": "use-branch",
    }); err != nil {
        panic(err)
    }
}
```

## Java

```java
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;

public class Branch {
    public static void main(String[] args) throws Exception {
        String base = System.getenv().getOrDefault("WEAVE_BASE_URL", "http://localhost:9117");
        HttpClient http = HttpClient.newHttpClient();

        HttpResponse<String> create = http.send(
            HttpRequest.newBuilder(URI.create(base + "/api/v2/ontologies/northwind/branches"))
                .header("Content-Type", "application/json")
                .POST(HttpRequest.BodyPublishers.ofString(
                    "{\"name\":\"feature-x\",\"parentBranch\":\"main\"}"))
                .build(),
            HttpResponse.BodyHandlers.ofString());
        if (create.statusCode() / 100 != 2) {
            throw new RuntimeException("create branch failed: " + create.statusCode());
        }

        HttpResponse<String> diff = http.send(
            HttpRequest.newBuilder(URI.create(
                base + "/api/v2/ontologies/northwind/branches/feature-x/diff")).GET().build(),
            HttpResponse.BodyHandlers.ofString());
        System.out.println(diff.body());
    }
}
```

## Pitfalls

- **Status normalisation.** Both lowercase (`open|merged|closed`) and
  uppercase (`ACTIVE|MERGED|ABANDONED`) are accepted on writes; reads
  return canonical lowercase. Don't case-compare against the wire form
  in your client — call `lower()` before comparing.
- **Parent-fallback resolution.** When a branch hasn't overridden an
  apiName, reads transparently fall through to the parent (US-383).
  Confirm what *you* changed via the diff endpoint, not by listing.
- **Branch + asOf compose.** Pass `?branch=feature-x&asOf=tx-123` to
  see the branch state at a historical transaction (US-381).
