package oss_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBDD_AttachmentPropertyMetadata_SizeBytesString pins the Foundry
// AttachmentV2 SafeLong contract on the object-path attachment metadata
// endpoint: sizeBytes must serialize as a quoted JSON string, matching the
// global attachment endpoints and Foundry's wire shape.
//
//	Given an object whose attachment property points at a stored blob
//	When the attachment property metadata endpoint is read through the router
//	Then sizeBytes appears as a quoted JSON string.
func TestBDD_AttachmentPropertyMetadata_SizeBytesString(t *testing.T) {
	r, _, _ := setupAttachmentPropertyTest(t)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/invoice/inv1/attachments/scan",
		nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	// "blob-body" is 9 bytes.
	if got := rec.Body.String(); !strings.Contains(got, `"sizeBytes":"9"`) {
		t.Errorf("attachment property metadata sizeBytes is not a JSON string; body=%s", got)
	}
}
