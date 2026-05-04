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

// SlackDriver posts a single message to a Slack incoming-webhook URL.
// The webhook URL is per-recipient — the dispatcher pulls it from
// notification_preferences.target, so a user can wire their personal
// channel without touching server config.
//
// HTTPClient is the test-injection seam; production wiring leaves it
// nil and the driver instantiates a 10s-timeout client.
type SlackDriver struct {
	HTTPClient *http.Client

	// DefaultURL is an optional fallback target used when the Envelope
	// carries no Recipient. Production wiring leaves it empty so the
	// per-user preference is the sole source of truth — but tests and
	// degraded-mode deployments that share one channel can set it.
	DefaultURL string
}

// Channel reports the registry key. Always returns ChannelSlack.
func (d *SlackDriver) Channel() string { return ChannelSlack }

// slackPayload is the minimal incoming-webhook envelope. We ship a
// `text` field built from the title + body + link rather than the
// blocks/attachments shape — webhook inputs accept the simpler form
// and richer surfaces are out of scope for the parity work.
type slackPayload struct {
	Text string `json:"text"`
}

// Send POSTs the envelope to the configured Slack webhook URL. Slack
// returns a plain-text "ok" body on 200 — anything else is logged as
// an error so the dispatcher can record per-recipient failure without
// retrying.
func (d *SlackDriver) Send(ctx context.Context, env Envelope) error {
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
	payload := slackPayload{Text: buildSlackText(env)}
	buf, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("slack marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("slack request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := d.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("slack post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("slack non-2xx: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
}

// buildSlackText assembles a Slack-friendly message from the envelope.
// Title is bolded with surrounding asterisks (Slack mrkdwn), body
// follows on a fresh line, and the deep link rides as a trailing
// "<URL|View>" hyperlink when present.
func buildSlackText(env Envelope) string {
	var b strings.Builder
	if env.Title != "" {
		b.WriteString("*")
		b.WriteString(env.Title)
		b.WriteString("*")
	}
	if env.Body != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(env.Body)
	}
	if env.Link != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("<")
		b.WriteString(env.Link)
		b.WriteString("|View>")
	}
	return b.String()
}

var _ Driver = (*SlackDriver)(nil)
