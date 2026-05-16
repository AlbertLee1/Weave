package funnel

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/metrics"
	"github.com/liyang/weave/pkg/oms"
)

const (
	// DefaultMaxDeliveries is the maximum number of delivery attempts before
	// a message is terminated (dead-lettered).
	DefaultMaxDeliveries = 5

	// markingsField is the reserved Bleve keyword field that carries every
	// object's marking set. Kept in lockstep with pkg/security.MarkingField
	// (which the policy engine's auto-marking clause targets). A separate
	// constant here avoids dragging pkg/security into pkg/funnel's import
	// graph — if you change one, change the other.
	markingsField = "_markings"
)

// HistoryRecorder is the minimal subset of oms.Repository required by the
// consumer to write ObjectHistory rows and drive US-021 user-edit-wins
// conflict resolution. Defined as an interface so the consumer never imports
// the full PG repo (and tests can supply a fake).
//
// LatestUserEditAt returns the recorded_at of the most recent row whose
// source == EditSourceUser for (objectTypeRID, primaryKey). The boolean is
// false when no user edit exists, in which case ingest edits overwrite
// normally. Implementations must return (zero, false, nil) — not an error —
// for a missing row.
type HistoryRecorder interface {
	InsertObjectHistory(ctx context.Context, h *oms.ObjectHistory) error
	LatestUserEditAt(ctx context.Context, objectTypeRID, primaryKey string) (time.Time, bool, error)
}

// DatasetTransactionRecorder is the narrow surface the consumer uses to
// stamp one dataset_transactions row per applied EditBatch (US-379). The
// chain is per-OntologyAPIName: ParentTxID points at the previous tx for
// the same ontology so /datasets/{rid}/history can walk it back to the
// genesis tx. Pass nil to disable transaction recording — every other
// downstream feature (object_history, US-223 asOf-by-timestamp, asOf=tx-
// lookup) degrades cleanly when the recorder is absent.
type DatasetTransactionRecorder interface {
	RecordDatasetTransaction(ctx context.Context, tx *oms.DatasetTransaction) error
	LatestForOntology(ctx context.Context, ontologyAPIName string) (*oms.DatasetTransaction, error)
}

// LinkEdgeWriter is the minimal interface for writing M2M link edges.
// Satisfied by *oms.PGRepository. Tests can supply a fake.
type LinkEdgeWriter interface {
	UpsertLinkEdge(ctx context.Context, edge *oms.LinkEdge) error
}

// LinkEdgeDeleter is the minimal interface for deleting M2M link edges.
// Satisfied by *oms.PGRepository. Tests can supply a fake.
type LinkEdgeDeleter interface {
	DeleteLinkEdge(ctx context.Context, linkTypeRID, sourcePK, targetPK string) error
}

// LinkPropagation describes the LinkType-level settings the consumer needs
// to drive US-261 marking inheritance. When PropagateMarkings is true, the
// consumer copies the source object's markings into the target after a
// successful LINK_CREATE upsert. The two API names are required so the
// consumer can scope Bleve fetches/writes to the correct per-objectType
// indexes; the field-name mismatch with LinkType.SourceObjectType (RIDs in
// PG) is intentional — pkg/funnel must not import pkg/oms's RID resolution.
type LinkPropagation struct {
	PropagateMarkings       bool
	SourceObjectTypeAPIName string
	TargetObjectTypeAPIName string
}

// LinkPropagationResolver returns the propagation settings for a LinkType
// RID. Nil resolver disables propagation altogether; (zero, false, nil)
// means "LinkType not found" and is treated as a soft skip so an out-of-band
// delete cannot poison the consumer. Implementations are expected to be
// thread-safe (the consumer calls them from its NATS callback goroutine).
type LinkPropagationResolver interface {
	LookupLinkPropagation(ctx context.Context, linkTypeRID string) (LinkPropagation, bool, error)
}

// PropagatingOutgoingEdge describes a single downstream propagating edge
// the consumer can walk during US-474 BFS marking propagation. Each entry
// is one (linkTypeRID, targetObjectType, targetPK) triple; the consumer
// uses TargetObjectTypeAPIName to scope the bleve doc fetch + re-index.
type PropagatingOutgoingEdge struct {
	LinkTypeRID             string
	TargetObjectTypeAPIName string
	TargetPK                string
}

// LinkPropagationTraverser is the US-474 capability the consumer consults
// when walking forward through the link graph during multi-hop marking
// propagation. Implementations must return only edges whose LinkType has
// PropagateMarkings=true — non-propagating outgoing edges are invisible
// to the BFS so a propagate=false hop naturally truncates the walk.
//
// Nil traverser disables BFS and the consumer falls back to pre-US-474
// one-hop propagation, which is the safe degradation mode when prod
// wiring has not yet been refreshed.
type LinkPropagationTraverser interface {
	ListPropagatingOutgoingEdges(
		ctx context.Context,
		sourceObjectTypeAPIName string,
		sourcePKs []string,
	) ([]PropagatingOutgoingEdge, error)
}

// PIIDetector reports whether an edit's property values carry PII
// (email / SSN / phone / credit card). When wired on the consumer
// every CREATE/MODIFY edit is scanned and a positive result auto-
// tags the indexed document with the well-known "PII" marking so the
// existing marking-based mandatory access control gates visibility
// without requiring the writer to remember to add the marking.
//
// Implementations are expected to be cheap to call and safe for
// concurrent use; pkg/security/pii.Scanner is the canonical impl.
type PIIDetector interface {
	DetectPII(properties map[string]interface{}) bool
}

// PIIMarkingName is the marking auto-attached to objects whose
// properties trigger a positive PII detection. Pinned to "PII" — the
// auth-side migration seeds this exact label and admins grant it via
// the existing marking-grant endpoints.
const PIIMarkingName = "PII"

