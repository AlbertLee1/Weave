package queryexec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/liyang/weave/pkg/oms"
)

// HTTPQueryExecutor dispatches query execution to an external HTTP function
// service. The FunctionRID is used as the endpoint URL directly.
type HTTPQueryExecutor struct {
	client  *http.Client
	timeout time.Duration
}

// NewHTTPQueryExecutor creates an HTTPQueryExecutor with default settings.
func NewHTTPQueryExecutor() *HTTPQueryExecutor {
	return &HTTPQueryExecutor{
		client:  &http.Client{Timeout: 30 * time.Second},
		timeout: 30 * time.Second,
	}
}

// Execute POSTs the query parameters to the FunctionRID URL and returns the
// parsed JSON response.
func (e *HTTPQueryExecutor) Execute(ctx context.Context, qt *oms.QueryType, params map[string]interface{}) (interface{}, error) {
	body := map[string]interface{}{
		"queryTypeRid":     qt.RID,
		"queryTypeApiName": qt.APIName,
		"functionRid":      qt.FunctionRID,
		"parameters":       params,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("query executor: marshal request: %w", err)
	}

	callCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, qt.FunctionRID, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("query executor: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query executor: call %s: %w", qt.FunctionRID, err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("query executor: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("query executor: function %s returned status %d: %s",
			qt.FunctionRID, resp.StatusCode, strings.TrimSpace(string(respBytes)))
	}

	var result interface{}
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("query executor: decode response: %w", err)
	}

	return result, nil
}
