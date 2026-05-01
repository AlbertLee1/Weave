// Command weave-mock starts an offline HTTP mock server backed by an
// OpenAPI 3.x specification. It is the offline-test surface mirrored
// into the SDKs: SDK consumers point their generated client at this
// binary and exercise their integration code without standing up the
// full Weave stack.
//
// Usage:
//
//	weave-mock --spec api/openapi.yaml --addr :9090
//	weave-mock --spec api/openapi.yaml --overrides overrides.json
//	weave-mock --spec api/openapi.yaml --admin
//
// The --admin flag mounts POST/DELETE /__mock/overrides for tests that
// need to swap responses at runtime. Off by default — admin endpoints
// leak the mock's existence and should be opt-in.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/liyang/weave/pkg/mockserver"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("weave-mock", flag.ContinueOnError)
	fs.SetOutput(stderr)
	specPath := fs.String("spec", "api/openapi.yaml", "Path to the OpenAPI document to mock")
	addr := fs.String("addr", ":9090", "Listen address")
	overridesPath := fs.String("overrides", "", "Optional JSON file with response overrides (single object or array)")
	admin := fs.Bool("admin", false, "Mount /__mock/overrides admin endpoints for runtime tweaking")
	defaultStatus := fs.Int("default-status", 200, "Status code for operations that declare no responses")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *specPath == "" {
		fmt.Fprintln(stderr, "weave-mock: --spec is required")
		return 2
	}

	spec, err := mockserver.LoadSpecFile(*specPath)
	if err != nil {
		fmt.Fprintf(stderr, "weave-mock: load spec: %v\n", err)
		return 2
	}

	var overrides []mockserver.Override
	if *overridesPath != "" {
		overrides, err = mockserver.LoadOverridesFile(*overridesPath)
		if err != nil {
			fmt.Fprintf(stderr, "weave-mock: load overrides: %v\n", err)
			return 2
		}
	}

	handler, err := mockserver.NewHandler(spec, mockserver.Options{
		Overrides:     overrides,
		EnableAdmin:   *admin,
		DefaultStatus: *defaultStatus,
	})
	if err != nil {
		fmt.Fprintf(stderr, "weave-mock: build handler: %v\n", err)
		return 2
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	idle := make(chan struct{})
	go func() {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		close(idle)
	}()

	fmt.Fprintf(stdout, "weave-mock: serving %d operations from %s on %s (admin=%v, overrides=%d)\n",
		len(spec.Operations), *specPath, *addr, *admin, len(overrides))

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(stderr, "weave-mock: %v\n", err)
		return 1
	}
	<-idle
	return 0
}
