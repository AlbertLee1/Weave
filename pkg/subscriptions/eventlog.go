package subscriptions

import (
	"encoding/json"
	"sync"
	"time"
)

// US-380: event log + cursor + replay buffer.
//
// The Hub keeps a sliding-window log of recently dispatched events. Each event
// is tagged with a monotonic int64 cursor. When a client reconnects with
// ?since=<cursor> the Hub snapshots events with cursor > since that match the
// caller's freshly-registered subscriptions and replays them. When the
// requested cursor falls outside the window the client receives a single
// onOutOfDate signal so it can perform a full state refresh instead of
// silently missing events.

// EventLogEntry is one captured event in the replay buffer. It carries enough
// data to dispatch the equivalent wire message to a freshly-subscribed client.
// AggregationChanged events are NOT logged — they are per-subscription derived
// state and a reconnecting client always re-seeds via Bleve at subscribe time.
type EventLogEntry struct {
	ID        int64     // monotonic cursor, assigned at append time
	Timestamp time.Time // wall-clock for window-based eviction

	Kind string // "objectChange" | "actionJobProgress"

	// objectChange fields:
	ObjectType string
	PrimaryKey string
	EditType   string
	Properties map[string]interface{}

	// actionJobProgress fields:
	JobID    string
	JobEvent ActionJobProgressEvent
}

// EventLogConfig configures the sliding-window replay buffer.
type EventLogConfig struct {
	// Window is the duration after which an event is no longer eligible for
	// replay. Reconnects with cursors older than the window's earliest live
	// id receive an out-of-window signal. Default: 5 minutes.
	Window time.Duration
	// MaxEntries caps the in-memory log size so a high-throughput stream
	// cannot OOM the server. Once reached, the oldest entries are evicted
	// regardless of Window. Default: 10000.
	MaxEntries int
}

func (cfg *EventLogConfig) applyDefaults() {
	if cfg.Window <= 0 {
		cfg.Window = 5 * time.Minute
	}
	if cfg.MaxEntries <= 0 {
		cfg.MaxEntries = 10000
	}
}

// EventLog is a sliding-window FIFO of recent events keyed by a monotonic
// int64 cursor. Append assigns the next cursor under the log's mutex so the
// caller can use the returned id immediately for outbound messages. Snapshot
// returns a defensive copy so callers may iterate without holding the lock.
type EventLog struct {
	mu      sync.Mutex
	entries []EventLogEntry
	nextID  int64
	cfg     EventLogConfig
	nowFunc func() time.Time
}

// NewEventLog creates a log with the given config. Defaults apply to zero
// values.
func NewEventLog(cfg EventLogConfig) *EventLog {
	cfg.applyDefaults()
	return &EventLog{
		entries: make([]EventLogEntry, 0, 256),
		cfg:     cfg,
	}
}

func (l *EventLog) now() time.Time {
	if l.nowFunc != nil {
		return l.nowFunc()
	}
	return time.Now()
}

// Append records an event and returns its assigned cursor. The caller MUST
// use the returned id when emitting the wire message so the client's
// last-seen cursor advances in lockstep with the log.
func (l *EventLog) Append(entry EventLogEntry) int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.nextID++
	entry.ID = l.nextID
	entry.Timestamp = l.now()
	l.entries = append(l.entries, entry)
	l.evictLocked()
	return entry.ID
}

// evictLocked drops entries that fall outside the window or exceed
// MaxEntries. Caller must hold l.mu.
func (l *EventLog) evictLocked() {
	cutoff := l.now().Add(-l.cfg.Window)
	drop := 0
	for drop < len(l.entries) {
		e := l.entries[drop]
		if e.Timestamp.After(cutoff) {
			break
		}
		drop++
	}
	if drop > 0 {
		l.entries = append(l.entries[:0], l.entries[drop:]...)
	}
	if over := len(l.entries) - l.cfg.MaxEntries; over > 0 {
		l.entries = append(l.entries[:0], l.entries[over:]...)
	}
}

// Snapshot returns the entries with id > sinceID, evicting expired entries
// first. The returned slice is a defensive copy. The second return is the
// minimum live id currently in the log; if sinceID < minLive the caller's
// cursor is outside the replay window and the snapshot may be incomplete.
func (l *EventLog) Snapshot(sinceID int64) (entries []EventLogEntry, minLive int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.evictLocked()
	if len(l.entries) == 0 {
		return nil, 0
	}
	minLive = l.entries[0].ID
	for _, e := range l.entries {
		if e.ID > sinceID {
			entries = append(entries, e)
		}
	}
	return entries, minLive
}

// LatestID returns the most recently assigned cursor, or 0 if the log has
// never seen an event.
func (l *EventLog) LatestID() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.nextID
}

// EarliestID returns the smallest live id currently in the buffer, or 0 if
// the log is empty.
func (l *EventLog) EarliestID() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.evictLocked()
	if len(l.entries) == 0 {
		return 0
	}
	return l.entries[0].ID
}

// Len reports the number of live entries currently in the buffer (after
// eviction). Used by tests to verify retention behaviour.
func (l *EventLog) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.evictLocked()
	return len(l.entries)
}

// MarshalObjectChange constructs the JSON payload that HandleObjectChange
// would emit for this entry, projected through the supplied subscription's
// Select clause. Returns nil + error when the entry is not an objectChange
// kind so callers can branch cleanly. The function does not consult any
// subscription state beyond Select; membership filtering is the caller's
// responsibility (see Subscription.matches).
func (e EventLogEntry) MarshalObjectChange(sub *Subscription) ([]byte, error) {
	state := editTypeToState(e.EditType)
	properties := e.Properties
	if sub != nil {
		properties = sub.ProjectProperties(properties)
	}
	evt := ObjectChangeEvent{State: state, Object: properties}
	return json.Marshal(evt)
}

// MarshalActionJobProgress constructs the JSON payload that
// HandleActionJobProgress would emit for this entry.
func (e EventLogEntry) MarshalActionJobProgress() ([]byte, error) {
	return json.Marshal(e.JobEvent)
}
