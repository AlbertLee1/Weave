package funnel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/nats-io/nats.go"
)

// US-470 surfaces operator-facing list/replay/discard primitives over the
// existing `OBJECT_EDITS_DLQ` JetStream stream so dead-lettered batches can
// be inspected and retried without standing up an external tool.
//
// The read path is split from production NATS plumbing via DLQReader so unit
// tests can drive the admin handler with fakes; the live impl
// (JetStreamDLQReader) iterates the stream by sequence numbers.
//
// ID semantics: every DLQ entry is uniquely identified by its JetStream
// sequence number rendered as a decimal string. Sequence numbers are
// monotonically increasing per stream and stable across server restarts,
// so they make a natural URL-safe handle. Replay maps {id} back to the seq
// number, fetches the raw envelope, and republishes OriginalData onto the
// derived OriginalSubject.

// ErrDLQEntryNotFound is returned by DLQReader.GetByID and ReplayDLQEntry when
// the requested id does not exist (either was never present or was already
// deleted via discard/age-out).
var ErrDLQEntryNotFound = errors.New("funnel: DLQ entry not found")

// DLQEntry pairs a stream sequence id (rendered as a decimal string) with the
// envelope payload. The Subject field is the DLQ-side subject the message was
// published under; OriginalSubject inside Message remains the canonical live
// subject the consumer was about to NAK on.
type DLQEntry struct {
	ID      string     `json:"id"`
	Subject string     `json:"subject"`
	Message DLQMessage `json:"message"`
}

// DLQReader is the read-side abstraction for the operator admin endpoints
// (list / replay / discard). Implementations must be safe for concurrent
// use.
type DLQReader interface {
	// ListPending returns DLQ entries in stream insertion order, up to
	// `limit`. A limit of <= 0 means "no bound"; callers SHOULD cap it
	// since the underlying stream can hold thousands of envelopes.
	ListPending(ctx context.Context, limit int) ([]DLQEntry, error)

	// GetByID fetches a single entry by its sequence-number id. Returns
	// ErrDLQEntryNotFound if the entry is missing.
	GetByID(ctx context.Context, id string) (DLQEntry, error)

	// DeleteByID removes the entry by sequence-number id. JetStream's
	// MaxAge takes care of eviction eventually; this surface is for the
	// "discard" / "successfully replayed" paths so dashboards see an
	// immediate decrement.
	DeleteByID(ctx context.Context, id string) error

	// Size returns the count of pending messages in the DLQ stream.
	// Surfaced through the weave_funnel_dlq_size Prometheus gauge.
	Size(ctx context.Context) (int64, error)
}

// JetStreamDLQReader is the production DLQReader backed by NATS JetStream.
// It uses StreamInfo for size + range bounds and GetMsg / DeleteMsg for
// per-entry I/O. No long-lived consumers are created — every call is a
// stateless request to the JetStream HTTP/NATS metadata API, which keeps
// the surface zero-cost when the admin endpoints are idle.
type JetStreamDLQReader struct {
	js         nats.JetStreamContext
	streamName string
}

// NewJetStreamDLQReader returns a JetStreamDLQReader bound to the default
// DLQStreamName. The streamName is not configurable because callers that want
// a different stream simply construct a JetStreamDLQReader{} literal with the
// name they need.
func NewJetStreamDLQReader(js nats.JetStreamContext) *JetStreamDLQReader {
	return &JetStreamDLQReader{js: js, streamName: DLQStreamName}
}

// Size returns the number of pending DLQ messages.
func (r *JetStreamDLQReader) Size(ctx context.Context) (int64, error) {
	info, err := r.js.StreamInfo(r.streamName)
	if err != nil {
		return 0, fmt.Errorf("dlq reader: StreamInfo(%s): %w", r.streamName, err)
	}
	return int64(info.State.Msgs), nil
}

