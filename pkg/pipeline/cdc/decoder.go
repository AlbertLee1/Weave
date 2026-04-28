package cdc

import (
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pglogrepl"
)

// nullSentinel is the in-process marker the decoder uses inside the
// flat string tuple maps for SQL NULL values. The pgoutput format
// distinguishes NULL ('n') from text ('t') from unchanged-TOASTed
// ('u'), but the downstream mapper only needs the binary "is the
// value present" decision. Picking a sentinel byte that PG cannot
// produce as a legitimate text value (control characters with NUL
// inside) keeps the round-trip unambiguous.
const nullSentinel = "\x00\x00CDC_NULL\x00\x00"

// IsNullValue reports whether s is the in-process NULL sentinel used
// inside ChangeEvent.Before/After maps. Exported so consumers writing
// custom mappers can distinguish "absent column" (key missing) from
// "explicit NULL" (key present with sentinel value).
func IsNullValue(s string) bool {
	return s == nullSentinel
}

// relationInfo is the cached metadata for one PG relation, populated
// from a Relation logical-decoding message. Subsequent Insert / Update
// / Delete messages reference the relation by ID; the decoder needs
// the cached column names + REPLICA IDENTITY shape to project tuple
// data back into a column-named map.
type relationInfo struct {
	id        uint32
	schema    string
	table     string
	columns   []string
	keyColumn []bool // true when the column is part of REPLICA IDENTITY
}

// txState captures Begin-time metadata that the decoder stamps onto
// every change event emitted before the matching Commit.
type txState struct {
	commitLSN  pglogrepl.LSN
	commitTime time.Time
	open       bool
}

// Decoder is the stateful aggregator on top of pglogrepl.Parse. It
// caches Relation metadata and translates Insert/Update/Delete typed
// messages into ChangeEvents tagged with the current transaction's
// commit metadata.
//
// One Decoder instance corresponds to one logical replication slot;
// concurrent calls are NOT supported (the pgoutput stream is
// single-threaded by design).
type Decoder struct {
	relations map[uint32]*relationInfo
	tx        txState
}

// NewDecoder returns a fresh Decoder ready to process the first
// XLogData payload from a logical replication slot.
func NewDecoder() *Decoder {
	return &Decoder{relations: make(map[uint32]*relationInfo)}
}

// ProcessWAL parses the WAL byte payload (typically from
// pglogrepl.XLogData.WALData) into a typed pglogrepl.Message and
// dispatches it through Process.
func (d *Decoder) ProcessWAL(buf []byte) ([]ChangeEvent, bool, error) {
	if len(buf) == 0 {
		return nil, false, errors.New("cdc: empty WAL buffer")
	}
	msg, err := pglogrepl.Parse(buf)
	if err != nil {
		return nil, false, fmt.Errorf("cdc: parse pgoutput message: %w", err)
	}
	return d.Process(msg)
}

// Process dispatches a typed pglogrepl Message:
//   - Begin opens a transaction and records its commit LSN/time
//   - Relation caches schema metadata
//   - Insert/Update/Delete emit a ChangeEvent stamped with the current
//     transaction's metadata
//   - Commit closes the transaction and signals the caller to flush
//     any per-transaction batch (commit==true on return)
//   - Origin / Type / Truncate / LogicalDecodingMessage are ignored
//     (they don't affect row state in the configured mappings)
//
// Returns ([]ChangeEvent, commit bool, err). Multiple events are
// theoretically possible when a single message produces several rows
// (e.g. truncate of a multi-table cascade) but pgoutput emits one
// row per Insert/Update/Delete so callers can treat the slice as
// "0 or 1" in practice.
func (d *Decoder) Process(msg pglogrepl.Message) ([]ChangeEvent, bool, error) {
	switch m := msg.(type) {
	case *pglogrepl.BeginMessage:
		d.tx = txState{
			commitLSN:  m.FinalLSN,
			commitTime: m.CommitTime,
			open:       true,
		}
		return nil, false, nil
	case *pglogrepl.RelationMessage:
		d.cacheRelation(m)
		return nil, false, nil
	case *pglogrepl.InsertMessage:
		ev, err := d.eventFromInsert(m)
		if err != nil {
			return nil, false, err
		}
		return []ChangeEvent{ev}, false, nil
	case *pglogrepl.UpdateMessage:
		ev, err := d.eventFromUpdate(m)
		if err != nil {
			return nil, false, err
		}
		return []ChangeEvent{ev}, false, nil
	case *pglogrepl.DeleteMessage:
		ev, err := d.eventFromDelete(m)
		if err != nil {
			return nil, false, err
		}
		return []ChangeEvent{ev}, false, nil
	case *pglogrepl.CommitMessage:
		d.tx.open = false
		return nil, true, nil
	default:
		// TypeMessage / OriginMessage / TruncateMessage /
		// LogicalDecodingMessage / keepalive variants. None of them
		// affect row-level state in the configured mappings — drop
		// them silently rather than failing so a future protocol
		// addition doesn't break the receiver.
		return nil, false, nil
	}
}

func (d *Decoder) cacheRelation(m *pglogrepl.RelationMessage) {
	info := &relationInfo{
		id:        m.RelationID,
		schema:    m.Namespace,
		table:     m.RelationName,
		columns:   make([]string, len(m.Columns)),
		keyColumn: make([]bool, len(m.Columns)),
	}
	for i, c := range m.Columns {
		info.columns[i] = c.Name
		info.keyColumn[i] = c.Flags&1 == 1
	}
	d.relations[m.RelationID] = info
}

