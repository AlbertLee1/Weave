package auth

import (
	"crypto/rsa"
	"errors"
	"fmt"
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

// JWTSigner signs and verifies access tokens with an RSA keypair (RS256).
// It is safe for concurrent use.
type JWTSigner struct {
	priv     *rsa.PrivateKey
	pub      *rsa.PublicKey
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

// NewJWTSigner constructs a signer. priv may be nil for a verifier-only
// signer (used by federated/JWKS deployments and by some tests). pub is
// always required.
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
		priv:     priv,
		pub:      pub,
		issuer:   opts.Issuer,
		audience: opts.Audience,
		ttl:      opts.AccessTokenTTL,
	}, nil
}

// Sign issues a fresh access token. Returns ErrMissingPrivateKey if the
// signer was constructed without a private key.
func (s *JWTSigner) Sign(in SignInput) (string, error) {
	if s.priv == nil {
		return "", ErrMissingPrivateKey
	}
	now := time.Now()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   in.UserID,
			Audience:  jwt.ClaimStrings{s.audience},
			ExpiresAt: jwt.NewNumericDate(now.Add(s.ttl)),
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
	return tok.SignedString(s.priv)
}

// Verify parses and validates a token. On success returns the populated
// Claims; on failure returns one of the sentinel errors above wrapped with
// additional context.
func (s *JWTSigner) Verify(tokenString string) (*Claims, error) {
	out := &Claims{}
	parsed, err := jwt.ParseWithClaims(tokenString, out, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("%w: unexpected signing method %v", ErrInvalidSignature, t.Header["alg"])
		}
		return s.pub, nil
	},
		jwt.WithIssuer(s.issuer),
		jwt.WithAudience(s.audience),
		jwt.WithValidMethods([]string{"RS256"}),
	)
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
		default:
			return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
		}
	}
	if !parsed.Valid {
		return nil, ErrInvalidToken
	}
	return out, nil
}
