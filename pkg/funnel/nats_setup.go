package funnel

import (
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

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
