package funnel

import (
	"fmt"
	"log"
	"time"

	"github.com/nats-io/nats.go"
)

// DefaultConnectOptions returns NATS connection options with reconnection
// handling suitable for production use.
func DefaultConnectOptions() []nats.Option {
	return []nats.Option{
		nats.MaxReconnects(60),
		nats.ReconnectWait(2 * time.Second),
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

// SetupJetStream creates the NATS JetStream stream for object edits.
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
	return nil
}
