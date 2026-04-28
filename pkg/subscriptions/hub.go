package subscriptions

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// Message is the envelope for all WebSocket messages (both client→server and
// server→client). The Type field discriminates the payload kind; Data carries
// the type-specific JSON body. ConnectionID is populated only in the welcome
// message; SubscriptionID is populated in subscription-related messages.
type Message struct {
	Type           string          `json:"type"`
	ConnectionID   string          `json:"connectionId,omitempty"`
	SubscriptionID string          `json:"subscriptionId,omitempty"`
	Data           json.RawMessage `json:"data,omitempty"`
	Error          string          `json:"error,omitempty"`
}

// HubConfig holds configurable parameters for the Hub.
type HubConfig struct {
	HeartbeatInterval    time.Duration // ping interval; default 30s
	HeartbeatTimeout     time.Duration // pong deadline; default 60s
	SendBufferSize       int           // per-connection outbound buffer; default 64
	AggregationScanLimit int           // initial scan size for aggregation subscriptions; default 10000
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
}

// Connection wraps a single WebSocket connection with its metadata and a
// buffered outbound message channel. The read and write goroutines are
// managed by the Hub.
type Connection struct {
	id   string
	conn *websocket.Conn
	send chan Message
	done chan struct{} // closed when the connection's goroutines have exited

	// Subscription management (US-133). Protected by subMu.
	subMu         sync.Mutex
	subscriptions map[string]*Subscription
	hub           *Hub // back-pointer for message dispatching

	// Overflow tracking (US-134). Protected by overflowMu.
	overflowMu   sync.Mutex
	overflowSubs map[string]bool
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
	ctx             context.Context
	stop            context.CancelFunc
	config          HubConfig
	resolver        ObjectSetResolver // optional; nil means subscribeObjectSet rejects {objectSetRid}
	indexResolverFn IndexResolver     // optional; nil means subscribeAggregation seeds with empty state
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
		conns:  make(map[string]*Connection),
		ctx:    ctx,
		stop:   stop,
		config: cfg,
	}
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
// the connection.
func (h *Hub) HandleWS(w http.ResponseWriter, r *http.Request) {
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
		conn:          wsConn,
		send:          make(chan Message, h.config.SendBufferSize),
		done:          make(chan struct{}),
		subscriptions: make(map[string]*Subscription),
		overflowSubs:  make(map[string]bool),
		hub:           h,
	}

	h.register(c)

	// Send welcome message
	welcome := Message{
		Type:         "welcome",
		ConnectionID: connID,
	}
	c.send <- welcome

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
	delete(h.conns, connID)
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
