package cliclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestServer builds an httptest.Server whose handler echoes back the request
// path and authorization header into a recorder so individual tests can assert
// on them. Each test sets the desired status / body for its endpoint.
type recordedRequest struct {
	method string
	path   string
	auth   string
	body   string
}

func newTestServer(t *testing.T, mux map[string]http.HandlerFunc) (*httptest.Server, *[]recordedRequest) {
	t.Helper()
	rec := []recordedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		if r.ContentLength > 0 {
			_, _ = r.Body.Read(buf)
		}
		rec = append(rec, recordedRequest{
			method: r.Method,
			path:   r.URL.RequestURI(),
			auth:   r.Header.Get("Authorization"),
			body:   string(buf),
		})
		key := r.Method + " " + r.URL.Path
		if h, ok := mux[key]; ok {
			h(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &rec
}

func TestNewClientDefaultsHTTPClientAndStripsTrailingSlash(t *testing.T) {
	c := NewClient("http://example.test/", "tok")
	if c.HTTP == nil {
		t.Fatalf("HTTP client should be initialized")
	}
	if c.BaseURL != "http://example.test" {
		t.Fatalf("trailing slash should be stripped, got %q", c.BaseURL)
	}
	if c.Token != "tok" {
		t.Fatalf("token not stored")
	}
}

func TestClientSendsBearerHeader(t *testing.T) {
	srv, rec := newTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v2/ontologies": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
		},
	})
	c := NewClient(srv.URL, "abcdef")
	if _, err := c.ListOntologies(context.Background()); err != nil {
		t.Fatalf("ListOntologies: %v", err)
	}
	if len(*rec) != 1 {
		t.Fatalf("expected 1 request, got %d", len(*rec))
	}
	if (*rec)[0].auth != "Bearer abcdef" {
		t.Fatalf("auth header = %q, want Bearer abcdef", (*rec)[0].auth)
	}
}

