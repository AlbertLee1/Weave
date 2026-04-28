package cdc_test

import (
	"encoding/binary"
	"time"
)

// Test-only encoders that round-trip into pglogrepl.Parse. The pgoutput
// wire format is documented at
// https://www.postgresql.org/docs/current/protocol-logicalrep-message-formats.html
// but pglogrepl only ships decoders. The handful of encoders below are
// sufficient to drive the cdc.Decoder + cdc.Receiver tests without
// standing up a real PG instance.

// pgEpoch is 2000-01-01 00:00:00 UTC; PG logical replication uses
// microseconds-since-epoch for timestamps in this format.
var pgEpoch = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

func pgTimestamp(t time.Time) int64 {
	return t.Sub(pgEpoch).Microseconds()
}

func writeUint8(b []byte, v uint8) []byte  { return append(b, v) }
func writeUint16(b []byte, v uint16) []byte {
	var buf [2]byte
	binary.BigEndian.PutUint16(buf[:], v)
	return append(b, buf[:]...)
}
func writeUint32(b []byte, v uint32) []byte {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], v)
	return append(b, buf[:]...)
}
func writeUint64(b []byte, v uint64) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], v)
	return append(b, buf[:]...)
}
func writeCString(b []byte, s string) []byte {
	b = append(b, []byte(s)...)
	return append(b, 0)
}

func encodeBegin(finalLSN uint64, commit time.Time) []byte {
	out := []byte{'B'}
	out = writeUint64(out, finalLSN)
	out = writeUint64(out, uint64(pgTimestamp(commit)))
	out = writeUint32(out, 1) // xid
	return out
}

func encodeCommit(commitLSN uint64, commit time.Time) []byte {
	out := []byte{'C'}
	out = writeUint8(out, 0) // flags
	out = writeUint64(out, commitLSN)
	out = writeUint64(out, commitLSN)
	out = writeUint64(out, uint64(pgTimestamp(commit)))
	return out
}

type relCol struct {
	name string
	key  bool
}

func encodeRelation(relID uint32, schema, table string, cols []relCol) []byte {
	out := []byte{'R'}
	out = writeUint32(out, relID)
	out = writeCString(out, schema)
	out = writeCString(out, table)
	out = writeUint8(out, 'd') // ReplicaIdentity = default
	out = writeUint16(out, uint16(len(cols)))
	for _, c := range cols {
		var flag uint8
		if c.key {
			flag = 1
		}
		out = writeUint8(out, flag)
		out = writeCString(out, c.name)
		out = writeUint32(out, 25) // arbitrary OID (text)
		out = writeUint32(out, 0xFFFFFFFF) // type modifier (-1)
	}
	return out
}

// textOrNull is one column value to embed in a TupleData payload.
// Use textVal(s) for normal text values, nullV() for SQL NULL.
type textOrNull struct {
	null bool
	text string
}

func textVal(s string) textOrNull { return textOrNull{text: s} }
func nullV() textOrNull           { return textOrNull{null: true} }

func encodeTuple(cols []textOrNull) []byte {
	out := writeUint16(nil, uint16(len(cols)))
	for _, c := range cols {
		if c.null {
			out = writeUint8(out, 'n')
			continue
		}
		out = writeUint8(out, 't')
		out = writeUint32(out, uint32(len(c.text)))
		out = append(out, []byte(c.text)...)
	}
	return out
}

func encodeInsert(relID uint32, cols []textOrNull) []byte {
	out := []byte{'I'}
	out = writeUint32(out, relID)
	out = writeUint8(out, 'N')
	out = append(out, encodeTuple(cols)...)
	return out
}