// Consumer subscribes to NATS and processes edit batches, updating Bleve indexes.
type Consumer struct {
	js            nats.JetStreamContext
	indexMgr      *index.Manager
	sub           *nats.Subscription
	mu            sync.Mutex
	running       bool
	lastOffset    atomic.Uint64
	onChange      func(ChangeEvent) // optional callback
	maxDeliveries uint64

	// dlqPublish publishes terminated messages to the DLQ stream. Nil means
	// DLQ is disabled and terminated messages are dropped with a log warning.
	dlqPublish DLQPublishFunc

	// idempotency dedupes ApplyBatch / handleMessage calls by batch.ID within
	// a sliding window. window<=0 (default) disables the cache and every
	// delivery is applied — JetStream's native Nats-Msg-Id dedup handles the
	// transport layer in that mode. Operators flipping SetIdempotencyWindow on
	// get belt-and-suspenders protection for in-process callers that bypass
	// JetStream entirely (integration tests, future ingest shims).
	idempotency idempotencyCache

	// historyRepo writes a row per applied edit when set. Tier 2.3 wires
	// this to the OMS PG repo. Nil = history disabled (and the US-021
	// user-edit-wins guard degrades to a no-op).
	historyRepo HistoryRecorder

	// alwaysApplyField is the US-021 stub for US-026's is_edit_only column.
	// When non-nil and the function returns true for (objectType, field),
	// the consumer applies that field from an ingest edit even if a newer
	// user edit exists. Default (nil) means "no fields are always-apply"
	// and every ingest write on a protected object is filtered out.
	alwaysApplyField func(objectType, field string) bool
	// editOnlyField is the US-027 hook for property.IsEditOnly. When
	// non-nil and the function returns true for (objectType, field), the
	// consumer strips that field from every ingest CREATE/MODIFY and
	// fetch-merges the current bleve value back so user-managed fields
	// like tags and notes are never overwritten by ingest, regardless of
	// batch timestamps. Default (nil) means no fields are protected.
	editOnlyField func(objectType, field string) bool
	// writablePropertyFilter is the US-076 hook for column-level write
	// enforcement. When non-nil and the function returns false for
	// (objectType, field), the consumer strips that field from every
	// ingest CREATE/MODIFY and merges the current bleve value back so
	// policy-restricted fields are preserved. Default (nil) means all
	// fields are writable (no filter).
	writablePropertyFilter func(objectType, field string) bool
	// objectTypeRIDs maps API name -> RID for resolving objectType -> RID
	// when recording history. Updated by callers via SetObjectTypeRIDs.
	objectTypeRIDs map[string]string
	// versionCounters tracks the next version number to assign per
	// (objectTypeRID, primaryKey). Stored in-memory; survives the lifetime
	// of the consumer process. Sufficient for single-machine Weave; on
	// restart the counters reset which is acceptable because postgres still
	// holds the source of truth and downstream UI sorts by recorded_at.
	versionMu       sync.Mutex
	versionCounters map[string]int64

	// linkEdgeWriter writes M2M link edges when LINK_CREATE edits are
	// processed. Nil = link edits are logged and skipped.
	linkEdgeWriter LinkEdgeWriter

	// linkEdgeDeleter deletes M2M link edges when LINK_DELETE edits are
	// processed. Nil = link delete edits are logged and skipped.
	linkEdgeDeleter LinkEdgeDeleter

	// linkPropagation, when set, resolves LinkType-level propagation
	// settings (US-261) so the consumer can copy a source object's markings
	// onto the target after a LINK_CREATE upsert. Nil disables propagation
	// entirely, which preserves pre-US-261 behaviour.
	linkPropagation LinkPropagationResolver

	// linkPropagationTraverser, when set, enables US-474 multi-hop BFS:
	// after merging markings into a LINK_CREATE target, the consumer walks
	// the target's outgoing propagating edges and continues until no
	// downstream node's marking set changes. Nil keeps the pre-US-474
	// one-hop behaviour — cmd/server wires a PG-backed adapter at boot.
	linkPropagationTraverser LinkPropagationTraverser

	// piiDetector, when set, scans every CREATE/MODIFY edit's property
	// values and auto-attaches the PII marking on a positive match
	// (US-263). Nil leaves the marking set untouched — the same shape
	// every other optional consumer hook follows.
	piiDetector PIIDetector

	// embedFields holds the optional embedding side-channel state. See
	// embeddings.go for the wiring methods and the per-batch hook.
	embedFields

	// txRecorder records one dataset_transactions row per applied batch
	// (US-379). Nil disables recording and the asOf=tx- lookup degrades
	// to "TransactionNotFound" — but US-223 timestamp-based asOf still
	// works against object_history, so this hook is purely additive.
	txRecorder DatasetTransactionRecorder

	// materializer persists every applied batch to durable columnar
	// storage (US-405). Nil disables materialization. Failures inside
	// the materializer are logged but never abort the batch — the
	// index commit is the source of truth, materialization is the
	// downstream snapshot-rebuild source.
	materializer EditMaterializer
}

// NewConsumer creates a new edit consumer.
func NewConsumer(js nats.JetStreamContext, indexMgr *index.Manager) *Consumer {
	return &Consumer{
		js:              js,
		indexMgr:        indexMgr,
		maxDeliveries:   DefaultMaxDeliveries,
		objectTypeRIDs:  map[string]string{},
		versionCounters: map[string]int64{},
	}
}

// SetMaxDeliveries sets the maximum delivery attempts before a message is terminated.
func (c *Consumer) SetMaxDeliveries(n uint64) {
	c.maxDeliveries = n
}

// SetDLQPublish sets the function used to publish terminated messages to the
// DLQ stream. Pass nil to disable (terminated messages will be logged and
// dropped). Safe to call before Start().
func (c *Consumer) SetDLQPublish(fn DLQPublishFunc) {
	c.dlqPublish = fn
}

// SetIdempotencyWindow enables in-process batch ID deduplication. Within the
// configured window, any ApplyBatch / handleMessage call carrying a batch.ID
// the consumer has already applied is short-circuited to a no-op (no second
// index write, no second history row, no second DLQ enqueue). Pass
// d <= 0 (the default) to disable. The 1024-entry bound covers a typical
// in-flight window; oldest entries are evicted on overflow. Safe to call
// before Start.
func (c *Consumer) SetIdempotencyWindow(d time.Duration) {
	c.idempotency.setWindow(d)
}

// SetHistoryRepo enables ObjectHistory recording for every applied edit.
// Pass nil to disable. Safe to call before Start().
func (c *Consumer) SetHistoryRepo(repo HistoryRecorder) {
	c.historyRepo = repo
}

// SetTxRecorder wires the optional US-379 dataset_transactions recorder.
// When set, every successful applyBatchWithHistory writes a row capturing
// (tx_id=batch.ID, parent_tx_id=prior tx for the ontology, committed_at=
// batch.Timestamp, edits_count=len(edits)) and stamps the same tx_id on
// each ObjectHistory row inserted by the same batch. Pass nil to disable.
// Safe to call before Start().
func (c *Consumer) SetTxRecorder(r DatasetTransactionRecorder) {
	c.txRecorder = r
}

// SetLinkEdgeWriter enables M2M link-edge persistence for LINK_CREATE edits.
// Pass nil to disable (link edits will be logged and skipped). Safe to call
// before Start().
func (c *Consumer) SetLinkEdgeWriter(w LinkEdgeWriter) {
	c.linkEdgeWriter = w
}

// SetLinkEdgeDeleter enables M2M link-edge deletion for LINK_DELETE edits.
// Pass nil to disable (link delete edits will be logged and skipped). Safe to
// call before Start().
func (c *Consumer) SetLinkEdgeDeleter(d LinkEdgeDeleter) {
	c.linkEdgeDeleter = d
}