func TestClientOmitsBearerWhenTokenEmpty(t *testing.T) {
	srv, rec := newTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v2/ontologies": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":[]}`))
		},
	})
	c := NewClient(srv.URL, "")
	_, _ = c.ListOntologies(context.Background())
	if (*rec)[0].auth != "" {
		t.Fatalf("auth header should be empty, got %q", (*rec)[0].auth)
	}
}

func TestListOntologiesParsesPayload(t *testing.T) {
	srv, _ := newTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v2/ontologies": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":[
				{"rid":"ri.ontology.main.ontology.northwind","apiName":"northwind","displayName":"Northwind","currentVersion":3},
				{"rid":"ri.ontology.main.ontology.chinook","apiName":"chinook","displayName":"Chinook","currentVersion":1}
			]}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	list, err := c.ListOntologies(context.Background())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 ontologies, got %d", len(list))
	}
	if list[0].APIName != "northwind" || list[0].DisplayName != "Northwind" {
		t.Fatalf("unexpected ontology: %+v", list[0])
	}
	if list[1].CurrentVersion != 1 {
		t.Fatalf("expected version 1, got %d", list[1].CurrentVersion)
	}
}

func TestGetOntologyParsesSingle(t *testing.T) {
	srv, _ := newTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v2/ontologies/northwind": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"rid":"ri.ontology.main.ontology.northwind","apiName":"northwind","displayName":"Northwind","currentVersion":3}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	o, err := c.GetOntology(context.Background(), "northwind")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if o.RID != "ri.ontology.main.ontology.northwind" {
		t.Fatalf("rid = %q", o.RID)
	}
}

func TestGetOntologyNotFoundReturnsTypedError(t *testing.T) {
	srv, _ := newTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v2/ontologies/missing": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errorCode":"NOT_FOUND","errorName":"OntologyNotFound","errorInstanceId":"00000000-0000-0000-0000-000000000000","parameters":{}}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	_, err := c.GetOntology(context.Background(), "missing")
	if err == nil {
		t.Fatalf("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T (%v)", err, err)
	}
	if apiErr.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d", apiErr.StatusCode)
	}
	if apiErr.ErrorName != "OntologyNotFound" {
		t.Fatalf("errorName = %q", apiErr.ErrorName)
	}
}

func TestListObjectTypesParsesData(t *testing.T) {
	srv, _ := newTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v2/ontologies/northwind/objectTypes": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":[
				{"rid":"ri.ontology.main.objecttype.customer","apiName":"Customer","displayName":"Customer","primaryKey":"customerId","status":"ACTIVE","visibility":"NORMAL"},
				{"rid":"ri.ontology.main.objecttype.order","apiName":"Order","displayName":"Order","primaryKey":"orderId","status":"ACTIVE","visibility":"NORMAL"}
			]}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	list, err := c.ListObjectTypes(context.Background(), "northwind")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(list) != 2 || list[0].APIName != "Customer" || list[1].APIName != "Order" {
		t.Fatalf("unexpected list: %+v", list)
	}
}

func TestListObjectsBuildsQueryAndDecodes(t *testing.T) {
	srv, rec := newTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v2/ontologies/northwind/objects/Customer": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":[
				{"__rid":"ri.obj.1","__primaryKey":"ALFKI","__apiName":"Customer","customerId":"ALFKI","companyName":"Alfreds"},
				{"__rid":"ri.obj.2","__primaryKey":"ANATR","__apiName":"Customer","customerId":"ANATR","companyName":"Ana Trujillo"}
			],"nextPageToken":"tok-2","totalCount":"42"}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	page, err := c.ListObjects(context.Background(), "northwind", "Customer", ListObjectsOptions{PageSize: 25, PageToken: "p1", OrderBy: "customerId"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(page.Data) != 2 || page.NextPageToken != "tok-2" || page.TotalCount != "42" {
		t.Fatalf("unexpected page: %+v", page)
	}
	if page.Data[0]["customerId"] != "ALFKI" {
		t.Fatalf("first row missing customerId: %+v", page.Data[0])
	}
	got := (*rec)[0].path
	if !strings.Contains(got, "pageSize=25") || !strings.Contains(got, "pageToken=p1") || !strings.Contains(got, "orderBy=customerId") {
		t.Fatalf("query string lost params: %q", got)
	}
}

func TestGetObjectFetchesByPrimaryKey(t *testing.T) {
	srv, _ := newTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v2/ontologies/northwind/objects/Customer/ALFKI": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"__rid":"ri.obj.1","__primaryKey":"ALFKI","customerId":"ALFKI","companyName":"Alfreds Futterkiste"}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	obj, err := c.GetObject(context.Background(), "northwind", "Customer", "ALFKI")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if obj["companyName"] != "Alfreds Futterkiste" {
		t.Fatalf("unexpected obj: %+v", obj)
	}
}

func TestGetObjectURLEscapesPrimaryKey(t *testing.T) {
	var capturedRawPath, capturedDecodedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRawPath = r.URL.EscapedPath()
		capturedDecodedPath = r.URL.Path
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, "tok")
	if _, err := c.GetObject(context.Background(), "nw", "Customer", "ALF KI/1"); err != nil {
		t.Fatalf("err: %v", err)
	}
	wantRaw := "/api/v2/ontologies/nw/objects/Customer/ALF%20KI%2F1"
	wantDecoded := "/api/v2/ontologies/nw/objects/Customer/ALF KI/1"
	if capturedRawPath != wantRaw {
		t.Fatalf("raw path = %q, want %q", capturedRawPath, wantRaw)
	}
	if capturedDecodedPath != wantDecoded {
		t.Fatalf("decoded path = %q, want %q", capturedDecodedPath, wantDecoded)
	}
}

func TestSearchObjectsPostsBody(t *testing.T) {
	srv, rec := newTestServer(t, map[string]http.HandlerFunc{
		"POST /api/v2/ontologies/nw/objects/Customer/search": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":[{"__primaryKey":"ALFKI","customerId":"ALFKI"}],"nextPageToken":"","totalCount":"1"}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	page, err := c.SearchObjects(context.Background(), "nw", "Customer", map[string]any{
		"type":  "eq",
		"field": "customerId",
		"value": "ALFKI",
	}, []string{"customerId", "companyName"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(page.Data) != 1 || page.TotalCount != "1" {
		t.Fatalf("page = %+v", page)
	}
	body := (*rec)[0].body
	if !strings.Contains(body, `"where"`) || !strings.Contains(body, `"ALFKI"`) {
		t.Fatalf("body = %q", body)
	}
	if !strings.Contains(body, `"select"`) || !strings.Contains(body, `"customerId"`) {
		t.Fatalf("body missing select: %q", body)
	}
}

func TestApplyActionPostsParameters(t *testing.T) {
	// Foundry shape: action API name lives in the URL, body carries only
	// parameters. A stale `actionType` field in the body would be silently
	// overridden server-side, so the client simply stops sending it.
	srv, rec := newTestServer(t, map[string]http.HandlerFunc{
		"POST /api/v2/ontologies/nw/actions/createCustomer/apply": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"operationId":"op-123","edits":{"type":"edits","addedObjectCount":1,"modifiedObjectCount":0,"deletedObjectCount":0,"addedLinksCount":0,"deletedLinksCount":0}}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	res, err := c.ApplyAction(context.Background(), "nw", "createCustomer", map[string]any{"name": "X"}, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.OperationID != "op-123" || res.Edits == nil || res.Edits.AddedObjectCount != 1 {
		t.Fatalf("response = %+v", res)
	}
	body := (*rec)[0].body
	if !strings.Contains(body, `"name":"X"`) {
		t.Fatalf("body = %q", body)
	}
	// Ensure the stale `actionType` field is no longer sent in the body.
	if strings.Contains(body, `"actionType"`) {
		t.Fatalf("body still carries actionType field: %q", body)
	}
	// Ensure no options key when opts is nil.
	if strings.Contains(body, `"options"`) {
		t.Fatalf("body should not carry options when nil: %q", body)
	}
}

func TestApplyActionWithOptions(t *testing.T) {
	srv, rec := newTestServer(t, map[string]http.HandlerFunc{
		"POST /api/v2/ontologies/nw/actions/createCustomer/apply": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"validation":{"result":"VALID"}}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	res, err := c.ApplyAction(context.Background(), "nw", "createCustomer", map[string]any{"name": "X"}, &ApplyOptions{Mode: "VALIDATE_ONLY"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Validation == nil || res.Validation.Result != "VALID" {
		t.Fatalf("expected validation result VALID, got %+v", res)
	}
	body := (*rec)[0].body
	if !strings.Contains(body, `"options"`) || !strings.Contains(body, `"VALIDATE_ONLY"`) {
		t.Fatalf("body missing options: %q", body)
	}
}

func TestLoginExchangesCredentialsForTokens(t *testing.T) {
	srv, rec := newTestServer(t, map[string]http.HandlerFunc{
		"POST /api/auth/login": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"access_token":"access-xyz","refresh_token":"refresh-abc","token_type":"Bearer","expires_in":900,"user":{"id":"u1","email":"a@b","name":"A","roles":["admin"],"ontologyRoles":{}}}`))
		},
	})
	c := NewClient(srv.URL, "")
	resp, err := c.Login(context.Background(), "a@b", "pw")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if resp.AccessToken != "access-xyz" || resp.RefreshToken != "refresh-abc" || resp.ExpiresIn != 900 {
		t.Fatalf("login resp = %+v", resp)
	}
	if resp.User.Email != "a@b" || len(resp.User.Roles) != 1 {
		t.Fatalf("user = %+v", resp.User)
	}
	body := (*rec)[0].body
	if !strings.Contains(body, `"email":"a@b"`) || !strings.Contains(body, `"password":"pw"`) {
		t.Fatalf("body = %q", body)
	}
	if (*rec)[0].auth != "" {
		t.Fatalf("login should not send Authorization, got %q", (*rec)[0].auth)
	}
}

