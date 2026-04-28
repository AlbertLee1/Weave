package subscriptions

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	"github.com/liyang/weave/pkg/oss/objectset"
)

// fakeResolver is an in-memory ObjectSetResolver for tests.
type fakeResolver struct {
	defs map[string]*objectset.Definition
	err  error
}

func (f *fakeResolver) Get(id string) (*objectset.Definition, error) {
	if f.err != nil {
		return nil, f.err
	}
	def, ok := f.defs[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return def, nil
}

// ---------- matchesDefinition unit tests ----------

func TestMatchesDefinition_Base(t *testing.T) {
	def := &objectset.Definition{Type: "base", ObjectType: "Employee"}
	if !matchesDefinition(def, "Employee", "e1", map[string]interface{}{"name": "John"}) {
		t.Error("base set should match same objectType")
	}
	if matchesDefinition(def, "Department", "d1", map[string]interface{}{}) {
		t.Error("base set should not match different objectType")
	}
}

func TestMatchesDefinition_Filter(t *testing.T) {
	def := &objectset.Definition{
		Type:      "filter",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "Employee"},
		Where:     json.RawMessage(`{"type":"eq","field":"department","value":"Engineering"}`),
	}
	if !matchesDefinition(def, "Employee", "e1", map[string]interface{}{"department": "Engineering"}) {
		t.Error("filter should match when where clause is satisfied")
	}
	if matchesDefinition(def, "Employee", "e2", map[string]interface{}{"department": "Sales"}) {
		t.Error("filter should not match when where clause fails")
	}
	if matchesDefinition(def, "Department", "d1", map[string]interface{}{"department": "Engineering"}) {
		t.Error("filter should not match wrong objectType even if where matches")
	}
}

func TestMatchesDefinition_Filter_EmptyWhere(t *testing.T) {
	def := &objectset.Definition{
		Type:      "filter",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "Employee"},
	}
	if !matchesDefinition(def, "Employee", "e1", map[string]interface{}{}) {
		t.Error("filter with empty where should pass through inner match")
	}
}

func TestMatchesDefinition_Union(t *testing.T) {
	def := &objectset.Definition{
		Type: "union",
		ObjectSets: []*objectset.Definition{
			{Type: "base", ObjectType: "Employee"},
			{Type: "base", ObjectType: "Department"},
		},
	}
	if !matchesDefinition(def, "Employee", "e1", map[string]interface{}{}) {
		t.Error("union should match when any child matches")
	}
	if !matchesDefinition(def, "Department", "d1", map[string]interface{}{}) {
		t.Error("union should match either child type")
	}
	if matchesDefinition(def, "Project", "p1", map[string]interface{}{}) {
		t.Error("union should not match unrelated type")
	}
}

func TestMatchesDefinition_Intersect(t *testing.T) {
	// Intersect of (base Employee) ∩ (filter Employee where dept=Engineering)
	def := &objectset.Definition{
		Type: "intersect",
		ObjectSets: []*objectset.Definition{
			{Type: "base", ObjectType: "Employee"},
			{
				Type:      "filter",
				ObjectSet: &objectset.Definition{Type: "base", ObjectType: "Employee"},
				Where:     json.RawMessage(`{"type":"eq","field":"department","value":"Engineering"}`),
			},
		},
	}
	if !matchesDefinition(def, "Employee", "e1", map[string]interface{}{"department": "Engineering"}) {
		t.Error("intersect should match when both children match")
	}
	if matchesDefinition(def, "Employee", "e2", map[string]interface{}{"department": "Sales"}) {
		t.Error("intersect should not match when one child fails")
	}
}

func TestMatchesDefinition_Subtract(t *testing.T) {
	// (base Employee) - (filter Employee where dept=Sales)
	def := &objectset.Definition{
		Type: "subtract",
		ObjectSets: []*objectset.Definition{
			{Type: "base", ObjectType: "Employee"},
			{
				Type:      "filter",
				ObjectSet: &objectset.Definition{Type: "base", ObjectType: "Employee"},
				Where:     json.RawMessage(`{"type":"eq","field":"department","value":"Sales"}`),
			},
		},
	}
	if !matchesDefinition(def, "Employee", "e1", map[string]interface{}{"department": "Engineering"}) {
		t.Error("subtract should match Employee not in Sales")
	}
	if matchesDefinition(def, "Employee", "e2", map[string]interface{}{"department": "Sales"}) {
		t.Error("subtract should exclude Employee in Sales")
	}
	if matchesDefinition(def, "Department", "d1", map[string]interface{}{}) {
		t.Error("subtract should not match different type")
	}
}

