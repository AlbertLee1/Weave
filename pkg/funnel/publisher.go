package funnel

import (
	"encoding/json"
	"fmt"
	"strings"
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

// Publish publishes an edit batch. The batch must carry an OntologyAPIName
// because subjects are scoped per ontology so the consumer can route edits
// to the correct per-ontology Bleve index.
// Subject format: edits.<ontologyApiName>.<objectType>
func (p *Publisher) Publish(batch *EditBatch) (uint64, error) {
	if len(batch.Edits) == 0 {
		return 0, fmt.Errorf("batch has no edits")
	}
	if batch.OntologyAPIName == "" {
		return 0, fmt.Errorf("batch has no ontologyApiName")
	}

	data, err := json.Marshal(batch)
	if err != nil {
		return 0, fmt.Errorf("marshal batch: %w", err)
	}

	subject := BuildSubject(batch.OntologyAPIName, batch.Edits[0].ObjectType)

	ack, err := p.js.Publish(subject, data)
	if err != nil {
		return 0, fmt.Errorf("publish: %w", err)
	}

	return ack.Sequence, nil
}

// PublishEdit is a convenience method to publish a single edit.
func (p *Publisher) PublishEdit(ontologyAPIName string, edit Edit, userID string) (uint64, error) {
	batch := &EditBatch{
		ID:              GenerateBatchID(),
		OntologyAPIName: ontologyAPIName,
		Edits:           []Edit{edit},
		UserID:          userID,
		Timestamp:       time.Now(),
	}
	return p.Publish(batch)
}

// BuildSubject returns the NATS subject for a (ontology, objectType) pair.
// US-044: subjects are scoped by ontology so two ontologies' identically-named
// ObjectTypes route to distinct Bleve indexes.
func BuildSubject(ontologyAPIName, objectType string) string {
	return fmt.Sprintf("%s.%s.%s", SubjectPrefix, ontologyAPIName, objectType)
}

// ParseSubject inverts BuildSubject. It returns the ontology api name and the
// object type embedded in a scoped edits subject. Legacy two-token subjects
// (edits.<objectType>) are rejected because they cannot be routed to a
// scoped Bleve index.
func ParseSubject(subject string) (ontologyAPIName, objectType string, err error) {
	const prefix = SubjectPrefix + "."
	rest, ok := strings.CutPrefix(subject, prefix)
	if !ok {
		return "", "", fmt.Errorf("subject %q missing %q prefix", subject, prefix)
	}
	dot := strings.IndexByte(rest, '.')
	if dot <= 0 || dot == len(rest)-1 {
		return "", "", fmt.Errorf("subject %q is not in scoped form edits.<ontology>.<objectType>", subject)
	}
	return rest[:dot], rest[dot+1:], nil
}

// GenerateBatchID returns a new unique batch ID.
func GenerateBatchID() string {
	return uuid.New().String()
}