func TestErrorBodyParsedWhenUnauthorized(t *testing.T) {
	srv, _ := newTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v2/ontologies": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"errorCode":"UNAUTHORIZED","errorName":"InvalidCredentials","errorInstanceId":"x","parameters":{}}`))
		},
	})
	c := NewClient(srv.URL, "bad")
	_, err := c.ListOntologies(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("not APIError: %T %v", err, err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized || apiErr.ErrorName != "InvalidCredentials" {
		t.Fatalf("apierr = %+v", apiErr)
	}
}

func TestNonJSONErrorIsReportedRaw(t *testing.T) {
	srv, _ := newTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v2/ontologies": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("upstream broke"))
		},
	})
	c := NewClient(srv.URL, "tok")
	_, err := c.ListOntologies(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("not APIError: %T %v", err, err)
	}
	if apiErr.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d", apiErr.StatusCode)
	}
	if !strings.Contains(apiErr.Error(), "upstream broke") {
		t.Fatalf("error message missing body: %v", apiErr)
	}
}

func TestContextDeadlineRespected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, "tok")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := c.ListOntologies(ctx)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestOntologyJSONRoundtrip(t *testing.T) {
	o := Ontology{RID: "rid", APIName: "n", DisplayName: "N", CurrentVersion: 2}
	b, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	var got Ontology
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got != o {
		t.Fatalf("roundtrip: %+v vs %+v", got, o)
	}
}

