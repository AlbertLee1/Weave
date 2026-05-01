package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
)

// VerifyOptions tunes the verification run.
type VerifyOptions struct {
	// BaseURL is prepended to interaction.Request.Path so a single pact can
	// target /api/v2/... endpoints without each interaction repeating the
	// prefix. Empty means use the path verbatim.
	BaseURL string
	// SetAuth, when non-nil, is invoked after the request is built and before
	// the handler is served — typically used to stamp Authorization or session
	// cookies that the SDK would carry but that aren't part of the consumer
	// contract per se.
	SetAuth func(*http.Request)
}

// VerifyInteraction replays a single interaction against the handler and
// returns a single combined error describing all detected drift, or nil when
// the response satisfies the consumer's expectations.
func VerifyInteraction(handler http.Handler, in Interaction, opts VerifyOptions) error {
	req, err := buildRequest(in.Request, opts)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if opts.SetAuth != nil {
		opts.SetAuth(req)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var problems []string
	if rec.Code != in.Response.Status {
		problems = append(problems, fmt.Sprintf("status: expected %d, got %d (body: %s)", in.Response.Status, rec.Code, truncate(rec.Body.String(), 200)))
	}
	if len(in.Response.Body) > 0 && rec.Body.Len() > 0 {
		var expected, actual interface{}
		if err := json.Unmarshal(in.Response.Body, &expected); err != nil {
			problems = append(problems, fmt.Sprintf("expected body is not valid JSON: %v", err))
		} else if err := json.Unmarshal(rec.Body.Bytes(), &actual); err != nil {
			problems = append(problems, fmt.Sprintf("actual body is not valid JSON: %v (raw: %s)", err, truncate(rec.Body.String(), 200)))
		} else {
			for _, e := range MatchBody(expected, actual, in.Response.Matchers, in.Response.Strict) {
				problems = append(problems, e.Error())
			}
		}
	} else if len(in.Response.Body) > 0 && rec.Body.Len() == 0 {
		problems = append(problems, "expected body but server returned empty body")
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("interaction %q failed:\n  - %s", in.Description, strings.Join(problems, "\n  - "))
}

// VerifyPact runs every interaction in pact against handler and returns the
// per-interaction errors that fired. An empty slice means full pass.
func VerifyPact(handler http.Handler, pact *Pact, opts VerifyOptions) []error {
	var errs []error
	for _, in := range pact.Interactions {
		if err := VerifyInteraction(handler, in, opts); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

func buildRequest(spec Request, opts VerifyOptions) (*http.Request, error) {
	target := opts.BaseURL + spec.Path
	if len(spec.Query) > 0 {
		q := url.Values{}
		for k, v := range spec.Query {
			q.Set(k, v)
		}
		sep := "?"
		if strings.Contains(target, "?") {
			sep = "&"
		}
		target = target + sep + q.Encode()
	}
	var body *bytes.Buffer
	if len(spec.Body) > 0 {
		body = bytes.NewBuffer([]byte(spec.Body))
	} else {
		body = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(spec.Method, target, body)
	for k, v := range spec.Headers {
		req.Header.Set(k, v)
	}
	if len(spec.Body) > 0 && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
