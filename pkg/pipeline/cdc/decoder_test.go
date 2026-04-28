package cdc_test

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pglogrepl"

	"github.com/liyang/weave/pkg/pipeline/cdc"
)

// helper: build a Relation message tagged with the given column flag/key
// shape and stamped with the right MessageType so a downstream Decoder
// dispatches it via the *RelationMessage branch.
func newRelation(id uint32, schema, table string, columns []*pglogrepl.RelationMessageColumn) *pglogrepl.RelationMessage {
	rel := &pglogrepl.RelationMessage{
		RelationID:      id,
		Namespace:       schema,
		RelationName:    table,
		ReplicaIdentity: 'd', // default
		ColumnNum:       uint16(len(columns)),
		Columns:         columns,
	}
	rel.SetType(pglogrepl.MessageTypeRelation)
	return rel
}

func newBegin(lsn pglogrepl.LSN, ts time.Time) *pglogrepl.BeginMessage {
	b := &pglogrepl.BeginMessage{FinalLSN: lsn, CommitTime: ts, Xid: 1}
	b.SetType(pglogrepl.MessageTypeBegin)
	return b
}

func newCommit(lsn pglogrepl.LSN, ts time.Time) *pglogrepl.CommitMessage {
	c := &pglogrepl.CommitMessage{CommitLSN: lsn, TransactionEndLSN: lsn, CommitTime: ts}
	c.SetType(pglogrepl.MessageTypeCommit)
	return c
}

func textCol(s string) *pglogrepl.TupleDataColumn {
	return &pglogrepl.TupleDataColumn{
		DataType: pglogrepl.TupleDataTypeText,
		Length:   uint32(len(s)),
		Data:     []byte(s),
	}
}

func nullCol() *pglogrepl.TupleDataColumn {
	return &pglogrepl.TupleDataColumn{DataType: pglogrepl.TupleDataTypeNull}
}

func toastCol() *pglogrepl.TupleDataColumn {
	return &pglogrepl.TupleDataColumn{DataType: pglogrepl.TupleDataTypeToast}
}

func newInsert(relID uint32, cols ...*pglogrepl.TupleDataColumn) *pglogrepl.InsertMessage {
	m := &pglogrepl.InsertMessage{
		RelationID: relID,
		Tuple: &pglogrepl.TupleData{
			ColumnNum: uint16(len(cols)),
			Columns:   cols,
		},
	}
	m.SetType(pglogrepl.MessageTypeInsert)
	return m
}

func newUpdate(relID uint32, oldType uint8, oldCols, newCols []*pglogrepl.TupleDataColumn) *pglogrepl.UpdateMessage {
	m := &pglogrepl.UpdateMessage{
		RelationID: relID,
		NewTuple: &pglogrepl.TupleData{
			ColumnNum: uint16(len(newCols)),
			Columns:   newCols,
		},
	}
	if oldCols != nil {
		m.OldTupleType = oldType
		m.OldTuple = &pglogrepl.TupleData{
			ColumnNum: uint16(len(oldCols)),
			Columns:   oldCols,
		}
	}
	m.SetType(pglogrepl.MessageTypeUpdate)
	return m
}

func newDelete(relID uint32, oldType uint8, oldCols []*pglogrepl.TupleDataColumn) *pglogrepl.DeleteMessage {
	m := &pglogrepl.DeleteMessage{
		RelationID: relID,
		OldTupleType: oldType,
		OldTuple: &pglogrepl.TupleData{
			ColumnNum: uint16(len(oldCols)),
			Columns:   oldCols,
		},
	}
	m.SetType(pglogrepl.MessageTypeDelete)
	return m
}

func ordersRelation() *pglogrepl.RelationMessage {
	return newRelation(7001, "public", "orders", []*pglogrepl.RelationMessageColumn{
		{Name: "id", Flags: 1, DataType: 23},
		{Name: "customer_id", Flags: 0, DataType: 25},
		{Name: "shipped_at", Flags: 0, DataType: 1184},
	})
}

func TestDecoder_InsertWithinTxn(t *testing.T) {
	dec := cdc.NewDecoder()
	commitTs := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)

	if _, _, err := dec.Process(newBegin(0xdeadbeef, commitTs)); err != nil {
		t.Fatalf("Process(begin) error: %v", err)
	}
	if _, _, err := dec.Process(ordersRelation()); err != nil {
		t.Fatalf("Process(relation) error: %v", err)
	}
	events, commit, err := dec.Process(newInsert(7001, textCol("10248"), textCol("ALFKI"), nullCol()))
	if err != nil {
		t.Fatalf("Process(insert) error: %v", err)
	}
	if commit {
		t.Fatalf("Insert should not signal commit")
	}
	if len(events) != 1 {
		t.Fatalf("len(events)=%d want 1", len(events))
	}
	ev := events[0]
	if ev.Op != cdc.ChangeOpInsert || ev.Schema != "public" || ev.Table != "orders" {
		t.Fatalf("event meta wrong: %+v", ev)
	}
	if ev.LSN != 0xdeadbeef || ev.CommitLSN != 0xdeadbeef || !ev.CommitTime.Equal(commitTs) {
		t.Fatalf("event txn metadata not stamped: %+v", ev)
	}
	if ev.After["id"] != "10248" || ev.After["customer_id"] != "ALFKI" {
		t.Fatalf("after tuple wrong: %#v", ev.After)
	}
	if !cdc.IsNullValue(ev.After["shipped_at"]) {
		t.Fatalf("shipped_at should be NULL sentinel: %q", ev.After["shipped_at"])
	}

	_, commit, err = dec.Process(newCommit(0xdeadbeef, commitTs))
	if err != nil {
		t.Fatalf("Process(commit) error: %v", err)
	}
	if !commit {
		t.Fatalf("Commit should signal commit=true")
	}
}

