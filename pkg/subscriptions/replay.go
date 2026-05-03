package subscriptions

// US-380: replay support. After a reconnecting client establishes a fresh
// subscription on the new connection, the Hub replays buffered events that
// occurred AFTER the cursor supplied via ?since=<cursor> on the upgrade URL.
// Replay runs once per subscription; the cursor is consumed (set to -1) to
// avoid double-delivery if the client adds multiple subscriptions of the
// same kind.
//
// Replay is best-effort within the buffer's bounds. The connection-level
// out-of-window check in HandleWSWithUser already gated the cursor against
// the live window, so by the time we reach this function the cursor is
// known to be within range OR the welcome path already fired onOutOfDate
// and zeroed replayCursor (-1 sentinel).

// replayObjectSubscription drains the replay buffer for events that match
// the freshly-registered object-change subscription and emits them to the
// connection's send channel as objectChanged messages tagged with their
// original cursor. Caller must NOT hold any hub locks.
func (h *Hub) replayObjectSubscription(c *Connection, sub *Subscription) {
	if c == nil || sub == nil {
		return
	}
	since := c.replayCursor
	if since <= 0 {
		return
	}
	// Take the snapshot AFTER the subscription has joined the routing
	// index so any event published while we drain is delivered live; the
	// snapshot's id range is already bounded above by the latest log id at
	// snapshot time, so live events with id > snapshot.maxID are
	// non-overlapping.
	snap, _ := h.eventLog.Snapshot(since)
	for _, entry := range snap {
		if entry.Kind != "objectChange" {
			continue
		}
		if !sub.matches(entry.ObjectType, entry.PrimaryKey, entry.Properties) {
			continue
		}
		data, err := entry.MarshalObjectChange(sub)
		if err != nil {
			continue
		}
		msg := Message{
			Type:           "objectChanged",
			SubscriptionID: sub.ID,
			Cursor:         entry.ID,
			Data:           data,
		}
		select {
		case c.send <- msg:
		default:
			c.markOverflow(sub.ID)
			return
		}
	}
}

// replayActionJobSubscription drains the replay buffer for actionJobProgress
// events matching the freshly-registered subscription and emits them as
// actionJobProgress messages tagged with the original cursor.
func (h *Hub) replayActionJobSubscription(c *Connection, sub *Subscription) {
	if c == nil || sub == nil || sub.JobID == "" {
		return
	}
	since := c.replayCursor
	if since <= 0 {
		return
	}
	snap, _ := h.eventLog.Snapshot(since)
	for _, entry := range snap {
		if entry.Kind != "actionJobProgress" || entry.JobID != sub.JobID {
			continue
		}
		data, err := entry.MarshalActionJobProgress()
		if err != nil {
			continue
		}
		msg := Message{
			Type:           "actionJobProgress",
			SubscriptionID: sub.ID,
			Cursor:         entry.ID,
			Data:           data,
		}
		select {
		case c.send <- msg:
		default:
			c.markOverflow(sub.ID)
			return
		}
	}
}
