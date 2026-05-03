package subscriptions

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// parseSinceCursor extracts a non-negative monotonic cursor from the
// ?since=<n> query parameter. Empty / malformed / non-positive values are
// reported as 0 ("no replay requested") so the upgrade path stays liberal —
// a misbehaving client merely loses replay, not the connection itself.
func parseSinceCursor(raw string) int64 {
	if raw == "" {
		return 0
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// Message is the envelope for all WebSocket messages (both client→server and
// server→client). The Type field discriminates the payload kind; Data carries
// the type-specific JSON body. ConnectionID is populated only in the welcome
// message; SubscriptionID is populated in subscription-related messages.
//
// US-380: Cursor carries the monotonic event id from the Hub's replay log on
// every server→client event message, plus the high-water mark on the welcome
// envelope. Clients persist the most recent cursor and supply it as
// ?since=<cursor> on reconnect; the server replays missed events from the
// 5-minute sliding window or emits a connection-level onOutOfDate when the
// cursor falls outside the window.
type Message struct {
	Type           string          `json:"type"`
	ConnectionID   string          `json:"connectionId,omitempty"`
	SubscriptionID string          `json:"subscriptionId,omitempty"`
	Cursor         int64           `json:"cursor,omitempty"`
	LastEventID    int64           `json:"lastEventId,omitempty"`
	Data           json.RawMessage `json:"data,omitempty"`
	Error          string          `json:"error,omitempty"`
}

// HubConfig holds configurable parameters for the Hub.
type HubConfig struct {
	HeartbeatInterval    time.Duration // ping interval; default 30s
	HeartbeatTimeout     time.Duration // pong deadline; default 60s
	SendBufferSize       int           // per-connection outbound buffer; default 64
	AggregationScanLimit int           // initial scan size for aggregation subscriptions; default 10000

	// MaxSubscriptionsPerUser caps how many active subscriptions a single
	// authenticated user may hold across ALL their open connections. Zero
	// disables the per-user cap entirely (the per-connection cap still
	// applies). Anonymous / dev-mode connections (empty userID) bypass the
	// cap regardless. Default: 50.
	MaxSubscriptionsPerUser int

	// EventRateLimit is the maximum number of outbound change-event messages
	// pushed to a single connection within EventRateWindow. Excess events
	// are dropped and the affected subscription is marked out-of-date so the
	// client can resync. Zero disables rate limiting. Default: 100.
	EventRateLimit int
	// EventRateWindow is the rolling window over which EventRateLimit is
	// counted. Default: 1 second.
	EventRateWindow time.Duration

	// EventLog configures the cursor-based replay buffer (US-380). Window
	// defaults to 5 minutes; MaxEntries to 10000.
	EventLog EventLogConfig
}

func (cfg *HubConfig) applyDefaults() {
	if cfg.HeartbeatInterval == 0 {
		cfg.HeartbeatInterval = 30 * time.Second
	}
	if cfg.HeartbeatTimeout == 0 {
		cfg.HeartbeatTimeout = 60 * time.Second
	}
	if cfg.SendBufferSize == 0 {
		cfg.SendBufferSize = 64
	}
	if cfg.AggregationScanLimit == 0 {
		cfg.AggregationScanLimit = 10000
	}
	if cfg.MaxSubscriptionsPerUser == 0 {
		cfg.MaxSubscriptionsPerUser = 50
	}
	if cfg.EventRateLimit == 0 {
		cfg.EventRateLimit = 100
	}
	if cfg.EventRateWindow == 0 {
		cfg.EventRateWindow = time.Second
	}
}

// Connection wraps a single WebSocket connection with its metadata and a
// buffered outbound message channel. The read and write goroutines are
// managed by the Hub.
type Connection struct {
	id     string
	userID string // authenticated user ID; empty for anonymous / dev-mode connections
	conn   *websocket.Conn
	send   chan Message
	done   chan struct{} // closed when the connection's goroutines have exited

	// US-380: replay cursor supplied via ?since=<cursor> on the WebSocket
	// upgrade URL. Set once at connect time and consumed by the first
	// successful subscribe call (per subscription type). 0 means "no
	// reconnect requested", -1 means "out of replay window — client has
	// already been told to refresh".
	replayCursor int64

	// Subscription management (US-133). Protected by subMu.
	subMu         sync.Mutex
	subscriptions map[string]*Subscription
	hub           *Hub // back-pointer for message dispatching

	// Overflow tracking (US-134). Protected by overflowMu.
	overflowMu   sync.Mutex
	overflowSubs map[string]bool

	// Rate limiting (US-308). Protected by rateMu. Rolling FIFO of recent
	// outbound event timestamps; admit checks evict expired entries before
	// deciding whether the next event fits within the window. nil rate
	// limiter (limit<=0 || window<=0) is a pass-through.
	rateMu     sync.Mutex
	rateLimit  int
	rateWindow time.Duration
	rateStamps []time.Time
	nowFunc    func() time.Time
}

// allowEvent reports whether one more outbound event may be pushed to this
// connection right now. A zero / disabled limiter always allows. Otherwise
// timestamps older than the window are evicted and the call is admitted iff
// the surviving bucket is below the limit.
func (c *Connection) allowEvent() bool {
	if c == nil || c.rateLimit <= 0 || c.rateWindow <= 0 {
		return true
	}
	c.rateMu.Lock()
	defer c.rateMu.Unlock()

	now := c.now()
	cutoff := now.Add(-c.rateWindow)
	keep := 0
	for _, t := range c.rateStamps {
		if t.After(cutoff) {
			c.rateStamps[keep] = t
			keep++
		}
	}
	c.rateStamps = c.rateStamps[:keep]

	if len(c.rateStamps) >= c.rateLimit {
		return false
	}
	c.rateStamps = append(c.rateStamps, now)
	return true
}

func (c *Connection) now() time.Time {
	if c.nowFunc != nil {
		return c.nowFunc()
	}
	return time.Now()
}

// markOverflow records that a subscription missed events due to buffer overflow.
func (c *Connection) markOverflow(subID string) {
	c.overflowMu.Lock()
	c.overflowSubs[subID] = true
	c.overflowMu.Unlock()
}

// drainOverflow sends onOutOfDate messages for any subscriptions that missed
// events. Called from writePump after each successful write.
func (c *Connection) drainOverflow(ctx context.Context) {
	c.overflowMu.Lock()
	if len(c.overflowSubs) == 0 {
		c.overflowMu.Unlock()
		return
	}
	subs := c.overflowSubs
	c.overflowSubs = make(map[string]bool)
	c.overflowMu.Unlock()

	for subID := range subs {
		msg := Message{
			Type:           "onOutOfDate",
			SubscriptionID: subID,
		}
		writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_ = wsjson.Write(writeCtx, c.conn, msg)
		cancel()
	}
}

// Hub manages active WebSocket connections and routes messages to them.
// It is safe for concurrent use.
type Hub struct {
	mu              sync.Mutex
	conns           map[string]*Connection
	subIndex        map[string][]*indexedSub // objectType → routing fanout list (US-306)
	jobIndex        map[string][]*indexedSub // jobID → action-job-progress fanout list (US-318)
	userSubs        map[string]int           // authenticated userID → live subscription count (US-308)
	ctx             context.Context
	stop            context.CancelFunc
	config          HubConfig
	resolver        ObjectSetResolver // optional; nil means subscribeObjectSet rejects {objectSetRid}
	indexResolverFn IndexResolver     // optional; nil means subscribeAggregation seeds with empty state
	eventLog        *EventLog         // US-380: cursor-based replay buffer
}

// NewHub creates a new Hub ready to accept WebSocket connections with
// default configuration.
func NewHub() *Hub {
	return NewHubWithConfig(HubConfig{})
}

// NewHubWithConfig creates a new Hub with the given configuration.
func NewHubWithConfig(cfg HubConfig) *Hub {
	cfg.applyDefaults()
	ctx, stop := context.WithCancel(context.Background())
	return &Hub{
		conns:    make(map[string]*Connection),
		subIndex: make(map[string][]*indexedSub),
		jobIndex: make(map[string][]*indexedSub),
		userSubs: make(map[string]int),
		ctx:      ctx,
		stop:     stop,
		config:   cfg,
		eventLog: NewEventLog(cfg.EventLog),
	}
}

// EventLog exposes the Hub's replay buffer. Tests use it to assert cursor
// monotonicity and window eviction; production callers should not need it.
func (h *Hub) EventLog() *EventLog {
	return h.eventLog
}

// SetObjectSetResolver wires the resolver used by subscribeObjectSet to
// translate an objectSetRid into a Definition. Inline-definition subscriptions
// work without a resolver. Passing nil detaches the hook.
func (h *Hub) SetObjectSetResolver(r ObjectSetResolver) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.resolver = r
}

// objectSetResolver returns the currently wired resolver, or nil.
func (h *Hub) objectSetResolver() ObjectSetResolver {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.resolver
}

// SetIndexResolver wires the resolver used by subscribeAggregation to seed an
// aggregator's initial state from a per-objectType Bleve index. Aggregation
// subscriptions still work without a resolver — they simply start empty and
// grow as change events arrive. Passing nil detaches the hook.
func (h *Hub) SetIndexResolver(r IndexResolver) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.indexResolverFn = r
}

