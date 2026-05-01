package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/liyang/weave/pkg/fixtures"
)

// runFixtures dispatches the `weave fixtures <subcommand>` family. Today the
// only subcommand is `generate`; future subcommands (validate, compare) plug
// in here without touching main.go.
func runFixtures(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: weave fixtures <generate> [flags]")
		return 2
	}
	switch args[0] {
	case "generate":
		return runFixturesGenerate(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "weave fixtures: unknown subcommand %q\n", args[0])
		return 2
	}
}

// runFixturesGenerate implements `weave fixtures generate`.
//
//	weave fixtures generate \
//	   --ontology <apiName> --type <objectType> --count <N>
//	   [--seed <int>] [--null-ratio <float>]
//	   [--output <path | -.ndjson>] [--format ndjson|json]
//
// It loads the target ObjectType via the fullMetadata endpoint and uses the
// resulting wire shape to drive [fixtures.Generate]. Output is NDJSON to stdout
// by default; pass --format json for a single JSON array.
func runFixturesGenerate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("fixtures generate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	ontology := fs.String("ontology", "", "ontology apiName (required)")
	objType := fs.String("type", "", "objectType apiName (required)")
	count := fs.Int("count", 100, "number of rows to generate")
	seed := fs.Int64("seed", 0, "RNG seed (0 = derive from ontology+type)")
	nullRatio := fs.Float64("null-ratio", 0, "probability a nullable property emits null (0–1)")
	output := fs.String("output", "-", "output path; '-' for stdout")
	format := fs.String("format", "ndjson", "ndjson | json")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *ontology == "" || *objType == "" {
		fmt.Fprintln(stderr, "usage: weave fixtures generate --ontology X --type Y [--count N]")
		return 2
	}
	if *count < 0 {
		fmt.Fprintln(stderr, "weave: --count must be non-negative")
		return 2
	}
	if *format != "ndjson" && *format != "json" {
		fmt.Fprintf(stderr, "weave: --format must be ndjson or json (got %q)\n", *format)
		return 2
	}

	c, code := newCLIClient(stderr)
	if c == nil {
		return code
	}

	meta, err := c.GetObjectTypeFullMetadata(context.Background(), *ontology, *objType)
	if err != nil {
		fmt.Fprintf(stderr, "load object type: %v\n", err)
		return 1
	}
	defs, primaryKeys, err := propsFromFullMetadata(meta)
	if err != nil {
		fmt.Fprintf(stderr, "parse object type metadata: %v\n", err)
		return 1
	}
	if len(defs) == 0 {
		fmt.Fprintf(stderr, "object type %q has no properties\n", *objType)
		return 1
	}

	resolvedSeed := *seed
	if resolvedSeed == 0 {
		resolvedSeed = fixtures.HashSeed(*ontology + "/" + *objType)
	}

	rows, err := fixtures.Generate(defs, *count, fixtures.Options{
		Seed:      resolvedSeed,
		NullRatio: *nullRatio,
	})
	if err != nil {
		fmt.Fprintf(stderr, "generate: %v\n", err)
		return 1
	}

	w, closer, err := openOutput(*output, stdout)
	if err != nil {
		fmt.Fprintf(stderr, "open output: %v\n", err)
		return 1
	}
	defer closer()

	if err := emitRows(w, rows, *format, *objType, primaryKeys); err != nil {
		fmt.Fprintf(stderr, "write output: %v\n", err)
		return 1
	}
	if *output == "-" {
		fmt.Fprintf(stderr, "generated %d rows of %s\n", len(rows), *objType)
	} else {
		fmt.Fprintf(stderr, "generated %d rows of %s -> %s\n", len(rows), *objType, *output)
	}
	return 0
}

// propsFromFullMetadata extracts the properties map and the primaryKeys slice
// from a fullMetadata payload. Both shapes (single-PK string and composite
// PK array) are accepted; the array form takes precedence.
func propsFromFullMetadata(meta map[string]any) ([]fixtures.PropertyDef, []string, error) {
	properties, ok := meta["properties"].(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("metadata has no 'properties' object")
	}
	var pks []string
	if arr, ok := meta["primaryKeys"].([]any); ok {
		for _, v := range arr {
			if s, ok := v.(string); ok {
				pks = append(pks, s)
			}
		}
	}
	if len(pks) == 0 {
		if s, ok := meta["primaryKey"].(string); ok && s != "" {
			pks = []string{s}
		}
	}
	defs, err := fixtures.PropertyDefsFromWire(properties, pks)
	return defs, pks, err
}

func emitRows(w io.Writer, rows []map[string]any, format, objType string, pks []string) error {
	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	default:
		// NDJSON — one JSON object per line. Stamp each row with a
		// __primaryKey envelope so the output mimics the loadObjects
		// shape and can be piped into action.applyBatch via jq.
		enc := json.NewEncoder(w)
		for _, r := range rows {
			row := r
			if len(pks) > 0 {
				row = enrichWithPrimaryKey(r, objType, pks)
			}
			if err := enc.Encode(row); err != nil {
				return err
			}
		}
		return nil
	}
}

func enrichWithPrimaryKey(row map[string]any, objType string, pks []string) map[string]any {
	if len(pks) == 0 {
		return row
	}
	out := make(map[string]any, len(row)+2)
	for k, v := range row {
		out[k] = v
	}
	out["__apiName"] = objType
	if len(pks) == 1 {
		if v, ok := row[pks[0]]; ok {
			out["__primaryKey"] = fmt.Sprint(v)
		}
	} else {
		parts := make([]string, 0, len(pks))
		for _, k := range pks {
			parts = append(parts, fmt.Sprint(row[k]))
		}
		out["__primaryKey"] = joinComposite(parts)
	}
	return out
}

func joinComposite(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ":"
		}
		out += p
	}
	return out
}

func openOutput(path string, stdout io.Writer) (io.Writer, func(), error) {
	if path == "" || path == "-" {
		return stdout, func() {}, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, err
	}
	return f, func() { _ = f.Close() }, nil
}