// ListPending walks the stream from FirstSeq to LastSeq, decoding each entry
// into a DLQEntry. Deleted sequences are skipped so the returned slice's
// length is the true pending count (capped at `limit`). Decode failures are
// logged into the entry's Message field with empty contents so operators
// can still discard the bad row.
func (r *JetStreamDLQReader) ListPending(ctx context.Context, limit int) ([]DLQEntry, error) {
	info, err := r.js.StreamInfo(r.streamName)
	if err != nil {
		return nil, fmt.Errorf("dlq reader: StreamInfo(%s): %w", r.streamName, err)
	}
	if info.State.Msgs == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = int(info.State.Msgs)
	}
	out := make([]DLQEntry, 0, limit)
	for seq := info.State.FirstSeq; seq <= info.State.LastSeq && len(out) < limit; seq++ {
		raw, err := r.js.GetMsg(r.streamName, seq)
		if err != nil {
			// Most common cause: the entry at this seq was deleted /
			// expired. Skip rather than fail the whole list.
			continue
		}
		entry := DLQEntry{
			ID:      strconv.FormatUint(seq, 10),
			Subject: raw.Subject,
		}
		if err := json.Unmarshal(raw.Data, &entry.Message); err != nil {
			// Preserve the raw payload reference so the operator can
			// still discard it. Body unmarshal failures are rare but
			// not theoretical — a future schema bump leaves prior
			// entries undecodable until they age out.
			entry.Message = DLQMessage{
				OriginalSubject: raw.Subject,
				OriginalData:    raw.Data,
				Reason:          fmt.Sprintf("decode failed: %v", err),
			}
		}
		out = append(out, entry)
	}
	return out, nil
}

// GetByID fetches a single entry by id.
func (r *JetStreamDLQReader) GetByID(ctx context.Context, id string) (DLQEntry, error) {
	seq, err := parseDLQID(id)
	if err != nil {
		return DLQEntry{}, err
	}
	raw, err := r.js.GetMsg(r.streamName, seq)
	if err != nil {
		if errors.Is(err, nats.ErrMsgNotFound) {
			return DLQEntry{}, ErrDLQEntryNotFound
		}
		return DLQEntry{}, fmt.Errorf("dlq reader: GetMsg(%s, %d): %w", r.streamName, seq, err)
	}
	entry := DLQEntry{ID: id, Subject: raw.Subject}
	if err := json.Unmarshal(raw.Data, &entry.Message); err != nil {
		return DLQEntry{}, fmt.Errorf("dlq reader: decode envelope: %w", err)
	}
	return entry, nil
}

// DeleteByID removes the entry; ErrDLQEntryNotFound is returned when the
// underlying NATS request reports the message is gone.
func (r *JetStreamDLQReader) DeleteByID(ctx context.Context, id string) error {
	seq, err := parseDLQID(id)
	if err != nil {
		return err
	}
	if err := r.js.DeleteMsg(r.streamName, seq); err != nil {
		if errors.Is(err, nats.ErrMsgNotFound) {
			return ErrDLQEntryNotFound
		}
		return fmt.Errorf("dlq reader: DeleteMsg(%s, %d): %w", r.streamName, seq, err)
	}
	return nil
}

func parseDLQID(id string) (uint64, error) {
	if id == "" {
		return 0, fmt.Errorf("invalid dlq id: empty")
	}
	seq, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid dlq id %q: %w", id, err)
	}
	return seq, nil
}

// ReplayDLQEntry re-publishes a DLQ entry's OriginalData onto its
// OriginalSubject (the live subject), then removes the entry from the
// underlying stream so a subsequent list does not show the row again.
//
// On publish failure the entry is left in place so operators can re-trigger
// once the downstream issue clears — this is the "safe degrade" Path:
// duplicates are tolerable, lost replays are not.
//
// Returns the destination subject for caller-side logging / response shaping
// (e.g. "edits.employee"). Sentinel ErrDLQEntryNotFound surfaces when the
// id does not exist.
func ReplayDLQEntry(ctx context.Context, reader DLQReader, id string, publish DLQPublishFunc) (string, error) {
	if reader == nil {
		return "", fmt.Errorf("replay DLQ entry: reader is nil")
	}
	if publish == nil {
		return "", fmt.Errorf("replay DLQ entry: publish func is nil")
	}
	entry, err := reader.GetByID(ctx, id)
	if err != nil {
		return "", err
	}
	if len(entry.Message.OriginalData) == 0 {
		return "", fmt.Errorf("replay DLQ entry: original payload empty for id=%s", id)
	}
	subject := entry.Message.OriginalSubject
	if subject == "" {
		subject = OriginalSubjectFromDLQ(entry.Subject)
	}
	if subject == "" {
		return "", fmt.Errorf("replay DLQ entry: cannot derive live subject for id=%s", id)
	}
	if err := publish(subject, entry.Message.OriginalData); err != nil {
		return "", fmt.Errorf("replay DLQ entry: publish: %w", err)
	}
	// Successful replay → delete the DLQ row so dashboards / metrics
	// reflect the drain immediately. Delete failure is logged-but-swallowed
	// because the JetStream MaxAge ultimately evicts the entry; failing
	// the entire replay would force operators into a retry storm.
	if err := reader.DeleteByID(ctx, id); err != nil && !errors.Is(err, ErrDLQEntryNotFound) {
		return subject, fmt.Errorf("replay DLQ entry: delete after publish: %w", err)
	}
	return subject, nil
}
