package audit

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// RootFileEntry is one parsed `<day>\t<hex>` line from the append-only
// root-hash file produced by RootHashPublisher.
type RootFileEntry struct {
	Day      string // YYYY-MM-DD
	RootHash string // hex sha256
}

// ParseRootFile consumes the append-only root-hash file produced by
// RootHashPublisher and returns every entry in appearance order.
// Malformed lines (anything other than exactly `<day>\t<hex>`) are
// rejected — the file is a compliance artefact and silently dropping
// unrecognised lines would hide corruption. Blank lines are tolerated.
func ParseRootFile(r io.Reader) ([]RootFileEntry, error) {
	var entries []RootFileEntry
	scanner := bufio.NewScanner(r)
	// Bump the buffer so single-line hashes longer than 64kb still parse;
	// not expected in practice but cheap insurance against future schema
	// growth (JSON-per-day, etc).
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		s := strings.TrimRight(scanner.Text(), "\r")
		if s == "" {
			continue
		}
		parts := strings.Split(s, "\t")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("audit: malformed root-file line %d: %q", line, s)
		}
		entries = append(entries, RootFileEntry{Day: parts[0], RootHash: parts[1]})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("audit: read root-file: %w", err)
	}
	return entries, nil
}

// VerifyRootFile cross-checks every entry in the parsed root-file
// against a precomputed `day -> events` map of the live DB chain.
// Returns nil when every anchored root hash matches the recomputed root
// over the corresponding DB rows. Extra days in `daily` that are NOT
// anchored in the file are allowed (events written after the last
// publication haven't been anchored yet).
func VerifyRootFile(entries []RootFileEntry, daily map[string][]AuditEvent) error {
	for _, e := range entries {
		events, ok := daily[e.Day]
		if !ok || len(events) == 0 {
			return fmt.Errorf("audit root mismatch on %s: file anchors root %s but DB has no events for that day",
				e.Day, e.RootHash)
		}
		got := ComputeRootHash(events)
		if got != e.RootHash {
			return fmt.Errorf("audit root mismatch on %s: file anchors %s, DB recomputes %s",
				e.Day, e.RootHash, got)
		}
	}
	return nil
}

// GroupEventsByUTCDay buckets events by the UTC calendar day of their
// Timestamp. Useful for feeding VerifyRootFile against a single
// ListChain dump.
func GroupEventsByUTCDay(events []AuditEvent) map[string][]AuditEvent {
	out := map[string][]AuditEvent{}
	for _, e := range events {
		key := e.Timestamp.UTC().Format("2006-01-02")
		out[key] = append(out[key], e)
	}
	return out
}
