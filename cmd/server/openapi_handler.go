package main

import (
	_ "embed"
	"net/http"
)

// openapiSpecYAML is the OpenAPI 3.0.3 spec for the Weave HTTP API.
//
// Source of truth: /api/openapi.yaml at the repo root. Go's //go:embed
// directive cannot reference files outside the package directory, so
// the spec is mirrored to cmd/server/openapi_spec.yaml and embedded
// here. The mirror is kept byte-identical to the source — edits land
// in /api/openapi.yaml and then `make sync-openapi` (or
// `go generate ./cmd/server/`) copies the file across.
//
// TestContract_EmbeddedSpecMatchesCanonical in contract_test.go fails
// loudly if the two files diverge, so a forgotten sync is caught by
// CI before reaching main.
//
//go:generate cp ../../api/openapi.yaml openapi_spec.yaml
//
//go:embed openapi_spec.yaml
var openapiSpecYAML []byte

// openapiSpecHandler serves the embedded OpenAPI YAML at /api/openapi.yaml.
// The content type follows the OpenAPI spec convention: application/yaml with
// a charset hint, falling back to text/yaml for older clients.
func openapiSpecHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(openapiSpecYAML)
	})
}

// swaggerUIHTML is the minimal Swagger UI page that loads the spec from the
// /api/openapi.yaml endpoint. The Swagger UI assets themselves are loaded
// from a public CDN (unpkg.com) so we do not need to vendor JavaScript.
//
// Try-it-out (US-422) is enabled explicitly via tryItOutEnabled so the
// contract is independent of swagger-ui-dist defaults: any future bundle
// upgrade that flipped the default to false would silently break the SDK
// quickstart flow without this guard. persistAuthorization keeps the bearer
// token across page reloads so an interactive session does not have to
// re-paste the JWT after every navigation.
const swaggerUIHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <title>Weave Ontology API — Swagger UI</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css" />
  <style>
    body { margin: 0; background: #fafafa; }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-standalone-preset.js"></script>
  <script>
    window.onload = function() {
      window.ui = SwaggerUIBundle({
        url: "/api/openapi.yaml",
        dom_id: "#swagger-ui",
        deepLinking: true,
        tryItOutEnabled: true,
        displayRequestDuration: true,
        persistAuthorization: true,
        filter: true,
        presets: [
          SwaggerUIBundle.presets.apis,
          SwaggerUIStandalonePreset
        ],
        plugins: [
          SwaggerUIBundle.plugins.DownloadUrl
        ],
        layout: "StandaloneLayout"
      });
    };
  </script>
</body>
</html>
`

// swaggerUIHandler serves a minimal Swagger UI page at /swagger/ that loads
// the embedded YAML from /api/openapi.yaml.
func swaggerUIHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(swaggerUIHTML))
	})
}
