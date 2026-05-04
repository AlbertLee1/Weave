// Command weave-pact publishes consumer-authored pact files to a Pact
// Broker and lists the latest pacts a broker holds for a provider.
// Designed for the US-445 multi-language SDK contract gate; the same wire
// shape is verified locally by `make test-contract` (no broker needed).
//
// Usage:
//
//	# Publish every cmd/server/testdata/pacts/*.pact.json to a local broker.
//	weave-pact publish \
//	    -broker http://localhost:9292 \
//	    -dir cmd/server/testdata/pacts \
//	    -version $(git rev-parse --short HEAD)
//
//	# List every pact the broker holds for the weave-server provider.
//	weave-pact list -broker http://localhost:9292 -provider weave-server
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/liyang/weave/pkg/contract"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "weave-pact:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printUsage(stderr)
		return fmt.Errorf("subcommand required")
	}
	switch args[0] {
	case "publish":
		return runPublish(args[1:], stdout, stderr)
	case "list":
		return runList(args[1:], stdout)
	case "-h", "--help", "help":
		printUsage(stdout)
		return nil
	default:
		printUsage(stderr)
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: weave-pact <subcommand> [flags]")
	fmt.Fprintln(w, "Subcommands:")
	fmt.Fprintln(w, "  publish  Publish pact files in a directory to a Pact Broker")
	fmt.Fprintln(w, "  list     List the latest pacts a broker holds for a provider")
}

func runPublish(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("publish", flag.ContinueOnError)
	fs.SetOutput(stderr)
	broker := fs.String("broker", os.Getenv("WEAVE_PACT_BROKER_URL"), "Pact Broker base URL (or WEAVE_PACT_BROKER_URL env)")
	dir := fs.String("dir", "cmd/server/testdata/pacts", "directory to scan for *.pact.json files")
	version := fs.String("version", "", "consumer version (defaults to WEAVE_PACT_VERSION env or 'dev')")
	auth := fs.String("auth", os.Getenv("WEAVE_PACT_BROKER_AUTH"), "Authorization header value (or WEAVE_PACT_BROKER_AUTH env)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *broker == "" {
		return fmt.Errorf("-broker is required (or set WEAVE_PACT_BROKER_URL)")
	}
	resolvedVersion := *version
	if resolvedVersion == "" {
		resolvedVersion = os.Getenv("WEAVE_PACT_VERSION")
	}
	if resolvedVersion == "" {
		resolvedVersion = "dev"
	}

	matches, err := filepath.Glob(filepath.Join(*dir, "*.pact.json"))
	if err != nil {
		return fmt.Errorf("scan %s: %w", *dir, err)
	}
	if len(matches) == 0 {
		return fmt.Errorf("no *.pact.json files under %s", *dir)
	}
	sort.Strings(matches)
	client := contract.NewBrokerClient(*broker, contract.BrokerClientOptions{
		AuthHeader: *auth,
	})
	for _, path := range matches {
		pact, err := contract.LoadPact(path)
		if err != nil {
			return fmt.Errorf("load %s: %w", path, err)
		}
		if err := client.PublishPact(pact, resolvedVersion); err != nil {
			return fmt.Errorf("publish %s: %w", filepath.Base(path), err)
		}
		fmt.Fprintf(stdout, "published %s (consumer=%s, provider=%s, version=%s)\n",
			filepath.Base(path), pact.Consumer.Name, pact.Provider.Name, resolvedVersion)
	}
	fmt.Fprintf(stdout, "OK — published %d pact(s) to %s\n", len(matches), strings.TrimRight(*broker, "/"))
	return nil
}

func runList(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	broker := fs.String("broker", os.Getenv("WEAVE_PACT_BROKER_URL"), "Pact Broker base URL (or WEAVE_PACT_BROKER_URL env)")
	provider := fs.String("provider", "weave-server", "Provider name to list pacts for")
	auth := fs.String("auth", os.Getenv("WEAVE_PACT_BROKER_AUTH"), "Authorization header value (or WEAVE_PACT_BROKER_AUTH env)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *broker == "" {
		return fmt.Errorf("-broker is required (or set WEAVE_PACT_BROKER_URL)")
	}
	client := contract.NewBrokerClient(*broker, contract.BrokerClientOptions{
		AuthHeader: *auth,
	})
	pacts, err := client.FetchProviderPacts(*provider)
	if err != nil {
		return err
	}
	if len(pacts) == 0 {
		fmt.Fprintf(stdout, "broker has no pacts for provider %q\n", *provider)
		return nil
	}
	consumers := make([]string, 0, len(pacts))
	for _, p := range pacts {
		consumers = append(consumers, p.Consumer.Name)
	}
	sort.Strings(consumers)
	for _, name := range consumers {
		fmt.Fprintln(stdout, name)
	}
	return nil
}