func TestMatchesDefinition_Static(t *testing.T) {
	def := &objectset.Definition{
		Type:        "static",
		ObjectType:  "Employee",
		PrimaryKeys: []string{"e1", "e2", "e3"},
	}
	if !matchesDefinition(def, "Employee", "e2", map[string]interface{}{}) {
		t.Error("static should match enumerated PK")
	}
	if matchesDefinition(def, "Employee", "e99", map[string]interface{}{}) {
		t.Error("static should not match PK outside enumeration")
	}
	if matchesDefinition(def, "Department", "e2", map[string]interface{}{}) {
		t.Error("static should not match wrong objectType")
	}
}

func TestMatchesDefinition_AsType(t *testing.T) {
	def := &objectset.Definition{
		Type:       "asType",
		ObjectType: "Employee",
		ObjectSet:  &objectset.Definition{Type: "base", ObjectType: "Employee"},
	}
	if !matchesDefinition(def, "Employee", "e1", map[string]interface{}{}) {
		t.Error("asType should match its declared type")
	}
	if matchesDefinition(def, "Department", "d1", map[string]interface{}{}) {
		t.Error("asType should not match other type")
	}
}

func TestMatchesDefinition_WithProperties_Passthrough(t *testing.T) {
	def := &objectset.Definition{
		Type:      "withProperties",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "Employee"},
	}
	if !matchesDefinition(def, "Employee", "e1", map[string]interface{}{}) {
		t.Error("withProperties should inherit inner membership")
	}
	if matchesDefinition(def, "Department", "d1", map[string]interface{}{}) {
		t.Error("withProperties should not match outside inner set")
	}
}

func TestMatchesDefinition_Nil(t *testing.T) {
	if matchesDefinition(nil, "Employee", "e1", nil) {
		t.Error("nil definition should not match")
	}
}

func TestMatchesDefinition_UnsupportedType_FailsClosed(t *testing.T) {
	// Unsupported types must short-circuit to false; subscribe-time validation
	// would normally reject them, so reaching this branch implies a bug — fail
	// closed rather than blindly admitting events.
	def := &objectset.Definition{
		Type:      "searchAround",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "Employee"},
		Link:      "manages",
	}
	if matchesDefinition(def, "Employee", "e1", map[string]interface{}{}) {
		t.Error("unsupported types must fail closed")
	}
}

// ---------- supportedDefinitionType validation tests ----------

func TestSupportedDefinitionType_OK(t *testing.T) {
	cases := []*objectset.Definition{
		{Type: "base", ObjectType: "X"},
		{Type: "static", ObjectType: "X", PrimaryKeys: []string{"a"}},
		{
			Type:      "filter",
			ObjectSet: &objectset.Definition{Type: "base", ObjectType: "X"},
			Where:     json.RawMessage(`{"type":"eq","field":"f","value":"v"}`),
		},
		{
			Type: "union",
			ObjectSets: []*objectset.Definition{
				{Type: "base", ObjectType: "X"},
				{Type: "base", ObjectType: "Y"},
			},
		},
		{
			Type:       "asType",
			ObjectType: "X",
			ObjectSet:  &objectset.Definition{Type: "base", ObjectType: "X"},
		},
		{
			Type:      "withProperties",
			ObjectSet: &objectset.Definition{Type: "base", ObjectType: "X"},
		},
	}
	for i, def := range cases {
		if err := supportedDefinitionType(def); err != nil {
			t.Errorf("case %d: expected supported, got %v", i, err)
		}
	}
}

