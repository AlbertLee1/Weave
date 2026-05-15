// Command weave-function-runtime is the in-stack HTTP target for the Tier 3.2
// function-backed action dispatcher (US-215, pkg/actions/http_dispatcher).
//
// It speaks the FunctionRequest / FunctionResponse contract declared in
// pkg/actions/function_dispatcher.go: POST /functions/{rid} with a
// FunctionRequest JSON body returns a FunctionResponse JSON body. The
// out-of-the-box behaviour is a permissive no-op — every call returns
// `{"edits": []}` — which is the right default for local docker-compose dev,
// CI smoke tests, and stand-alone playground use where operators have not yet
// authored real function code. Production deployments should replace this
// container with a real runtime (goja sandbox, V8 isolate, language-specific
// worker, etc.) that respects the same wire contract.
//
// Endpoints:
//
//	GET  /health             -> 200 "ok"
//	POST /functions/{rid}    -> 200 FunctionResponse{Edits: []}
//
// Flags / env:
//
//	-addr / WEAVE_FUNCTION_RUNTIME_ADDR (default :9000)
//
// VTX-124 wires this binary into docker-compose.yml so `make docker-up`
// brings up PG / TimescaleDB / NATS / function-runtime / Weave in one shot.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/liyang/weave/pkg/actions"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("weave-function-runtime", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", defaultAddr(), "Listen address (overrides WEAVE_FUNCTION_RUNTIME_ADDR)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/functions/", handleFunctions(stdout))

	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
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

	fmt.Fprintf(stdout, "weave-function-runtime: listening on %s (POST /functions/{rid}, GET /health)\n", *addr)

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(stderr, "weave-function-runtime: %v\n", err)
		return 1
	}
	<-idle
	return 0
}

func defaultAddr() string {
	if v := os.Getenv("WEAVE_FUNCTION_RUNTIME_ADDR"); v != "" {
		return v
	}
	return ":9000"
}

// handleHealth answers the docker-compose healthcheck and any operator
// liveness probe. Kept off the JSON contract so curl can verify readiness
// without parsing.
func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// handleFunctions implements the no-op stub. Real runtimes plug in here.
// A 405 covers misrouted GETs; malformed JSON returns 400 with the same
// FunctionResponse envelope so downstream parsers don't have to special-case
// the error shape.
func handleFunctions(logTo io.Writer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "function runtime: POST required")
			return
		}
		rid := strings.TrimPrefix(r.URL.Path, "/functions/")
		if rid == "" || strings.Contains(rid, "/") {
			writeError(w, http.StatusBadRequest, "function runtime: invalid function rid in path")
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "function runtime: read body: "+err.Error())
			return
		}
		var req actions.FunctionRequest
		if len(body) > 0 {
			if err := json.Unmarshal(body, &req); err != nil {
				writeError(w, http.StatusBadRequest, "function runtime: parse request: "+err.Error())
				return
			}
		}

		fmt.Fprintf(logTo, "weave-function-runtime: stub call rid=%s actionType=%s params=%d\n",
			rid, req.ActionTypeAPI, len(req.Parameters))

		resp := actions.FunctionResponse{Edits: []actions.FunctionEdit{}}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp := actions.FunctionResponse{Edits: []actions.FunctionEdit{}, Error: msg}
	_ = json.NewEncoder(w).Encode(resp)
}
