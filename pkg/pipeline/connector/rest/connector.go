// Package rest implements a polling REST API read connector for the
// pipeline framework (US-294). It executes one HTTP request per
// ReadPage call, extracts a JSON array of records via a dotted JSON
// path, and resumes paginated reads via the cursor / page / offset
// strategy declared on the connector's Config.
//
// The connector follows the same pagination contract as the JDBC
// (US-292) and S3 (US-293) connectors: ReadPage returns rows + an
// opaque cursor + a hasMore flag, and resumes from the cursor on the
// next call. The cursor's wire format is strategy-specific:
//
//   - PaginationNone:   ReadPage always returns hasMore=false.
//   - PaginationPage:   cursor = next page number as decimal string.
//   - PaginationOffset: cursor = next row offset as decimal string.
//   - PaginationCursor: cursor = opaque token plucked from the
//     previous response via Pagination.CursorJSONPath.
//
// Example wiring (cursor pagination over a Bearer-authed endpoint):
//
//	c, err := rest.New(rest.Config{
//	    URL:      "https://api.example.com/v1/events",
//	    Method:   http.MethodGet,
//	    Headers:  map[string]string{"Accept": "application/json"},
//	    JSONPath: "data.items",
//	    Auth: rest.Auth{
//	        Type:  rest.AuthBearer,
//	        Token: "sk-...",
//	    },
//	    Pagination: rest.Pagination{
//	        Type:           rest.PaginationCursor,
//	        CursorParam:    "cursor",
//	        CursorJSONPath: "data.nextCursor",
//	    },
//	})
//	for cursor, more := "", true; more; {
//	    rows, next, hasMore, err := c.ReadPage(ctx, cursor)
//	    …
//	    cursor, more = next, hasMore
//	}
package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultPageSize is the per-page row count requested when
// Pagination.PageSize <= 0 for page / offset strategies. Mirrors the
// JDBC + S3 connector defaults at 1000.
const DefaultPageSize = 1000

// MaxPageSize caps the per-page row count so a misconfigured caller
// can't hammer an upstream API for an unreasonably large response.
const MaxPageSize = 100000

// DefaultTimeout is the per-request timeout used when Config.Timeout
// is zero. Conservative enough to cover slow upstreams without
// blocking pipeline scheduler ticks indefinitely.
const DefaultTimeout = 30 * time.Second

// DefaultMaxPages is the safety cap on the number of paginated pages
// drained by a single ReadPage loop, set on the connector when
// Pagination.MaxPages is zero. The connector enforces the cap by
// returning hasMore=false after the cap is reached, so callers may
// resume from a fresh cursor on the next pipeline run.
const DefaultMaxPages = 100000

// Method defaults — applied when Config.Method is empty.
const defaultMethod = http.MethodGet

// PaginationType selects the pagination strategy. Each strategy has
// its own cursor wire format and per-request parameter shape.
type PaginationType string

const (
	// PaginationNone disables pagination — ReadPage always returns
	// hasMore=false. Useful for endpoints whose entire payload fits in
	// one response.
	PaginationNone PaginationType = ""

	// PaginationPage drives "page=N" style pagination. Each call
	// increments the page number; the connector stops when a page
	// returns fewer rows than PageSize OR when the response array is
	// empty.
	PaginationPage PaginationType = "page"

	// PaginationOffset drives "offset=N&limit=M" style pagination.
	// Same termination rules as PaginationPage but the cursor encodes
	// the next row offset rather than a page number.
	PaginationOffset PaginationType = "offset"

	// PaginationCursor drives "cursor=TOKEN" style pagination. The
	// connector reads the next-cursor token out of each response via
	// Pagination.CursorJSONPath and forwards it on the next call. The
	// loop stops when the response yields no cursor (missing key /
	// empty string / null).
	PaginationCursor PaginationType = "cursor"
)

// AuthType selects the authentication scheme applied to every request.
type AuthType string

