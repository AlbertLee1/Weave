package oss_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/cipher"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
)

// US-040: CipherTextProperty decrypt endpoint.
//
//   GET /api/v2/ontologies/{o}/objects/{type}/{pk}/ciphertexts/{property}/decrypt
//
// Returns DecryptionResult { plaintext }. The handler loads the addressed
// object, reads the string stored under the {property} path segment, and
// delegates to the configured Decryptor.

const cipherTestKey = "0123456789abcdef0123456789abcdef"

func setupCipherTest(t *testing.T) (http.Handler, cipher.Decryptor) {
	t.Helper()

	svc, mgr, repo, _ := setupOSSTest(t)

	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.secret",
		OntologyRID: testOntologyRID,
		APIName:     "secret",
		DisplayName: "Secret",
		PrimaryKey:  "secretId",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	})

	if _, err := mgr.EnsureIndex("secret", []index.Property{
		{APIName: "secretId", BaseType: "string", IsSearchable: true},
		{APIName: "token", BaseType: "ciphertext", IsSearchable: false},
	}); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}

	dec, err := cipher.NewAESGCMDecryptor(cipherTestKey)
	if err != nil {
		t.Fatalf("NewAESGCMDecryptor: %v", err)
	}
	ct, err := dec.Encrypt(context.Background(), "super-secret-password")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	if err := mgr.IndexDocument("secret", "s1", map[string]interface{}{
		"secretId": "s1",
		"token":    ct,
	}); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	h := oss.NewHandler(svc)
	h.SetCipherDecryptor(dec)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r, dec
}

func TestDecryptCipherTextProperty_Success(t *testing.T) {
	r, _ := setupCipherTest(t)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/secret/s1/ciphertexts/token/decrypt",
		nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out["plaintext"] != "super-secret-password" {
		t.Errorf("plaintext = %v, want super-secret-password", out["plaintext"])
	}
}

func TestDecryptCipherTextProperty_ObjectNotFound(t *testing.T) {
	r, _ := setupCipherTest(t)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/secret/missing/ciphertexts/token/decrypt",
		nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body=%s", rec.Code, rec.Body.String())
	}
}

func TestDecryptCipherTextProperty_PropertyNotFound(t *testing.T) {
	r, _ := setupCipherTest(t)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/secret/s1/ciphertexts/missingProp/decrypt",
		nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body=%s", rec.Code, rec.Body.String())
	}
}

func TestDecryptCipherTextProperty_NotConfigured(t *testing.T) {
	svc, mgr, repo, _ := setupOSSTest(t)
	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.secret",
		OntologyRID: testOntologyRID,
		APIName:     "secret",
		DisplayName: "Secret",
		PrimaryKey:  "secretId",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	})
	if _, err := mgr.EnsureIndex("secret", []index.Property{
		{APIName: "secretId", BaseType: "string"},
		{APIName: "token", BaseType: "ciphertext"},
	}); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	if err := mgr.IndexDocument("secret", "s1", map[string]interface{}{
		"secretId": "s1",
		"token":    "v1:whatever",
	}); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	// No SetCipherDecryptor → decryptor nil.
	h := oss.NewHandler(svc)
	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/secret/s1/ciphertexts/token/decrypt",
		nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500, body=%s", rec.Code, rec.Body.String())
	}
}

func TestDecryptCipherTextProperty_InvalidCiphertext(t *testing.T) {
	svc, mgr, repo, _ := setupOSSTest(t)
	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.secret",
		OntologyRID: testOntologyRID,
		APIName:     "secret",
		DisplayName: "Secret",
		PrimaryKey:  "secretId",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	})
	if _, err := mgr.EnsureIndex("secret", []index.Property{
		{APIName: "secretId", BaseType: "string"},
		{APIName: "token", BaseType: "ciphertext"},
	}); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	if err := mgr.IndexDocument("secret", "s1", map[string]interface{}{
		"secretId": "s1",
		"token":    "not-a-valid-ciphertext",
	}); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	dec, _ := cipher.NewAESGCMDecryptor(cipherTestKey)
	h := oss.NewHandler(svc)
	h.SetCipherDecryptor(dec)
	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/secret/s1/ciphertexts/token/decrypt",
		nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}
