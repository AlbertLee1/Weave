package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/liyang/weave/internal/cliclient"
	"github.com/liyang/weave/pkg/weavepkg"
)

// runPkgInstall implements the `weave pkg install <archive.weavepkg>`
// command (US-412). It reads the archive via pkg/weavepkg.Read, POSTs the
// parsed contents to /api/v2/pkg/install on the server, and renders the
// response (or conflict list).
//
// The conflict-resolution UX is non-interactive: a 409 response prints the
// conflict list and exits 1 unless the operator passed --on-conflict to
// pre-resolve. This keeps the command scriptable; future stories can add an
// interactive `--prompt` mode on top without breaking the existing surface.
func runPkgInstall(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pkg install", flag.ContinueOnError)
	fs.SetOutput(stderr)
	onConflict := fs.String("on-conflict", "fail", "behaviour on apiName conflict: fail|overwrite|skip")
	asJSON := fs.Bool("json", false, "emit the raw server response envelope")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: weave pkg install <archive.weavepkg> [--on-conflict=fail|overwrite|skip] [--json]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) != 1 {
		fmt.Fprintln(stderr, "weave pkg install: exactly one archive path is required")
		fs.Usage()
		return 2
	}
	archive := rest[0]

	switch strings.ToLower(strings.TrimSpace(*onConflict)) {
	case "", "fail", "overwrite", "skip":
	default:
		fmt.Fprintf(stderr, "weave pkg install: --on-conflict must be one of fail|overwrite|skip, got %q\n", *onConflict)
		return 2
	}

	body, err := os.ReadFile(archive)
	if err != nil {
		fmt.Fprintf(stderr, "weave pkg install: read %s: %v\n", archive, err)
		return 1
	}
	pkg, err := weavepkg.ReadBytes(body)
	if err != nil {
		fmt.Fprintf(stderr, "weave pkg install: parse archive: %v\n", err)
		return 1
	}

	c, code := newCLIClient(stderr)
	if c == nil {
		return code
	}

	resp, err := installPackage(context.Background(), c, pkg, *onConflict)
	if err != nil {
		return reportInstallError(stderr, err)
	}

	if *asJSON {
		buf, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Fprintln(stdout, string(buf))
		return 0
	}

	fmt.Fprintf(stdout, "Installed %s@%s into ontology %q (objectTypes=%d, actions=%d, functions=%d, migrations=%d/%d)\n",
		stringField(resp, "name"),
		stringField(resp, "version"),
		stringField(resp, "ontology"),
		intField(resp, "imported", "objectTypes"),
		intField(resp, "imported", "actionTypes"),
		intField(resp, "imported", "functions"),
		intField(resp, "migrationsRan"),
		intField(resp, "migrationsTotal"),
	)
	return 0
}

// installPackage POSTs the parsed archive contents to /api/v2/pkg/install.
// The migration entries are sent verbatim — Go's encoding/json base64
// encodes the []byte payload so non-UTF-8 SQL bodies survive the wire trip.
func installPackage(ctx context.Context, c *cliclient.Client, pkg *weavepkg.Package, onConflict string) (map[string]any, error) {
	migrations := make([]map[string]any, 0, len(pkg.Migrations))
	for _, m := range pkg.Migrations {
		migrations = append(migrations, map[string]any{
			"filename": m.Filename,
			"content":  m.Content,
		})
	}
	body := map[string]any{
		"manifest":   pkg.Manifest,
		"ontology":   json.RawMessage(pkg.Ontology),
		"migrations": migrations,
		"onConflict": onConflict,
	}
	return c.PostInstallPackage(ctx, body)
}

// reportInstallError renders an APIError with conflict-aware UX and returns
// the right CLI exit code (1 for soft failures, 2 for usage errors).
func reportInstallError(stderr io.Writer, err error) int {
	var apiErr *cliclient.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.StatusCode == 409 && apiErr.ErrorName == "PackageConflict":
			fmt.Fprintln(stderr, "weave pkg install: apiName conflicts detected:")
			if conflictsJSON, ok := apiErr.Parameters["conflicts"]; ok {
				var conflicts []map[string]string
				if jerr := json.Unmarshal([]byte(conflictsJSON), &conflicts); jerr == nil {
					for _, c := range conflicts {
						fmt.Fprintf(stderr, "  - %s/%s\n", c["kind"], c["apiName"])
					}
				} else {
					fmt.Fprintf(stderr, "  %s\n", conflictsJSON)
				}
			}
			if hint, ok := apiErr.Parameters["hint"]; ok {
				fmt.Fprintf(stderr, "  hint: %s\n", hint)
			}
			return 1
		case apiErr.ErrorName == "PackageMinWeaveVersionUnsatisfied":
			fmt.Fprintf(stderr, "weave pkg install: server version %s is older than required %s\n",
				apiErr.Parameters["server"], apiErr.Parameters["required"])
			return 1
		}
	}
	fmt.Fprintf(stderr, "weave pkg install: %v\n", err)
	return 1
}

// stringField pulls a nested string out of the response map, returning the
// empty string when any intermediate hop is absent. Tolerates the wire
// shape's loose typing without a struct-decoded dependency.
func stringField(m map[string]any, keys ...string) string {
	cur := any(m)
	for _, k := range keys {
		obj, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = obj[k]
	}
	if s, ok := cur.(string); ok {
		return s
	}
	return ""
}

// intField pulls a nested integer out of the response map, returning 0
// when any hop is missing. JSON numbers come back as float64 from the
// generic decoder so the helper coerces both.
func intField(m map[string]any, keys ...string) int {
	cur := any(m)
	for _, k := range keys {
		obj, ok := cur.(map[string]any)
		if !ok {
			return 0
		}
		cur = obj[k]
	}
	switch v := cur.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	}
	return 0
}