// ----- New endpoint tests -------------------------------------------------

func TestLoadOntologyMetadata(t *testing.T) {
	srv, rec := newTestServer(t, map[string]http.HandlerFunc{
		"POST /api/v2/ontologies/nw/metadata": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"objectTypes":["Customer"]}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	resp, err := c.LoadOntologyMetadata(context.Background(), "nw", map[string]bool{"objectTypes": true})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if resp["objectTypes"] == nil {
		t.Fatalf("expected objectTypes key, got %+v", resp)
	}
	body := (*rec)[0].body
	if !strings.Contains(body, `"objectTypes"`) {
		t.Fatalf("body = %q", body)
	}
}

func TestGetOntologyFullMetadata(t *testing.T) {
	srv, rec := newTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v2/ontologies/nw/fullMetadata": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"ontology":{"apiName":"nw"}}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	resp, err := c.GetOntologyFullMetadata(context.Background(), "nw")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if resp["ontology"] == nil {
		t.Fatalf("expected ontology key, got %+v", resp)
	}
	if !strings.Contains((*rec)[0].path, "preview=true") {
		t.Fatalf("missing preview param: %q", (*rec)[0].path)
	}
}

func TestGetObjectTypeFullMetadata(t *testing.T) {
	srv, rec := newTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v2/ontologies/nw/objectTypes/Customer/fullMetadata": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"objectType":{"apiName":"Customer"}}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	resp, err := c.GetObjectTypeFullMetadata(context.Background(), "nw", "Customer")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if resp["objectType"] == nil {
		t.Fatalf("expected objectType key, got %+v", resp)
	}
	if !strings.Contains((*rec)[0].path, "preview=true") {
		t.Fatalf("missing preview param: %q", (*rec)[0].path)
	}
}

func TestGetObjectTypesByRidBatch(t *testing.T) {
	srv, rec := newTestServer(t, map[string]http.HandlerFunc{
		"POST /api/v2/ontologies/nw/objectTypes/getByRidBatch": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":[{"rid":"rid1","apiName":"Customer"},{"rid":"rid2","apiName":"Order"}]}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	data, err := c.GetObjectTypesByRidBatch(context.Background(), "nw", []string{"rid1", "rid2"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(data) != 2 {
		t.Fatalf("expected 2, got %d", len(data))
	}
	body := (*rec)[0].body
	if !strings.Contains(body, `"rids"`) || !strings.Contains(body, `"rid1"`) {
		t.Fatalf("body = %q", body)
	}
}

func TestGetActionType(t *testing.T) {
	srv, _ := newTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v2/ontologies/nw/actionTypes/createCustomer": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"rid":"rid-at1","apiName":"createCustomer","displayName":"Create Customer","status":"ACTIVE"}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	at, err := c.GetActionType(context.Background(), "nw", "createCustomer")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if at.APIName != "createCustomer" || at.Status != "ACTIVE" {
		t.Fatalf("unexpected: %+v", at)
	}
}

func TestGetActionTypeByRid(t *testing.T) {
	srv, rec := newTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v2/ontologies/nw/actionTypes/byRid/ri.action.1": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"rid":"ri.action.1","apiName":"createOrder","displayName":"Create Order","status":"ACTIVE"}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	at, err := c.GetActionTypeByRid(context.Background(), "nw", "ri.action.1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if at.RID != "ri.action.1" || at.APIName != "createOrder" {
		t.Fatalf("unexpected: %+v", at)
	}
	if !strings.Contains((*rec)[0].path, "byRid/ri.action.1") {
		t.Fatalf("path = %q", (*rec)[0].path)
	}
}

