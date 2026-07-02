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
)

// runAction dispatches `weave action <subcommand>`. OSV2-304 introduces
// `apply` — submit a single Action and print the validation + edit envelope
// the server returned.
func runAction(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: weave action <apply> [flags]")
		return 2
	}
	switch args[0] {
	case "apply":
		return actionApply(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "weave action: unknown subcommand %q\n", args[0])
		return 2
	}
}

// stringSliceFlag captures `--param key=value` repeated. Each entry is
// kept verbatim and split lazily so callers can distinguish `--param qty=`
// (empty value) from `--param qty` (missing `=`).
type stringSliceFlag []string

func (s *stringSliceFlag) String() string     { return strings.Join(*s, ",") }
func (s *stringSliceFlag) Set(v string) error { *s = append(*s, v); return nil }

func actionApply(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("action apply", flag.ContinueOnError)
	fs.SetOutput(stderr)
	ontology := fs.String("ontology", "", "ontology api name (required)")
	action := fs.String("action", "", "action type api name (required)")
	paramsRef := fs.String("params", "", "JSON params: literal '{...}' or '@/path/to/file.json'")
	var paramKV stringSliceFlag
	fs.Var(&paramKV, "param", "single key=value parameter (repeatable; values are kept as strings)")
	returnEdits := fs.String("returnEdits", "", "edits return policy: ALL | ALL_V2 | NONE (server default = ALL)")
	mode := fs.String("mode", "", "validation mode: VALIDATE_ONLY | VALIDATE_AND_EXECUTE (server default = execute)")
	asJSON := fs.Bool("json", true, "emit the raw JSON response (default true)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*ontology) == "" {
		fmt.Fprintln(stderr, "weave action apply: --ontology is required (usage: weave action apply --ontology <name> --action <api-name> [...])")
		return 2
	}
	if strings.TrimSpace(*action) == "" {
		fmt.Fprintln(stderr, "weave action apply: --action is required (usage: weave action apply --ontology <name> --action <api-name> [...])")
		return 2
	}

	params, err := resolveActionParams(*paramsRef, paramKV)
	if err != nil {
		fmt.Fprintf(stderr, "weave action apply: %v\n", err)
		return 2
	}

	var opts *cliclient.ApplyOptions
	if *returnEdits != "" || *mode != "" {
		opts = &cliclient.ApplyOptions{}
		if *mode != "" {
			opts.Mode = strings.ToUpper(*mode)
		}
		if *returnEdits != "" {
			opts.ReturnEdits = normaliseReturnEdits(*returnEdits)
		}
	}

	c, code := newCLIClient(stderr)
	if c == nil {
		return code
	}
	resp, err := c.ApplyAction(context.Background(), *ontology, *action, params, opts)
	if err != nil {
		printAPIError(stderr, "action apply", err)
		return 1
	}
	if *asJSON {
		buf, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Fprintln(stdout, string(buf))
		return 0
	}
	if resp.Validation != nil {
		fmt.Fprintln(stdout, "validation:", resp.Validation.Result)
	}
	if resp.Edits != nil {
		fmt.Fprintf(stdout, "edits: added=%d modified=%d deleted=%d\n",
			resp.Edits.AddedObjectCount, resp.Edits.ModifiedObjectsCount, resp.Edits.DeletedObjectsCount)
	}
	return 0
}

// resolveActionParams merges --params (file or literal JSON) with --param
// key=value entries. Precedence: --params seeds the map, --param entries
// override individual keys (as strings).
func resolveActionParams(paramsRef string, kvs stringSliceFlag) (map[string]any, error) {
	out := map[string]any{}
	if paramsRef != "" {
		raw, err := readJSONBlobRef(paramsRef)
		if err != nil {
			return nil, fmt.Errorf("--params: %w", err)
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, fmt.Errorf("--params: not a JSON object: %w", err)
		}
	}
	for _, kv := range kvs {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			return nil, fmt.Errorf("--param %q is not in key=value form", kv)
		}
		out[kv[:eq]] = kv[eq+1:]
	}
	if len(out) == 0 {
		// Allow the server to validate "no parameters" rather than rejecting
		// here — some ActionTypes legitimately take none.
		return out, nil
	}
	return out, nil
}

// normaliseReturnEdits accepts the short `ALL_V2` synonym and maps it to the
// Foundry-shaped `ALL_V2_WITH_DELETIONS`. Other values are upper-cased and
// passed through; the server is the authority on what it accepts.
func normaliseReturnEdits(v string) string {
	up := strings.ToUpper(strings.TrimSpace(v))
	if up == "ALL_V2" {
		return "ALL_V2_WITH_DELETIONS"
	}
	return up
}

// readJSONBlobRef returns the bytes for either a literal JSON string ("{...}")
// or `@path/to/file.json`. The path may also be a single dash to read stdin.
func readJSONBlobRef(ref string) ([]byte, error) {
	if ref == "" {
		return nil, errors.New("empty reference")
	}
	if strings.HasPrefix(ref, "@") {
		path := ref[1:]
		if path == "-" {
			return io.ReadAll(os.Stdin)
		}
		return os.ReadFile(path)
	}
	return []byte(ref), nil
}

// printAPIError unwraps a *cliclient.APIError so the caller sees the
// errorCode/errorName rather than the generic "weave: 4xx ..." string.
func printAPIError(stderr io.Writer, prefix string, err error) {
	var apiErr *cliclient.APIError
	if errors.As(err, &apiErr) {
		fmt.Fprintf(stderr, "%s: %d %s/%s\n", prefix, apiErr.StatusCode, apiErr.ErrorCode, apiErr.ErrorName)
		if len(apiErr.Parameters) > 0 {
			buf, _ := json.Marshal(apiErr.Parameters)
			fmt.Fprintf(stderr, "  parameters: %s\n", string(buf))
		}
		return
	}
	fmt.Fprintf(stderr, "%s: %v\n", prefix, err)
}
