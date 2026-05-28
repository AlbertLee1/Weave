package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestBDD_MCP_CompletionComplete covers PRD-V2 Gap-D4 round 46:
// the MCP completion/complete protocol method must respond with a
// well-shaped completion envelope so AI clients can offer
// autocomplete UX for prompt arguments and resource template
// variables.
//
// Three concerns:
//  1. Protocol contract — params validation matches the MCP spec,
//     malformed requests get -32602 InvalidParams with clear msgs.
//  2. Provider integration — a wired CompletionSource's results
//     flow through verbatim (subject to the 100-value cap).
//  3. Default no-source state — completion/complete still returns
//     a valid empty envelope so clients don't see "method not
//     found" and abandon autocomplete UX.
//
// Acceptance criteria (Given → When → Then):
//
//	Given a Server with NO completion source wired
//	When  completion/complete arrives with valid ref+argument
//	Then  it returns success with completion.values=[],
//	      total=0, hasMore=false
//
//	Given a Server with a provider that yields 3 candidates
//	When  completion/complete arrives
//	Then  the values are returned in the provider's order,
//	      total=3, hasMore=false
//
//	Given a provider that yields >100 candidates
//	When  completion/complete arrives
//	Then  values is truncated to 100, hasMore=true, total
//	      reflects the pre-truncation count
//
//	Given completion/complete with missing ref.type
//	When  the handler runs
//	Then  -32602 InvalidParams with a clear message
//
//	Given ref.type="ref/prompt" without ref.name
//	When  the handler runs
//	Then  -32602 InvalidParams mentioning ref.name is required
//
//	Given ref.type="ref/resource" without ref.uri
//	When  the handler runs
//	Then  -32602 InvalidParams mentioning ref.uri is required
//
//	Given an unknown ref.type
//	When  the handler runs
//	Then  -32602 InvalidParams mentioning the unsupported type
//
//	Given missing argument.name
//	When  the handler runs
//	Then  -32602 InvalidParams mentioning argument.name
//
//	Given an empty params field
//	When  the handler runs
//	Then  -32602 InvalidParams (params is required)
//
//	Given the initialize handshake
//	When  it runs
//	Then  capabilities advertises "completions" so clients know
//	      the server supports the method.
func TestBDD_MCP_CompletionComplete(t *testing.T) {
	t.Run("no source wired → empty completion envelope", func(t *testing.T) {
		srv := NewServer(nil, nil, nil)
		req := mustCompletionRequest(t, CompletionParams{
			Ref:      CompletionRef{Type: "ref/prompt", Name: "northwind/createOrder"},
			Argument: CompletionArgument{Name: "customerId", Value: "ALF"},
		})
		resp := srv.Handle(context.Background(), req)
		got := unmarshalCompletionResult(t, resp)
		if len(got.Values) != 0 || got.Total != 0 || got.HasMore {
			t.Errorf("empty source: completion = %+v, want zero-state", got)
		}
	})

	t.Run("wired source: values flow through, total + hasMore correct", func(t *testing.T) {
		srv := NewServer(nil, nil, nil)
		srv.SetCompletionSource(&staticCompletionSource{
			values: []string{"ALFKI", "ANATR", "ANTON"},
		})
		req := mustCompletionRequest(t, CompletionParams{
			Ref:      CompletionRef{Type: "ref/prompt", Name: "northwind/createOrder"},
			Argument: CompletionArgument{Name: "customerId", Value: "A"},
		})
		resp := srv.Handle(context.Background(), req)
		got := unmarshalCompletionResult(t, resp)
		if got.Total != 3 || len(got.Values) != 3 || got.HasMore {
			t.Errorf("got %+v, want {Total:3, len(Values):3, HasMore:false}", got)
		}
		if got.Values[0] != "ALFKI" || got.Values[2] != "ANTON" {
			t.Errorf("provider order not preserved: %v", got.Values)
		}
	})

	t.Run("source yields >100: response truncated, hasMore=true", func(t *testing.T) {
		values := make([]string, 0, 250)
		for i := 0; i < 250; i++ {
			// Lexicographic-friendly padding so a future sort doesn't
			// shuffle the verification.
			values = append(values, padInt(i, 3))
		}
		srv := NewServer(nil, nil, nil)
		srv.SetCompletionSource(&staticCompletionSource{values: values})
		req := mustCompletionRequest(t, CompletionParams{
			Ref:      CompletionRef{Type: "ref/resource", URI: "weave://objecttype/nw/Customer"},
			Argument: CompletionArgument{Name: "primaryKey", Value: ""},
		})
		resp := srv.Handle(context.Background(), req)
		got := unmarshalCompletionResult(t, resp)
		if len(got.Values) != 100 {
			t.Errorf("len(Values) = %d, want 100 (cap)", len(got.Values))
		}
		if got.Total != 250 {
			t.Errorf("Total = %d, want 250 (pre-truncation count)", got.Total)
		}
		if !got.HasMore {
			t.Error("HasMore = false, want true when truncated")
		}
	})

	t.Run("missing ref.type → -32602 InvalidParams", func(t *testing.T) {
		srv := NewServer(nil, nil, nil)
		req := mustCompletionRequest(t, CompletionParams{
			Argument: CompletionArgument{Name: "x"},
		})
		resp := srv.Handle(context.Background(), req)
		expectError(t, resp, CodeInvalidParams, "ref.type is required")
	})

	t.Run(`ref/prompt missing ref.name → InvalidParams`, func(t *testing.T) {
		srv := NewServer(nil, nil, nil)
		req := mustCompletionRequest(t, CompletionParams{
			Ref:      CompletionRef{Type: "ref/prompt"},
			Argument: CompletionArgument{Name: "x"},
		})
		expectError(t, srv.Handle(context.Background(), req), CodeInvalidParams, "ref.name is required")
	})

	t.Run(`ref/resource missing ref.uri → InvalidParams`, func(t *testing.T) {
		srv := NewServer(nil, nil, nil)
		req := mustCompletionRequest(t, CompletionParams{
			Ref:      CompletionRef{Type: "ref/resource"},
			Argument: CompletionArgument{Name: "x"},
		})
		expectError(t, srv.Handle(context.Background(), req), CodeInvalidParams, "ref.uri is required")
	})

	t.Run("unsupported ref.type → InvalidParams with the bad type echoed", func(t *testing.T) {
		srv := NewServer(nil, nil, nil)
		req := mustCompletionRequest(t, CompletionParams{
			Ref:      CompletionRef{Type: "ref/never-heard-of-this", Name: "x"},
			Argument: CompletionArgument{Name: "x"},
		})
		expectError(t, srv.Handle(context.Background(), req), CodeInvalidParams, "ref/never-heard-of-this")
	})

	t.Run("missing argument.name → InvalidParams", func(t *testing.T) {
		srv := NewServer(nil, nil, nil)
		req := mustCompletionRequest(t, CompletionParams{
			Ref: CompletionRef{Type: "ref/prompt", Name: "x"},
		})
		expectError(t, srv.Handle(context.Background(), req), CodeInvalidParams, "argument.name is required")
	})

	t.Run("empty params → InvalidParams", func(t *testing.T) {
		srv := NewServer(nil, nil, nil)
		req := &Request{ID: json.RawMessage(`1`), Method: "completion/complete"}
		expectError(t, srv.Handle(context.Background(), req), CodeInvalidParams, "params is required")
	})

	t.Run("initialize advertises completions capability", func(t *testing.T) {
		srv := NewServer(nil, nil, nil)
		resp := srv.Handle(context.Background(), &Request{ID: json.RawMessage(`1`), Method: "initialize"})
		if resp.Error != nil {
			t.Fatalf("initialize error: %+v", resp.Error)
		}
		result, ok := resp.Result.(map[string]any)
		if !ok {
			t.Fatalf("result not a map: %T", resp.Result)
		}
		caps, ok := result["capabilities"].(map[string]any)
		if !ok {
			t.Fatalf("capabilities missing or not a map: %T", result["capabilities"])
		}
		if _, ok := caps["completions"]; !ok {
			t.Error("capabilities.completions missing; clients won't try the method")
		}
	})
}