func TestGetActionTypesByRidBatch(t *testing.T) {
	srv, rec := newTestServer(t, map[string]http.HandlerFunc{
		"POST /api/v2/ontologies/nw/actionTypes/getByRidBatch": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":[{"rid":"rid-a1","apiName":"createOrder"}]}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	data, err := c.GetActionTypesByRidBatch(context.Background(), "nw", []string{"rid-a1"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(data) != 1 {
		t.Fatalf("expected 1, got %d", len(data))
	}
	body := (*rec)[0].body
	if !strings.Contains(body, `"rids"`) {
		t.Fatalf("body = %q", body)
	}
}

func TestGetActionTypeFullMetadata(t *testing.T) {
	srv, rec := newTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v2/ontologies/nw/actionTypes/createCustomer/fullMetadata": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"actionType":{"apiName":"createCustomer"}}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	resp, err := c.GetActionTypeFullMetadata(context.Background(), "nw", "createCustomer")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if resp["actionType"] == nil {
		t.Fatalf("expected actionType key, got %+v", resp)
	}
	if !strings.Contains((*rec)[0].path, "preview=true") {
		t.Fatalf("missing preview param: %q", (*rec)[0].path)
	}
}

func TestListActionTypesFullMetadata(t *testing.T) {
	srv, rec := newTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v2/ontologies/nw/actionTypesFullMetadata": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":[{"apiName":"createCustomer"},{"apiName":"deleteCustomer"}]}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	data, err := c.ListActionTypesFullMetadata(context.Background(), "nw")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(data) != 2 {
		t.Fatalf("expected 2, got %d", len(data))
	}
	if !strings.Contains((*rec)[0].path, "preview=true") {
		t.Fatalf("missing preview param: %q", (*rec)[0].path)
	}
}

func TestCountObjects(t *testing.T) {
	srv, rec := newTestServer(t, map[string]http.HandlerFunc{
		"POST /api/v2/ontologies/nw/objects/Customer/count": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"count":42}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	count, err := c.CountObjects(context.Background(), "nw", "Customer")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if count != 42 {
		t.Fatalf("expected 42, got %d", count)
	}
	if (*rec)[0].method != "POST" {
		t.Fatalf("expected POST, got %s", (*rec)[0].method)
	}
}

func TestListLinkedObjects(t *testing.T) {
	srv, rec := newTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v2/ontologies/nw/objects/Customer/ALFKI/links/orders": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":[{"__primaryKey":"10643","orderId":"10643"}],"nextPageToken":"p2","totalCount":"5"}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	page, err := c.ListLinkedObjects(context.Background(), "nw", "Customer", "ALFKI", "orders", ListObjectsOptions{PageSize: 10})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(page.Data) != 1 || page.NextPageToken != "p2" {
		t.Fatalf("page = %+v", page)
	}
	if !strings.Contains((*rec)[0].path, "pageSize=10") {
		t.Fatalf("path missing pageSize: %q", (*rec)[0].path)
	}
}

func TestGetLinkedObject(t *testing.T) {
	srv, _ := newTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v2/ontologies/nw/objects/Customer/ALFKI/links/orders/10643": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"__primaryKey":"10643","orderId":"10643","customerId":"ALFKI"}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	obj, err := c.GetLinkedObject(context.Background(), "nw", "Customer", "ALFKI", "orders", "10643")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if obj["orderId"] != "10643" {
		t.Fatalf("unexpected obj: %+v", obj)
	}
}

