package attachment

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBDD_AttachmentSizeBytesSerializedAsString pins the Foundry OSv2
// AttachmentV2 wire contract: sizeBytes is a SafeLong, which serializes on the
// wire as a JSON *string* (e.g. "11"), never as a bare number. SDKs that model
// sizeBytes as a string (Foundry's AttachmentsAPI) fail to deserialize a bare
// number, so the deviation is user-visible.
//
//	Given a client uploads a blob through the chi router
//	When the upload response and the follow-up GET metadata are read
//	Then sizeBytes appears as a quoted JSON string in both response bodies.
func TestBDD_AttachmentSizeBytesSerializedAsString(t *testing.T) {
	r, _ := newTestRouter(t)

	// "hello world" is 11 bytes.
	body := "hello world"
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/attachments/upload?filename=hello.txt",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload status = %d, body=%s", rec.Code, rec.Body.String())
	}

	if got := rec.Body.String(); !strings.Contains(got, `"sizeBytes":"11"`) {
		t.Errorf("upload response sizeBytes is not a JSON string; body=%s", got)
	}

	// Read the generated RID back to exercise the GET metadata exit point.
	var parsed struct {
		RID string `json:"rid"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("decode rid: %v", err)
	}

	req2 := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/attachments/"+parsed.RID, nil)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("metadata status = %d, body=%s", rec2.Code, rec2.Body.String())
	}
	if got := rec2.Body.String(); !strings.Contains(got, `"sizeBytes":"11"`) {
		t.Errorf("metadata response sizeBytes is not a JSON string; body=%s", got)
	}
}

// TestAttachmentSizeBytes_WireStringRoundTrip verifies both directions of the
// SafeLong contract: Attachment marshals sizeBytes as a quoted string, and it
// unmarshals that same quoted string back to the numeric field so read paths
// stay compatible.
func TestAttachmentSizeBytes_WireStringRoundTrip(t *testing.T) {
	raw, err := json.Marshal(Attachment{RID: "ri.attachments.main.attachment.x", SizeBytes: 1024})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"sizeBytes":"1024"`) {
		t.Fatalf("marshal did not quote sizeBytes; got %s", raw)
	}

	var back Attachment
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.SizeBytes != 1024 {
		t.Errorf("round-trip SizeBytes = %d, want 1024", back.SizeBytes)
	}
}
