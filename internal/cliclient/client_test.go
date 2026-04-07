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
	})
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
}

func TestApplyActionPostsParameters(t *testing.T) {
	srv, rec := newTestServer(t, map[string]http.HandlerFunc{
		"POST /api/v2/ontologies/nw/actions/apply": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"actionRid":"ri.act.123","edits":[{"type":"CREATE","objectType":"Customer"}],"batchId":"b1","offset":7}`))
		},
	})
	c := NewClient(srv.URL, "tok")
	res, err := c.ApplyAction(context.Background(), "nw", "createCustomer", map[string]any{"name": "X"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.ActionRID != "ri.act.123" || res.BatchID != "b1" || res.Offset != 7 || len(res.Edits) != 1 {
		t.Fatalf("response = %+v", res)
	}
	body := (*rec)[0].body
	if !strings.Contains(body, `"actionType":"createCustomer"`) || !strings.Contains(body, `"name":"X"`) {
		t.Fatalf("body = %q", body)
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
