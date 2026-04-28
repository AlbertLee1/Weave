package rest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// drainAll loops ReadPage until !hasMore and returns the flattened
// row slice plus the call count. Mirrors the s3 connector test
// helper for cross-connector consistency.
func drainAll(t *testing.T, c *Connector) ([]map[string]any, int) {
	t.Helper()
	all := []map[string]any{}
	calls := 0
	cursor := ""
	for {
		rows, next, more, err := c.ReadPage(context.Background(), cursor)
		if err != nil {
			t.Fatalf("ReadPage[%d]: %v", calls, err)
		}
		calls++
		all = append(all, rows...)
		if !more {
			break
		}
		cursor = next
		if calls > 100 {
			t.Fatalf("drainAll: cursor pagination did not terminate after 100 calls")
		}
	}
	return all, calls
}

// --- Config / Auth / Pagination validation ---

func TestConfig_Validate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"empty", Config{}, true},
		{"missing-url", Config{Method: http.MethodGet}, true},
		{"good-default", Config{URL: "http://example.com"}, false},
		{"good-explicit-get", Config{URL: "http://example.com", Method: "GET"}, false},
		{"good-post", Config{URL: "http://example.com", Method: "POST"}, false},
		{"good-lowercase", Config{URL: "http://example.com", Method: "get"}, false},
		{"unsupported-method", Config{URL: "http://example.com", Method: "TRACE"}, true},
		{"negative-timeout", Config{URL: "http://example.com", Timeout: -1}, true},
		{"good-bearer", Config{URL: "http://example.com", Auth: Auth{Type: AuthBearer, Token: "x"}}, false},
		{"bearer-missing-token", Config{URL: "http://example.com", Auth: Auth{Type: AuthBearer}}, true},
		{"basic-missing-user", Config{URL: "http://example.com", Auth: Auth{Type: AuthBasic, Password: "p"}}, true},
		{"header-missing-key", Config{URL: "http://example.com", Auth: Auth{Type: AuthHeader, Value: "x"}}, true},
		{"unknown-auth", Config{URL: "http://example.com", Auth: Auth{Type: "oauth2"}}, true},
		{"cursor-no-path", Config{URL: "http://example.com", Pagination: Pagination{Type: PaginationCursor}}, true},
		{"cursor-with-path", Config{URL: "http://example.com", Pagination: Pagination{Type: PaginationCursor, CursorJSONPath: "next"}}, false},
		{"unknown-pagination", Config{URL: "http://example.com", Pagination: Pagination{Type: "weird"}}, true},
		{"negative-pagesize", Config{URL: "http://example.com", Pagination: Pagination{Type: PaginationPage, PageSize: -1}}, true},
		{"negative-maxpages", Config{URL: "http://example.com", Pagination: Pagination{Type: PaginationPage, MaxPages: -5}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestPagination_DefaultsAndCaps(t *testing.T) {
	p := Pagination{}
	if p.effectivePageSize() != DefaultPageSize {
		t.Errorf("default page size = %d want %d", p.effectivePageSize(), DefaultPageSize)
	}
	p.PageSize = MaxPageSize + 100
	if got := p.effectivePageSize(); got != MaxPageSize {
		t.Errorf("oversized page size = %d want clamp to %d", got, MaxPageSize)
	}
	p = Pagination{}
	if p.effectiveStartPage() != 1 {
		t.Errorf("default start page = %d want 1", p.effectiveStartPage())
	}
	if p.effectivePageParam() != "page" {
		t.Errorf("default PageParam = %q", p.effectivePageParam())
	}
	if p.effectiveOffsetParam() != "offset" {
		t.Errorf("default OffsetParam = %q", p.effectiveOffsetParam())
	}
	if p.effectiveCursorParam() != "cursor" {
		t.Errorf("default CursorParam = %q", p.effectiveCursorParam())
	}
	if p.effectiveSizeParam() != "limit" {
		t.Errorf("default SizeParam = %q", p.effectiveSizeParam())
	}
}

// --- New / NewWithClient ---

func TestNew_RejectsInvalidConfig(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("empty config should be rejected")
	}
}

func TestNewWithClient_RequiresClient(t *testing.T) {
	if _, err := NewWithClient(nil, Config{URL: "http://x"}); err == nil {
		t.Fatal("nil client should be rejected")
	}
}

// --- JSONPath ---

func TestWalkPath_Basics(t *testing.T) {
	root := map[string]any{
		"data": map[string]any{
			"items": []any{"a", "b"},
		},
	}
	cases := []struct {
		path string
		want any
	}{
		{"", root},
		{"$", root},
		{"data", root["data"]},
		{"$.data", root["data"]},
		{"data.items", []any{"a", "b"}},
		{"$.data.items", []any{"a", "b"}},
		{"data.items.0", "a"},
		{"data.items.1", "b"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			got, err := walkPath(root, tc.path)
			if err != nil {
				t.Fatalf("walkPath(%q): %v", tc.path, err)
			}
			if !equalAny(got, tc.want) {
				t.Errorf("walkPath(%q) = %v want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestWalkPath_MissingKeyIsError(t *testing.T) {
	root := map[string]any{"data": map[string]any{}}
	if _, err := walkPath(root, "data.missing"); err == nil {
		t.Fatal("missing key should error in walkPath")
	}
}

func TestWalkPathOptional_MissingIsClean(t *testing.T) {
	root := map[string]any{"data": map[string]any{}}
	got, err := walkPathOptional(root, "data.missing.deep")
	if err != nil {
		t.Fatalf("walkPathOptional missing: %v", err)
	}
	if got != nil {
		t.Errorf("walkPathOptional missing = %v want nil", got)
	}
}

func TestWalkPath_ArrayIndexOutOfRange(t *testing.T) {
	root := map[string]any{"items": []any{"a"}}
	if _, err := walkPath(root, "items.5"); err == nil {
		t.Fatal("out-of-range index should error in walkPath")
	}
}

func TestWalkPath_DescendIntoPrimitive(t *testing.T) {
	root := map[string]any{"x": "scalar"}
	if _, err := walkPath(root, "x.y"); err == nil {
		t.Fatal("descending into a primitive should error")
	}
}

// --- PaginationNone ---

func TestReadPage_PaginationNone_SingleRequest(t *testing.T) {
	calls := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = io.WriteString(w, `[{"id":1},{"id":2}]`)
	}))
	defer srv.Close()
	c, err := NewWithClient(srv.Client(), Config{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	rows, next, more, err := c.ReadPage(context.Background(), "")
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if len(rows) != 2 || rows[0]["id"] != float64(1) {
		t.Errorf("rows=%v", rows)
	}
	if more || next != "" {
		t.Errorf("more=%v next=%q want false/empty", more, next)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("server hit %d times, want 1", got)
	}
}

func TestReadPage_PaginationNone_RejectsNonEmptyCursor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[]`)
	}))
	defer srv.Close()
	c, _ := NewWithClient(srv.Client(), Config{URL: srv.URL})
	if _, _, _, err := c.ReadPage(context.Background(), "junk"); err == nil {
		t.Fatal("PaginationNone should reject non-empty cursor")
	}
}

// --- PaginationPage ---

func TestReadPage_Page_Pagination(t *testing.T) {
	// 11 rows, PageSize=5 → pages 1,2,3 with sizes 5,5,1.
	rowsByPage := map[int][]map[string]any{
		1: makeRows(1, 5),
		2: makeRows(6, 10),
		3: makeRows(11, 11),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page == 0 {
			page = 1
		}
		writeJSONArray(w, rowsByPage[page])
	}))
	defer srv.Close()
	c, err := NewWithClient(srv.Client(), Config{
		URL:        srv.URL,
		Pagination: Pagination{Type: PaginationPage, PageSize: 5},
	})
	if err != nil {
		t.Fatal(err)
	}
	all, calls := drainAll(t, c)
	if len(all) != 11 {
		t.Fatalf("rows=%d want 11", len(all))
	}
	if calls != 3 {
		t.Errorf("calls=%d want 3", calls)
	}
	if all[0]["id"] != float64(1) || all[10]["id"] != float64(11) {
		t.Errorf("rows order wrong: first=%v last=%v", all[0]["id"], all[10]["id"])
	}
}

func TestReadPage_Page_RespectsStartPage(t *testing.T) {
	var pagesSeen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pagesSeen = append(pagesSeen, r.URL.Query().Get("page"))
		writeJSONArray(w, makeRows(1, 1)) // always one row → less than PageSize, terminates
	}))
	defer srv.Close()
	c, _ := NewWithClient(srv.Client(), Config{
		URL:        srv.URL,
		Pagination: Pagination{Type: PaginationPage, PageSize: 5, StartPage: 7},
	})
	if _, _, _, err := c.ReadPage(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if len(pagesSeen) != 1 || pagesSeen[0] != "7" {
		t.Errorf("first page = %v want [7]", pagesSeen)
	}
}

func TestReadPage_Page_CursorParseError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSONArray(w, nil)
	}))
	defer srv.Close()
	c, _ := NewWithClient(srv.Client(), Config{
		URL:        srv.URL,
		Pagination: Pagination{Type: PaginationPage},
	})
	if _, _, _, err := c.ReadPage(context.Background(), "not-a-number"); err == nil {
		t.Fatal("malformed page cursor should error")
	}
}

func TestReadPage_Page_MaxPagesCap(t *testing.T) {
	// Always return a full page → would loop forever absent the cap.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSONArray(w, makeRows(1, 5))
	}))
	defer srv.Close()
	c, _ := NewWithClient(srv.Client(), Config{
		URL:        srv.URL,
		Pagination: Pagination{Type: PaginationPage, PageSize: 5, MaxPages: 3},
	})
	all, calls := drainAll(t, c)
	if calls != 3 {
		t.Errorf("calls=%d want 3 (MaxPages cap)", calls)
	}
	if len(all) != 15 {
		t.Errorf("rows=%d want 15", len(all))
	}
}

// --- PaginationOffset ---

func TestReadPage_Offset_Pagination(t *testing.T) {
	// 7 rows total, PageSize=3 → pages at offsets 0,3,6.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		end := offset + limit
		if end > 7 {
			end = 7
		}
		var rows []map[string]any
		for i := offset; i < end; i++ {
			rows = append(rows, map[string]any{"id": i + 1})
		}
		writeJSONArray(w, rows)
	}))
	defer srv.Close()
	c, _ := NewWithClient(srv.Client(), Config{
		URL:        srv.URL,
		Pagination: Pagination{Type: PaginationOffset, PageSize: 3},
	})
	all, _ := drainAll(t, c)
	if len(all) != 7 {
		t.Fatalf("rows=%d want 7", len(all))
	}
	for i, r := range all {
		if r["id"] != float64(i+1) {
			t.Errorf("row[%d].id=%v want %d", i, r["id"], i+1)
		}
	}
}

func TestReadPage_Offset_ParamOverrides(t *testing.T) {
	var seenOffset, seenLimit string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenOffset = r.URL.Query().Get("skip")
		seenLimit = r.URL.Query().Get("take")
		writeJSONArray(w, nil)
	}))
	defer srv.Close()
	c, _ := NewWithClient(srv.Client(), Config{
		URL: srv.URL,
		Pagination: Pagination{
			Type:        PaginationOffset,
			PageSize:    50,
			OffsetParam: "skip",
			SizeParam:   "take",
		},
	})
	if _, _, _, err := c.ReadPage(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if seenOffset != "0" || seenLimit != "50" {
		t.Errorf("offset=%q limit=%q want 0/50", seenOffset, seenLimit)
	}
}

// --- PaginationCursor ---

func TestReadPage_Cursor_Chain(t *testing.T) {
	// Three-page chain: rows 1-2 (cursor=p2), 3-4 (cursor=p3), 5 (no cursor)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := r.URL.Query().Get("cursor")
		var resp map[string]any
		switch cur {
		case "":
			resp = map[string]any{
				"data": map[string]any{
					"items":      makeRows(1, 2),
					"nextCursor": "p2",
				},
			}
		case "p2":
			resp = map[string]any{
				"data": map[string]any{
					"items":      makeRows(3, 4),
					"nextCursor": "p3",
				},
			}
		case "p3":
			resp = map[string]any{
				"data": map[string]any{
					"items":      makeRows(5, 5),
					"nextCursor": "",
				},
			}
		default:
			http.Error(w, "unexpected cursor", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	c, _ := NewWithClient(srv.Client(), Config{
		URL:      srv.URL,
		JSONPath: "data.items",
		Pagination: Pagination{
			Type:           PaginationCursor,
			CursorJSONPath: "data.nextCursor",
		},
	})
	all, calls := drainAll(t, c)
	if len(all) != 5 || calls != 3 {
		t.Fatalf("rows=%d calls=%d want 5/3", len(all), calls)
	}
	for i, r := range all {
		if r["id"] != float64(i+1) {
			t.Errorf("row[%d].id=%v want %d", i, r["id"], i+1)
		}
	}
}

func TestReadPage_Cursor_NumericCursor(t *testing.T) {
	// API returns next-cursor as an integer; connector should treat
	// it as the cursor wire string for the next request.
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := r.URL.Query().Get("cursor")
		calls++
		switch cur {
		case "":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": makeRows(1, 1),
				"next":  42,
			})
		case "42":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": makeRows(2, 2),
				"next":  nil,
			})
		default:
			http.Error(w, "unexpected cursor "+cur, http.StatusBadRequest)
		}
	}))
	defer srv.Close()
	c, _ := NewWithClient(srv.Client(), Config{
		URL:      srv.URL,
		JSONPath: "items",
		Pagination: Pagination{
			Type:           PaginationCursor,
			CursorJSONPath: "next",
		},
	})
	all, _ := drainAll(t, c)
	if len(all) != 2 {
		t.Fatalf("rows=%d want 2 (numeric cursor should round-trip)", len(all))
	}
	if calls != 2 {
		t.Errorf("calls=%d want 2", calls)
	}
}

func TestReadPage_Cursor_MissingCursorTerminates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No "next" key at all → walkPathOptional returns nil →
		// hasMore=false.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": makeRows(1, 1),
		})
	}))
	defer srv.Close()
	c, _ := NewWithClient(srv.Client(), Config{
		URL:      srv.URL,
		JSONPath: "items",
		Pagination: Pagination{
			Type:           PaginationCursor,
			CursorJSONPath: "next",
		},
	})
	rows, _, more, err := c.ReadPage(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if more {
		t.Error("missing cursor key should terminate the loop")
	}
	if len(rows) != 1 {
		t.Errorf("rows=%d want 1", len(rows))
	}
}

func TestReadPage_Cursor_ForwardsPageSize(t *testing.T) {
	var seen url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.Query()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []any{},
			"next":  "",
		})
	}))
	defer srv.Close()
	c, _ := NewWithClient(srv.Client(), Config{
		URL:      srv.URL,
		JSONPath: "items",
		Pagination: Pagination{
			Type:           PaginationCursor,
			CursorJSONPath: "next",
			PageSize:       25,
		},
	})
	if _, _, _, err := c.ReadPage(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if seen.Get("limit") != "25" {
		t.Errorf("limit=%q want 25 (PageSize should forward when set)", seen.Get("limit"))
	}
}

func TestReadPage_Cursor_OmitsPageSizeWhenZero(t *testing.T) {
	var seen url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.Query()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []any{},
			"next":  "",
		})
	}))
	defer srv.Close()
	c, _ := NewWithClient(srv.Client(), Config{
		URL:      srv.URL,
		JSONPath: "items",
		Pagination: Pagination{
			Type:           PaginationCursor,
			CursorJSONPath: "next",
		},
	})
	if _, _, _, err := c.ReadPage(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := seen["limit"]; ok {
		t.Errorf("limit=%v should be absent when PageSize is zero", seen["limit"])
	}
}

// --- Auth ---

func TestAuth_Bearer(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, `[]`)
	}))
	defer srv.Close()
	c, _ := NewWithClient(srv.Client(), Config{
		URL:  srv.URL,
		Auth: Auth{Type: AuthBearer, Token: "sk-abc"},
	})
	if _, _, _, err := c.ReadPage(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if got != "Bearer sk-abc" {
		t.Errorf("Authorization=%q want %q", got, "Bearer sk-abc")
	}
}

func TestAuth_Basic(t *testing.T) {
	var user, pass string
	var ok bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok = r.BasicAuth()
		_, _ = io.WriteString(w, `[]`)
	}))
	defer srv.Close()
	c, _ := NewWithClient(srv.Client(), Config{
		URL:  srv.URL,
		Auth: Auth{Type: AuthBasic, Username: "alice", Password: "wonderland"},
	})
	if _, _, _, err := c.ReadPage(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if !ok || user != "alice" || pass != "wonderland" {
		t.Errorf("basic auth got user=%q pass=%q ok=%v", user, pass, ok)
	}
}

func TestAuth_HeaderCustom(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-API-Key")
		_, _ = io.WriteString(w, `[]`)
	}))
	defer srv.Close()
	c, _ := NewWithClient(srv.Client(), Config{
		URL:  srv.URL,
		Auth: Auth{Type: AuthHeader, Header: "X-API-Key", Value: "secret123"},
	})
	if _, _, _, err := c.ReadPage(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if got != "secret123" {
		t.Errorf("X-API-Key=%q want secret123", got)
	}
}

func TestHeaders_StaticPropagation(t *testing.T) {
	var accept, ua string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accept = r.Header.Get("Accept")
		ua = r.Header.Get("User-Agent")
		_, _ = io.WriteString(w, `[]`)
	}))
	defer srv.Close()
	c, _ := NewWithClient(srv.Client(), Config{
		URL: srv.URL,
		Headers: map[string]string{
			"Accept":     "application/json",
			"User-Agent": "weave-pipeline/1.0",
		},
	})
	if _, _, _, err := c.ReadPage(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if accept != "application/json" || ua != "weave-pipeline/1.0" {
		t.Errorf("headers got accept=%q ua=%q", accept, ua)
	}
}

// --- POST + body ---

func TestReadPage_POSTWithBody(t *testing.T) {
	var method string
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		body, _ = io.ReadAll(r.Body)
		_, _ = io.WriteString(w, `[]`)
	}))
	defer srv.Close()
	c, _ := NewWithClient(srv.Client(), Config{
		URL:    srv.URL,
		Method: http.MethodPost,
		Body:   `{"q":"foo"}`,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
	})
	if _, _, _, err := c.ReadPage(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost {
		t.Errorf("method=%q want POST", method)
	}
	if string(body) != `{"q":"foo"}` {
		t.Errorf("body=%q want %q", body, `{"q":"foo"}`)
	}
}

// --- Errors ---

func TestReadPage_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"err":"forbidden"}`, http.StatusForbidden)
	}))
	defer srv.Close()
	c, _ := NewWithClient(srv.Client(), Config{URL: srv.URL})
	_, _, _, err := c.ReadPage(context.Background(), "")
	if err == nil {
		t.Fatal("expected non-2xx to surface an error")
	}
	var herr *HTTPError
	if !errors.As(err, &herr) {
		t.Fatalf("expected *HTTPError; got %T %v", err, err)
	}
	if herr.Status != http.StatusForbidden {
		t.Errorf("status=%d want %d", herr.Status, http.StatusForbidden)
	}
	if !strings.Contains(herr.Body, "forbidden") {
		t.Errorf("body preview missing 'forbidden': %q", herr.Body)
	}
}