func TestApplyBatch(t *testing.T) {
	srv, rec := newTestServer(t, map[string]http.HandlerFunc{
		"POST /api/v2/ontologies/nw/actions/createCustomer/applyBatch": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"edits":{"type":"edits","addedObjectCount":3,"modifiedObjectCount":0,"deletedObjectCount":0,"addedLinksCount":0,"deletedLinksCount":0}}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	reqs := []map[string]any{
		{"parameters": map[string]any{"name": "A"}},
		{"parameters": map[string]any{"name": "B"}},
		{"parameters": map[string]any{"name": "C"}},
	}
	resp, err := c.ApplyBatch(context.Background(), "nw", "createCustomer", reqs, "ALL")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if resp.Edits == nil || resp.Edits.AddedObjectCount != 3 {
		t.Fatalf("response = %+v", resp)
	}
	body := (*rec)[0].body
	if !strings.Contains(body, `"requests"`) || !strings.Contains(body, `"returnEdits"`) {
		t.Fatalf("body = %q", body)
	}
}

func TestListInterfaceTypes(t *testing.T) {
	srv, rec := newTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v2/ontologies/nw/interfaceTypes": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":[{"rid":"rid-if1","apiName":"Trackable","displayName":"Trackable"}]}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	list, err := c.ListInterfaceTypes(context.Background(), "nw")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(list) != 1 || list[0].APIName != "Trackable" {
		t.Fatalf("unexpected: %+v", list)
	}
	if !strings.Contains((*rec)[0].path, "preview=true") {
		t.Fatalf("missing preview param: %q", (*rec)[0].path)
	}
}

func TestGetInterfaceType(t *testing.T) {
	srv, _ := newTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v2/ontologies/nw/interfaceTypes/Trackable": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"rid":"rid-if1","apiName":"Trackable","displayName":"Trackable"}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	it, err := c.GetInterfaceType(context.Background(), "nw", "Trackable")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if it.APIName != "Trackable" {
		t.Fatalf("unexpected: %+v", it)
	}
}

func TestListValueTypes(t *testing.T) {
	srv, rec := newTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v2/ontologies/nw/valueTypes": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":[{"rid":"rid-vt1","apiName":"Currency","displayName":"Currency","baseType":"string","version":1}]}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	list, err := c.ListValueTypes(context.Background(), "nw")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(list) != 1 || list[0].APIName != "Currency" || list[0].Version != 1 {
		t.Fatalf("unexpected: %+v", list)
	}
	if !strings.Contains((*rec)[0].path, "preview=true") {
		t.Fatalf("missing preview param: %q", (*rec)[0].path)
	}
}

func TestGetValueType(t *testing.T) {
	srv, _ := newTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v2/ontologies/nw/valueTypes/Currency": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"rid":"rid-vt1","apiName":"Currency","displayName":"Currency","baseType":"string","version":1}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	vt, err := c.GetValueType(context.Background(), "nw", "Currency")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if vt.APIName != "Currency" || vt.BaseType != "string" {
		t.Fatalf("unexpected: %+v", vt)
	}
}

func TestListQueryTypes(t *testing.T) {
	srv, _ := newTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v2/ontologies/nw/queryTypes": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":[{"rid":"rid-qt1","apiName":"topCustomers","displayName":"Top Customers","status":"ACTIVE"}]}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	list, err := c.ListQueryTypes(context.Background(), "nw")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(list) != 1 || list[0].APIName != "topCustomers" {
		t.Fatalf("unexpected: %+v", list)
	}
}

func TestGetQueryType(t *testing.T) {
	srv, _ := newTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v2/ontologies/nw/queryTypes/topCustomers": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"rid":"rid-qt1","apiName":"topCustomers","displayName":"Top Customers","status":"ACTIVE"}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	qt, err := c.GetQueryType(context.Background(), "nw", "topCustomers")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if qt.APIName != "topCustomers" || qt.Status != "ACTIVE" {
		t.Fatalf("unexpected: %+v", qt)
	}
}

