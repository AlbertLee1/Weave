package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// WebhookDriver POSTs a JSON envelope to a generic HTTP endpoint. The
// payload shape is intentionally stable and self-describing so a third
// party can build a receiver without an SDK round-trip — fields mirror
// the Envelope plus a top-level `eventType` discriminator.
//
// HTTPClient + DefaultURL follow the same pattern as SlackDriver: the
// per-user webhook URL stored in notification_preferences.target is
// the primary source of truth, with DefaultURL as an optional
// deployment-wide fallback for shared monitoring endpoints.
type WebhookDriver struct {
	HTTPClient *http.Client

	// DefaultURL is an optional fallback when the Envelope carries no
	// Recipient. Empty by default.
	DefaultURL string

	// Headers are added to every outbound request. Useful for shared
	// auth tokens (e.g. {"Authorization": "Bearer ..."}). Per-recipient
	// auth is out of scope; future work could thread per-pref headers
	// through notification_preferences.target.
	Headers map[string]string
}

// Channel reports the registry key. Always returns ChannelWebhook.
func (d *WebhookDriver) Channel() string { return ChannelWebhook }

// webhookPayload is the wire shape this driver POSTs. Stable to allow
// receivers to evolve their JSON parsers independently of the server.
type webhookPayload struct {
	EventType  string                 `json:"eventType"`
	Channel    string                 `json:"channel"`
	UserID     string                 `json:"userId,omitempty"`
	Title      string                 `json:"title"`
	Body       string                 `json:"body,omitempty"`
	Link       string                 `json:"link,omitempty"`
	Properties map[string]interface{} `json:"properties,omitempty"`
}

// Send POSTs the JSON envelope. Any 2xx is success; anything else
// surfaces as an error so the dispatcher can log it.
func (d *WebhookDriver) Send(ctx context.Context, env Envelope) error {
	if d == nil {
		return nil
	}
	target := strings.TrimSpace(env.Recipient)
	if target == "" {
		target = strings.TrimSpace(d.DefaultURL)
	}
	if target == "" {
		return nil
	}
	payload := webhookPayload{
		EventType:  "notification",
		Channel:    env.Channel,
		UserID:     env.UserID,
		Title:      env.Title,
		Body:       env.Body,
		Link:       env.Link,
		Properties: env.Properties,
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("webhook marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range d.Headers {
		req.Header.Set(k, v)
	}
	client := d.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("webhook non-2xx: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
}

var _ Driver = (*WebhookDriver)(nil)