func TestReadPage_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `not json`)
	}))
	defer srv.Close()
	c, _ := NewWithClient(srv.Client(), Config{URL: srv.URL})
	if _, _, _, err := c.ReadPage(context.Background(), ""); err == nil {
		t.Fatal("expected JSON decode error")
	}
}

func TestReadPage_JSONPathNotArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":{"items":42}}`)
	}))
	defer srv.Close()
	c, _ := NewWithClient(srv.Client(), Config{URL: srv.URL, JSONPath: "data.items"})
	if _, _, _, err := c.ReadPage(context.Background(), ""); err == nil {
		t.Fatal("non-array path should error")
	}
}

func TestReadPage_JSONPathArrayOfPrimitives(t *testing.T) {
	// Records must be objects; primitives at the array slot fail.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"items":[1,2,3]}`)
	}))
	defer srv.Close()
	c, _ := NewWithClient(srv.Client(), Config{URL: srv.URL, JSONPath: "items"})
	if _, _, _, err := c.ReadPage(context.Background(), ""); err == nil {
		t.Fatal("array of primitives should error")
	}
}

// --- URL handling ---

func TestMergeURL_PreservesExistingQuery(t *testing.T) {
	var seen url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.Query()
		writeJSONArray(w, makeRows(1, 1))
	}))
	defer srv.Close()
	// Append our own ?team=alpha to the server URL — pagination
	// params should be added without nuking it.
	base := srv.URL + "/?team=alpha"
	c, _ := NewWithClient(srv.Client(), Config{
		URL:        base,
		Pagination: Pagination{Type: PaginationPage, PageSize: 1},
	})
	if _, _, _, err := c.ReadPage(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if seen.Get("team") != "alpha" {
		t.Errorf("team param dropped: %v", seen)
	}
	if seen.Get("page") != "1" {
		t.Errorf("page param missing: %v", seen)
	}
}

