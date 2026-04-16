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

// Connection wraps a single WebSocket connection with its metadata and a
// buffered outbound message channel. The read and write goroutines are
// managed by the Hub.
type Connection struct {
	id   string
	conn *websocket.Conn
	send chan Message
	done chan struct{} // closed when the connection's goroutines have exited
}

// Hub manages active WebSocket connections and routes messages to them.
// It is safe for concurrent use.
type Hub struct {
	mu    sync.Mutex
	conns map[string]*Connection
	ctx   context.Context
	stop  context.CancelFunc
}

// NewHub creates a new Hub ready to accept WebSocket connections.
func NewHub() *Hub {
	ctx, stop := context.WithCancel(context.Background())
	return &Hub{
		conns: make(map[string]*Connection),
		ctx:   ctx,
		stop:  stop,
	}
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
		id:   connID,
		conn: wsConn,
		send: make(chan Message, 64),
		done: make(chan struct{}),
	}

	h.register(c)

	// Send welcome message
	welcome := Message{
		Type:         "welcome",
		ConnectionID: connID,
	}
	c.send <- welcome

	// Start read and write goroutines
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		c.writePump(connCtx)
	}()

	go func() {
		defer wg.Done()
		c.readPump(connCtx)
		connCancel() // signal writePump to exit on client disconnect
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
		case <-ctx.Done():
			return
		}
	}
}

// readPump reads messages from the WebSocket. Currently it discards
// incoming messages (subscription handling will be added in US-133).
// It exits when the connection is closed or an error occurs.
func (c *Connection) readPump(ctx context.Context) {
	c.conn.SetReadLimit(32768) // 32KB max message size
	for {
		_, _, err := c.conn.Read(ctx)
		if err != nil {
			return
		}
		// Message handling will be added in US-133 (subscribe/unsubscribe).
	}
}