// indexResolver returns the currently wired index resolver, or nil.
func (h *Hub) indexResolver() IndexResolver {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.indexResolverFn
}

// ConnectionCount returns the number of active connections.
func (h *Hub) ConnectionCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.conns)
}

// HandleWS is the HTTP handler that upgrades an HTTP request to a WebSocket
// connection and registers it with the Hub. It blocks for the lifetime of
// the connection. Anonymous variant — for authenticated callers prefer
// HandleWSWithUser so the per-user subscription quota applies.
func (h *Hub) HandleWS(w http.ResponseWriter, r *http.Request) {
	h.HandleWSWithUser(w, r, "")
}

// HandleWSWithUser is the user-scoped variant of HandleWS. The userID is
// recorded on the connection and used to enforce HubConfig.MaxSubscriptionsPerUser
// across every connection the user holds open. An empty userID falls back to
// the anonymous-quota-bypassed path.
func (h *Hub) HandleWSWithUser(w http.ResponseWriter, r *http.Request, userID string) {
	// US-380: parse ?since=<cursor> BEFORE the upgrade so a malformed
	// cursor still produces a clean ws frame after handshake (we cannot
	// 4xx an HTTP request after Accept).
	sinceParam := r.URL.Query().Get("since")
	requestedCursor := parseSinceCursor(sinceParam)

	wsConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // origin check handled at routing layer
	})
	if err != nil {
		log.Printf("websocket accept: %v", err)
		return
	}

	// Per-connection context: cancelled when either the Hub shuts down
	// or the readPump exits (client disconnect / error).
	connCtx, connCancel := context.WithCancel(h.ctx)
	defer connCancel()

	connID := uuid.New().String()
	c := &Connection{
		id:            connID,
		userID:        userID,
		conn:          wsConn,
		send:          make(chan Message, h.config.SendBufferSize),
		done:          make(chan struct{}),
		subscriptions: make(map[string]*Subscription),
		overflowSubs:  make(map[string]bool),
		hub:           h,
		rateLimit:     h.config.EventRateLimit,
		rateWindow:    h.config.EventRateWindow,
		replayCursor:  requestedCursor,
	}

	h.register(c)

	// Send welcome message — US-380 stamps the current high-water cursor
	// so a fresh client knows the starting point for events that arrive
	// AFTER this welcome frame.
	welcome := Message{
		Type:         "welcome",
		ConnectionID: connID,
		LastEventID:  h.eventLog.LatestID(),
	}
	c.send <- welcome

	// US-380: out-of-window detection. If the caller asked to resume from
	// a cursor older than the live buffer's earliest id, surface the
	// connection-level onOutOfDate signal RIGHT after welcome so the
	// client can refresh its full state before re-subscribing. The
	// per-subscription replay path stays disabled for this connection.
	if requestedCursor > 0 {
		earliest := h.eventLog.EarliestID()
		latest := h.eventLog.LatestID()
		// requestedCursor < earliest → cursor predates the live window.
		// requestedCursor > latest → cursor refers to a future event the
		// server has not yet emitted (treat as ahead-of-window: refresh).
		// requestedCursor == latest → no missed events; nothing to replay.
		if (earliest > 0 && requestedCursor < earliest) || requestedCursor > latest {
			c.replayCursor = -1
			c.send <- Message{
				Type:        "onOutOfDate",
				LastEventID: latest,
			}
		}
	}

	// Start read, write, and heartbeat goroutines
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		c.writePump(connCtx)
	}()

	go func() {
		defer wg.Done()
		c.readPump(connCtx)
		connCancel() // signal writePump and heartbeat to exit on client disconnect
	}()

	go func() {
		defer wg.Done()
		c.heartbeatPump(connCtx, h.config.HeartbeatInterval, h.config.HeartbeatTimeout)
	}()

	wg.Wait()
	close(c.done)
	h.unregister(connID)
}