// --- Context cancellation ---

func TestReadPage_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
			writeJSONArray(w, nil)
		case <-r.Context().Done():
			return
		}
	}))
	defer srv.Close()
	c, _ := NewWithClient(srv.Client(), Config{URL: srv.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, _, _, err := c.ReadPage(ctx, ""); err == nil {
		t.Fatal("expected context cancellation to surface")
	}
}

// --- HTTP errors are typed ---

func TestHTTPError_StringFormat(t *testing.T) {
	err := &HTTPError{Status: 404, Body: "not found"}
	if got := err.Error(); !strings.Contains(got, "404") || !strings.Contains(got, "not found") {
		t.Errorf("Error()=%q does not embed status/body", got)
	}
	bare := &HTTPError{Status: 500}
	if got := bare.Error(); !strings.Contains(got, "500") {
		t.Errorf("bare HTTPError missing status: %q", got)
	}
}

// --- helpers ---

// makeRows produces a closed [from, to] inclusive range of rows
// shaped like {"id": N}. Returns nil for empty ranges.
func makeRows(from, to int) []map[string]any {
	if to < from {
		return nil
	}
	rows := make([]map[string]any, 0, to-from+1)
	for i := from; i <= to; i++ {
		rows = append(rows, map[string]any{"id": i})
	}
	return rows
}

// writeJSONArray writes rows as a JSON array (or "null" for nil) to
// w. Test helper — keeps response-writing one line per case.
func writeJSONArray(w http.ResponseWriter, rows []map[string]any) {
	if rows == nil {
		_, _ = io.WriteString(w, `[]`)
		return
	}
	_ = json.NewEncoder(w).Encode(rows)
}

// equalAny compares two arbitrary JSON-decoded values for equality.
// Used by jsonpath tests to assert traversal results without
// importing reflect.DeepEqual everywhere.
func equalAny(a, b any) bool {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, va := range av {
			vb, ok := bv[k]
			if !ok || !equalAny(va, vb) {
				return false
			}
		}
		return true
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !equalAny(av[i], bv[i]) {
				return false
			}
		}
		return true
	default:
		return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
	}
}