// SetLinkPropagationResolver wires the US-261 marking inheritance hook.
// When set, every LINK_CREATE that successfully upserts an edge consults
// the resolver to decide whether to copy the source object's marking set
// into the target object's `_markings` field. Pass nil to disable. Safe
// to call before Start().
func (c *Consumer) SetLinkPropagationResolver(r LinkPropagationResolver) {
	c.linkPropagation = r
}

// SetLinkPropagationTraverser wires the US-474 multi-hop BFS hook. When
// set, the consumer walks downstream propagating edges from the target
// of every LINK_CREATE so transitively-linked nodes inherit markings in
// the same consumer pass. Pass nil to disable BFS (one-hop only). Safe
// to call before Start().
func (c *Consumer) SetLinkPropagationTraverser(t LinkPropagationTraverser) {
	c.linkPropagationTraverser = t
}

// SetPIIDetector wires the US-263 PII auto-detection hook. When set,
// every CREATE/MODIFY edit has its Properties scanned via the detector
// and a positive result appends the well-known "PII" marking to the
// edit's Markings slice (deduplicated) before the index write — so the
// per-row `_markings` field reflects the auto-tagging on the very
// first read. Pass nil to disable. Safe to call before Start().
func (c *Consumer) SetPIIDetector(d PIIDetector) {
	c.piiDetector = d
}

// SetAlwaysApplyField wires the US-021 always-apply hook. When set, ingest
// edits are allowed to overwrite user state for any (objectType, field)
// tuple the function returns true for, even if a newer user edit exists.
// Pass nil to disable (default behaviour). Safe to call before Start().
// US-026 wires the real implementation against the schema is_edit_only flag.
func (c *Consumer) SetAlwaysApplyField(fn func(objectType, field string) bool) {
	c.alwaysApplyField = fn
}

// SetEditOnlyField wires the US-027 edit-only preservation hook. When set,
// every ingest CREATE/MODIFY has its Properties stripped of any
// (objectType, field) pair the function returns true for; the consumer
// then fetches the current bleve doc and re-merges those editOnly fields
// on top of the stripped ingest map so a subsequent upsert cannot lose
// the user value. The guard runs BEFORE the US-021 timestamp guard so
// ingest edits with newer timestamps still cannot touch editOnly fields.
// Pass nil to disable (default behaviour). Safe to call before Start().
func (c *Consumer) SetEditOnlyField(fn func(objectType, field string) bool) {
	c.editOnlyField = fn
}

// SetWritablePropertyFilter wires the US-076 write-level column policy hook.
// When set, every ingest CREATE/MODIFY has its Properties filtered: only
// fields for which the function returns true are kept. Stripped fields are
// restored from the current bleve doc so the upsert preserves the prior
// value. Pass nil to disable (default behaviour). Safe to call before Start().
func (c *Consumer) SetWritablePropertyFilter(fn func(objectType, field string) bool) {
	c.writablePropertyFilter = fn
}

// SetObjectTypeRIDs supplies the API-name -> RID lookup table used when
// writing history rows. The map is copied; subsequent caller mutations have
// no effect on the consumer's internal state.
func (c *Consumer) SetObjectTypeRIDs(m map[string]string) {
	cp := make(map[string]string, len(m))
	for k, v := range m {
		cp[k] = v
	}
	c.objectTypeRIDs = cp
}

// shouldTerminate returns true if the delivery count exceeds the max deliveries threshold.
func (c *Consumer) shouldTerminate(numDelivered uint64) bool {
	return numDelivered > c.maxDeliveries
}

// MessageOutcome is the consumer's verdict on a delivered NATS message.
type MessageOutcome int

const (
	// OutcomeAck means the message was processed successfully and should be
	// acknowledged so JetStream advances the consumer position.
	OutcomeAck MessageOutcome = iota
	// OutcomeNak means processing failed and the message should be re-queued
	// for redelivery. Used for transient errors (unmarshal, apply) that may
	// succeed on a subsequent attempt.
	OutcomeNak
	// OutcomeTerm means the message has exceeded its delivery budget and
	// should be terminated. Combined with PublishToDLQ=true the consumer
	// also writes the original payload to the DLQ stream for inspection.
	OutcomeTerm
)

// MessageDecision pairs an outcome with the optional DLQ flag and a reason
// string for telemetry. Reason is empty for OutcomeAck; for Nak/Term it
// surfaces the underlying error or termination cause.
type MessageDecision struct {
	Outcome      MessageOutcome
	PublishToDLQ bool
	Reason       string
}

// decideOutcome is the pure decision logic behind handleMessage. Given the
// delivery count and any unmarshal/apply errors, it returns the verdict the
// caller should act on. Termination wins over Nak so a stuck batch (apply
// keeps failing) eventually moves to the DLQ instead of looping forever.
// haveMetadata=false means JetStream metadata was unavailable; in that mode
// numDelivered is ignored and the function falls back to the error-driven
// branches (Nak on any error, Ack otherwise).
func (c *Consumer) decideOutcome(numDelivered uint64, haveMetadata bool, unmarshalErr, applyErr error) MessageDecision {
	if haveMetadata && c.shouldTerminate(numDelivered) {
		return MessageDecision{
			Outcome:      OutcomeTerm,
			PublishToDLQ: true,
			Reason:       fmt.Sprintf("exceeded max deliveries (%d/%d)", numDelivered, c.maxDeliveries),
		}
	}
	if unmarshalErr != nil {
		return MessageDecision{Outcome: OutcomeNak, Reason: "unmarshal error: " + unmarshalErr.Error()}
	}
	if applyErr != nil {
		return MessageDecision{Outcome: OutcomeNak, Reason: "apply error: " + applyErr.Error()}
	}
	return MessageDecision{Outcome: OutcomeAck}
}

// SetOnChange sets a callback for change events.
func (c *Consumer) SetOnChange(fn func(ChangeEvent)) {
	c.onChange = fn
}

// Start begins consuming edits from NATS.
func (c *Consumer) Start(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return fmt.Errorf("consumer already running")
	}

	sub, err := c.js.Subscribe(
		SubjectPrefix+".>",
		c.handleMessage,
		nats.Durable("funnel-consumer"),
		nats.ManualAck(),
		nats.AckWait(30*time.Second),
	)
	if err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	c.sub = sub
	c.running = true
	return nil
}

// Stop stops the consumer.
func (c *Consumer) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	if c.sub != nil {
		if err := c.sub.Unsubscribe(); err != nil {
			return err
		}
	}
	c.running = false
	return nil
}

