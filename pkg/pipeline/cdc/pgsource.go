package cdc

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
)

// PGSource implements Source on top of a PostgreSQL logical
// replication slot using the pgoutput plugin. The connection must
// have been opened with `replication=database` in the DSN — pgconn
// puts the connection into walsender mode automatically when this
// flag is present.
//
// Lifecycle:
//
//	1. NewPGSource(conn, opts) — wraps the open replication conn.
//	2. EnsureSlot(ctx)         — creates the logical slot if missing.
//	3. EnsurePublication(ctx)  — creates the publication if missing
//	                             (DDL must run on a normal conn; this
//	                             helper is a no-op when AdminConn is
//	                             not supplied).
//	4. Start(ctx)              — IDENTIFY_SYSTEM + START_REPLICATION
//	5. Next(ctx) / CommitWatermark / Close — the Source contract.
//
// Logical replication setup at the database is the operator's
// responsibility (`wal_level=logical`, `max_replication_slots > 0`,
// `max_wal_senders > 0`). The package fails with a clear error if
// any of these prerequisites are missing.
type PGSource struct {
	conn      *pgconn.PgConn
	slotName  string
	pubName   string
	startLSN  pglogrepl.LSN
	keepalive time.Duration

	mu       sync.Mutex
	clientLSN pglogrepl.LSN
	nextDeadline time.Time
}

// PGSourceOptions tunes PGSource construction.
type PGSourceOptions struct {
	// SlotName is the logical replication slot name. Required.
	SlotName string
	// PublicationName is the pgoutput publication name. Required.
	PublicationName string
	// StartLSN is the resume position. The zero value means "wherever
	// the slot currently sits"; pass a known LSN to fast-forward past
	// already-applied transactions on a fresh receiver attached to a
	// pre-existing slot.
	StartLSN pglogrepl.LSN
	// KeepaliveInterval bounds how often the source sends a
	// StandbyStatusUpdate even when no transactions arrive. Defaults
	// to 10s — short enough for `pg_stat_replication` to see the
	// receiver as live, long enough to be invisible at PG load.
	KeepaliveInterval time.Duration
}

// NewPGSource wraps an open replication connection. The caller owns
// conn lifecycle — Source.Close calls conn.Close so the receiver's
// shutdown drains the connection cleanly, but a still-running pump
// can be aborted by closing conn directly.
func NewPGSource(conn *pgconn.PgConn, opts PGSourceOptions) (*PGSource, error) {
	if conn == nil {
		return nil, errors.New("cdc: PGSource requires a non-nil pgconn.PgConn")
	}
	if opts.SlotName == "" {
		return nil, errors.New("cdc: PGSource.SlotName must not be empty")
	}
	if opts.PublicationName == "" {
		return nil, errors.New("cdc: PGSource.PublicationName must not be empty")
	}
	keepalive := opts.KeepaliveInterval
	if keepalive <= 0 {
		keepalive = 10 * time.Second
	}
	return &PGSource{
		conn:      conn,
		slotName:  opts.SlotName,
		pubName:   opts.PublicationName,
		startLSN:  opts.StartLSN,
		keepalive: keepalive,
		clientLSN: opts.StartLSN,
	}, nil
}

// EnsureSlot creates the logical replication slot if it does not
// exist. Existing slots are left untouched so a receiver crash + restart
// resumes from the slot's confirmed_flush_lsn (the standard PG
// guarantee — once a slot exists, PG retains every WAL record beyond
// confirmed_flush_lsn until the receiver acknowledges them).
//
// adminConn is a NORMAL (non-replication) connection used for the
// SQL-level slot existence check. The CREATE_REPLICATION_SLOT
// command runs on the replication connection itself.
func (s *PGSource) EnsureSlot(ctx context.Context, adminConn *pgconn.PgConn) error {
	if adminConn == nil {
		return errors.New("cdc: EnsureSlot requires a non-nil admin connection")
	}
	exists, err := slotExists(ctx, adminConn, s.slotName)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err = pglogrepl.CreateReplicationSlot(ctx, s.conn, s.slotName, "pgoutput", pglogrepl.CreateReplicationSlotOptions{
		Temporary: false,
	})
	if err != nil {
		return fmt.Errorf("cdc: create replication slot %q: %w", s.slotName, err)
	}
	return nil
}

// Start runs IDENTIFY_SYSTEM (to confirm the connection is in
// walsender mode) and START_REPLICATION (to begin streaming).
func (s *PGSource) Start(ctx context.Context) error {
	sysident, err := pglogrepl.IdentifySystem(ctx, s.conn)
	if err != nil {
		return fmt.Errorf("cdc: IDENTIFY_SYSTEM: %w", err)
	}
	startLSN := s.startLSN
	if startLSN == 0 {
		startLSN = sysident.XLogPos
	}
	pluginArgs := []string{
		"proto_version '1'",
		fmt.Sprintf("publication_names '%s'", s.pubName),
	}
	err = pglogrepl.StartReplication(ctx, s.conn, s.slotName, startLSN, pglogrepl.StartReplicationOptions{
		PluginArgs: pluginArgs,
	})
	if err != nil {
		return fmt.Errorf("cdc: START_REPLICATION on slot %q: %w", s.slotName, err)
	}
	s.mu.Lock()
	if s.clientLSN < startLSN {
		s.clientLSN = startLSN
	}
	s.nextDeadline = time.Now().Add(s.keepalive)
	s.mu.Unlock()
	return nil
}