// SendToConnection sends a message to a specific connection by ID.
// Returns false if the connection is not found or the send buffer is full.
func (h *Hub) SendToConnection(connID string, msg Message) bool {
	h.mu.Lock()
	c, ok := h.conns[connID]
	h.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case c.send <- msg:
		return true
	default:
		// Buffer full — drop the message for this connection.
		return false
	}
}

// Broadcast sends a message to all active connections. Connections with
// full send buffers will have the message dropped.
func (h *Hub) Broadcast(msg Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.conns {
		select {
		case c.send <- msg:
		default:
		}
	}
}

// Close gracefully shuts down the Hub by closing all active connections.
// It blocks until all connection goroutines have exited.
func (h *Hub) Close() {
	h.stop()

	h.mu.Lock()
	conns := make([]*Connection, 0, len(h.conns))
	for _, c := range h.conns {
		conns = append(conns, c)
	}
	h.mu.Unlock()

	// Close all WebSocket connections with GoingAway
	for _, c := range conns {
		c.conn.Close(websocket.StatusGoingAway, "server shutting down")
	}

	// Wait for all connection goroutines to exit
	for _, c := range conns {
		select {
		case <-c.done:
		case <-time.After(5 * time.Second):
			log.Printf("websocket: timed out waiting for connection %s to close", c.id)
		}
	}
}

