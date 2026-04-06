package funnel

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/liyang/weave/pkg/index"
)

const (
	// DefaultMaxDeliveries is the maximum number of delivery attempts before
	// a message is terminated (dead-lettered).
	DefaultMaxDeliveries = 5
)

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
}

// NewConsumer creates a new edit consumer.
func NewConsumer(js nats.JetStreamContext, indexMgr *index.Manager) *Consumer {
	return &Consumer{
		js:            js,
		indexMgr:      indexMgr,
		maxDeliveries: DefaultMaxDeliveries,
	}
}

// SetMaxDeliveries sets the maximum delivery attempts before a message is terminated.
func (c *Consumer) SetMaxDeliveries(n uint64) {
	c.maxDeliveries = n
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

	if err := c.applyBatchEdits(batch.Edits); err != nil {
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