const (
	// AuthNone disables auth (default).
	AuthNone AuthType = ""
	// AuthBasic adds an "Authorization: Basic ..." header from
	// Auth.Username + Auth.Password.
	AuthBasic AuthType = "basic"
	// AuthBearer adds an "Authorization: Bearer ..." header from
	// Auth.Token.
	AuthBearer AuthType = "bearer"
	// AuthHeader adds a custom header — Auth.Header: Auth.Value. Use
	// for API-key schemes like "X-API-Key: ..." or vendor-specific
	// auth headers.
	AuthHeader AuthType = "header"
)

// Auth carries the credentials applied to outgoing requests. The
// fields used depend on Type; unused fields are ignored.
type Auth struct {
	Type     AuthType
	Username string
	Password string
	Token    string
	Header   string
	Value    string
}

// Pagination tunes the per-request pagination behaviour. Defaults are
// applied at execution time; Validate rejects internally inconsistent
// configurations (e.g. PaginationCursor without CursorJSONPath).
type Pagination struct {
	Type PaginationType

	// PageSize is the per-page row count for page/offset strategies.
	// Surfaced in the request as the SizeParam query parameter.
	// Defaults to DefaultPageSize when <= 0; clamped at MaxPageSize.
	PageSize int

	// PageParam overrides the page-number query parameter name. Only
	// applies to PaginationPage. Defaults to "page" when empty.
	PageParam string

	// OffsetParam overrides the offset query parameter name. Only
	// applies to PaginationOffset. Defaults to "offset" when empty.
	OffsetParam string

	// CursorParam overrides the cursor query parameter name. Only
	// applies to PaginationCursor. Defaults to "cursor" when empty.
	CursorParam string

	// CursorJSONPath is the dotted path to the next-cursor token in
	// the response payload (e.g. "data.nextCursor"). Required for
	// PaginationCursor. Ignored otherwise.
	CursorJSONPath string

	// SizeParam overrides the per-page-size query parameter name.
	// Applies to PaginationPage and PaginationOffset. Defaults to
	// "limit" when empty.
	SizeParam string

	// StartPage is the page number used for the first request when
	// Type=PaginationPage. Defaults to 1. Some APIs use 0-based pages;
	// override accordingly.
	StartPage int

	// MaxPages caps the number of pages traversed in a single
	// ReadPage drain loop. Defaults to DefaultMaxPages when <= 0.
	// Once the cap is hit ReadPage returns hasMore=false even if the
	// upstream still has more pages — the next pipeline run can
	// resume.
	MaxPages int
}

// Config describes one REST polling source. Validate rejects
// structurally invalid configurations; New() / NewWithClient() call
// it before doing any work.
type Config struct {
	// URL is the base URL of the endpoint. Required. Existing query
	// parameters are preserved; pagination params are appended to the
	// final URL on each request.
	URL string

	// Method is the HTTP verb. Defaults to GET when empty.
	Method string

	// Headers is the static request-header set sent with every
	// request. Authorization headers added by Auth take precedence
	// when keys collide.
	Headers map[string]string

	// Body is the optional request body. Sent verbatim with the
	// request's Content-Type as configured in Headers (commonly
	// "application/json"). Treated as a literal byte stream — the
	// connector does NOT template or rewrite it.
	Body string

	// JSONPath is the dotted path to the array of records inside the
	// response payload. Empty path means "the whole response is the
	// records array". Supports nested keys ("data.items") and
	// optional leading "$." for compatibility with JSONPath-style
	// configs ("$.data.items" == "data.items").
	JSONPath string

	// Auth selects the auth scheme + credentials.
	Auth Auth

	// Pagination selects the pagination strategy.
	Pagination Pagination

	// Timeout bounds each HTTP request. Defaults to DefaultTimeout
	// when zero.
	Timeout time.Duration
}

// effectiveMethod returns the HTTP method to use, defaulting to GET.
func (c *Config) effectiveMethod() string {
	if c.Method == "" {
		return defaultMethod
	}
	return strings.ToUpper(c.Method)
}

// effectiveTimeout returns the per-request timeout, defaulting to
// DefaultTimeout.
func (c *Config) effectiveTimeout() time.Duration {
	if c.Timeout <= 0 {
		return DefaultTimeout
	}
	return c.Timeout
}

