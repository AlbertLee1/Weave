package funnel

import (
	"context"
	"fmt"
	"time"
)

// WaitForOffset blocks until the consumer has processed up to the given offset,
// or the context is canceled.
func (c *Consumer) WaitForOffset(ctx context.Context, offset uint64) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		if c.lastOffset.Load() >= offset {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for offset %d (current: %d)", offset, c.lastOffset.Load())
		case <-ticker.C:
			continue
		}
	}
}
