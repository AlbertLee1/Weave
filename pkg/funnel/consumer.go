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

// Consumer subscribes to NATS and processes edit batches, updating Bleve indexes.
type Consumer struct {
	js         nats.JetStreamContext
	indexMgr   *index.Manager
	sub        *nats.Subscription
	mu         sync.Mutex
	running    bool
	lastOffset atomic.Uint64
	onChange   func(ChangeEvent) // optional callback
}

// NewConsumer creates a new edit consumer.
func NewConsumer(js nats.JetStreamContext, indexMgr *index.Manager) *Consumer {
	return &Consumer{
		js:       js,
		indexMgr: indexMgr,
	}
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
	var batch EditBatch
	if err := json.Unmarshal(msg.Data, &batch); err != nil {
		log.Printf("funnel: unmarshal error: %v", err)
		if err := msg.Nak(); err != nil {
			log.Printf("funnel: nak error: %v", err)
		}
		return
	}

	for _, edit := range batch.Edits {
		if err := c.applyEdit(edit); err != nil {
			log.Printf("funnel: apply edit error: %v", err)
			if err := msg.Nak(); err != nil {
				log.Printf("funnel: nak error: %v", err)
			}
			return
		}
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