// LastOffset returns the last processed NATS sequence number.
func (c *Consumer) LastOffset() uint64 {
	return c.lastOffset.Load()
}

func (c *Consumer) handleMessage(msg *nats.Msg) {
	meta, metaErr := msg.Metadata()
	haveMeta := metaErr == nil
	var numDelivered, streamSeq, consumerSeq uint64
	if haveMeta {
		numDelivered = meta.NumDelivered
		streamSeq = meta.Sequence.Stream
		consumerSeq = meta.Sequence.Consumer
	}

	// OSV2-306: extract trace context the publisher injected so the
	// consume-side work shows up as a child of the publish span on the
	// HTTP request's trace. Missing or malformed traceparent headers fall
	// back to a fresh root span, which is acceptable for ingest-from-cli
	// paths that never had a parent in the first place.
	traceCtx := ExtractTraceContext(context.Background(), msg.Header)
	traceCtx, span := otel.Tracer(tracerName).Start(traceCtx, consumeSpanName,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("funnel.subject", msg.Subject),
			attribute.Int64("funnel.stream_seq", int64(streamSeq)),
			attribute.Int64("funnel.num_delivered", int64(numDelivered)),
		),
	)
	defer span.End()

	// Skip the work path entirely when the message is already over the
	// delivery cap — we want to DLQ the original payload, not a partially
	// decoded batch. decideOutcome with nil errors returns Term in that case.
	overCap := haveMeta && c.shouldTerminate(numDelivered)

	var batch EditBatch
	var unmarshalErr, applyErr error
	if !overCap {
		unmarshalErr = json.Unmarshal(msg.Data, &batch)
		if unmarshalErr == nil {
			applyErr = c.applyBatchWithHistory(traceCtx, batch)
		}
	}

	decision := c.decideOutcome(numDelivered, haveMeta, unmarshalErr, applyErr)

	switch decision.Outcome {
	case OutcomeTerm:
		log.Printf("funnel: terminating batch — %s", decision.Reason)
		if decision.PublishToDLQ {
			if err := c.publishToDLQ(msg.Subject, msg.Data, numDelivered, streamSeq, consumerSeq); err != nil {
				log.Printf("funnel: DLQ publish error: %v", err)
			}
		}
		if err := msg.Term(); err != nil {
			log.Printf("funnel: term error: %v", err)
		}
		return
	case OutcomeNak:
		log.Printf("funnel: %s", decision.Reason)
		if err := msg.Nak(); err != nil {
			log.Printf("funnel: nak error: %v", err)
		}
		return
	}

	// OutcomeAck: stamp the offset, fan-out change events, then ack.
	if haveMeta {
		c.lastOffset.Store(streamSeq)
		if c.onChange != nil {
			for _, edit := range batch.Edits {
				c.onChange(ChangeEvent{
					ObjectType:      edit.ObjectType,
					PrimaryKey:      edit.PrimaryKey,
					EditType:        edit.Type,
					Offset:          streamSeq,
					Properties:      edit.Properties,
					ActorID:         batch.UserID,
					OntologyAPIName: batch.OntologyAPIName,
				})
			}
		}
	}
	if err := msg.Ack(); err != nil {
		log.Printf("funnel: ack error: %v", err)
	}
}

func (c *Consumer) applyEdit(ontologyAPIName string, edit Edit) error {
	switch edit.Type {
	case EditTypeCreate, EditTypeModify:
		scopedKey := index.ScopedKey(ontologyAPIName, edit.ObjectType)
		doc := buildIndexDoc(edit)
		return c.indexMgr.IndexDocument(scopedKey, edit.PrimaryKey, doc)
	case EditTypeDelete:
		scopedKey := index.ScopedKey(ontologyAPIName, edit.ObjectType)
		return c.indexMgr.DeleteDocument(scopedKey, edit.PrimaryKey)
	case EditTypeLinkCreate:
		return c.applyLinkCreate(ontologyAPIName, edit)
	case EditTypeLinkDelete:
		return c.applyLinkDelete(edit)
	default:
		return fmt.Errorf("unknown edit type: %q", edit.Type)
	}
}

// applyLinkCreate writes a M2M link edge via the configured LinkEdgeWriter
// and, when a LinkPropagationResolver is wired, propagates the source
// object's markings onto the target (US-261). Propagation failures are
// logged but never fail the edge upsert — the link is the source of truth
// for graph traversal, marking inheritance is best-effort enrichment.
func (c *Consumer) applyLinkCreate(ontologyAPIName string, edit Edit) error {
	if c.linkEdgeWriter == nil {
		log.Printf("funnel: LINK_CREATE skipped (no link edge writer): %s %s→%s",
			edit.LinkTypeRID, edit.PrimaryKey, edit.TargetPrimaryKey)
		return nil
	}
	edge := &oms.LinkEdge{
		LinkTypeRID:    edit.LinkTypeRID,
		SourceObjectPK: edit.PrimaryKey,
		TargetObjectPK: edit.TargetPrimaryKey,
	}
	if err := c.linkEdgeWriter.UpsertLinkEdge(context.Background(), edge); err != nil {
		return err
	}
	if err := c.propagateMarkings(ontologyAPIName, edit); err != nil {
		log.Printf("funnel: marking propagation failed for %s %s→%s: %v",
			edit.LinkTypeRID, edit.PrimaryKey, edit.TargetPrimaryKey, err)
	}
	return nil
}

// propagateMarkings is the US-261/US-474 hook that copies the source
// object's `_markings` onto the target after a successful LINK_CREATE
// upsert when the LinkType opts in via PropagateMarkings=true, then
// (when a LinkPropagationTraverser is wired) walks the link graph
// forward via BFS so transitively-linked downstream nodes inherit the
// same markings in one consumer pass. Returns nil for every "soft" skip
// (resolver not wired, LinkType not found, propagation disabled, source
// has no markings, target not yet indexed) so the caller only logs
// genuine errors. Bleve fetch + index writes are scoped to the
// per-objectType indexes that the resolver / traverser return.
func (c *Consumer) propagateMarkings(ontologyAPIName string, edit Edit) error {
	if c.linkPropagation == nil || edit.LinkTypeRID == "" {
		return nil
	}
	ctx := context.Background()
	info, found, err := c.linkPropagation.LookupLinkPropagation(ctx, edit.LinkTypeRID)
	if err != nil {
		return err
	}
	if !found || !info.PropagateMarkings {
		return nil
	}
	if info.SourceObjectTypeAPIName == "" || info.TargetObjectTypeAPIName == "" {
		return nil
	}
	srcDoc := c.fetchDocument(ontologyAPIName, info.SourceObjectTypeAPIName, edit.PrimaryKey)
	sourceMarkings := decodeMarkings(srcDoc)
	if len(sourceMarkings) == 0 {
		return nil
	}
	return c.bfsPropagateMarkings(ctx, ontologyAPIName, info.TargetObjectTypeAPIName, edit.TargetPrimaryKey, sourceMarkings)
}

