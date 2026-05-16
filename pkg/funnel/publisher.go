package funnel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
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
//
// US-006: every published message carries the batch ID as the Nats-Msg-Id
// header so JetStream's native dedupe window collapses duplicate publishes
// (retry storms, double-shipped saga commits) before the consumer even sees
// the second copy. Empty batch.IDs are tolerated — the header is omitted in
// that case and dedupe degrades to consumer-side bleve upsert idempotency.
func (p *Publisher) Publish(batch *EditBatch) (uint64, error) {
	return p.PublishContext(context.Background(), batch)
}

// PublishContext is the context-aware sibling of Publish (OSV2-306). It
// opens a "funnel.publish" span around the marshal + NATS write so the
// upstream HTTP trace context (chi middleware in cmd/server, propagated
// down through pkg/actions / pkg/oss) carries through into the JetStream
// envelope. The propagated trace headers are written onto msg.Header so
// the Consumer side can stitch a child span onto the same trace id when
// it later pulls the message.
//
// Publish() — the historical signature — simply forwards a
// context.Background to here so existing callers keep working unchanged.
func (p *Publisher) PublishContext(ctx context.Context, batch *EditBatch) (uint64, error) {
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
	msg := BuildPublishMsg(batch, data, subject)

	ctx, span := otel.Tracer(tracerName).Start(ctx, publishSpanName,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("funnel.subject", subject),
			attribute.String("funnel.ontology", batch.OntologyAPIName),
			attribute.Int("funnel.batch_size", len(batch.Edits)),
		),
	)
	defer span.End()
	// Inject AFTER starting the span so traceparent points at the publish
	// span as the parent on the consumer side, not at whatever ctx held
	// before this call.
	InjectTraceContext(ctx, msg.Header)

	ack, err := p.js.PublishMsg(msg)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return 0, fmt.Errorf("publish: %w", err)
	}
	span.SetAttributes(attribute.Int64("funnel.stream_seq", int64(ack.Sequence)))
	return ack.Sequence, nil
}

// tracerName is the package-scoped tracer name used by both publisher
// and consumer spans so dashboards / sampling configs can filter on a
// stable identifier.
const tracerName = "github.com/liyang/weave/pkg/funnel"

// BuildPublishMsg wraps an encoded EditBatch in a *nats.Msg with the
// JetStream-native dedupe header populated. Exposed so callers that need to
// add their own publish options (rate-limit headers, traceparent, etc.)
// can layer on top of the same envelope the Publisher writes. data must be
// the JSON encoding of batch and subject must already include the per-ontology
// scope — the helper does not re-marshal or re-derive either.
func BuildPublishMsg(batch *EditBatch, data []byte, subject string) *nats.Msg {
	msg := nats.NewMsg(subject)
	msg.Data = data
	if batch != nil && batch.ID != "" {
		msg.Header.Set(nats.MsgIdHdr, batch.ID)
	}
	return msg
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