func TestDecoder_UpdateAndDelete(t *testing.T) {
	dec := cdc.NewDecoder()
	if _, _, err := dec.Process(newBegin(100, time.Now())); err != nil {
		t.Fatal(err)
	}
	if _, _, err := dec.Process(ordersRelation()); err != nil {
		t.Fatal(err)
	}
	updateMsg := newUpdate(
		7001,
		pglogrepl.UpdateMessageTupleTypeKey,
		[]*pglogrepl.TupleDataColumn{textCol("10248"), nullCol(), nullCol()},
		[]*pglogrepl.TupleDataColumn{textCol("10248"), textCol("VINET"), textCol("2026-04-28")},
	)
	events, commit, err := dec.Process(updateMsg)
	if err != nil {
		t.Fatalf("update process error: %v", err)
	}
	if commit {
		t.Fatalf("update should not signal commit")
	}
	if len(events) != 1 {
		t.Fatalf("update event count=%d", len(events))
	}
	upd := events[0]
	if upd.Op != cdc.ChangeOpUpdate {
		t.Fatalf("update Op=%s", upd.Op)
	}
	// REPLICA IDENTITY DEFAULT means before tuple only carries key columns.
	if got, want := upd.Before["id"], "10248"; got != want {
		t.Fatalf("before.id=%q want %q", got, want)
	}
	if _, ok := upd.Before["customer_id"]; ok {
		t.Fatalf("non-key column should be absent in keys-only before tuple: %#v", upd.Before)
	}
	if upd.After["customer_id"] != "VINET" {
		t.Fatalf("after.customer_id=%q want VINET", upd.After["customer_id"])
	}

	deleteMsg := newDelete(7001, pglogrepl.DeleteMessageTupleTypeKey, []*pglogrepl.TupleDataColumn{textCol("10248"), nullCol(), nullCol()})
	events, _, err = dec.Process(deleteMsg)
	if err != nil {
		t.Fatalf("delete process error: %v", err)
	}
	if len(events) != 1 || events[0].Op != cdc.ChangeOpDelete {
		t.Fatalf("expected one DELETE event: %#v", events)
	}
	if events[0].Before["id"] != "10248" {
		t.Fatalf("delete.before.id=%q", events[0].Before["id"])
	}
}

func TestDecoder_ToastedColumnAbsent(t *testing.T) {
	dec := cdc.NewDecoder()
	if _, _, err := dec.Process(newBegin(1, time.Now())); err != nil {
		t.Fatal(err)
	}
	if _, _, err := dec.Process(ordersRelation()); err != nil {
		t.Fatal(err)
	}
	events, _, err := dec.Process(newInsert(7001, textCol("10248"), textCol("ALFKI"), toastCol()))
	if err != nil {
		t.Fatalf("Process(insert) error: %v", err)
	}
	if _, ok := events[0].After["shipped_at"]; ok {
		t.Fatalf("toasted column should be absent: %#v", events[0].After)
	}
}

func TestDecoder_UnknownRelation(t *testing.T) {
	dec := cdc.NewDecoder()
	_, _, err := dec.Process(newInsert(99, textCol("x")))
	if err == nil || !strings.Contains(err.Error(), "unknown relation id") {
		t.Fatalf("expected unknown-relation error, got %v", err)
	}
}

func TestDecoder_DeleteWithoutOldTuple(t *testing.T) {
	dec := cdc.NewDecoder()
	if _, _, err := dec.Process(ordersRelation()); err != nil {
		t.Fatal(err)
	}
	bad := &pglogrepl.DeleteMessage{RelationID: 7001}
	bad.SetType(pglogrepl.MessageTypeDelete)
	_, _, err := dec.Process(bad)
	if err == nil || !strings.Contains(err.Error(), "no old tuple") {
		t.Fatalf("expected no-old-tuple error, got %v", err)
	}
}

func TestDecoder_TupleColumnCountMismatch(t *testing.T) {
	dec := cdc.NewDecoder()
	if _, _, err := dec.Process(newBegin(1, time.Now())); err != nil {
		t.Fatal(err)
	}
	if _, _, err := dec.Process(ordersRelation()); err != nil {
		t.Fatal(err)
	}
	// Insert with 2 cols against a 3-col relation should error.
	_, _, err := dec.Process(newInsert(7001, textCol("10248"), textCol("ALFKI")))
	if err == nil || !strings.Contains(err.Error(), "tuple column count") {
		t.Fatalf("expected column-count error, got %v", err)
	}
}

func TestDecoder_OutsideTxnEventsHaveZeroLSN(t *testing.T) {
	// Pgoutput normally always wraps DML in Begin/Commit, but if the
	// stream resumes mid-transaction (slot replay started at a point
	// past Begin) the decoder should still emit the events with
	// zero-valued txn metadata rather than panicking.
	dec := cdc.NewDecoder()
	if _, _, err := dec.Process(ordersRelation()); err != nil {
		t.Fatal(err)
	}
	events, _, err := dec.Process(newInsert(7001, textCol("10248"), textCol("ALFKI"), textCol("2026")))
	if err != nil {
		t.Fatalf("Process(insert) error: %v", err)
	}
	if events[0].LSN != 0 || !events[0].CommitTime.IsZero() {
		t.Fatalf("expected zero txn metadata, got %+v", events[0])
	}
}

func TestDecoder_ProcessWAL_EmptyBuf(t *testing.T) {
	dec := cdc.NewDecoder()
	_, _, err := dec.ProcessWAL(nil)
	if err == nil || !strings.Contains(err.Error(), "empty WAL buffer") {
		t.Fatalf("expected empty-buffer error, got %v", err)
	}
}