// bfsPropagateMarkings walks the link graph forward starting from
// (rootObjectType, rootPK), merging `incoming` into each visited node's
// `_markings`. The walk stops on a per-node basis when the merge is a
// no-op (the node already covers the incoming set) so cycles, fan-out
// re-converging diamonds, and "propagate=false" truncation all terminate
// naturally. Visited bookkeeping is keyed on (objectType, pk) so a node
// reachable via multiple paths is re-indexed at most once.
//
// When linkPropagationTraverser is nil, only the root is touched —
// equivalent to the pre-US-474 one-hop behaviour. The first merge
// failure short-circuits the rest of the walk so callers see the same
// error semantics as a one-hop propagation failure.
func (c *Consumer) bfsPropagateMarkings(
	ctx context.Context,
	ontologyAPIName, rootObjectType, rootPK string,
	incoming []string,
) error {
	type frontierEntry struct {
		objectType string
		pk         string
		incoming   []string
	}
	visited := map[string]bool{rootObjectType + "\x00" + rootPK: true}
	queue := []frontierEntry{{rootObjectType, rootPK, incoming}}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		doc := c.fetchDocument(ontologyAPIName, cur.objectType, cur.pk)
		if doc == nil {
			// Target not indexed yet — skip. US-474 BFS does not retry; the
			// downstream link's own LINK_CREATE will re-trigger propagation
			// once the target is indexed.
			continue
		}
		existing := decodeMarkings(doc)
		merged := mergeMarkings(existing, cur.incoming)
		if equalStringSet(existing, merged) {
			// No delta on this node — anything reachable via this node has
			// already been covered by an earlier walk (or is irrelevant
			// because we'd send the same merged set). Stop expanding here.
			continue
		}
		newDoc := make(map[string]interface{}, len(doc))
		for k, v := range doc {
			if k == markingsField {
				continue
			}
			newDoc[k] = v
		}
		newDoc[markingsField] = merged
		if err := c.indexMgr.IndexDocument(
			index.ScopedKey(ontologyAPIName, cur.objectType), cur.pk, newDoc); err != nil {
			return err
		}

		if c.linkPropagationTraverser == nil {
			continue
		}
		edges, err := c.linkPropagationTraverser.ListPropagatingOutgoingEdges(ctx, cur.objectType, []string{cur.pk})
		if err != nil {
			// Surface traverser errors so applyLinkCreate logs them. The
			// edge upsert has already succeeded; partial propagation is
			// acceptable because the next LINK_CREATE on any downstream
			// hop will re-trigger the BFS over the affected branch.
			return err
		}
		for _, e := range edges {
			if e.TargetObjectTypeAPIName == "" || e.TargetPK == "" {
				continue
			}
			key := e.TargetObjectTypeAPIName + "\x00" + e.TargetPK
			if visited[key] {
				continue
			}
			visited[key] = true
			queue = append(queue, frontierEntry{
				objectType: e.TargetObjectTypeAPIName,
				pk:         e.TargetPK,
				// Downstream nodes see the merged set: the markings that
				// propagated into the current node now flow to every
				// outgoing propagating neighbour.
				incoming: merged,
			})
		}
	}
	return nil
}

