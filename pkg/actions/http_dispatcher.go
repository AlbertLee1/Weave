package actions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
)

// defaultFunctionTimeout is the per-call timeout the HTTP dispatcher applies
// when the operator did not configure one. 30s mirrors the operator-facing
// default in WEAVE_FUNCTIONS_TIMEOUT and matches the upstream Foundry default.
const defaultFunctionTimeout = 30 * time.Second

// HTTPDispatcher is the production FunctionDispatcher: it POSTs the
// FunctionRequest envelope to {BaseURL}/{FunctionRID} as JSON and converts
// the FunctionResponse back into funnel.Edits.
//
// Zero-value safe: Client and Timeout are filled in lazily so callers can
// construct one with `&HTTPDispatcher{BaseURL: ...}` if needed.
type HTTPDispatcher struct {
	// BaseURL is the function endpoint base, e.g. "http://localhost:9000/functions".
	BaseURL string
	// Client overrides the default HTTP client. If nil, a fresh client is used.
	Client *http.Client
	// Timeout bounds the per-call duration. Defaults to 30s.
	Timeout time.Duration
	// Headers are merged into every outbound request (API key, tracing).
	Headers map[string]string
}

// NewHTTPDispatcher returns an HTTPDispatcher with the default 30s timeout
// and a fresh net/http client. Callers can mutate exported fields after
// construction (e.g. attach Headers, override Timeout) before first use.
func NewHTTPDispatcher(baseURL string) *HTTPDispatcher {
	return &HTTPDispatcher{
		BaseURL: baseURL,
		Client:  &http.Client{Timeout: defaultFunctionTimeout},
		Timeout: defaultFunctionTimeout,
	}
}

// Dispatch POSTs the action envelope to the function endpoint and converts
// the response edits into funnel.Edits.
func (d *HTTPDispatcher) Dispatch(ctx context.Context, at *oms.ActionType, params map[string]interface{}) ([]funnel.Edit, error) {
	if at == nil {
		return nil, fmt.Errorf("function dispatcher: action type is nil")
	}
	if at.FunctionRID == "" {
		return nil, fmt.Errorf("function dispatcher: action type %q has empty FunctionRID", at.APIName)
	}

	body := FunctionRequest{
		ActionTypeRID: at.RID,
		ActionTypeAPI: at.APIName,
		FunctionRID:   at.FunctionRID,
		Parameters:    params,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("function dispatcher: marshal request: %w", err)
	}

	url := joinURL(d.BaseURL, at.FunctionRID)

	timeout := d.Timeout
	if timeout <= 0 {
		timeout = defaultFunctionTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("function dispatcher: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for k, v := range d.Headers {
		req.Header.Set(k, v)
	}

	client := d.Client
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("function dispatcher: call %s: %w", url, err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("function dispatcher: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("function dispatcher: function %s returned status %d: %s",
			at.FunctionRID, resp.StatusCode, strings.TrimSpace(string(respBytes)))
	}

	var fnResp FunctionResponse
	if err := json.Unmarshal(respBytes, &fnResp); err != nil {
		return nil, fmt.Errorf("function dispatcher: decode response from %s: %w", at.FunctionRID, err)
	}
	if fnResp.Error != "" {
		return nil, fmt.Errorf("function dispatcher: function %s reported error: %s", at.FunctionRID, fnResp.Error)
	}

	// Validate the response structure before converting edits.
	if err := ValidateFunctionOutput(&fnResp); err != nil {
		return nil, err
	}

	edits := make([]funnel.Edit, 0, len(fnResp.Edits))
	for i, fe := range fnResp.Edits {
		edit, err := fe.ToFunnelEdit()
		if err != nil {
			return nil, fmt.Errorf("function dispatcher: convert edit %d from %s: %w", i, at.FunctionRID, err)
		}
		edits = append(edits, edit)
	}
	return edits, nil
}

// joinURL appends a path segment to a base URL, handling trailing/leading
// slash mismatches so config typos like "http://x/functions/" still work.
func joinURL(base, segment string) string {
	base = strings.TrimRight(base, "/")
	segment = strings.TrimLeft(segment, "/")
	return base + "/" + segment
}
