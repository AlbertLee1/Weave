package funnel

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
)

const (
	// DLQStreamName is the NATS JetStream stream for dead-lettered messages.
	DLQStreamName = "OBJECT_EDITS_DLQ"
	// DLQSubjectPrefix is the subject prefix for dead-letter messages.
	DLQSubjectPrefix = "edits.dlq"
)

// DLQMessage wraps a terminated message with metadata about why it was
// dead-lettered. Published to the DLQ stream so operators can inspect and
// replay failed messages.
type DLQMessage struct {
	OriginalSubject  string          `json:"originalSubject"`
	OriginalData     json.RawMessage `json:"originalData"`
	Reason           string          `json:"reason"`
	NumDelivered     uint64          `json:"numDelivered"`
	MaxDeliveries    uint64          `json:"maxDeliveries"`
	TerminatedAt     time.Time       `json:"terminatedAt"`
	StreamSequence   uint64          `json:"streamSequence"`
	ConsumerSequence uint64          `json:"consumerSequence"`
}

// DLQPublishFunc is the signature for publishing a message to the DLQ stream.
// Decoupled from nats.JetStreamContext so unit tests can supply a fake.
type DLQPublishFunc func(subject string, data []byte) error

// BuildDLQSubject returns the DLQ subject for a given original subject.
// It replaces the "edits." prefix with "edits.dlq.".
func BuildDLQSubject(objectType string) string {
	return fmt.Sprintf("%s.%s", DLQSubjectPrefix, objectType)
}

// DLQStreamConfig returns the NATS stream configuration for the DLQ stream.
func DLQStreamConfig() *nats.StreamConfig {
	return &nats.StreamConfig{
		Name:      DLQStreamName,
		Subjects:  []string{DLQSubjectPrefix + ".>"},
		Retention: nats.LimitsPolicy,
		MaxAge:    72 * time.Hour,
		Storage:   nats.FileStorage,
	}
}

// SetupDLQStream creates the NATS JetStream DLQ stream.
func SetupDLQStream(js nats.JetStreamContext) error {
	_, err := js.AddStream(DLQStreamConfig())
	if err != nil {
		return fmt.Errorf("create DLQ stream: %w", err)
	}
	return nil
}

// NewDLQPublishFunc returns a DLQPublishFunc backed by a real JetStream context.
func NewDLQPublishFunc(js nats.JetStreamContext) DLQPublishFunc {
	return func(subject string, data []byte) error {
		_, err := js.Publish(subject, data)
		return err
	}
}

// publishToDLQ wraps the original message in a DLQMessage envelope and
// publishes it to the DLQ stream. If dlqPublish is nil, the method logs a
// warning and returns nil (graceful degradation).
func (c *Consumer) publishToDLQ(originalSubject string, originalData []byte, numDelivered, streamSeq, consumerSeq uint64) error {
	if c.dlqPublish == nil {
		log.Printf("funnel: DLQ not configured, dropping terminated message from %s", originalSubject)
		return nil
	}

	// Extract object type from "edits.<objectType>" subject.
	objectType := strings.TrimPrefix(originalSubject, SubjectPrefix+".")
	dlqSubject := BuildDLQSubject(objectType)

	dlqMsg := DLQMessage{
		OriginalSubject:  originalSubject,
		OriginalData:     originalData,
		Reason:           fmt.Sprintf("exceeded max deliveries (%d/%d)", numDelivered, c.maxDeliveries),
		NumDelivered:     numDelivered,
		MaxDeliveries:    c.maxDeliveries,
		TerminatedAt:     time.Now(),
		StreamSequence:   streamSeq,
		ConsumerSequence: consumerSeq,
	}

	data, err := json.Marshal(dlqMsg)
	if err != nil {
		return fmt.Errorf("marshal DLQ message: %w", err)
	}

	if err := c.dlqPublish(dlqSubject, data); err != nil {
		return fmt.Errorf("publish to DLQ: %w", err)
	}

	log.Printf("funnel: dead-lettered message to %s (deliveries: %d/%d, stream seq: %d)",
		dlqSubject, numDelivered, c.maxDeliveries, streamSeq)
	return nil
}
