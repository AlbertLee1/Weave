package pagination

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Sentinel errors so HTTP handlers can map cursor-integrity failures to
// 400 Bad Request rather than swallowing them as 500. Tampered = HMAC
// mismatch or missing signature when one is required. Expired = the
// cursor's IssuedAt + the configured MaxAge is in the past.
var (
	ErrTamperedCursor = errors.New("invalid cursor: tampered")
	ErrExpiredCursor  = errors.New("invalid cursor: expired")
)

// Cursor represents a pagination cursor that encodes position info.
type Cursor struct {
	Offset   int    `json:"o"`
	IssuedAt int64  `json:"iat,omitempty"`
	Sig      string `json:"sig,omitempty"`
}

var (
	sigMu      sync.RWMutex
	signingKey []byte
	maxAge     time.Duration
)

// SetSigningKey enables HMAC integrity for cursor encoding/decoding.
// Passing an empty key (len(k)==0) disables HMAC and restores the
// plain base64+JSON wire format used before signing was introduced.
// Existing v2 callers see no schema change unless a key is set.
func SetSigningKey(k []byte) {
	sigMu.Lock()
	defer sigMu.Unlock()
	if len(k) == 0 {
		signingKey = nil
		return
	}
	signingKey = append([]byte(nil), k...)
}

// SetMaxAge bounds how long an issued cursor remains decodable. A zero
// duration disables expiry. When set, Encode stamps IssuedAt and Decode
// rejects cursors older than the window with ErrExpiredCursor.
func SetMaxAge(d time.Duration) {
	sigMu.Lock()
	defer sigMu.Unlock()
	maxAge = d
}

func currentSigningKey() []byte {
	sigMu.RLock()
	defer sigMu.RUnlock()
	if len(signingKey) == 0 {
		return nil
	}
	out := make([]byte, len(signingKey))
	copy(out, signingKey)
	return out
}

func currentMaxAge() time.Duration {
	sigMu.RLock()
	defer sigMu.RUnlock()
	return maxAge
}

func signPayload(body, key []byte) string {
	h := hmac.New(sha256.New, key)
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// Encode serializes the cursor to a base64 string. Encode never mutates
// its receiver; the IssuedAt and Sig fields are computed onto a copy so
// callers can reuse a cursor across pages without leaking stale state.
func (c *Cursor) Encode() string {
	out := *c
	if currentMaxAge() > 0 && out.IssuedAt == 0 {
		out.IssuedAt = time.Now().Unix()
	}
	if key := currentSigningKey(); key != nil {
		out.Sig = ""
		body, _ := json.Marshal(out)
		out.Sig = signPayload(body, key)
	}
	data, _ := json.Marshal(out)
	return base64.URLEncoding.EncodeToString(data)
}

// DecodeCursor parses a base64 cursor string back into a Cursor.
func DecodeCursor(s string) (*Cursor, error) {
	if s == "" {
		return &Cursor{Offset: 0}, nil
	}
	data, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor: %w", err)
	}
	var c Cursor
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("invalid cursor: %w", err)
	}
	if c.Offset < 0 {
		return nil, fmt.Errorf("invalid cursor: negative offset")
	}
	if key := currentSigningKey(); key != nil {
		if c.Sig == "" {
			return nil, ErrTamperedCursor
		}
		unsigned := c
		unsigned.Sig = ""
		body, _ := json.Marshal(unsigned)
		expected := signPayload(body, key)
		if !hmac.Equal([]byte(expected), []byte(c.Sig)) {
			return nil, ErrTamperedCursor
		}
	}
	if age := currentMaxAge(); age > 0 && c.IssuedAt > 0 {
		if time.Since(time.Unix(c.IssuedAt, 0)) > age {
			return nil, ErrExpiredCursor
		}
	}
	return &c, nil
}