// effectivePageSize applies the per-strategy default + cap rules.
func (p *Pagination) effectivePageSize() int {
	if p.PageSize <= 0 {
		return DefaultPageSize
	}
	if p.PageSize > MaxPageSize {
		return MaxPageSize
	}
	return p.PageSize
}

// effectiveStartPage returns the first page number for page-based
// pagination, defaulting to 1.
func (p *Pagination) effectiveStartPage() int {
	if p.StartPage <= 0 {
		return 1
	}
	return p.StartPage
}

// effectivePageParam returns the page-number query parameter name.
func (p *Pagination) effectivePageParam() string {
	if p.PageParam == "" {
		return "page"
	}
	return p.PageParam
}

// effectiveOffsetParam returns the offset query parameter name.
func (p *Pagination) effectiveOffsetParam() string {
	if p.OffsetParam == "" {
		return "offset"
	}
	return p.OffsetParam
}

// effectiveCursorParam returns the cursor query parameter name.
func (p *Pagination) effectiveCursorParam() string {
	if p.CursorParam == "" {
		return "cursor"
	}
	return p.CursorParam
}

// effectiveSizeParam returns the per-page-size query parameter name.
func (p *Pagination) effectiveSizeParam() string {
	if p.SizeParam == "" {
		return "limit"
	}
	return p.SizeParam
}

// effectiveMaxPages returns the safety cap on traversed pages.
func (p *Pagination) effectiveMaxPages() int {
	if p.MaxPages <= 0 {
		return DefaultMaxPages
	}
	return p.MaxPages
}

// Validate reports the first structural issue with c. Pure function;
// safe to call from admin handlers / pipeline-DSL parsers before
// performing any network I/O.
func (c Config) Validate() error {
	if c.URL == "" {
		return errors.New("rest: Config.URL must not be empty")
	}
	if _, err := url.Parse(c.URL); err != nil {
		return fmt.Errorf("rest: Config.URL is invalid: %w", err)
	}
	switch m := strings.ToUpper(c.Method); m {
	case "", http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodHead:
		// ok
	default:
		return fmt.Errorf("rest: unsupported Method %q", c.Method)
	}
	if c.Timeout < 0 {
		return fmt.Errorf("rest: Config.Timeout must be >= 0 (got %s)", c.Timeout)
	}
	if err := c.Auth.Validate(); err != nil {
		return err
	}
	if err := c.Pagination.Validate(); err != nil {
		return err
	}
	return nil
}

// Validate reports the first structural issue with the auth config.
func (a Auth) Validate() error {
	switch a.Type {
	case AuthNone:
		return nil
	case AuthBasic:
		if a.Username == "" {
			return errors.New("rest: Auth.Username required for AuthBasic")
		}
		return nil
	case AuthBearer:
		if a.Token == "" {
			return errors.New("rest: Auth.Token required for AuthBearer")
		}
		return nil
	case AuthHeader:
		if a.Header == "" {
			return errors.New("rest: Auth.Header required for AuthHeader")
		}
		return nil
	default:
		return fmt.Errorf("rest: unsupported Auth.Type %q (supported: \"\", basic, bearer, header)", a.Type)
	}
}

// Validate reports the first structural issue with the pagination
// config.
func (p Pagination) Validate() error {
	switch p.Type {
	case PaginationNone, PaginationPage, PaginationOffset:
		// ok
	case PaginationCursor:
		if p.CursorJSONPath == "" {
			return errors.New("rest: Pagination.CursorJSONPath required for PaginationCursor")
		}
	default:
		return fmt.Errorf("rest: unsupported Pagination.Type %q (supported: \"\", page, offset, cursor)", p.Type)
	}
	if p.PageSize < 0 {
		return fmt.Errorf("rest: Pagination.PageSize must be >= 0 (got %d)", p.PageSize)
	}
	if p.MaxPages < 0 {
		return fmt.Errorf("rest: Pagination.MaxPages must be >= 0 (got %d)", p.MaxPages)
	}
	return nil
}

