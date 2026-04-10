package funnel

import (
	"encoding/json"
	"testing"
	"time"
)

// --- DLQ constants tests ---

func TestDLQStreamName(t *testing.T) {
	if DLQStreamName != "OBJECT_EDITS_DLQ" {
		t.Fatalf("expected DLQ stream name %q, got %q", "OBJECT_EDITS_DLQ", DLQStreamName)
	}
}

func TestDLQSubjectPrefix(t *testing.T) {
	if DLQSubjectPrefix != "edits.dlq" {
		t.Fatalf("expected DLQ subject prefix %q, got %q", "edits.dlq", DLQSubjectPrefix)
	}
}

func TestBuildDLQSubject(t *testing.T) {
	got := BuildDLQSubject("employee")
	want := "edits.dlq.employee"
	if got != want {
		t.Fatalf("expected DLQ subject %q, got %q", want, got)
	}
}

// --- DLQ message envelope test ---

func TestDLQMessage_JSON(t *testing.T) {
	original := EditBatch{
		ID:        "batch-1",
		UserID:    "user-1",
		Timestamp: time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
		Edits: []Edit{
			{
				Type:       EditTypeCreate,
				ObjectType: "employee",
				PrimaryKey: "emp-1",
				Properties: map[string]interface{}{"name": "Alice"},
			},
		},
	}
	origData, _ := json.Marshal(original)

	dlqMsg := DLQMessage{
		OriginalSubject:  "edits.employee",
		OriginalData:     origData,
		Reason:           "exceeded max deliveries (6/5)",
		NumDelivered:     6,
		MaxDeliveries:    5,
		TerminatedAt:     time.Date(2026, 1, 15, 10, 5, 0, 0, time.UTC),
		StreamSequence:   42,
		ConsumerSequence: 38,
	}

	data, err := json.Marshal(dlqMsg)
	if err != nil {
		t.Fatalf("marshal DLQMessage: %v", err)
	}

	var decoded DLQMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal DLQMessage: %v", err)
	}

	if decoded.OriginalSubject != "edits.employee" {
		t.Fatalf("expected OriginalSubject %q, got %q", "edits.employee", decoded.OriginalSubject)
	}
	if decoded.NumDelivered != 6 {
		t.Fatalf("expected NumDelivered 6, got %d", decoded.NumDelivered)
	}
	if decoded.MaxDeliveries != 5 {
		t.Fatalf("expected MaxDeliveries 5, got %d", decoded.MaxDeliveries)
	}
	if decoded.StreamSequence != 42 {
		t.Fatalf("expected StreamSequence 42, got %d", decoded.StreamSequence)
	}
	if decoded.ConsumerSequence != 38 {
		t.Fatalf("expected ConsumerSequence 38, got %d", decoded.ConsumerSequence)
	}
	if decoded.Reason != "exceeded max deliveries (6/5)" {
		t.Fatalf("expected Reason %q, got %q", "exceeded max deliveries (6/5)", decoded.Reason)
	}

	// Verify original data round-trips
	var roundTripped EditBatch
	if err := json.Unmarshal(decoded.OriginalData, &roundTripped); err != nil {
		t.Fatalf("unmarshal original data from DLQ: %v", err)
	}
	if roundTripped.ID != "batch-1" {
		t.Fatalf("expected batch ID %q, got %q", "batch-1", roundTripped.ID)
	}
}

// --- SetupDLQStream config test ---

func TestDLQStreamConfig(t *testing.T) {
	cfg := DLQStreamConfig()
	if cfg.Name != DLQStreamName {
		t.Fatalf("expected stream name %q, got %q", DLQStreamName, cfg.Name)
	}
	if len(cfg.Subjects) != 1 || cfg.Subjects[0] != DLQSubjectPrefix+".>" {
		t.Fatalf("expected subjects [%q], got %v", DLQSubjectPrefix+".>", cfg.Subjects)
	}
	if cfg.MaxAge != 72*time.Hour {
		t.Fatalf("expected MaxAge 72h, got %v", cfg.MaxAge)
	}
}

