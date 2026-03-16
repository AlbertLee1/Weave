package funnel

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

const (
	StreamName    = "OBJECT_EDITS"
	SubjectPrefix = "edits"
)

// Publisher publishes edit batches to NATS JetStream.
type Publisher struct {
	js nats.JetStreamContext
}

// NewPublisher creates a new edit publisher.
func NewPublisher(js nats.JetStreamContext) *Publisher {
	return &Publisher{js: js}
}

// Publish publishes an edit batch.
// Subject format: edits.<objectType>
func (p *Publisher) Publish(batch *EditBatch) (uint64, error) {
	if len(batch.Edits) == 0 {
		return 0, fmt.Errorf("batch has no edits")
	}

	data, err := json.Marshal(batch)
	if err != nil {
		return 0, fmt.Errorf("marshal batch: %w", err)
	}

	subject := BuildSubject(batch.Edits[0].ObjectType)

	ack, err := p.js.Publish(subject, data)
	if err != nil {
		return 0, fmt.Errorf("publish: %w", err)
	}

	return ack.Sequence, nil
}

// PublishEdit is a convenience method to publish a single edit.
func (p *Publisher) PublishEdit(edit Edit, userID string) (uint64, error) {
	batch := &EditBatch{
		ID:        GenerateBatchID(),
		Edits:     []Edit{edit},
		UserID:    userID,
		Timestamp: time.Now(),
	}
	return p.Publish(batch)
}

// BuildSubject returns the NATS subject for a given object type.
func BuildSubject(objectType string) string {
	return fmt.Sprintf("%s.%s", SubjectPrefix, objectType)
}

// GenerateBatchID returns a new unique batch ID.
func GenerateBatchID() string {
	return uuid.New().String()
}