func TestSupportedDefinitionType_Rejected(t *testing.T) {
	cases := []*objectset.Definition{
		{Type: "searchAround", ObjectSet: &objectset.Definition{Type: "base", ObjectType: "X"}, Link: "l"},
		{Type: "nearestNeighbors", ObjectSet: &objectset.Definition{Type: "base", ObjectType: "X"}},
		{Type: "interfaceBase", InterfaceType: "I"},
		{Type: "reference", Reference: "abc"},
		{Type: "sample", ObjectSet: &objectset.Definition{Type: "base", ObjectType: "X"}},
		{Type: "methodInput", Input: "param"},
	}
	for i, def := range cases {
		if err := supportedDefinitionType(def); err == nil {
			t.Errorf("case %d (%s): expected unsupported, got nil", i, def.Type)
		}
	}
}

func TestSupportedDefinitionType_NestedUnsupported(t *testing.T) {
	// A union whose child is searchAround must be rejected at the parent.
	def := &objectset.Definition{
		Type: "union",
		ObjectSets: []*objectset.Definition{
			{Type: "base", ObjectType: "X"},
			{Type: "searchAround", ObjectSet: &objectset.Definition{Type: "base", ObjectType: "X"}, Link: "l"},
		},
	}
	if err := supportedDefinitionType(def); err == nil {
		t.Error("expected nested unsupported child to bubble up")
	}
}

func TestSupportedDefinitionType_UnionTooFewChildren(t *testing.T) {
	def := &objectset.Definition{
		Type:       "union",
		ObjectSets: []*objectset.Definition{{Type: "base", ObjectType: "X"}},
	}
	if err := supportedDefinitionType(def); err == nil {
		t.Error("expected union with <2 children to be rejected")
	}
}

func TestSupportedDefinitionType_Nil(t *testing.T) {
	if err := supportedDefinitionType(nil); err == nil {
		t.Error("expected nil definition to be rejected")
	}
}

// ---------- resolveDefinition tests ----------

func TestResolveDefinition_Inline(t *testing.T) {
	want := &objectset.Definition{Type: "base", ObjectType: "X"}
	got, err := resolveDefinition(ObjectSetSubscribeRequest{Definition: want}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Error("expected pointer-equal inline definition")
	}
}

