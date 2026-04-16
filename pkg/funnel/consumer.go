package funnel

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/nats-io/nats.go"

	"github.com/liyang/weave/pkg/index"
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

	// embedFields holds the optional embedding side-channel state. See
	// embeddings.go for the wiring methods and the per-batch hook.
	embedFields
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

// SetHistoryRepo enables ObjectHistory recording for every applied edit.
// Pass nil to disable. Safe to call before Start().
func (c *Consumer) SetHistoryRepo(repo HistoryRecorder) {
	c.historyRepo = repo
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
	// Check delivery count for dead-letter logic
	meta, metaErr := msg.Metadata()
	if metaErr == nil && c.shouldTerminate(meta.NumDelivered) {
		log.Printf("funnel: message exceeded max deliveries (%d/%d), terminating batch",
			meta.NumDelivered, c.maxDeliveries)
		// Publish to DLQ before terminating so the message is preserved for
		// operator inspection and potential replay.
		if err := c.publishToDLQ(msg.Subject, msg.Data, meta.NumDelivered, meta.Sequence.Stream, meta.Sequence.Consumer); err != nil {
			log.Printf("funnel: DLQ publish error: %v", err)
		}
		if err := msg.Term(); err != nil {
			log.Printf("funnel: term error: %v", err)
		}
		return
	}

	var batch EditBatch
	if err := json.Unmarshal(msg.Data, &batch); err != nil {
		log.Printf("funnel: unmarshal error: %v", err)
		if err := msg.Nak(); err != nil {
			log.Printf("funnel: nak error: %v", err)
		}
		return
	}

	if err := c.applyBatchWithHistory(context.Background(), batch); err != nil {
		log.Printf("funnel: apply batch error: %v", err)
		if err := msg.Nak(); err != nil {
			log.Printf("funnel: nak error: %v", err)
		}
		return
	}

	// Get the sequence number from message metadata
	meta, err := msg.Metadata()
	if err == nil {
		c.lastOffset.Store(meta.Sequence.Stream)

		if c.onChange != nil {
			for _, edit := range batch.Edits {
				c.onChange(ChangeEvent{
					ObjectType: edit.ObjectType,
					PrimaryKey: edit.PrimaryKey,
					EditType:   edit.Type,
					Offset:     meta.Sequence.Stream,
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
		return c.applyLinkCreate(edit)
	case EditTypeLinkDelete:
		return c.applyLinkDelete(edit)
	default:
		return fmt.Errorf("unknown edit type: %q", edit.Type)
	}
}

// applyLinkCreate writes a M2M link edge via the configured LinkEdgeWriter.
func (c *Consumer) applyLinkCreate(edit Edit) error {
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
	return c.linkEdgeWriter.UpsertLinkEdge(context.Background(), edge)
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
			if err := c.applyLinkCreate(edit); err != nil {
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
		}
		if err := c.historyRepo.InsertObjectHistory(ctx, row); err != nil {
			log.Printf("funnel: history insert error for %s/%s: %v",
				edit.ObjectType, edit.PrimaryKey, err)
		}
	}
	return nil
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
