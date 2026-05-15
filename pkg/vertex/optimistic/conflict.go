// Package optimistic implements the If-Match optimistic-locking check
// the Vertex PUT /graphs/{rid} handler enforces. A stale client returns
// 409 + "Graph has been updated. Reload or merge.".
package optimistic

import (
	"errors"
	"strconv"
	"strings"
)

// ErrVersionConflict indicates client and server versions disagree.
var ErrVersionConflict = errors.New("version conflict")

// ErrInvalidIfMatch indicates the header value did not parse.
var ErrInvalidIfMatch = errors.New("invalid If-Match header")

// CheckIfMatch returns nil when client and server versions match,
// ErrVersionConflict otherwise (client ahead also treated as a conflict
// because the server is the source of truth).
func CheckIfMatch(clientVersion, serverVersion int) error {
	if clientVersion != serverVersion {
		return ErrVersionConflict
	}
	return nil
}

// ParseIfMatchHeader extracts an integer version from an If-Match header.
// Accepts the bare integer ("5"), the quoted ETag ("\"5\""), and the
// weak ETag ("W/\"5\"") forms. Returns ErrInvalidIfMatch on empty,
// non-numeric, or negative values.
func ParseIfMatchHeader(raw string) (int, error) {
	if raw == "" {
		return 0, ErrInvalidIfMatch
	}
	s := raw
	if strings.HasPrefix(s, "W/") {
		s = s[2:]
	}
	s = strings.Trim(s, `"`)
	v, err := strconv.Atoi(s)
	if err != nil || v < 0 {
		return 0, ErrInvalidIfMatch
	}
	return v, nil
}
