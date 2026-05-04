# Chapter 1 — Login

Mint a Bearer token via `POST /api/auth/login`, then send it on every
subsequent request as `Authorization: Bearer <token>`. Under `AUTH_MODE=dev`
the login round-trip is optional — the SDKs treat an empty token as "no
auth header".

## Endpoint shape

Request body:

```json
{ "username": "alice", "password": "..." }
```

Response (200):

```json
{ "token": "eyJhbG...", "expiresAt": "2026-05-06T12:34:56Z" }
```

Errors: `401 InvalidCredentials`, `429 TooManyAttempts`.

---

## Python

```python
import os
import httpx
from weave_client import Client

base = os.environ.get("WEAVE_BASE_URL", "http://localhost:9117")
resp = httpx.post(f"{base}/api/auth/login", json={
    "username": os.environ["WEAVE_USER"],
    "password": os.environ["WEAVE_PASSWORD"],
})
resp.raise_for_status()
token = resp.json()["token"]

client = Client(base, access_token=token)
print(list(client.ontologies.list()))
```

## TypeScript

```typescript
import { WeaveClient } from './client.js';

const base = process.env['WEAVE_BASE_URL'] ?? 'http://localhost:9117';
const resp = await fetch(`${base}/api/auth/login`, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    username: process.env['WEAVE_USER'],
    password: process.env['WEAVE_PASSWORD'],
  }),
});
if (!resp.ok) throw new Error(`login failed: ${resp.status}`);
const { token } = (await resp.json()) as { token: string };

const client = new WeaveClient({ baseUrl: base, token });
console.log(await client.listOntologies());
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

func login(base, user, pw string) (string, error) {
    body, _ := json.Marshal(map[string]string{"username": user, "password": pw})
    resp, err := http.Post(base+"/api/auth/login", "application/json", bytes.NewReader(body))
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()
    if resp.StatusCode != 200 {
        return "", fmt.Errorf("login failed: %d", resp.StatusCode)
    }
    var out struct{ Token string `json:"token"` }
    if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
        return "", err
    }
    return out.Token, nil
}

func main() {
    base := os.Getenv("WEAVE_BASE_URL")
    token, err := login(base, os.Getenv("WEAVE_USER"), os.Getenv("WEAVE_PASSWORD"))
    if err != nil {
        panic(err)
    }
    req, _ := http.NewRequest("GET", base+"/api/v2/ontologies", nil)
    req.Header.Set("Authorization", "Bearer "+token)
    resp, _ := http.DefaultClient.Do(req)
    defer resp.Body.Close()
}
```

## Java

```java
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;

public class Login {
    public static void main(String[] args) throws Exception {
        String base = System.getenv().getOrDefault("WEAVE_BASE_URL", "http://localhost:9117");
        String body = String.format(
            "{\"username\":\"%s\",\"password\":\"%s\"}",
            System.getenv("WEAVE_USER"), System.getenv("WEAVE_PASSWORD"));
        HttpClient http = HttpClient.newHttpClient();
        HttpResponse<String> resp = http.send(
            HttpRequest.newBuilder(URI.create(base + "/api/auth/login"))
                .header("Content-Type", "application/json")
                .POST(HttpRequest.BodyPublishers.ofString(body))
                .build(),
            HttpResponse.BodyHandlers.ofString());
        if (resp.statusCode() != 200) {
            throw new RuntimeException("login failed: " + resp.statusCode());
        }
        // Pull "token":"..." out of the response.
        String token = resp.body().replaceFirst(".*\"token\"\\s*:\\s*\"([^\"]+)\".*", "$1");
        System.out.println("token len=" + token.length());
    }
}
```

## Pitfalls

- **MFA-required accounts** get `202 MFAChallenge` instead of `200 OK`. The
  response carries a `mfaToken` field; you re-POST it to
  `/api/auth/mfa/verify` along with the OTP code to mint the final Bearer.
- **Token expiry** surfaces as `401 InvalidToken` — the SDKs do NOT auto-refresh
  for you. Catch the typed error and re-login.
- **Dev mode** still mounts `/api/auth/login` but accepts any payload. Use
  `AUTH_MODE=token` (or `jwt`) to exercise the real flow.
