package auth

import (
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Sentinel errors for JWT verification failures. Tests and middleware match
// against these via errors.Is.
var (
	ErrTokenExpired      = errors.New("token expired")
	ErrInvalidSignature  = errors.New("invalid signature")
	ErrInvalidIssuer     = errors.New("invalid issuer")
	ErrInvalidAudience   = errors.New("invalid audience")
	ErrInvalidToken      = errors.New("invalid token")
	ErrMissingPrivateKey = errors.New("signer has no private key")
)

// WeaveClaims holds the namespaced custom claims for Weave JWTs. All app
// fields live under "weave" so the top level stays compatible with future
// RFC-registered claims.
type WeaveClaims struct {
	Roles         []string          `json:"roles,omitempty"`
	OntologyRoles map[string]string `json:"ontology_roles,omitempty"`
	Email         string            `json:"email,omitempty"`
	Name          string            `json:"name,omitempty"`
	// Markings carries the caller's held marking names for Foundry-style
	// mandatory access control. Middleware injects them into
	// auth.User.Attributes[security.userMarkingsKey] so the row-level
	// policy engine's RuleTypeMarkingSubset check can enforce
	// object ⊆ user at query time. Empty on tokens minted for users with
	// no marking grants; omitempty keeps those payloads compact.
	Markings []string `json:"markings,omitempty"`
	Version  int      `json:"v"`
}

// Claims is the full JWT claim set used by Weave: registered claims plus the
// "weave" custom-claim object.
type Claims struct {
	jwt.RegisteredClaims
	Weave WeaveClaims `json:"weave"`
}

// JWTSignerOptions configures issuance behaviour.
type JWTSignerOptions struct {
	Issuer         string
	Audience       string
	AccessTokenTTL time.Duration
}

// signingKey is a single entry in the JWTSigner's keyring. priv may be nil
// for verifier-only keys (e.g. retired keys still trusted for verification).
type signingKey struct {
	kid  string
	priv *rsa.PrivateKey
	pub  *rsa.PublicKey
}

// JWTSigner signs and verifies access tokens with an RSA keypair (RS256).
// US-490: the signer holds an append-only key ring. The newest entry is used
// to sign; all entries are used to verify, so a key rotation is zero-downtime
// — tokens minted under the previous key keep verifying until their natural
// expiration, and freshly minted tokens carry the new key's kid in their JOSE
// header so multi-instance deployments can decide which key to use without
// shared state.
//
// Safe for concurrent use: Sign / Verify read the ring under an RLock,
// Rotate takes the write lock.
type JWTSigner struct {
	mu       sync.RWMutex
	keys     []signingKey
	issuer   string
	audience string
	ttl      time.Duration
}

// SignInput is the per-token payload for JWTSigner.Sign. The signer fills
// the registered claims (iss, aud, exp, iat, nbf, jti).
type SignInput struct {
	UserID        string
	Email         string
	Name          string
	Roles         []string
	OntologyRoles map[string]string
	Markings      []string
}

// NewJWTSigner constructs a signer seeded with one (priv, pub) keypair. priv
// may be nil for a verifier-only signer (used by federated/JWKS deployments
// and by some tests). pub is always required.
func NewJWTSigner(priv *rsa.PrivateKey, pub *rsa.PublicKey, opts JWTSignerOptions) (*JWTSigner, error) {
	if pub == nil {
		return nil, errors.New("public key is required")
	}
	if opts.Issuer == "" {
		opts.Issuer = "weave"
	}
	if opts.Audience == "" {
		opts.Audience = "weave-api"
	}
	if opts.AccessTokenTTL == 0 {
		opts.AccessTokenTTL = 15 * time.Minute
	}
	return &JWTSigner{
		keys: []signingKey{{
			kid:  computeKid(pub),
			priv: priv,
			pub:  pub,
		}},
		issuer:   opts.Issuer,
		audience: opts.Audience,
		ttl:      opts.AccessTokenTTL,
	}, nil
}

// Rotate appends a fresh keypair to the ring and makes it the active signing
// key. Tokens previously issued under older keys keep verifying because all
// ring entries are tried during Verify. Returns the new key's kid so callers
// (admin handler, audit log) can record which key is now active.
//
// Both priv and pub are required; passing nil for either returns an error.
// The pub MUST correspond to priv — callers are expected to pass
// (k, &k.PublicKey) for a freshly generated *rsa.PrivateKey, which is what
// the admin rotate handler does.
func (s *JWTSigner) Rotate(priv *rsa.PrivateKey, pub *rsa.PublicKey) (string, error) {
	if priv == nil {
		return "", errors.New("private key is required for rotation")
	}
	if pub == nil {
		return "", errors.New("public key is required for rotation")
	}
	kid := computeKid(pub)
	s.mu.Lock()
	defer s.mu.Unlock()
	// Guard against accidental duplicate rotate: if the freshly computed
	// kid already exists, treat the call as a no-op and return the
	// existing kid rather than appending a second copy.
	for _, k := range s.keys {
		if k.kid == kid {
			return kid, nil
		}
	}
	s.keys = append(s.keys, signingKey{kid: kid, priv: priv, pub: pub})
	return kid, nil
}

// ActiveKeyID returns the kid of the newest key in the ring. This is the kid
// stamped into the JOSE header of every freshly minted token.
func (s *JWTSigner) ActiveKeyID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.keys) == 0 {
		return ""
	}
	return s.keys[len(s.keys)-1].kid
}

