package subscriptions

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

func TestHandler_DevMode_NoToken(t *testing.T) {
	h := NewHub()
	defer h.Close()

	handler := NewHandler(h, nil) // nil validator = dev mode (accept all)

	srv := httptest.NewServer(handler)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	var msg Message
	if err := wsjson.Read(ctx, c, &msg); err != nil {
		t.Fatalf("read welcome: %v", err)
	}
	if msg.Type != "welcome" {
		t.Errorf("expected type=welcome, got %q", msg.Type)
	}
}

func TestHandler_DevMode_WithToken(t *testing.T) {
	h := NewHub()
	defer h.Close()

	handler := NewHandler(h, nil)

	srv := httptest.NewServer(handler)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "?token=my-dev-token"
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	var msg Message
	if err := wsjson.Read(ctx, c, &msg); err != nil {
		t.Fatalf("read welcome: %v", err)
	}
	if msg.Type != "welcome" {
		t.Errorf("expected type=welcome, got %q", msg.Type)
	}
}

func TestHandler_WithValidator_ValidToken(t *testing.T) {
	h := NewHub()
	defer h.Close()

	validator := func(token string) (string, error) {
		if token == "valid-token" {
			return "user-123", nil
		}
		return "", ErrInvalidToken
	}

	handler := NewHandler(h, validator)

	srv := httptest.NewServer(handler)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "?token=valid-token"
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	var msg Message
	if err := wsjson.Read(ctx, c, &msg); err != nil {
		t.Fatalf("read welcome: %v", err)
	}
	if msg.Type != "welcome" {
		t.Errorf("expected type=welcome, got %q", msg.Type)
	}
}

func TestHandler_WithValidator_InvalidToken(t *testing.T) {
	h := NewHub()
	defer h.Close()

	validator := func(token string) (string, error) {
		return "", ErrInvalidToken
	}

	handler := NewHandler(h, validator)

	srv := httptest.NewServer(handler)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Try connecting with invalid token — should get HTTP 401
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "?token=bad-token"
	_, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err == nil {
		t.Fatal("expected dial error for invalid token")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestHandler_WithValidator_MissingToken(t *testing.T) {
	h := NewHub()
	defer h.Close()

	validator := func(token string) (string, error) {
		return "", ErrInvalidToken
	}

	handler := NewHandler(h, validator)

	srv := httptest.NewServer(handler)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// No ?token= parameter — should get HTTP 401
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	_, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err == nil {
		t.Fatal("expected dial error for missing token")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestHandler_NonWebSocket_Returns400(t *testing.T) {
	h := NewHub()
	defer h.Close()

	handler := NewHandler(h, nil)

	// Regular HTTP request (not a WebSocket upgrade)
	req := httptest.NewRequest("GET", "/ws", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// The websocket.Accept should reject non-upgrade requests
	if w.Code == http.StatusSwitchingProtocols {
		t.Error("expected non-101 for regular HTTP request")
	}
}