// SubscriptionCount returns the number of active subscriptions on a connection.
// Returns -1 if the connection is not found.
func (h *Hub) SubscriptionCount(connID string) int {
	h.mu.Lock()
	c, ok := h.conns[connID]
	h.mu.Unlock()
	if !ok {
		return -1
	}
	c.subMu.Lock()
	defer c.subMu.Unlock()
	return len(c.subscriptions)
}

func (h *Hub) register(c *Connection) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.conns[c.id] = c
}

func (h *Hub) unregister(connID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c, ok := h.conns[connID]; ok {
		h.removeConnectionFromIndexLocked(c)
		// Decrement the per-user counter by however many subscriptions the
		// connection still held at disconnect time so the user's allowance
		// reopens cleanly.
		if c.userID != "" {
			c.subMu.Lock()
			h.releaseUserSubsLocked(c.userID, len(c.subscriptions))
			c.subMu.Unlock()
		}
	}
	delete(h.conns, connID)
}

// reserveUserSubLocked attempts to claim one subscription slot for userID
// against MaxSubscriptionsPerUser. Returns true on success. Empty userID or
// MaxSubscriptionsPerUser <= 0 always succeeds without bookkeeping (anonymous
// / dev-mode connections bypass the per-user cap). Caller must hold h.mu.
func (h *Hub) reserveUserSubLocked(userID string) bool {
	if userID == "" || h.config.MaxSubscriptionsPerUser <= 0 {
		return true
	}
	if h.userSubs == nil {
		h.userSubs = make(map[string]int)
	}
	if h.userSubs[userID] >= h.config.MaxSubscriptionsPerUser {
		return false
	}
	h.userSubs[userID]++
	return true
}