// --- Consumer DLQ publish test (unit, using fake publisher) ---

func TestConsumer_PublishToDLQ(t *testing.T) {
	consumer, _ := setupTestConsumer(t)
	fake := &fakeDLQPublisher{}
	consumer.dlqPublish = fake.Publish

	batch := EditBatch{
		ID:     "batch-dead",
		UserID: "user-1",
		Edits: []Edit{
			{
				Type:       EditTypeCreate,
				ObjectType: "employee",
				PrimaryKey: "emp-dead",
				Properties: map[string]interface{}{"name": "Dead"},
			},
		},
	}
	data, _ := json.Marshal(batch)

	err := consumer.publishToDLQ("edits.employee", data, 6, 42, 38)
	if err != nil {
		t.Fatalf("publishToDLQ: %v", err)
	}

	if len(fake.published) != 1 {
		t.Fatalf("expected 1 DLQ publish, got %d", len(fake.published))
	}

	pub := fake.published[0]
	if pub.subject != "edits.dlq.employee" {
		t.Fatalf("expected DLQ subject %q, got %q", "edits.dlq.employee", pub.subject)
	}

	var dlqMsg DLQMessage
	if err := json.Unmarshal(pub.data, &dlqMsg); err != nil {
		t.Fatalf("unmarshal DLQ message: %v", err)
	}
	if dlqMsg.OriginalSubject != "edits.employee" {
		t.Fatalf("expected OriginalSubject %q, got %q", "edits.employee", dlqMsg.OriginalSubject)
	}
	if dlqMsg.NumDelivered != 6 {
		t.Fatalf("expected NumDelivered 6, got %d", dlqMsg.NumDelivered)
	}
	if dlqMsg.StreamSequence != 42 {
		t.Fatalf("expected StreamSequence 42, got %d", dlqMsg.StreamSequence)
	}
	if dlqMsg.ConsumerSequence != 38 {
		t.Fatalf("expected ConsumerSequence 38, got %d", dlqMsg.ConsumerSequence)
	}
}

// TestConsumer_PublishToDLQ_SubjectExtraction verifies subject-to-DLQ mapping
// for various object type names.
func TestConsumer_PublishToDLQ_SubjectExtraction(t *testing.T) {
	tests := []struct {
		originalSubject string
		wantDLQSubject  string
	}{
		{"edits.employee", "edits.dlq.employee"},
		{"edits.project", "edits.dlq.project"},
		{"edits.order-item", "edits.dlq.order-item"},
	}

	for _, tt := range tests {
		t.Run(tt.originalSubject, func(t *testing.T) {
			consumer, _ := setupTestConsumer(t)
			fake := &fakeDLQPublisher{}
			consumer.dlqPublish = fake.Publish

			err := consumer.publishToDLQ(tt.originalSubject, []byte(`{}`), 6, 1, 1)
			if err != nil {
				t.Fatalf("publishToDLQ: %v", err)
			}
			if len(fake.published) != 1 {
				t.Fatalf("expected 1 publish, got %d", len(fake.published))
			}
			if fake.published[0].subject != tt.wantDLQSubject {
				t.Fatalf("expected DLQ subject %q, got %q", tt.wantDLQSubject, fake.published[0].subject)
			}
		})
	}
}

// TestConsumer_DLQPublish_NilFunc verifies that publishToDLQ is a no-op
// (logs only, no error) when dlqPublish is nil.
func TestConsumer_DLQPublish_NilFunc(t *testing.T) {
	consumer, _ := setupTestConsumer(t)
	// dlqPublish is nil by default

	err := consumer.publishToDLQ("edits.employee", []byte(`{}`), 6, 1, 1)
	if err != nil {
		t.Fatalf("publishToDLQ with nil func should not error: %v", err)
	}
}

// --- Test helpers ---

type fakeDLQPublisher struct {
	published []publishedMsg
}

type publishedMsg struct {
	subject string
	data    []byte
}

func (f *fakeDLQPublisher) Publish(subj string, data []byte) error {
	f.published = append(f.published, publishedMsg{subject: subj, data: data})
	return nil
}