func TestResolveDefinition_Rid(t *testing.T) {
	def := &objectset.Definition{Type: "base", ObjectType: "X"}
	r := &fakeResolver{defs: map[string]*objectset.Definition{"abc": def}}
	got, err := resolveDefinition(ObjectSetSubscribeRequest{ObjectSetRid: "abc"}, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != def {
		t.Error("expected resolver to return registered definition")
	}
}

func TestResolveDefinition_RidNotFound(t *testing.T) {
	r := &fakeResolver{defs: map[string]*objectset.Definition{}}
	if _, err := resolveDefinition(ObjectSetSubscribeRequest{ObjectSetRid: "missing"}, r); err == nil {
		t.Error("expected error for missing rid")
	}
}

func TestResolveDefinition_NoSource(t *testing.T) {
	if _, err := resolveDefinition(ObjectSetSubscribeRequest{}, nil); err == nil {
		t.Error("expected error when neither definition nor rid supplied")
	}
}

func TestResolveDefinition_RidWithoutResolver(t *testing.T) {
	if _, err := resolveDefinition(ObjectSetSubscribeRequest{ObjectSetRid: "abc"}, nil); err == nil {
		t.Error("expected error when resolver not configured")
	}
}

// ---------- WebSocket integration tests ----------

func TestSubscribeObjectSet_InlineDefinition_Success(t *testing.T) {
	h := NewHub()
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _ := dialAndWelcome(t, ctx, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	subMsg := Message{
		Type: "subscribeObjectSet",
		Data: json.RawMessage(`{"definition":{"type":"base","objectType":"Employee"}}`),
	}
	if err := wsjson.Write(ctx, c, subMsg); err != nil {
		t.Fatalf("write: %v", err)
	}

	var resp Message
	if err := wsjson.Read(ctx, c, &resp); err != nil {
		t.Fatalf("read: %v", err)
	}
	if resp.Type != "subscribed" {
		t.Errorf("expected subscribed, got %q (%s)", resp.Type, resp.Error)
	}
	if resp.SubscriptionID == "" {
		t.Error("expected non-empty subscriptionId")
	}
}

func TestSubscribeObjectSet_ByRid_Success(t *testing.T) {
	h := NewHub()
	defer h.Close()
	store := objectset.NewStore(time.Hour)
	rid := store.Put(&objectset.Definition{Type: "base", ObjectType: "Employee"})
	h.SetObjectSetResolver(store)

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _ := dialAndWelcome(t, ctx, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	subMsg := Message{
		Type: "subscribeObjectSet",
		Data: json.RawMessage(`{"objectSetRid":"` + rid + `"}`),
	}
	if err := wsjson.Write(ctx, c, subMsg); err != nil {
		t.Fatalf("write: %v", err)
	}

	var resp Message
	if err := wsjson.Read(ctx, c, &resp); err != nil {
		t.Fatalf("read: %v", err)
	}
	if resp.Type != "subscribed" {
		t.Errorf("expected subscribed, got %q (%s)", resp.Type, resp.Error)
	}
}

func TestSubscribeObjectSet_RidNotFound(t *testing.T) {
	h := NewHub()
	defer h.Close()
	store := objectset.NewStore(time.Hour)
	h.SetObjectSetResolver(store)

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _ := dialAndWelcome(t, ctx, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	subMsg := Message{
		Type: "subscribeObjectSet",
		Data: json.RawMessage(`{"objectSetRid":"does-not-exist"}`),
	}
	if err := wsjson.Write(ctx, c, subMsg); err != nil {
		t.Fatalf("write: %v", err)
	}

	var resp Message
	if err := wsjson.Read(ctx, c, &resp); err != nil {
		t.Fatalf("read: %v", err)
	}
	if resp.Type != "error" {
		t.Errorf("expected error, got %q", resp.Type)
	}
}

func TestSubscribeObjectSet_UnsupportedType_Rejected(t *testing.T) {
	h := NewHub()
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _ := dialAndWelcome(t, ctx, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	subMsg := Message{
		Type: "subscribeObjectSet",
		Data: json.RawMessage(`{"definition":{
			"type":"searchAround",
			"objectSet":{"type":"base","objectType":"Employee"},
			"link":"manages"
		}}`),
	}
	if err := wsjson.Write(ctx, c, subMsg); err != nil {
		t.Fatalf("write: %v", err)
	}

	var resp Message
	if err := wsjson.Read(ctx, c, &resp); err != nil {
		t.Fatalf("read: %v", err)
	}
	if resp.Type != "error" {
		t.Errorf("expected error, got %q", resp.Type)
	}
	if !strings.Contains(resp.Error, "searchAround") && !strings.Contains(resp.Error, "unsupported") {
		t.Errorf("expected error mentioning searchAround or unsupported, got %q", resp.Error)
	}
}

func TestSubscribeObjectSet_InvalidJSON(t *testing.T) {
	h := NewHub()
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _ := dialAndWelcome(t, ctx, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	subMsg := Message{
		Type: "subscribeObjectSet",
		Data: json.RawMessage(`"not-an-object"`),
	}
	if err := wsjson.Write(ctx, c, subMsg); err != nil {
		t.Fatalf("write: %v", err)
	}

	var resp Message
	if err := wsjson.Read(ctx, c, &resp); err != nil {
		t.Fatalf("read: %v", err)
	}
	if resp.Type != "error" {
		t.Errorf("expected error, got %q", resp.Type)
	}
}

func TestSubscribeObjectSet_MissingDefinitionAndRid(t *testing.T) {
	h := NewHub()
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _ := dialAndWelcome(t, ctx, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	subMsg := Message{
		Type: "subscribeObjectSet",
		Data: json.RawMessage(`{}`),
	}
	if err := wsjson.Write(ctx, c, subMsg); err != nil {
		t.Fatalf("write: %v", err)
	}

	var resp Message
	if err := wsjson.Read(ctx, c, &resp); err != nil {
		t.Fatalf("read: %v", err)
	}
	if resp.Type != "error" {
		t.Errorf("expected error, got %q", resp.Type)
	}
}

func TestSubscribeObjectSet_FilterDefinition_RoutesEditBatch(t *testing.T) {
	h := NewHub()
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _ := dialAndWelcome(t, ctx, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	// Subscribe to filter Employee where dept=Engineering.
	subMsg := Message{
		Type: "subscribeObjectSet",
		Data: json.RawMessage(`{"definition":{
			"type":"filter",
			"objectSet":{"type":"base","objectType":"Employee"},
			"where":{"type":"eq","field":"department","value":"Engineering"}
		}}`),
	}
	if err := wsjson.Write(ctx, c, subMsg); err != nil {
		t.Fatalf("write subscribe: %v", err)
	}
	var subResp Message
	if err := wsjson.Read(ctx, c, &subResp); err != nil {
		t.Fatalf("read subscribed: %v", err)
	}
	if subResp.Type != "subscribed" {
		t.Fatalf("expected subscribed, got %q (%s)", subResp.Type, subResp.Error)
	}

	// Non-matching change (Sales) — should NOT route.
	h.HandleObjectChange("Employee", "e1", "CREATE", map[string]interface{}{
		"name":       "Alice",
		"department": "Sales",
	})

	// Matching change (Engineering) — should route.
	h.HandleObjectChange("Employee", "e2", "MODIFY", map[string]interface{}{
		"name":       "Bob",
		"department": "Engineering",
	})

	var evt Message
	if err := wsjson.Read(ctx, c, &evt); err != nil {
		t.Fatalf("read event: %v", err)
	}
	if evt.Type != "objectChanged" {
		t.Fatalf("expected objectChanged, got %q", evt.Type)
	}
	var change ObjectChangeEvent
	if err := json.Unmarshal(evt.Data, &change); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if change.Object["name"] != "Bob" {
		t.Errorf("expected Bob (matching event), got %v", change.Object["name"])
	}
}

func TestSubscribeObjectSet_StaticDefinition_RoutesByPK(t *testing.T) {
	h := NewHub()
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _ := dialAndWelcome(t, ctx, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	subMsg := Message{
		Type: "subscribeObjectSet",
		Data: json.RawMessage(`{"definition":{
			"type":"static",
			"objectType":"Employee",
			"primaryKeys":["e1","e3"]
		}}`),
	}
	if err := wsjson.Write(ctx, c, subMsg); err != nil {
		t.Fatalf("write: %v", err)
	}
	var subResp Message
	if err := wsjson.Read(ctx, c, &subResp); err != nil {
		t.Fatalf("read sub: %v", err)
	}
	if subResp.Type != "subscribed" {
		t.Fatalf("expected subscribed, got %q (%s)", subResp.Type, subResp.Error)
	}

	// PK not in static set — should NOT route.
	h.HandleObjectChange("Employee", "e2", "MODIFY", map[string]interface{}{"name": "Bob"})

	// PK in static set — should route.
	h.HandleObjectChange("Employee", "e3", "MODIFY", map[string]interface{}{"name": "Carol"})

	var evt Message
	if err := wsjson.Read(ctx, c, &evt); err != nil {
		t.Fatalf("read event: %v", err)
	}
	var change ObjectChangeEvent
	if err := json.Unmarshal(evt.Data, &change); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if change.Object["name"] != "Carol" {
		t.Errorf("expected Carol (matching event), got %v", change.Object["name"])
	}
}

func TestSubscribeObjectSet_Select_Projects(t *testing.T) {
	h := NewHub()
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _ := dialAndWelcome(t, ctx, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	subMsg := Message{
		Type: "subscribeObjectSet",
		Data: json.RawMessage(`{
			"definition":{"type":"base","objectType":"Employee"},
			"select":["name"]
		}`),
	}
	if err := wsjson.Write(ctx, c, subMsg); err != nil {
		t.Fatalf("write: %v", err)
	}
	var subResp Message
	if err := wsjson.Read(ctx, c, &subResp); err != nil {
		t.Fatalf("read sub: %v", err)
	}

	h.HandleObjectChange("Employee", "e1", "CREATE", map[string]interface{}{
		"name":       "John",
		"email":      "john@co.com",
		"department": "Engineering",
	})

	var evt Message
	if err := wsjson.Read(ctx, c, &evt); err != nil {
		t.Fatalf("read event: %v", err)
	}
	var change ObjectChangeEvent
	if err := json.Unmarshal(evt.Data, &change); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if change.Object["name"] != "John" {
		t.Errorf("expected name=John, got %v", change.Object["name"])
	}
	if _, ok := change.Object["email"]; ok {
		t.Error("email should be projected away")
	}
	if _, ok := change.Object["department"]; ok {
		t.Error("department should be projected away")
	}
}