// releaseUserSubsLocked returns n subscription slots to userID's allowance.
// Caller must hold h.mu. Empty userID is a no-op.
func (h *Hub) releaseUserSubsLocked(userID string, n int) {
	if userID == "" || n <= 0 {
		return
	}
	if h.userSubs == nil {
		return
	}
	cur := h.userSubs[userID]
	cur -= n
	if cur <= 0 {
		delete(h.userSubs, userID)
		return
	}
	h.userSubs[userID] = cur
}

// UserSubscriptionCount reports the live subscription count for an
// authenticated user across every open connection. Returns 0 for unknown or
// empty userIDs. Used by tests; callers should not rely on the value being
// monotonic across concurrent subscribe/unsubscribe traffic.
func (h *Hub) UserSubscriptionCount(userID string) int {
	if userID == "" {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.userSubs[userID]
}

// writePump writes messages from the connection's send channel to the
// WebSocket. It exits when the send channel is drained after the Hub
// context is cancelled, or when a write error occurs.
func (c *Connection) writePump(ctx context.Context) {
	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				return
			}
			writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := wsjson.Write(writeCtx, c.conn, msg)
			cancel()
			if err != nil {
				return
			}
			// After successful write, drain any overflow notifications
			c.drainOverflow(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// readPump reads messages from the WebSocket and dispatches subscribe /
// unsubscribe requests. It exits when the connection is closed or an
// error occurs.
func (c *Connection) readPump(ctx context.Context) {
	c.conn.SetReadLimit(32768) // 32KB max message size
	for {
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			return
		}

		var envelope Message
		if err := json.Unmarshal(data, &envelope); err != nil {
			c.send <- Message{Type: "error", Error: "invalid JSON message"}
			continue
		}

		var resp Message
		switch envelope.Type {
		case "subscribe":
			resp = c.hub.handleSubscribe(c, envelope.Data)
		case "subscribeObjectSet":
			resp = c.hub.handleSubscribeObjectSet(c, envelope.Data)
		case "subscribeAggregation":
			resp = c.hub.handleSubscribeAggregation(c, envelope.Data)
		case "subscribeActionJob":
			resp = c.hub.handleSubscribeActionJob(c, envelope.Data)
		case "unsubscribe":
			resp = c.hub.handleUnsubscribe(c, envelope.Data)
		default:
			resp = Message{Type: "error", Error: "unknown message type: " + envelope.Type}
		}

		// Type=="" is the sentinel handlers use to opt out of the default
		// dispatch — they pushed their own response sequence directly to
		// c.send (e.g. handleSubscribeAggregation needs subscribed before
		// the initial snapshot).
		if resp.Type == "" {
			continue
		}
		select {
		case c.send <- resp:
		default:
		}
	}
}

// heartbeatPump sends periodic pings to detect dead connections.
// If a pong is not received within the timeout, the connection is closed.
func (c *Connection) heartbeatPump(ctx context.Context, interval, timeout time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, timeout)
			err := c.conn.Ping(pingCtx)
			cancel()
			if err != nil {
				c.conn.Close(websocket.StatusPolicyViolation, "heartbeat timeout")
				return
			}
		case <-ctx.Done():
			return
		}
	}
}
