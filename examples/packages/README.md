# Built-in Example Packages (US-414)

This directory holds the source material for the three .weavepkg archives the
Marketplace UI exposes under its **Built-in** section.

Each subdirectory is a single package:

| Package    | Ontology   | Description                                                  |
| ---------- | ---------- | ------------------------------------------------------------ |
| `northwind/` | `northwind` | Customer / Order / Product sales ledger.                    |
| `chinook/`   | `chinook`   | Artist / Album / Track / Customer / Invoice media catalog.  |
| `iot-demo/`  | `iotDemo`   | Device / Sensor / Reading telemetry tutorial.               |

## Layout

```
examples/packages/
├── README.md
├── embed.go                 # `//go:embed` declaration consumed by the server
├── northwind/
│   ├── manifest.json        # name / version / author / license / description
│   └── ontology.json        # OntologyExport-shaped envelope
├── chinook/
└── iot-demo/
```

`manifest.json` and `ontology.json` are the canonical source of truth. The
backend embeds them via `//go:embed` and exposes them through:

- `GET  /api/v2/pkg/builtin` — list the available built-in packages
- `POST /api/v2/pkg/builtin/{name}/install` — one-click install

## Building physical .weavepkg archives

These packages can also be materialised to disk as `.weavepkg` ZIP archives by
running:

```bash
weave-cli pkg build-examples --output examples/packages
```

That writes `examples/packages/{name}.weavepkg` for each of the three packages
above. The Marketplace install flow does NOT need these files — it serves the
embedded source directly — but they are useful for offline testing of the
`weave pkg install <archive>` CLI surface.
