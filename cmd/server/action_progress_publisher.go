package main

import (
	"github.com/nats-io/nats.go"
)

// natsActionProgressPublisher satisfies actions.ProgressPublisher by
// publishing ephemeral progress events onto plain-NATS (NOT JetStream). US-241.
//
// Ephemeral is deliberate: progress updates have no replay semantics and
// should not land on the OBJECT_EDITS JetStream — subscribers that miss an
// update can simply GET /actions/jobs/{id} to catch up, which is why the
// action_jobs row is the source of truth and the NATS subject is the
// low-latency fanout channel.
type natsActionProgressPublisher struct {
	nc *nats.Conn
}

func newNATSActionProgressPublisher(nc *nats.Conn) *natsActionProgressPublisher {
	return &natsActionProgressPublisher{nc: nc}
}

// PublishProgress forwards data to NATS on the given subject (typically
// actions.progress.<jobId>). Returns the underlying nats.Conn error for the
// reporter to log; the async apply path never propagates this to JS.
func (p *natsActionProgressPublisher) PublishProgress(subject string, data []byte) error {
	if p == nil || p.nc == nil {
		return nil
	}
	return p.nc.Publish(subject, data)
}