// Next blocks until the next pgoutput-bearing CopyData arrives, then
// returns its WALData payload. Keepalive messages are answered
// in-line (so the receiver loop never sees them) and the loop
// re-blocks. ctx cancellation surfaces immediately.
func (s *PGSource) Next(ctx context.Context) ([]byte, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		s.mu.Lock()
		deadline := s.nextDeadline
		s.mu.Unlock()
		if deadline.IsZero() {
			deadline = time.Now().Add(s.keepalive)
		}
		recvCtx, cancel := context.WithDeadline(ctx, deadline)
		msg, err := s.conn.ReceiveMessage(recvCtx)
		cancel()
		if err != nil {
			if pgconn.Timeout(err) {
				if err := s.sendStandbyStatus(ctx); err != nil {
					return nil, err
				}
				continue
			}
			return nil, err
		}
		copyData, ok := msg.(*pgproto3.CopyData)
		if !ok {
			// Server can interleave NoticeResponse, CommandComplete,
			// etc. Drop quietly and re-block.
			continue
		}
		switch copyData.Data[0] {
		case pglogrepl.PrimaryKeepaliveMessageByteID:
			pkm, err := pglogrepl.ParsePrimaryKeepaliveMessage(copyData.Data[1:])
			if err != nil {
				return nil, fmt.Errorf("cdc: parse keepalive: %w", err)
			}
			if pkm.ReplyRequested {
				if err := s.sendStandbyStatus(ctx); err != nil {
					return nil, err
				}
			}
			continue
		case pglogrepl.XLogDataByteID:
			xld, err := pglogrepl.ParseXLogData(copyData.Data[1:])
			if err != nil {
				return nil, fmt.Errorf("cdc: parse XLogData: %w", err)
			}
			s.mu.Lock()
			if xld.WALStart > s.clientLSN {
				s.clientLSN = xld.WALStart
			}
			s.mu.Unlock()
			return xld.WALData, nil
		default:
			// Unknown copy-data byte; PG protocol reserves none other
			// than the two above today, so just drop and continue.
			continue
		}
	}
}

// CommitWatermark records the LSN the receiver has fully published
// upstream, then sends a StandbyStatusUpdate so PG advances
// confirmed_flush_lsn. Failure to send aborts the receiver — losing
// the watermark would replay the same transactions on next start,
// duplicating edits in NATS.
func (s *PGSource) CommitWatermark(ctx context.Context, lsn pglogrepl.LSN) error {
	s.mu.Lock()
	if lsn > s.clientLSN {
		s.clientLSN = lsn
	}
	s.mu.Unlock()
	return s.sendStandbyStatus(ctx)
}

// Close terminates the replication connection.
func (s *PGSource) Close(ctx context.Context) error {
	if s.conn == nil {
		return nil
	}
	return s.conn.Close(ctx)
}

func (s *PGSource) sendStandbyStatus(ctx context.Context) error {
	s.mu.Lock()
	pos := s.clientLSN
	s.nextDeadline = time.Now().Add(s.keepalive)
	s.mu.Unlock()
	err := pglogrepl.SendStandbyStatusUpdate(ctx, s.conn, pglogrepl.StandbyStatusUpdate{
		WALWritePosition: pos,
		WALFlushPosition: pos,
		WALApplyPosition: pos,
		ClientTime:       time.Now(),
	})
	if err != nil {
		return fmt.Errorf("cdc: send standby status: %w", err)
	}
	return nil
}

// slotExists checks pg_replication_slots for slotName via the supplied
// admin connection (a normal pgconn, NOT a replication connection).
func slotExists(ctx context.Context, conn *pgconn.PgConn, slotName string) (bool, error) {
	mrr := conn.Exec(ctx, fmt.Sprintf(
		"SELECT 1 FROM pg_replication_slots WHERE slot_name = %s",
		quoteSQLLiteral(slotName),
	))
	results, err := mrr.ReadAll()
	if err != nil {
		return false, fmt.Errorf("cdc: query pg_replication_slots: %w", err)
	}
	for _, r := range results {
		if len(r.Rows) > 0 {
			return true, nil
		}
	}
	return false, nil
}

// quoteSQLLiteral renders s as a single-quoted SQL literal with
// embedded single quotes doubled. Used only for the slot-name
// identity check above where parameter binding isn't available on
// pgconn.Exec.
func quoteSQLLiteral(s string) string {
	out := make([]byte, 0, len(s)+2)
	out = append(out, '\'')
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' {
			out = append(out, '\'', '\'')
			continue
		}
		out = append(out, s[i])
	}
	out = append(out, '\'')
	return string(out)
}