func (d *Decoder) eventFromInsert(m *pglogrepl.InsertMessage) (ChangeEvent, error) {
	rel, err := d.lookupRelation(m.RelationID)
	if err != nil {
		return ChangeEvent{}, err
	}
	after, err := tupleToMap(rel, m.Tuple, false)
	if err != nil {
		return ChangeEvent{}, fmt.Errorf("cdc: insert on %s.%s: %w", rel.schema, rel.table, err)
	}
	return d.stampEvent(ChangeEvent{
		Op:         ChangeOpInsert,
		Schema:     rel.schema,
		Table:      rel.table,
		RelationID: rel.id,
		After:      after,
	}), nil
}

func (d *Decoder) eventFromUpdate(m *pglogrepl.UpdateMessage) (ChangeEvent, error) {
	rel, err := d.lookupRelation(m.RelationID)
	if err != nil {
		return ChangeEvent{}, err
	}
	after, err := tupleToMap(rel, m.NewTuple, false)
	if err != nil {
		return ChangeEvent{}, fmt.Errorf("cdc: update on %s.%s: %w", rel.schema, rel.table, err)
	}
	var before map[string]string
	if m.OldTuple != nil {
		keysOnly := m.OldTupleType == pglogrepl.UpdateMessageTupleTypeKey
		before, err = tupleToMap(rel, m.OldTuple, keysOnly)
		if err != nil {
			return ChangeEvent{}, fmt.Errorf("cdc: update old-tuple on %s.%s: %w", rel.schema, rel.table, err)
		}
	}
	return d.stampEvent(ChangeEvent{
		Op:         ChangeOpUpdate,
		Schema:     rel.schema,
		Table:      rel.table,
		RelationID: rel.id,
		Before:     before,
		After:      after,
	}), nil
}

func (d *Decoder) eventFromDelete(m *pglogrepl.DeleteMessage) (ChangeEvent, error) {
	rel, err := d.lookupRelation(m.RelationID)
	if err != nil {
		return ChangeEvent{}, err
	}
	if m.OldTuple == nil {
		return ChangeEvent{}, fmt.Errorf("cdc: delete on %s.%s arrived with no old tuple — REPLICA IDENTITY must be DEFAULT/USING/FULL", rel.schema, rel.table)
	}
	keysOnly := m.OldTupleType == pglogrepl.DeleteMessageTupleTypeKey
	before, err := tupleToMap(rel, m.OldTuple, keysOnly)
	if err != nil {
		return ChangeEvent{}, fmt.Errorf("cdc: delete old-tuple on %s.%s: %w", rel.schema, rel.table, err)
	}
	return d.stampEvent(ChangeEvent{
		Op:         ChangeOpDelete,
		Schema:     rel.schema,
		Table:      rel.table,
		RelationID: rel.id,
		Before:     before,
	}), nil
}

func (d *Decoder) lookupRelation(id uint32) (*relationInfo, error) {
	rel, ok := d.relations[id]
	if !ok {
		return nil, fmt.Errorf("cdc: unknown relation id %d (no Relation message seen on this slot)", id)
	}
	return rel, nil
}

// stampEvent stamps the current transaction's commit metadata onto an
// in-flight ChangeEvent. Events emitted outside a Begin/Commit pair
// (extremely rare; normally pgoutput wraps every DML in a txn) carry
// zero-valued LSN/time so the consumer can still ingest them but with
// no notion of monotonicity.
func (d *Decoder) stampEvent(ev ChangeEvent) ChangeEvent {
	ev.LSN = d.tx.commitLSN
	ev.CommitLSN = d.tx.commitLSN
	ev.CommitTime = d.tx.commitTime
	return ev
}

// tupleToMap projects a pgoutput TupleData onto the named-column map
// the rest of the package consumes. keysOnly==true marks tuples that
// only carry REPLICA IDENTITY columns (the rest are absent rather
// than NULL) so the decoder skips non-key columns instead of
// surfacing them as NULL.
func tupleToMap(rel *relationInfo, tuple *pglogrepl.TupleData, keysOnly bool) (map[string]string, error) {
	if tuple == nil {
		return nil, errors.New("cdc: nil tuple data")
	}
	if int(tuple.ColumnNum) != len(rel.columns) {
		return nil, fmt.Errorf("cdc: tuple column count %d does not match relation column count %d", tuple.ColumnNum, len(rel.columns))
	}
	out := make(map[string]string, len(rel.columns))
	for i, col := range tuple.Columns {
		if keysOnly && !rel.keyColumn[i] {
			continue
		}
		switch col.DataType {
		case pglogrepl.TupleDataTypeNull:
			out[rel.columns[i]] = nullSentinel
		case pglogrepl.TupleDataTypeToast:
			// Unchanged TOASTed value — the actual bytes are not on
			// the wire. Skip silently; downstream consumers preserve
			// the existing indexed value when a column is absent.
			continue
		case pglogrepl.TupleDataTypeText, pglogrepl.TupleDataTypeBinary:
			out[rel.columns[i]] = string(col.Data)
		default:
			return nil, fmt.Errorf("cdc: unknown tuple data type byte %q on column %q", col.DataType, rel.columns[i])
		}
	}
	return out, nil
}
