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
)

// HistoryRecorder is the minimal subset of oms.Repository required by the
// consumer to write ObjectHistory rows. Defined as an interface so the
// consumer never imports the full PG repo (and tests can supply a fake).
type HistoryRecorder interface {
	InsertObjectHistory(ctx context.Context, h *oms.ObjectHistory) error
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

	// historyRepo writes a row per applied edit when set. Tier 2.3 wires
	// this to the OMS PG repo. Nil = history disabled.
	historyRepo HistoryRecorder
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

// SetHistoryRepo enables ObjectHistory recording for every applied edit.
// Pass nil to disable. Safe to call before Start().
func (c *Consumer) SetHistoryRepo(repo HistoryRecorder) {
	c.historyRepo = repo
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

func (c *Consumer) applyEdit(edit Edit) error {
	switch edit.Type {
	case EditTypeCreate, EditTypeModify:
		doc := make(map[string]interface{})
		for k, v := range edit.Properties {
			doc[k] = v
		}
		return c.indexMgr.IndexDocument(edit.ObjectType, edit.PrimaryKey, doc)
	case EditTypeDelete:
		return c.indexMgr.DeleteDocument(edit.ObjectType, edit.PrimaryKey)
	default:
		return fmt.Errorf("unknown edit type: %q", edit.Type)
	}
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
func (c *Consumer) applyBatchEdits(edits []Edit) error {
	if len(edits) == 0 {
		return nil
	}

	// Preserve insertion order of object types so single-type batches keep
	// their historical per-type ordering for downstream consumers.
	type group struct {
		objectType string
		ops        []index.BatchOp
	}
	groupIdx := make(map[string]int)
	var groups []group

	for _, edit := range edits {
		op, err := toBatchOp(edit)
		if err != nil {
			return err
		}
		if i, ok := groupIdx[edit.ObjectType]; ok {
			groups[i].ops = append(groups[i].ops, op)
			continue
		}
		groupIdx[edit.ObjectType] = len(groups)
		groups = append(groups, group{objectType: edit.ObjectType, ops: []index.BatchOp{op}})
	}

	for _, g := range groups {
		if err := c.indexMgr.ApplyBatch(g.objectType, g.ops); err != nil {
			return fmt.Errorf("apply batch for %q: %w", g.objectType, err)
		}
	}
	return nil
}

// toBatchOp converts a funnel.Edit into an index.BatchOp, copying the
// properties map so downstream mutations cannot race with bleve.
func toBatchOp(edit Edit) (index.BatchOp, error) {
	switch edit.Type {
	case EditTypeCreate, EditTypeModify:
		doc := make(map[string]interface{}, len(edit.Properties))
		for k, v := range edit.Properties {
			doc[k] = v
		}
		return index.BatchOp{
			Type:       index.BatchOpIndex,
			PrimaryKey: edit.PrimaryKey,
			Document:   doc,
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

// applyBatchWithHistory captures the prior bleve state for each MODIFY/DELETE
// edit, applies the batch via applyBatchEdits, and then writes one
// ObjectHistory row per edit when a HistoryRecorder is configured. History
// failures are logged but never abort the index commit, since the index is
// the source of truth for read paths and history is best-effort audit data.
//
// The empty edits slice is a no-op (matches applyBatchEdits semantics).
func (c *Consumer) applyBatchWithHistory(ctx context.Context, batch EditBatch) error {
	if len(batch.Edits) == 0 {
		return nil
	}

	// Capture prev_state for each edit BEFORE the batch is applied. CREATE
	// edits get a nil prev_state by definition; MODIFY/DELETE pull the
	// current document from bleve. Failures are tolerated as nil prev.
	prevStates := make([]json.RawMessage, len(batch.Edits))
	if c.historyRepo != nil {
		for i, e := range batch.Edits {
			if e.Type == EditTypeCreate {
				continue
			}
			doc := c.fetchDocument(e.ObjectType, e.PrimaryKey)
			if doc != nil {
				if data, err := json.Marshal(doc); err == nil {
					prevStates[i] = data
				}
			}
		}
	}

	if err := c.applyBatchEdits(batch.Edits); err != nil {
		return err
	}

	if c.historyRepo == nil {
		return nil
	}

	for i, edit := range batch.Edits {
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

		row := &oms.ObjectHistory{
			ObjectTypeRID: otRID,
			PrimaryKey:    edit.PrimaryKey,
			Version:       c.nextVersion(otRID, edit.PrimaryKey),
			PrevState:     prevStates[i],
			NewState:      newState,
			EditType:      string(edit.Type),
			UserID:        batch.UserID,
		}
		if err := c.historyRepo.InsertObjectHistory(ctx, row); err != nil {
			log.Printf("funnel: history insert error for %s/%s: %v",
				edit.ObjectType, edit.PrimaryKey, err)
		}
	}
	return nil
}

// fetchDocument loads the current bleve document for (objectType, pk) as a
// flat map[string]interface{}. Returns nil when the index does not exist or
// the document is missing — both are non-fatal for history capture.
func (c *Consumer) fetchDocument(objectType, primaryKey string) map[string]interface{} {
	q := bleve.NewDocIDQuery([]string{primaryKey})
	req := bleve.NewSearchRequest(q)
	req.Fields = []string{"*"}
	req.Size = 1
	res, err := c.indexMgr.Search(objectType, req)
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
