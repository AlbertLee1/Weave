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

// BuildDLQSubject returns the DLQ subject for a given suffix. The suffix is
// the portion of the original subject after "edits." and may itself be a
// scoped "{ontology}.{objectType}" pair. The DLQ subject preserves whatever
// suffix it's given so DLQ consumers can replay back to the original scoped
// stream subject.
func BuildDLQSubject(suffix string) string {
	return fmt.Sprintf("%s.%s", DLQSubjectPrefix, suffix)
}

// IsDLQSubject reports whether subject names a DLQ message (subject starts
// with "edits.dlq."). DLQ consumers use this to gate replay logic so an
// accidentally mis-routed live edit cannot trigger the replay path.
func IsDLQSubject(subject string) bool {
	return strings.HasPrefix(subject, DLQSubjectPrefix+".")
}

// OriginalSubjectFromDLQ inverts BuildDLQSubject: given a DLQ subject of the
// form "edits.dlq.<suffix>" it returns "edits.<suffix>". Non-DLQ subjects
// are returned unchanged, which is the safe degrade — a replay handler with
// a corrupted subject still routes back to live edits rather than dropping
// the payload on the floor.
func OriginalSubjectFromDLQ(dlqSubject string) string {
	suffix, ok := strings.CutPrefix(dlqSubject, DLQSubjectPrefix+".")
	if !ok {
		return dlqSubject
	}
	return SubjectPrefix + "." + suffix
}

// ReplayDLQMessage re-publishes a previously dead-lettered message back onto
// its original live subject so the consumer takes another pass at it. Returns
// an error when the envelope's OriginalData is empty (nothing to replay) or
// the publish function fails; the caller is expected to surface the error
// upstream so operators can decide between giving up (DiscardDLQMessage) or
// retrying after the underlying outage clears. The DLQ entry itself is left
// in place — JetStream's MaxAge handles eviction so replay is naturally
// idempotent against duplicate calls.
func ReplayDLQMessage(msg DLQMessage, publish DLQPublishFunc) error {
	if publish == nil {
		return fmt.Errorf("replay DLQ message: publish func is nil")
	}
	if len(msg.OriginalData) == 0 {
		return fmt.Errorf("replay DLQ message: original payload empty")
	}
	subject := OriginalSubjectFromDLQ(msg.OriginalSubject)
	if subject == "" {
		return fmt.Errorf("replay DLQ message: cannot derive original subject from %q", msg.OriginalSubject)
	}
	if err := publish(subject, msg.OriginalData); err != nil {
		return fmt.Errorf("replay DLQ message: %w", err)
	}
	return nil
}

// DiscardDLQMessage is the explicit "give up on this payload" counterpart to
// ReplayDLQMessage. It is a no-op against the DLQ stream — the operator UI
// calls it as a marker the message has been triaged and intentionally not
// replayed; the JetStream MaxAge window evicts the entry on its own schedule.
// The helper returns the DLQ subject so callers can log/audit the discard
// without re-parsing the envelope.
func DiscardDLQMessage(msg DLQMessage) string {
	if msg.OriginalSubject == "" {
		return ""
	}
	if IsDLQSubject(msg.OriginalSubject) {
		return msg.OriginalSubject
	}
	// OriginalSubject in well-formed envelopes is the live subject (set when
	// the consumer dead-lettered the message), so the DLQ subject is derived
	// from it the same way publishToDLQ did at enqueue time.
	suffix, ok := strings.CutPrefix(msg.OriginalSubject, SubjectPrefix+".")
	if !ok {
		return msg.OriginalSubject
	}
	return BuildDLQSubject(suffix)
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