func TestExecuteQuery(t *testing.T) {
	srv, rec := newTestServer(t, map[string]http.HandlerFunc{
		"POST /api/v2/ontologies/nw/queries/topCustomers/execute": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"value":["ALFKI","ANATR"]}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	resp, err := c.ExecuteQuery(context.Background(), "nw", "topCustomers", map[string]any{"limit": 2})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if resp["value"] == nil {
		t.Fatalf("expected value key, got %+v", resp)
	}
	body := (*rec)[0].body
	if !strings.Contains(body, `"parameters"`) || !strings.Contains(body, `"limit"`) {
		t.Fatalf("body = %q", body)
	}
}

func TestLoadObjectSetObjects(t *testing.T) {
	srv, rec := newTestServer(t, map[string]http.HandlerFunc{
		"POST /api/v2/ontologies/nw/objectSets/loadObjects": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":[{"__primaryKey":"ALFKI","customerId":"ALFKI"}],"nextPageToken":"p2","totalCount":"10"}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	os := map[string]any{"type": "base", "objectType": "Customer"}
	page, err := c.LoadObjectSetObjects(context.Background(), "nw", os, []string{"customerId"}, 5, "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(page.Data) != 1 || page.NextPageToken != "p2" {
		t.Fatalf("page = %+v", page)
	}
	body := (*rec)[0].body
	if !strings.Contains(body, `"objectSet"`) || !strings.Contains(body, `"select"`) || !strings.Contains(body, `"pageSize"`) {
		t.Fatalf("body = %q", body)
	}
}

func TestLoadObjectSetLinks(t *testing.T) {
	srv, rec := newTestServer(t, map[string]http.HandlerFunc{
		"POST /api/v2/ontologies/nw/objectSets/loadLinks": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":[{"__primaryKey":"10643"}]}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	os := map[string]any{"type": "base", "objectType": "Customer"}
	page, err := c.LoadObjectSetLinks(context.Background(), "nw", os, "orders", []string{"orderId"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(page.Data) != 1 {
		t.Fatalf("page = %+v", page)
	}
	body := (*rec)[0].body
	if !strings.Contains(body, `"linkType"`) || !strings.Contains(body, `"orders"`) {
		t.Fatalf("body = %q", body)
	}
}

func TestAggregateObjectSet(t *testing.T) {
	srv, rec := newTestServer(t, map[string]http.HandlerFunc{
		"POST /api/v2/ontologies/nw/objectSets/aggregate": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":[{"group":{},"metrics":[{"name":"count","value":42}]}]}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	os := map[string]any{"type": "base", "objectType": "Customer"}
	agg := map[string]any{"aggregation": []any{map[string]any{"type": "count", "name": "count"}}}
	resp, err := c.AggregateObjectSet(context.Background(), "nw", os, agg)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if resp["data"] == nil {
		t.Fatalf("expected data key, got %+v", resp)
	}
	body := (*rec)[0].body
	if !strings.Contains(body, `"objectSet"`) || !strings.Contains(body, `"aggregation"`) {
		t.Fatalf("body = %q", body)
	}
}

func TestCreateTemporaryObjectSet(t *testing.T) {
	srv, _ := newTestServer(t, map[string]http.HandlerFunc{
		"POST /api/v2/ontologies/nw/objectSets/createTemporary": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"objectSetRid":"ri.objectset.main.tmp.abc123"}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	rid, err := c.CreateTemporaryObjectSet(context.Background(), "nw", map[string]any{"type": "base", "objectType": "Customer"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rid != "ri.objectset.main.tmp.abc123" {
		t.Fatalf("rid = %q", rid)
	}
}

func TestGetObjectSet(t *testing.T) {
	srv, _ := newTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v2/ontologies/nw/objectSets/ri.objectset.main.tmp.abc123": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"type":"base","objectType":"Customer"}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	resp, err := c.GetObjectSet(context.Background(), "nw", "ri.objectset.main.tmp.abc123")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if resp["type"] != "base" || resp["objectType"] != "Customer" {
		t.Fatalf("unexpected resp: %+v", resp)
	}
}

// TestRollbackDataset verifies the URL shape (including the ?to= query
// parameter) and the response decode round-trip for the US-390 PITR API.
func TestRollbackDataset(t *testing.T) {
	srv, rec := newTestServer(t, map[string]http.HandlerFunc{
		"POST /api/v2/datasets/shop/rollback": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"rolledBackTxIds":["tx-2","tx-3"],
				"restoredObjects":1,
				"deletedObjects":1,
				"newTransaction":{"txId":"tx-bookkeeping","parentTxId":"tx-1","ontologyApiName":"shop","committedAt":"2026-05-03T00:00:00Z","editsCount":2,"rolledBackToTxId":"tx-1"},
				"targetTx":{"txId":"tx-1","ontologyApiName":"shop","committedAt":"2026-01-01T00:00:00Z","editsCount":1}
			}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	resp, err := c.RollbackDataset(context.Background(), "shop", "tx-1")
	if err != nil {
		t.Fatalf("RollbackDataset: %v", err)
	}
	if resp.RestoredObjects != 1 || resp.DeletedObjects != 1 {
		t.Errorf("counts = (%d, %d), want (1, 1)", resp.RestoredObjects, resp.DeletedObjects)
	}
	if len(resp.RolledBackTxIDs) != 2 {
		t.Errorf("RolledBackTxIDs = %v", resp.RolledBackTxIDs)
	}
	if resp.NewTransaction == nil || resp.NewTransaction.TxID != "tx-bookkeeping" {
		t.Errorf("NewTransaction = %+v", resp.NewTransaction)
	}
	if resp.TargetTx == nil || resp.TargetTx.TxID != "tx-1" {
		t.Errorf("TargetTx = %+v", resp.TargetTx)
	}
	if len(*rec) != 1 {
		t.Fatalf("expected 1 request, got %d", len(*rec))
	}
	if want := "/api/v2/datasets/shop/rollback?to=tx-1"; (*rec)[0].path != want {
		t.Errorf("request URI = %q, want %q", (*rec)[0].path, want)
	}
}

// TestRollbackDatasetSurfacesAPIError ensures a 404 from the server is
// surfaced as a typed *APIError with the right ErrorName so the CLI can
// render it cleanly.
func TestRollbackDatasetSurfacesAPIError(t *testing.T) {
	srv, _ := newTestServer(t, map[string]http.HandlerFunc{
		"POST /api/v2/datasets/shop/rollback": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errorCode":"NOT_FOUND","errorName":"RollbackTargetNotFound","parameters":{"to":"tx-missing"}}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	_, err := c.RollbackDataset(context.Background(), "shop", "tx-missing")
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T (%v)", err, err)
	}
	if apiErr.ErrorName != "RollbackTargetNotFound" {
		t.Errorf("ErrorName = %q", apiErr.ErrorName)
	}
}

// TestCreateDatasetTransactionStampsBody verifies the optional body is
// posted when supplied and that the omit-empty fields don't leak through.
func TestCreateDatasetTransactionStampsBody(t *testing.T) {
	srv, rec := newTestServer(t, map[string]http.HandlerFunc{
		"POST /api/v2/datasets/shop/transactions": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"txId":"tx-newhead","parentTxId":"tx-prev","ontologyApiName":"shop","committedAt":"2026-05-03T00:00:00Z","editsCount":0,"userId":"alice"}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	tx, err := c.CreateDatasetTransaction(context.Background(), "shop", &CreateDatasetTransactionRequest{
		UserID: "alice",
	})
	if err != nil {
		t.Fatalf("CreateDatasetTransaction: %v", err)
	}
	if tx.TxID != "tx-newhead" || tx.ParentTxID != "tx-prev" || tx.UserID != "alice" {
		t.Errorf("tx = %+v", tx)
	}
	if !strings.Contains((*rec)[0].body, `"userId":"alice"`) {
		t.Errorf("body should carry userId, got %q", (*rec)[0].body)
	}
}

// TestDatasetHistoryParsesTransactions verifies the history endpoint shape.
func TestDatasetHistoryParsesTransactions(t *testing.T) {
	srv, _ := newTestServer(t, map[string]http.HandlerFunc{
		"GET /api/v2/datasets/shop/history": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"transactions":[
				{"txId":"tx-2","parentTxId":"tx-1","ontologyApiName":"shop","committedAt":"2026-01-01T00:01:00Z","editsCount":3},
				{"txId":"tx-1","ontologyApiName":"shop","committedAt":"2026-01-01T00:00:00Z","editsCount":1}
			]}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	hist, err := c.DatasetHistory(context.Background(), "shop")
	if err != nil {
		t.Fatalf("DatasetHistory: %v", err)
	}
	if len(hist.Transactions) != 2 {
		t.Fatalf("len = %d, want 2", len(hist.Transactions))
	}
	if hist.Transactions[0].TxID != "tx-2" || hist.Transactions[1].TxID != "tx-1" {
		t.Errorf("ordering = %s, %s", hist.Transactions[0].TxID, hist.Transactions[1].TxID)
	}
}
