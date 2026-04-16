package subscriptions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// ---------- Hub unit tests ----------

func TestNewHub(t *testing.T) {
	h := NewHub()
	if h == nil {
		t.Fatal("expected non-nil Hub")
	}
}

func TestHub_ConnectAndReceiveWelcome(t *testing.T) {
	h := NewHub()
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleWS(w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	// Read welcome message
	var msg Message
	if err := wsjson.Read(ctx, c, &msg); err != nil {
		t.Fatalf("read welcome: %v", err)
	}
	if msg.Type != "welcome" {
		t.Errorf("expected type=welcome, got %q", msg.Type)
	}
	if msg.ConnectionID == "" {
		t.Error("expected non-empty connectionId in welcome")
	}
}

func TestHub_MultipleConnections(t *testing.T) {
	h := NewHub()
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleWS(w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	conns := make([]*websocket.Conn, 3)
	ids := make([]string, 3)
	for i := range conns {
		c, _, err := websocket.Dial(ctx, wsURL, nil)
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		conns[i] = c

		var msg Message
		if err := wsjson.Read(ctx, c, &msg); err != nil {
			t.Fatalf("read welcome %d: %v", i, err)
		}
		ids[i] = msg.ConnectionID
	}

	// All connection IDs should be unique
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Errorf("duplicate connection ID: %s", id)
		}
		seen[id] = true
	}

	if h.ConnectionCount() != 3 {
		t.Errorf("expected 3 connections, got %d", h.ConnectionCount())
	}
}

func TestHub_DisconnectRemovesConnection(t *testing.T) {
	h := NewHub()
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleWS(w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// Read welcome
	var msg Message
	if err := wsjson.Read(ctx, c, &msg); err != nil {
		t.Fatalf("read welcome: %v", err)
	}

	// Wait for connection to be registered
	time.Sleep(50 * time.Millisecond)
	if h.ConnectionCount() != 1 {
		t.Fatalf("expected 1 connection, got %d", h.ConnectionCount())
	}

	// Close the connection
	c.Close(websocket.StatusNormalClosure, "done")

	// Wait for cleanup
	time.Sleep(100 * time.Millisecond)
	if h.ConnectionCount() != 0 {
		t.Errorf("expected 0 connections after disconnect, got %d", h.ConnectionCount())
	}
}

func TestHub_Close_DrainsAllConnections(t *testing.T) {
	h := NewHub()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleWS(w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		c, _, err := websocket.Dial(ctx, wsURL, nil)
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}

		var msg Message
		if err := wsjson.Read(ctx, c, &msg); err != nil {
			t.Fatalf("read welcome %d: %v", i, err)
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			// Read until connection is closed by hub
			for {
				_, _, err := c.Read(ctx)
				if err != nil {
					return
				}
			}
		}()
	}

	// Wait for connections to register
	time.Sleep(50 * time.Millisecond)
	if h.ConnectionCount() != 3 {
		t.Fatalf("expected 3 connections, got %d", h.ConnectionCount())
	}

	// Close hub — should drain all connections
	h.Close()

	// All reader goroutines should complete
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(3 * time.Second):
		t.Error("timed out waiting for connections to drain after Hub.Close")
	}

	if h.ConnectionCount() != 0 {
		t.Errorf("expected 0 connections after Close, got %d", h.ConnectionCount())
	}
}

func TestHub_SendToConnection(t *testing.T) {
	h := NewHub()
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleWS(w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	// Read welcome
	var welcome Message
	if err := wsjson.Read(ctx, c, &welcome); err != nil {
		t.Fatalf("read welcome: %v", err)
	}

	// Send a message to the connection via Hub
	testMsg := Message{
		Type: "test",
		Data: json.RawMessage(`{"hello":"world"}`),
	}
	h.SendToConnection(welcome.ConnectionID, testMsg)

	// Read the message
	var received Message
	if err := wsjson.Read(ctx, c, &received); err != nil {
		t.Fatalf("read test msg: %v", err)
	}
	if received.Type != "test" {
		t.Errorf("expected type=test, got %q", received.Type)
	}
}

// ---------- Message type tests ----------

func TestMessage_JSON(t *testing.T) {
	msg := Message{
		Type:         "welcome",
		ConnectionID: "conn-123",
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Type != "welcome" {
		t.Errorf("expected type=welcome, got %q", decoded.Type)
	}
	if decoded.ConnectionID != "conn-123" {
		t.Errorf("expected connectionId=conn-123, got %q", decoded.ConnectionID)
	}
}
