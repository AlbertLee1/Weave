package metrics

import (
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNATSPublish_Success(t *testing.T) {
	natsPublishTotal.Reset()

	NATSPublish("weave.edits", nil, 5*time.Millisecond)

	got := testutil.ToFloat64(natsPublishTotal.WithLabelValues("weave.edits", "ok"))
	if got != 1 {
		t.Fatalf("expected ok counter=1, got %v", got)
	}
	gotErr := testutil.ToFloat64(natsPublishTotal.WithLabelValues("weave.edits", "error"))
	if gotErr != 0 {
		t.Fatalf("expected error counter=0, got %v", gotErr)
	}
}

func TestNATSPublish_Error(t *testing.T) {
	natsPublishTotal.Reset()

	NATSPublish("weave.edits", errors.New("connection lost"), 1*time.Millisecond)

	got := testutil.ToFloat64(natsPublishTotal.WithLabelValues("weave.edits", "error"))
	if got != 1 {
		t.Fatalf("expected error counter=1, got %v", got)
	}
	gotOk := testutil.ToFloat64(natsPublishTotal.WithLabelValues("weave.edits", "ok"))
	if gotOk != 0 {
		t.Fatalf("expected ok counter=0, got %v", gotOk)
	}
}

func TestNATSConsume_Duration(t *testing.T) {
	natsConsumeTotal.Reset()
	natsConsumeDuration.Reset()

	NATSConsume("weave.edits", nil, 12*time.Millisecond)

	got := testutil.ToFloat64(natsConsumeTotal.WithLabelValues("weave.edits", "ok"))
	if got != 1 {
		t.Fatalf("expected consume ok=1, got %v", got)
	}
	count := testutil.CollectAndCount(natsConsumeDuration, "weave_nats_consume_duration_seconds")
	if count == 0 {
		t.Fatalf("expected at least one observation in weave_nats_consume_duration_seconds")
	}
}

func TestNATSConsume_Error(t *testing.T) {
	natsConsumeTotal.Reset()
	natsConsumeDuration.Reset()

	NATSConsume("weave.edits", errors.New("decode failure"), 3*time.Millisecond)

	got := testutil.ToFloat64(natsConsumeTotal.WithLabelValues("weave.edits", "error"))
	if got != 1 {
		t.Fatalf("expected consume error=1, got %v", got)
	}
}