// KeyIDs returns the kids of every key in the ring, ordered oldest → newest.
// The slice is a copy; callers may mutate it freely.
func (s *JWTSigner) KeyIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.keys))
	for i, k := range s.keys {
		out[i] = k.kid
	}
	return out
}

// Sign issues a fresh access token. Returns ErrMissingPrivateKey if the
// active key was constructed without a private key.
func (s *JWTSigner) Sign(in SignInput) (string, error) {
	s.mu.RLock()
	active := signingKey{}
	if len(s.keys) > 0 {
		active = s.keys[len(s.keys)-1]
	}
	issuer, audience, ttl := s.issuer, s.audience, s.ttl
	s.mu.RUnlock()

	if active.priv == nil {
		return "", ErrMissingPrivateKey
	}
	now := time.Now()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   in.UserID,
			Audience:  jwt.ClaimStrings{audience},
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			NotBefore: jwt.NewNumericDate(now),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        uuid.NewString(),
		},
		Weave: WeaveClaims{
			Roles:         in.Roles,
			OntologyRoles: in.OntologyRoles,
			Email:         in.Email,
			Name:          in.Name,
			Markings:      in.Markings,
			Version:       1,
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = active.kid
	return tok.SignedString(active.priv)
}

// Verify parses and validates a token. On success returns the populated
// Claims; on failure returns one of the sentinel errors above wrapped with
// additional context.
//
// Key resolution: if the JOSE header carries a "kid" string, Verify looks up
// that kid in the ring and only attempts the matching public key. If the
// header has no kid (e.g. tokens minted by pre-US-490 signer builds), Verify
// falls back to trying every key in oldest→newest order until one succeeds.
// A token with an explicit-but-unknown kid is rejected as ErrInvalidSignature
// — that pattern matches a token issued under a key that has since been
// retired and removed from the ring.
func (s *JWTSigner) Verify(tokenString string) (*Claims, error) {
	s.mu.RLock()
	keys := make([]signingKey, len(s.keys))
	copy(keys, s.keys)
	issuer, audience := s.issuer, s.audience
	s.mu.RUnlock()

	out := &Claims{}
	parsed, err := jwt.ParseWithClaims(tokenString, out, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("%w: unexpected signing method %v", ErrInvalidSignature, t.Header["alg"])
		}
		if kidVal, ok := t.Header["kid"]; ok {
			if kid, _ := kidVal.(string); kid != "" {
				for _, k := range keys {
					if k.kid == kid {
						return k.pub, nil
					}
				}
				return nil, fmt.Errorf("%w: unknown kid %q", ErrInvalidSignature, kid)
			}
		}
		// No kid header: return a verify-any sentinel so we can try
		// each key. jwt-go does not support multi-key verification in
		// one ParseWithClaims call, so we surface the case as an error
		// and retry below.
		return nil, errNoKidFallback
	},
		jwt.WithIssuer(issuer),
		jwt.WithAudience(audience),
		jwt.WithValidMethods([]string{"RS256"}),
	)
	if err != nil && errors.Is(err, errNoKidFallback) {
		// Try every key in the ring; first success wins.
		for _, k := range keys {
			out = &Claims{}
			parsed, err = jwt.ParseWithClaims(tokenString, out, func(t *jwt.Token) (interface{}, error) {
				if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
					return nil, fmt.Errorf("%w: unexpected signing method %v", ErrInvalidSignature, t.Header["alg"])
				}
				return k.pub, nil
			},
				jwt.WithIssuer(issuer),
				jwt.WithAudience(audience),
				jwt.WithValidMethods([]string{"RS256"}),
			)
			if err == nil && parsed != nil && parsed.Valid {
				return out, nil
			}
		}
	}
	if err != nil {
		switch {
		case errors.Is(err, jwt.ErrTokenExpired):
			return nil, fmt.Errorf("%w: %v", ErrTokenExpired, err)
		case errors.Is(err, jwt.ErrTokenInvalidIssuer):
			return nil, fmt.Errorf("%w: %v", ErrInvalidIssuer, err)
		case errors.Is(err, jwt.ErrTokenInvalidAudience):
			return nil, fmt.Errorf("%w: %v", ErrInvalidAudience, err)
		case errors.Is(err, jwt.ErrTokenSignatureInvalid):
			return nil, fmt.Errorf("%w: %v", ErrInvalidSignature, err)
		case errors.Is(err, ErrInvalidSignature):
			// US-490: keyfunc surfaces unknown-kid as ErrInvalidSignature;
			// surface it untouched (jwt wraps the keyfunc error with
			// ErrTokenUnverifiable, which would otherwise fall through to
			// the default branch and lose the sentinel).
			return nil, err
		default:
			return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
		}
	}
	if !parsed.Valid {
		return nil, ErrInvalidToken
	}
	return out, nil
}

// errNoKidFallback is an internal sentinel used by Verify to signal that the
// JOSE header had no kid and the caller should fall back to trying every key
// in the ring. It is never surfaced to callers.
var errNoKidFallback = errors.New("no kid header; fallback to ring")

// computeKid derives a stable, short key id from an RSA public key by hashing
// its PKIX DER encoding with SHA-256 and hex-encoding the first 8 bytes. The
// id is byte-identical across process restarts as long as the key material is
// unchanged, so JWKS publishers and rolling deployments converge on the same
// kid for the same key.
func computeKid(pub *rsa.PublicKey) string {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		// Should be unreachable for a well-formed *rsa.PublicKey;
		// fall back to a random kid so signing still works.
		return uuid.NewString()
	}
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:8])
}