// decodeMarkings extracts a deduplicated, sorted slice of marking names
// from a Bleve-returned document. Bleve hands array fields back as either
// `[]interface{}` or a single string when the array has length 1, so the
// helper handles both shapes; an absent or non-string-valued key returns
// nil (treated as "no markings" by callers).
func decodeMarkings(doc map[string]interface{}) []string {
	if doc == nil {
		return nil
	}
	raw, ok := doc[markingsField]
	if !ok || raw == nil {
		return nil
	}
	seen := map[string]struct{}{}
	add := func(s string) {
		if s == "" {
			return
		}
		seen[s] = struct{}{}
	}
	switch v := raw.(type) {
	case string:
		add(v)
	case []string:
		for _, s := range v {
			add(s)
		}
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				add(s)
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// mergeMarkings returns the deduplicated, sorted union of existing and
// incoming marking names. Sorting keeps the on-disk shape stable so a
// no-op re-index (existing already covers incoming) is detectable via
// equalStringSet without re-sorting.
func mergeMarkings(existing, incoming []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(incoming))
	for _, s := range existing {
		if s != "" {
			seen[s] = struct{}{}
		}
	}
	for _, s := range incoming {
		if s != "" {
			seen[s] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func equalStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// applyLinkDelete removes a M2M link edge via the configured LinkEdgeDeleter.
func (c *Consumer) applyLinkDelete(edit Edit) error {
	if c.linkEdgeDeleter == nil {
		log.Printf("funnel: LINK_DELETE skipped (no link edge deleter): %s %s→%s",
			edit.LinkTypeRID, edit.PrimaryKey, edit.TargetPrimaryKey)
		return nil
	}
	return c.linkEdgeDeleter.DeleteLinkEdge(context.Background(), edit.LinkTypeRID, edit.PrimaryKey, edit.TargetPrimaryKey)
}

// buildIndexDoc copies edit.Properties into a fresh map and merges Markings
// under the reserved markingsField key so the policy engine's auto-marking
// clause can AND-combine a TermQuery against the same field. An absent or
// empty marking slice leaves the key unset so "public object" docs stay
// distinguishable from "denied all" docs at query time.
func buildIndexDoc(edit Edit) map[string]interface{} {
	doc := make(map[string]interface{}, len(edit.Properties)+1)
	for k, v := range edit.Properties {
		doc[k] = v
	}
	if len(edit.Markings) > 0 {
		markings := make([]string, len(edit.Markings))
		copy(markings, edit.Markings)
		doc[markingsField] = markings
	}
	return doc
}

// applyBatchEdits groups edits by object type (preserving per-type order) and
// commits each group as a single atomic bleve batch via the index manager.
// An empty edits slice is a no-op. If any per-index commit fails the method
// returns an error; the caller is responsible for Nak-ing the upstream message.
//
// Atomicity is guaranteed per index only. For a batch spanning N object types
// the commit may partially succeed across indexes on the first delivery; in
// that case the caller Naks and the redelivered message re-applies every
// group. CREATE/MODIFY replay is idempotent via bleve upsert; DELETE+CREATE
// across types during redelivery is at-least-once and documented in the
// design report.
func (c *Consumer) applyBatchEdits(ontologyAPIName string, edits []Edit) error {
	if len(edits) == 0 {
		return nil
	}
	if ontologyAPIName == "" {
		return fmt.Errorf("apply batch: ontologyApiName is empty")
	}

	// Separate link edits from object edits — link edits go to the edge
	// writer/deleter, not to Bleve.
	var objectEdits []Edit
	var linkEdits []Edit
	for _, edit := range edits {
		if edit.Type == EditTypeLinkCreate || edit.Type == EditTypeLinkDelete {
			linkEdits = append(linkEdits, edit)
		} else {
			objectEdits = append(objectEdits, edit)
		}
	}

	// Apply object edits to Bleve in batches grouped by object type.
	if len(objectEdits) > 0 {
		type group struct {
			objectType string
			scopedKey  string
			ops        []index.BatchOp
		}
		groupIdx := make(map[string]int)
		var groups []group

		for _, edit := range objectEdits {
			op, err := toBatchOp(edit)
			if err != nil {
				return err
			}
			if i, ok := groupIdx[edit.ObjectType]; ok {
				groups[i].ops = append(groups[i].ops, op)
				continue
			}
			groupIdx[edit.ObjectType] = len(groups)
			groups = append(groups, group{
				objectType: edit.ObjectType,
				scopedKey:  index.ScopedKey(ontologyAPIName, edit.ObjectType),
				ops:        []index.BatchOp{op},
			})
		}

		for _, g := range groups {
			if err := c.indexMgr.ApplyBatch(g.scopedKey, g.ops); err != nil {
				return fmt.Errorf("apply batch for %q: %w", g.scopedKey, err)
			}
		}
	}

	// Apply link edits via the edge writer/deleter.
	for _, edit := range linkEdits {
		switch edit.Type {
		case EditTypeLinkCreate:
			if err := c.applyLinkCreate(ontologyAPIName, edit); err != nil {
				return fmt.Errorf("apply link create: %w", err)
			}
		case EditTypeLinkDelete:
			if err := c.applyLinkDelete(edit); err != nil {
				return fmt.Errorf("apply link delete: %w", err)
			}
		}
	}

	return nil
}

// toBatchOp converts a funnel.Edit into an index.BatchOp, copying the
// properties map so downstream mutations cannot race with bleve. US-051
// merges edit.Markings into the doc under markingsField so the index write
// and the policy engine's auto-marking clause agree on the field name.
func toBatchOp(edit Edit) (index.BatchOp, error) {
	switch edit.Type {
	case EditTypeCreate, EditTypeModify:
		return index.BatchOp{
			Type:       index.BatchOpIndex,
			PrimaryKey: edit.PrimaryKey,
			Document:   buildIndexDoc(edit),
		}, nil
	case EditTypeDelete:
		return index.BatchOp{
			Type:       index.BatchOpDelete,
			PrimaryKey: edit.PrimaryKey,
		}, nil
	default:
		return index.BatchOp{}, fmt.Errorf("unknown edit type: %q", edit.Type)
	}
}

// resolveConflicts applies the US-021 user-edit-wins filter to ingest edits.
// For every Source == EditSourceIngest edit whose target PK has a user edit
// in object_history newer than batch.Timestamp, the filter either drops the
// edit (DELETE, or CREATE/MODIFY with no always-apply fields) or rewrites
// its Properties to the merge of the current bleve document with the subset
// of ingest fields permitted by alwaysApplyField. Non-ingest edits and
// edits whose target has no newer user state pass through unchanged.
//
// A nil historyRepo degrades this to a pass-through so legacy callers and
// in-memory tests keep working. Lookup errors fail open (the edit is passed
// through) so a transient PG hiccup never silently swallows ingest data.
func (c *Consumer) resolveConflicts(ctx context.Context, batch EditBatch) []Edit {
	if c.historyRepo == nil {
		return batch.Edits
	}
	out := make([]Edit, 0, len(batch.Edits))
	for _, e := range batch.Edits {
		if e.Source != EditSourceIngest {
			out = append(out, e)
			continue
		}
		otRID := c.objectTypeRIDs[e.ObjectType]
		if otRID == "" {
			otRID = e.ObjectType
		}
		latest, hasUser, err := c.historyRepo.LatestUserEditAt(ctx, otRID, e.PrimaryKey)
		if err != nil {
			log.Printf("funnel: conflict lookup error for %s/%s: %v", e.ObjectType, e.PrimaryKey, err)
			out = append(out, e)
			continue
		}
		if !hasUser || !latest.After(batch.Timestamp) {
			out = append(out, e)
			continue
		}

		if e.Type == EditTypeDelete {
			log.Printf("funnel: US-021 skip stale ingest DELETE for %s/%s", e.ObjectType, e.PrimaryKey)
			continue
		}

		// Filter Properties down to the always-apply set.
		filtered := make(map[string]interface{})
		for k, v := range e.Properties {
			if c.alwaysApplyField != nil && c.alwaysApplyField(e.ObjectType, k) {
				filtered[k] = v
			}
		}
		if len(filtered) == 0 {
			log.Printf("funnel: US-021 skip stale ingest %s for %s/%s", e.Type, e.ObjectType, e.PrimaryKey)
			continue
		}

		// Merge filtered fields on top of the current bleve document so the
		// upsert preserves the user-protected fields. fetchDocument returns
		// nil when the target is missing, in which case the merge is just
		// the filtered props.
		merged := map[string]interface{}{}
		if current := c.fetchDocument(batch.OntologyAPIName, e.ObjectType, e.PrimaryKey); current != nil {
			for k, v := range current {
				merged[k] = v
			}
		}
		for k, v := range filtered {
			merged[k] = v
		}

		rewritten := e
		rewritten.Properties = merged
		out = append(out, rewritten)
	}
	return out
}

// filterWritableProperties implements the US-076 write-level column policy.
// For every ingest CREATE/MODIFY edit, any (objectType, field) for which
// writablePropertyFilter returns false is stripped from the incoming
// Properties and the current bleve doc's value for that field is merged
// back in. This guarantees ingest edits cannot overwrite policy-protected
// fields even when the batch timestamp is newer than the latest user edit.
//
// A nil writablePropertyFilter hook degrades this to a pass-through.
// Non-ingest edits pass through unchanged.
func (c *Consumer) filterWritableProperties(batch EditBatch) []Edit {
	if c.writablePropertyFilter == nil {
		return batch.Edits
	}
	out := make([]Edit, 0, len(batch.Edits))
	for _, e := range batch.Edits {
		if e.Source != EditSourceIngest || e.Type == EditTypeDelete {
			out = append(out, e)
			continue
		}

		hasStripped := false
		filtered := make(map[string]interface{}, len(e.Properties))
		for k, v := range e.Properties {
			if c.writablePropertyFilter(e.ObjectType, k) {
				filtered[k] = v
			} else {
				hasStripped = true
			}
		}

		if !hasStripped {
			out = append(out, e)
			continue
		}

		// Merge stripped fields from the current bleve doc so the upsert
		// preserves prior values for policy-protected fields.
		if current := c.fetchDocument(batch.OntologyAPIName, e.ObjectType, e.PrimaryKey); current != nil {
			for k, v := range current {
				if _, inFiltered := filtered[k]; !inFiltered {
					filtered[k] = v
				}
			}
		}

		if len(filtered) == 0 {
			log.Printf("funnel: US-076 drop empty ingest %s for %s/%s after writable filter",
				e.Type, e.ObjectType, e.PrimaryKey)
			continue
		}

		rewritten := e
		rewritten.Properties = filtered
		out = append(out, rewritten)
	}
	return out
}

// preserveEditOnlyFields implements the US-027 always-preserve semantics.
// For every ingest CREATE/MODIFY edit, any (objectType, field) flagged by
// editOnlyField is stripped from the incoming Properties and the current
// bleve doc's value for that field is merged back in. This guarantees a
// downstream bleve upsert cannot overwrite the user-managed field even
// when the ingest batch timestamp is newer than the latest user edit
// (which would otherwise let the US-021 guard fall through).
//
// Empty edits — where stripping leaves no ingest-controlled Properties and
// the current doc holds no editOnly values either — are dropped entirely
// rather than committed as a clobbering empty upsert. DELETE edits bypass
// the guard because DELETE is all-or-nothing and must be handled by the
// higher-level US-021 timestamp protection.
//
// A nil editOnlyField hook degrades this to a pass-through. Non-ingest
// edits and edits whose ObjectType has no editOnly fields are returned
// unchanged.
func (c *Consumer) preserveEditOnlyFields(batch EditBatch) []Edit {
	if c.editOnlyField == nil {
		return batch.Edits
	}
	out := make([]Edit, 0, len(batch.Edits))
	for _, e := range batch.Edits {
		if e.Source != EditSourceIngest || e.Type == EditTypeDelete {
			out = append(out, e)
			continue
		}

		hasEditOnlyKey := false
		stripped := make(map[string]interface{}, len(e.Properties))
		for k, v := range e.Properties {
			if c.editOnlyField(e.ObjectType, k) {
				hasEditOnlyKey = true
				continue
			}
			stripped[k] = v
		}

		current := c.fetchDocument(batch.OntologyAPIName, e.ObjectType, e.PrimaryKey)
		mergedEditOnlyCount := 0
		if current != nil {
			for k, v := range current {
				if c.editOnlyField(e.ObjectType, k) {
					stripped[k] = v
					mergedEditOnlyCount++
				}
			}
		}

		if !hasEditOnlyKey && mergedEditOnlyCount == 0 {
			out = append(out, e)
			continue
		}

		if len(stripped) == 0 {
			log.Printf("funnel: US-027 drop empty ingest %s for %s/%s after editOnly strip",
				e.Type, e.ObjectType, e.PrimaryKey)
			continue
		}

		rewritten := e
		rewritten.Properties = stripped
		out = append(out, rewritten)
	}
	return out
}

// ApplyBatch is the exported entry point for in-process callers that need
// to apply an EditBatch without routing through NATS — integration tests
// and future ingest shims use it to drive the full applyBatchWithHistory
// path (conflict resolution + history recording) synchronously. Returns
// the same errors as the NATS message path: an unmarshal-equivalent error
// aborts the whole batch and the caller is expected to retry or surface
// it upstream.
func (c *Consumer) ApplyBatch(ctx context.Context, batch EditBatch) error {
	return c.applyBatchWithHistory(ctx, batch)
}

// applyBatchWithHistory captures the prior bleve state for each MODIFY/DELETE
// edit, applies the batch via applyBatchEdits, and then writes one
// ObjectHistory row per edit when a HistoryRecorder is configured. History
// failures are logged but never abort the index commit, since the index is
// the source of truth for read paths and history is best-effort audit data.
//
// The empty edits slice is a no-op (matches applyBatchEdits semantics).
//
// US-021: ingest edits (Source == EditSourceIngest) are filtered through
// resolveConflicts before any index writes so that a stale ingest batch
// cannot silently overwrite a newer user edit. Non-ingest edits pass through
// unchanged.
func (c *Consumer) applyBatchWithHistory(ctx context.Context, batch EditBatch) error {
	if len(batch.Edits) == 0 {
		return nil
	}
	if batch.OntologyAPIName == "" {
		return fmt.Errorf("apply batch: ontologyApiName is empty")
	}
	if c.idempotency.seenAndStamp(batch.ID, time.Now()) {
		log.Printf("funnel: skipping duplicate batch ID %q (idempotency cache hit)", batch.ID)
		return nil
	}

	// US-447 cost tracking: charge the entire apply path (filtering,
	// conflict resolution, index commit, materialize, history) to the
	// originating ontology. The NATS counter increments on entry so a
	// failing batch still shows up in the cost surface — operators want
	// to see "this ontology is generating noise even when nothing
	// commits" as much as success traffic. CPU charges on the way out
	// via defer so partial failures still account for the work done.
	metrics.RecordOntologyNATSMessage(batch.OntologyAPIName)
	costStart := time.Now()
	defer func() {
		metrics.RecordOntologyCPUSeconds(batch.OntologyAPIName, metrics.CostCPUOpApplyBatch, time.Since(costStart))
	}()

	batch.Edits = c.filterWritableProperties(batch)
	if len(batch.Edits) == 0 {
		return nil
	}
	batch.Edits = c.preserveEditOnlyFields(batch)
	if len(batch.Edits) == 0 {
		return nil
	}
	batch.Edits = c.resolveConflicts(ctx, batch)
	if len(batch.Edits) == 0 {
		return nil
	}
	batch.Edits = c.autoTagPIIMarkings(batch.Edits)

	// Capture prev_state for each edit BEFORE the batch is applied. CREATE
	// and link edits get a nil prev_state by definition; MODIFY/DELETE pull
	// the current document from bleve. Failures are tolerated as nil prev.
	prevStates := make([]json.RawMessage, len(batch.Edits))
	if c.historyRepo != nil {
		for i, e := range batch.Edits {
			if e.Type == EditTypeCreate || e.Type == EditTypeLinkCreate || e.Type == EditTypeLinkDelete {
				continue
			}
			doc := c.fetchDocument(batch.OntologyAPIName, e.ObjectType, e.PrimaryKey)
			if doc != nil {
				if data, err := json.Marshal(doc); err == nil {
					prevStates[i] = data
				}
			}
		}
	}

	if err := c.applyBatchEdits(batch.OntologyAPIName, batch.Edits); err != nil {
		return err
	}

	// Embedding generation is best-effort: failures here are logged but
	// must not roll back the index commit. Runs after the index is updated
	// so a failed embed cannot strand a half-applied batch.
	c.generateEmbeddings(ctx, batch)

	// US-405: best-effort materialization to durable columnar storage.
	// Same fail-soft contract as embeddings — the index is the source of
	// truth for reads, materialization is the source for cold-tier rebuilds.
	c.runMaterialize(ctx, batch)

	// US-379: record one dataset_transactions row per applied batch and
	// surface its tx_id on every subsequent object_history row. Failures
	// downgrade the back-reference but never abort the batch — the index
	// commit is the source of truth for read paths.
	txID := c.recordDatasetTransaction(ctx, batch)

	if c.historyRepo == nil {
		return nil
	}

	for i, edit := range batch.Edits {
		// Link edits have no object-level history — skip.
		if edit.Type == EditTypeLinkCreate || edit.Type == EditTypeLinkDelete {
			continue
		}

		otRID := c.objectTypeRIDs[edit.ObjectType]
		if otRID == "" {
			// Fall back to the API name when no RID mapping is configured.
			// Callers can supply richer mappings via SetObjectTypeRIDs.
			otRID = edit.ObjectType
		}

		var newState json.RawMessage
		if edit.Type != EditTypeDelete {
			doc := make(map[string]interface{}, len(edit.Properties))
			for k, v := range edit.Properties {
				doc[k] = v
			}
			if data, err := json.Marshal(doc); err == nil {
				newState = data
			}
		}

		// US-021 requires history rows to carry the source discriminator so
		// the LatestUserEditAt query can distinguish user vs. ingest writes.
		source := edit.Source
		if source == "" {
			source = oms.EditSourceUser
		}
		row := &oms.ObjectHistory{
			ObjectTypeRID: otRID,
			PrimaryKey:    edit.PrimaryKey,
			Version:       c.nextVersion(otRID, edit.PrimaryKey),
			PrevState:     prevStates[i],
			NewState:      newState,
			EditType:      string(edit.Type),
			Source:        source,
			UserID:        batch.UserID,
			RecordedAt:    batch.Timestamp,
			TxID:          txID,
		}
		if err := c.historyRepo.InsertObjectHistory(ctx, row); err != nil {
			log.Printf("funnel: history insert error for %s/%s: %v",
				edit.ObjectType, edit.PrimaryKey, err)
		}
	}
	return nil
}

// autoTagPIIMarkings is the US-263 hook that scans every CREATE/MODIFY
// edit's property values via the configured PIIDetector and appends the
// PII marking to the edit's Markings slice on a positive match. The
// step runs after every other property-rewriting filter (US-076 writable
// columns, US-027 edit-only preserve, US-021 conflict resolution) so the
// detector sees the FINAL property set the index will store. A nil
// detector or DELETE/link edits short-circuit unchanged. The marking is
// deduplicated against the edit's existing Markings so an explicit PII
// tag from the writer is never duplicated.
func (c *Consumer) autoTagPIIMarkings(edits []Edit) []Edit {
	if c.piiDetector == nil {
		return edits
	}
	for i := range edits {
		e := &edits[i]
		if e.Type != EditTypeCreate && e.Type != EditTypeModify {
			continue
		}
		if !c.piiDetector.DetectPII(e.Properties) {
			continue
		}
		if containsString(e.Markings, PIIMarkingName) {
			continue
		}
		e.Markings = append(e.Markings, PIIMarkingName)
	}
	return edits
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// fetchDocument loads the current bleve document for (ontology, objectType, pk)
// as a flat map[string]interface{}. Returns nil when the index does not exist
// or the document is missing — both are non-fatal for history capture.
func (c *Consumer) fetchDocument(ontologyAPIName, objectType, primaryKey string) map[string]interface{} {
	q := bleve.NewDocIDQuery([]string{primaryKey})
	req := bleve.NewSearchRequest(q)
	req.Fields = []string{"*"}
	req.Size = 1
	res, err := c.indexMgr.Search(index.ScopedKey(ontologyAPIName, objectType), req)
	if err != nil || res == nil || res.Total == 0 {
		return nil
	}
	hit := res.Hits[0]
	if hit == nil || len(hit.Fields) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(hit.Fields))
	for k, v := range hit.Fields {
		out[k] = v
	}
	return out
}

// recordDatasetTransaction is the US-379 hook that lands a row in
// dataset_transactions for the just-applied EditBatch. The tx_id derives
// from batch.ID (prefixed with "tx-" so the OSS asOf parser can route on
// the prefix); parent_tx_id resolves to the most-recent prior tx for the
// same ontology so /datasets/{rid}/history can walk a linear chain.
//
// Returns the stamped tx_id (or empty string when recording is disabled
// or fails), so the caller can stamp the same id onto every
// object_history row produced by the batch. Failures are logged but never
// abort the batch — the index commit is the source of truth.
func (c *Consumer) recordDatasetTransaction(ctx context.Context, batch EditBatch) string {
	if c.txRecorder == nil {
		return ""
	}
	if batch.ID == "" || batch.OntologyAPIName == "" {
		return ""
	}
	txID := batch.ID
	if !strings.HasPrefix(txID, oms.DatasetTransactionIDPrefix) {
		txID = oms.DatasetTransactionIDPrefix + txID
	}

	var parentID string
	if prior, err := c.txRecorder.LatestForOntology(ctx, batch.OntologyAPIName); err != nil {
		log.Printf("funnel: tx parent lookup error for %s: %v", batch.OntologyAPIName, err)
	} else if prior != nil {
		parentID = prior.TxID
	}

	committedAt := batch.Timestamp
	if committedAt.IsZero() {
		committedAt = time.Now().UTC()
	}

	row := &oms.DatasetTransaction{
		TxID:            txID,
		ParentTxID:      parentID,
		OntologyAPIName: batch.OntologyAPIName,
		CommittedAt:     committedAt,
		EditsCount:      len(batch.Edits),
		UserID:          batch.UserID,
	}
	if err := c.txRecorder.RecordDatasetTransaction(ctx, row); err != nil {
		log.Printf("funnel: tx record error for %s: %v", batch.OntologyAPIName, err)
		return ""
	}
	return txID
}

// nextVersion returns the next monotonically increasing version number for
// the given (objectTypeRID, primaryKey) pair. Versions are tracked in
// memory; on consumer restart they reset to 1, which is acceptable because
// downstream UIs sort by recorded_at.
func (c *Consumer) nextVersion(objectTypeRID, primaryKey string) int64 {
	c.versionMu.Lock()
	defer c.versionMu.Unlock()
	if c.versionCounters == nil {
		c.versionCounters = map[string]int64{}
	}
	key := objectTypeRID + "|" + primaryKey
	c.versionCounters[key]++
	return c.versionCounters[key]
}