func TestPrefixFilter_FiltersAndCaps(t *testing.T) {
	all := []string{"Order", "OrderItem", "Customer", "OrderHistory", "Order"} // duplicate "Order"
	got := PrefixFilter(all, "Ord", 10)
	want := []string{"Order", "OrderHistory", "OrderItem"} // sorted, deduped, prefix-matched
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("PrefixFilter got %v, want %v", got, want)
	}

	got2 := PrefixFilter(all, "", 2)
	if len(got2) != 2 {
		t.Errorf("limit=2: got %d, want 2", len(got2))
	}

	got3 := PrefixFilter(all, "ord", 10)
	if len(got3) != 3 {
		t.Errorf("case-insensitive: got %d, want 3", len(got3))
	}
}

// ----------------------------------------------------------------------
// helpers
// ----------------------------------------------------------------------

type staticCompletionSource struct {
	values []string
}

func (s *staticCompletionSource) Complete(_ context.Context, _ CompletionRef, _ CompletionArgument, _ int) ([]string, error) {
	out := make([]string, len(s.values))
	copy(out, s.values)
	return out, nil
}

func mustCompletionRequest(t *testing.T, params CompletionParams) *Request {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return &Request{ID: json.RawMessage(`1`), Method: "completion/complete", Params: raw}
}

func unmarshalCompletionResult(t *testing.T, resp *Response) CompletionValues {
	t.Helper()
	if resp == nil {
		t.Fatal("nil response")
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error response: %+v", resp.Error)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result not a map: %T", resp.Result)
	}
	completion, ok := result["completion"]
	if !ok {
		t.Fatalf("completion key missing in result: %v", result)
	}
	// Round-trip through JSON so the type-assertions match the CompletionValues
	// struct definition without relying on internal map shapes.
	raw, err := json.Marshal(completion)
	if err != nil {
		t.Fatalf("re-marshal completion: %v", err)
	}
	var out CompletionValues
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal completion: %v", err)
	}
	return out
}

func expectError(t *testing.T, resp *Response, code int, msgSub string) {
	t.Helper()
	if resp == nil {
		t.Fatal("nil response")
	}
	if resp.Error == nil {
		t.Fatalf("expected error %d, got success: %v", code, resp.Result)
	}
	if resp.Error.Code != code {
		t.Errorf("error code = %d, want %d", resp.Error.Code, code)
	}
	if !strings.Contains(resp.Error.Message, msgSub) {
		t.Errorf("error msg = %q, want it to mention %q", resp.Error.Message, msgSub)
	}
}

// padInt returns the decimal representation of n padded with zeros
// to the requested width — used for deterministic, sort-stable
// candidate sets in the >100 truncation test.
func padInt(n, width int) string {
	s := strings.Builder{}
	dec := []byte{}
	if n == 0 {
		dec = []byte{'0'}
	} else {
		for n > 0 {
			dec = append([]byte{byte('0' + n%10)}, dec...)
			n /= 10
		}
	}
	for len(dec) < width {
		dec = append([]byte{'0'}, dec...)
	}
	s.Write(dec)
	return s.String()
}