// HTTPDoer is the minimal interface the connector needs from an HTTP
// client. Production callers pass a *http.Client (configured with
// timeouts, retries, transport, …); tests can satisfy this with an
// httptest.Server or a stub.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Connector is one open REST source. Connectors are safe for
// concurrent use; the underlying *http.Client is responsible for its
// own concurrency story (Go's net/http client is goroutine-safe).
type Connector struct {
	client HTTPDoer
	cfg    Config
}

// New wraps a Config in a Connector that uses an internal *http.Client
// configured with cfg.effectiveTimeout(). The default client is
// sufficient for most callers; tests + advanced production wiring
// (custom transport, mTLS, retries) should use NewWithClient.
func New(cfg Config) (*Connector, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: cfg.effectiveTimeout()}
	return &Connector{client: client, cfg: cfg}, nil
}

// NewWithClient wraps a Config + caller-provided HTTPDoer. cfg is
// validated at construction; subsequent ReadPage calls trust the
// config. Use this with httptest.Server for unit tests:
//
//	srv := httptest.NewServer(handler)
//	c, _ := rest.NewWithClient(srv.Client(), rest.Config{URL: srv.URL, ...})
//
// or with a custom *http.Client for advanced production wiring.
func NewWithClient(client HTTPDoer, cfg Config) (*Connector, error) {
	if client == nil {
		return nil, errors.New("rest: NewWithClient requires a non-nil HTTPDoer")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Connector{client: client, cfg: cfg}, nil
}

// ReadPage runs ONE paginated HTTP round-trip per call.
//
// cursor semantics:
//   - "" — first call. The connector seeds the strategy-specific
//     starting state (page = StartPage, offset = 0, cursor token
//     empty).
//   - non-empty — strategy-specific:
//       PaginationNone   — invalid (returns error).
//       PaginationPage   — decimal page number for the next request.
//       PaginationOffset — decimal row offset for the next request.
//       PaginationCursor — opaque cursor token plucked from the
//                          previous response.
//
// Return values:
//   - rows: the decoded record array. Each map carries one record's
//     keys / values as produced by encoding/json — strings for JSON
//     strings, float64 for JSON numbers, etc. An empty array still
//     returns []map[string]any{} — never nil.
//   - nextCursor: the cursor for the next call. Empty when hasMore is
//     false.
//   - hasMore: true when the connector knows there's at least one
//     more page to read. False once the strategy's termination rule
//     fires (short page, missing cursor, MaxPages cap, …).
//   - err: surfaced from net/http, JSON decoding, or non-2xx HTTP
//     responses. The cursor state is left unmodified on err so
//     callers may resume from the same cursor on the next attempt.
func (c *Connector) ReadPage(ctx context.Context, cursor string) (rows []map[string]any, nextCursor string, hasMore bool, err error) {
	switch c.cfg.Pagination.Type {
	case PaginationNone:
		return c.readNone(ctx, cursor)
	case PaginationPage:
		return c.readPage(ctx, cursor)
	case PaginationOffset:
		return c.readOffset(ctx, cursor)
	case PaginationCursor:
		return c.readCursor(ctx, cursor)
	default:
		return nil, "", false, fmt.Errorf("rest: unsupported pagination type %q", c.cfg.Pagination.Type)
	}
}

// readNone executes the request once and returns hasMore=false.
// Cursor is meaningless in this mode; reject non-empty input so
// misconfigured callers don't silently re-read.
func (c *Connector) readNone(ctx context.Context, cursor string) ([]map[string]any, string, bool, error) {
	if cursor != "" {
		return nil, "", false, fmt.Errorf("rest: cursor must be empty for PaginationNone (got %q)", cursor)
	}
	body, err := c.executeRequest(ctx, nil)
	if err != nil {
		return nil, "", false, err
	}
	rows, err := extractRecords(body, c.cfg.JSONPath)
	if err != nil {
		return nil, "", false, err
	}
	return rows, "", false, nil
}

// readPage drives "page=N" pagination. Empty cursor seeds the page
// counter at Pagination.effectiveStartPage(); non-empty cursors are
// parsed as decimal page numbers.
func (c *Connector) readPage(ctx context.Context, cursor string) ([]map[string]any, string, bool, error) {
	page := c.cfg.Pagination.effectiveStartPage()
	if cursor != "" {
		n, err := strconv.Atoi(cursor)
		if err != nil || n < 0 {
			return nil, "", false, fmt.Errorf("rest: cursor %q is not a non-negative integer page", cursor)
		}
		page = n
	}
	size := c.cfg.Pagination.effectivePageSize()
	params := url.Values{}
	params.Set(c.cfg.Pagination.effectivePageParam(), strconv.Itoa(page))
	params.Set(c.cfg.Pagination.effectiveSizeParam(), strconv.Itoa(size))
	body, err := c.executeRequest(ctx, params)
	if err != nil {
		return nil, "", false, err
	}
	rows, err := extractRecords(body, c.cfg.JSONPath)
	if err != nil {
		return nil, "", false, err
	}
	hasMore := len(rows) >= size && c.belowPageCap(page+1)
	if !hasMore {
		return rows, "", false, nil
	}
	return rows, strconv.Itoa(page + 1), true, nil
}

// readOffset drives "offset=N&limit=M" pagination. Empty cursor seeds
// offset at 0; non-empty cursors are parsed as decimal offsets.
func (c *Connector) readOffset(ctx context.Context, cursor string) ([]map[string]any, string, bool, error) {
	offset := 0
	if cursor != "" {
		n, err := strconv.Atoi(cursor)
		if err != nil || n < 0 {
			return nil, "", false, fmt.Errorf("rest: cursor %q is not a non-negative integer offset", cursor)
		}
		offset = n
	}
	size := c.cfg.Pagination.effectivePageSize()
	params := url.Values{}
	params.Set(c.cfg.Pagination.effectiveOffsetParam(), strconv.Itoa(offset))
	params.Set(c.cfg.Pagination.effectiveSizeParam(), strconv.Itoa(size))
	body, err := c.executeRequest(ctx, params)
	if err != nil {
		return nil, "", false, err
	}
	rows, err := extractRecords(body, c.cfg.JSONPath)
	if err != nil {
		return nil, "", false, err
	}
	pageIdx := offset/size + 1
	hasMore := len(rows) >= size && c.belowPageCap(pageIdx+1)
	if !hasMore {
		return rows, "", false, nil
	}
	return rows, strconv.Itoa(offset + len(rows)), true, nil
}

// readCursor drives "cursor=TOKEN" pagination. Empty cursor seeds the
// first request without the cursor param; non-empty cursors are
// forwarded verbatim.
func (c *Connector) readCursor(ctx context.Context, cursor string) ([]map[string]any, string, bool, error) {
	params := url.Values{}
	if cursor != "" {
		params.Set(c.cfg.Pagination.effectiveCursorParam(), cursor)
	}
	if size := c.cfg.Pagination.PageSize; size > 0 {
		// PageSize is optional for cursor pagination — only forward
		// when the caller explicitly set it, so endpoints that don't
		// accept a size parameter aren't poked with one.
		params.Set(c.cfg.Pagination.effectiveSizeParam(), strconv.Itoa(c.cfg.Pagination.effectivePageSize()))
	}
	body, err := c.executeRequest(ctx, params)
	if err != nil {
		return nil, "", false, err
	}
	rows, err := extractRecords(body, c.cfg.JSONPath)
	if err != nil {
		return nil, "", false, err
	}
	next, err := extractCursor(body, c.cfg.Pagination.CursorJSONPath)
	if err != nil {
		return nil, "", false, err
	}
	hasMore := next != ""
	if !hasMore {
		return rows, "", false, nil
	}
	return rows, next, true, nil
}

// belowPageCap returns true while pageIdx is below the configured
// MaxPages safety cap. Returning false short-circuits hasMore so the
// caller stops mid-stream — they can resume on the next pipeline run
// from the cursor we'd otherwise have produced.
func (c *Connector) belowPageCap(pageIdx int) bool {
	cap := c.cfg.Pagination.effectiveMaxPages()
	return pageIdx <= cap
}

// executeRequest builds + sends one HTTP request and returns the
// decoded response body bytes. Non-2xx responses surface as a typed
// error so callers can distinguish transport failures from server-
// side rejections.
func (c *Connector) executeRequest(ctx context.Context, extraParams url.Values) ([]byte, error) {
	finalURL, err := mergeURL(c.cfg.URL, extraParams)
	if err != nil {
		return nil, err
	}
	var body io.Reader
	if c.cfg.Body != "" {
		body = strings.NewReader(c.cfg.Body)
	}
	req, err := http.NewRequestWithContext(ctx, c.cfg.effectiveMethod(), finalURL, body)
	if err != nil {
		return nil, fmt.Errorf("rest: build request: %w", err)
	}
	for k, v := range c.cfg.Headers {
		req.Header.Set(k, v)
	}
	if err := applyAuth(req, c.cfg.Auth); err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rest: do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Buffer a bounded prefix of the response body so the caller
		// can see what the server actually said — crucial for
		// diagnosing 4xx responses without spelunking server logs.
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, &HTTPError{Status: resp.StatusCode, Body: string(bytes.TrimSpace(preview))}
	}
	return io.ReadAll(resp.Body)
}

