package subscriptions

import (
	"encoding/json"

	"github.com/google/uuid"
)

// ActionJobSubscribeRequest is the payload of a {type:"subscribeActionJob"}
// message. JobID names the action_jobs row whose progress events the caller
// wants to follow. The server stops emitting once the job reaches a terminal
// state (SUCCEEDED / FAILED / CANCELED) and the client should unsubscribe;
// however the Hub never auto-closes the subscription so a client that wants
// only deltas can keep the channel open without leaking server state. US-318.
type ActionJobSubscribeRequest struct {
	JobID string `json:"jobId"`
}

// ActionJobProgressEvent is the wire payload pushed on every progress tick.
// Mirrors actions.ProgressEvent but keeps the type free of any pkg/actions
// import so pkg/subscriptions stays at the bottom of the dependency stack.
// Status is empty for in-flight progress events and populated on terminal
// transitions (SUCCEEDED/FAILED/CANCELED) when the publisher tags one.
type ActionJobProgressEvent struct {
	JobID        string          `json:"jobId"`
	Status       string          `json:"status,omitempty"`
	Percent      int             `json:"percent"`
	Message      string          `json:"message,omitempty"`
	Ontology     string          `json:"ontologyApiName,omitempty"`
	ActionType   string          `json:"actionType,omitempty"`
	Result       json.RawMessage `json:"result,omitempty"`
	ErrorMessage string          `json:"errorMessage,omitempty"`
}

// HandleActionJobProgress fans out a progress event to every connection
// subscribed to jobID. Same overflow / rate-limit semantics as the
// HandleObjectChange dispatch path so a slow client can't back the publisher
// up. Safe to call concurrently. US-318.
func (h *Hub) HandleActionJobProgress(jobID string, evt ActionJobProgressEvent) {
	if jobID == "" {
		return
	}
	h.mu.Lock()
	entries := h.jobSubscriptionsLocked(jobID)
	h.mu.Unlock()
	if len(entries) == 0 {
		return
	}
	data, err := json.Marshal(evt)
	if err != nil {
		return
	}
	for _, e := range entries {
		conn := e.conn
		sub := e.sub
		if !conn.allowEvent() {
			conn.markOverflow(sub.ID)
			continue
		}
		msg := Message{
			Type:           "actionJobProgress",
			SubscriptionID: sub.ID,
			Data:           data,
		}
		select {
		case conn.send <- msg:
		default:
			conn.markOverflow(sub.ID)
		}
	}
}

// handleSubscribeActionJob processes a {type:"subscribeActionJob"} request,
// adding the (conn, sub) pair to the jobIndex so HandleActionJobProgress
// finds it. Quotas and limits mirror the other subscribe handlers.
func (h *Hub) handleSubscribeActionJob(c *Connection, raw json.RawMessage) Message {
	var req ActionJobSubscribeRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return Message{Type: "error", Error: "invalid subscribeActionJob request: " + err.Error()}
	}
	if req.JobID == "" {
		return Message{Type: "error", Error: "jobId is required"}
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	c.subMu.Lock()
	defer c.subMu.Unlock()

	if len(c.subscriptions) >= MaxSubscriptionsPerConnection {
		return Message{
			Type:  "error",
			Error: "maximum subscriptions per connection reached (10)",
		}
	}
	if !h.reserveUserSubLocked(c.userID) {
		return Message{
			Type:  "error",
			Error: "maximum subscriptions per user reached",
		}
	}

	sub := &Subscription{
		ID:    uuid.New().String(),
		JobID: req.JobID,
	}
	c.subscriptions[sub.ID] = sub
	h.addJobSubLocked(c, sub)

	return Message{
		Type:           "subscribed",
		SubscriptionID: sub.ID,
	}
}

// addJobSubLocked registers (conn, sub) under sub.JobID. Caller must hold h.mu.
func (h *Hub) addJobSubLocked(c *Connection, sub *Subscription) {
	if sub == nil || sub.JobID == "" {
		return
	}
	if h.jobIndex == nil {
		h.jobIndex = make(map[string][]*indexedSub)
	}
	h.jobIndex[sub.JobID] = append(h.jobIndex[sub.JobID], &indexedSub{conn: c, sub: sub})
}

// removeJobSubLocked drops every jobIndex entry pointing at the given
// subscription. No-op for non-job subscriptions. Caller must hold h.mu.
func (h *Hub) removeJobSubLocked(sub *Subscription) {
	if sub == nil || sub.JobID == "" || h.jobIndex == nil {
		return
	}
	bucket := h.jobIndex[sub.JobID]
	if len(bucket) == 0 {
		return
	}
	filtered := bucket[:0]
	for _, e := range bucket {
		if e.sub != sub {
			filtered = append(filtered, e)
		}
	}
	if len(filtered) == 0 {
		delete(h.jobIndex, sub.JobID)
	} else {
		h.jobIndex[sub.JobID] = filtered
	}
}

// jobSubscriptionsLocked snapshots the current job-index entries for jobID.
// Caller must hold h.mu. Returned slice is a copy so dispatch can iterate
// safely after the lock is released.
func (h *Hub) jobSubscriptionsLocked(jobID string) []*indexedSub {
	if h.jobIndex == nil {
		return nil
	}
	bucket := h.jobIndex[jobID]
	if len(bucket) == 0 {
		return nil
	}
	snap := make([]*indexedSub, len(bucket))
	copy(snap, bucket)
	return snap
}

// JobSubscriptionCount reports the number of active subscriptions for jobID.
// Used by tests to assert routing-index invariants without inspecting Hub
// internals directly.
func (h *Hub) JobSubscriptionCount(jobID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.jobIndex == nil {
		return 0
	}
	return len(h.jobIndex[jobID])
}
