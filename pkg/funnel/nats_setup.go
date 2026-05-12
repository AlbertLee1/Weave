package funnel

import (
	"fmt"
	"log"
	"time"

	"github.com/nats-io/nats.go"
)

// DefaultConnectOptions returns NATS connection options with reconnection
// handling suitable for production use. The jitter values are deliberately
// asymmetric (TLS path is slower to recover so a slightly larger jitter
// avoids thundering-herd on shared infrastructure).
func DefaultConnectOptions() []nats.Option {
	return []nats.Option{
		nats.MaxReconnects(60),
		nats.ReconnectWait(2 * time.Second),
		nats.ReconnectJitter(500*time.Millisecond, 2*time.Second),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			log.Printf("funnel: NATS disconnected: %v", err)
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			log.Printf("funnel: NATS reconnected to %s", nc.ConnectedUrl())
		}),
	}
}

// Connect connects to NATS with default reconnection options.
func Connect(url string) (*nats.Conn, error) {
	nc, err := nats.Connect(url, DefaultConnectOptions()...)
	if err != nil {
		return nil, fmt.Errorf("nats connect: %w", err)
	}
	return nc, nil
}

// SetupJetStream creates the NATS JetStream streams for object edits and the DLQ.
func SetupJetStream(js nats.JetStreamContext) error {
	_, err := js.AddStream(&nats.StreamConfig{
		Name:      StreamName,
		Subjects:  []string{SubjectPrefix + ".>"},
		Retention: nats.WorkQueuePolicy,
		MaxAge:    24 * time.Hour,
		Storage:   nats.FileStorage,
	})
	if err != nil {
		return fmt.Errorf("create stream: %w", err)
	}

	if err := SetupDLQStream(js); err != nil {
		return err
	}

	return nil
}
