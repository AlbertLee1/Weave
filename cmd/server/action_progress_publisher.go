package main

import (
	"encoding/json"
	"log"
	"strings"

	"github.com/liyang/weave/pkg/subscriptions"
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

// progressFanoutPublisher is the actions.ProgressPublisher wired in
// production: it forwards every event to (a) NATS for SDK subscribers that
// want to consume the ephemeral subject directly and (b) the in-process
// WebSocket Hub so browser clients see live progress without a separate NATS
// hop. Either inner publisher may be nil (degraded mode); both nil collapses
// to a no-op. US-318.
type progressFanoutPublisher struct {
	nats *natsActionProgressPublisher
	hub  *subscriptions.Hub
}

// newProgressFanoutPublisher composes a NATS publisher and a Hub fanout into
// a single actions.ProgressPublisher. Either side may be nil; the resulting
// publisher dispatches to the wired sides only.
func newProgressFanoutPublisher(nc *nats.Conn, hub *subscriptions.Hub) *progressFanoutPublisher {
	out := &progressFanoutPublisher{hub: hub}
	if nc != nil {
		out.nats = newNATSActionProgressPublisher(nc)
	}
	return out
}

// PublishProgress fans out to NATS and the WebSocket Hub. NATS errors are
// returned (caller logs them); a Hub dispatch failure isn't possible because
// HandleActionJobProgress drops silently on full buffers. The data shape is
// the same JSON-marshalled actions.ProgressEvent the NATS subscriber sees so
// SDKs and the WS Hub agree on the wire format.
func (p *progressFanoutPublisher) PublishProgress(subject string, data []byte) error {
	if p == nil {
		return nil
	}
	if p.hub != nil {
		jobID := jobIDFromProgressSubject(subject)
		if jobID != "" {
			var evt subscriptions.ActionJobProgressEvent
			if err := json.Unmarshal(data, &evt); err == nil {
				// Defensive: if the underlying ProgressEvent shape ever drifts
				// from ActionJobProgressEvent (extra fields), we still emit
				// the subset that decoded cleanly. JobID is repopulated from
				// the subject in case the event JSON omits it.
				if evt.JobID == "" {
					evt.JobID = jobID
				}
				p.hub.HandleActionJobProgress(jobID, evt)
			} else {
				log.Printf("actions: progress fanout: hub decode for subject %s failed: %v", subject, err)
			}
		}
	}
	if p.nats != nil {
		return p.nats.PublishProgress(subject, data)
	}
	return nil
}

// jobIDFromProgressSubject extracts the trailing jobID from
// "actions.progress.<jobId>" subjects emitted by actions.ProgressSubject.
// Returns "" for unrelated subjects so an unknown topic skips the Hub
// fanout silently.
func jobIDFromProgressSubject(subject string) string {
	const prefix = "actions.progress."
	if !strings.HasPrefix(subject, prefix) {
		return ""
	}
	return strings.TrimPrefix(subject, prefix)
}