// HTTPError surfaces a non-2xx HTTP response as a typed error so
// callers can distinguish transport failures (`rest: do: ...`) from
// server-side rejections.
type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("rest: http %d", e.Status)
	}
	return fmt.Sprintf("rest: http %d: %s", e.Status, e.Body)
}

// mergeURL appends extraParams to base's existing query string. base's
// own params are preserved; pagination params overwrite same-named
// existing entries (caller-supplied "page=" in URL wins for the first
// request only since extraParams is merged via url.Values.Set).
func mergeURL(base string, extra url.Values) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("rest: parse url: %w", err)
	}
	if len(extra) == 0 {
		return u.String(), nil
	}
	q := u.Query()
	for k, vs := range extra {
		for _, v := range vs {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// applyAuth sets the request's auth headers per a's Type.
func applyAuth(req *http.Request, a Auth) error {
	switch a.Type {
	case AuthNone:
		return nil
	case AuthBasic:
		req.SetBasicAuth(a.Username, a.Password)
		return nil
	case AuthBearer:
		req.Header.Set("Authorization", "Bearer "+a.Token)
		return nil
	case AuthHeader:
		req.Header.Set(a.Header, a.Value)
		return nil
	default:
		return fmt.Errorf("rest: unsupported auth type %q", a.Type)
	}
}

// extractRecords decodes body as JSON and pulls the records array
// from the dotted path. An empty path treats the entire payload as
// the array. Non-array values at the path are an error — the
// connector's contract is "a list of records", and silently coercing
// a single-object response would mask schema bugs.
func extractRecords(body []byte, path string) ([]map[string]any, error) {
	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("rest: decode response: %w", err)
	}
	val, err := walkPath(raw, path)
	if err != nil {
		return nil, err
	}
	arr, ok := val.([]any)
	if !ok {
		return nil, fmt.Errorf("rest: jsonPath %q resolved to %T, want array", path, val)
	}
	rows := make([]map[string]any, 0, len(arr))
	for i, el := range arr {
		obj, ok := el.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("rest: jsonPath %q[%d] is %T, want object", path, i, el)
		}
		rows = append(rows, obj)
	}
	return rows, nil
}

// extractCursor reads the next-cursor token from the response body
// at the dotted path. Missing keys / null / empty string surface as
// "" so callers can treat all three as "end of stream". Non-string
// values get fmt.Sprintf'd so numeric cursors round-trip — APIs
// occasionally return integer ids as the next cursor.
func extractCursor(body []byte, path string) (string, error) {
	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		return "", fmt.Errorf("rest: decode response: %w", err)
	}
	val, err := walkPathOptional(raw, path)
	if err != nil {
		return "", err
	}
	if val == nil {
		return "", nil
	}
	switch v := val.(type) {
	case string:
		return v, nil
	case float64:
		// json.Unmarshal turns all JSON numbers into float64; treat
		// integer-valued cursors as integers on the wire.
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10), nil
		}
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	case bool:
		return strconv.FormatBool(v), nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}
