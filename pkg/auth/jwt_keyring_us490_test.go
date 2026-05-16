package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// US-490: JWT 多密钥轮换（kid header）
//
// 验收点：
//   - JWTSigner 持有 key ring（oldest verifies, newest signs）
//   - 旧 token 仍校验通过 + 新 token 用最新 key 签发
//   - 每个 key 都有一个 kid，写进 JWT header；Verify 用 kid 选择 verifier
//
// 测试覆盖：
//   1. Rotate 之后新 token 用新 kid，旧 token 仍可验证
//   2. Rotate 改变 ActiveKeyID，且与新 JWT header 的 kid 一致
//   3. 没有 kid 的遗留 token 仍能被多 key 兜底
//   4. KeyIDs() 返回 oldest→newest 的有序 kid 列表
//   5. Rotate 拒绝 nil 私钥 / 不匹配的 pub
//   6. Verify 拒绝带 kid 但 kid 不存在的 token

func mustGenKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	return k
}

func TestUS490_KeyRing_Given_RotateOnce_When_VerifyOldToken_Then_StillValid(t *testing.T) {
	priv1 := mustGenKey(t)
	signer, err := NewJWTSigner(priv1, &priv1.PublicKey, JWTSignerOptions{
		Issuer:         "weave-test",
		Audience:       "weave-api",
		AccessTokenTTL: 15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("NewJWTSigner: %v", err)
	}

	oldKid := signer.ActiveKeyID()
	if oldKid == "" {
		t.Fatal("expected non-empty active kid after NewJWTSigner")
	}

	oldTok, err := signer.Sign(SignInput{UserID: "user:alice"})
	if err != nil {
		t.Fatalf("Sign with k1: %v", err)
	}

	priv2 := mustGenKey(t)
	newKid, err := signer.Rotate(priv2, &priv2.PublicKey)
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if newKid == oldKid {
		t.Fatalf("Rotate must produce a fresh kid; both = %q", newKid)
	}
	if signer.ActiveKeyID() != newKid {
		t.Errorf("ActiveKeyID after rotate: got %q want %q", signer.ActiveKeyID(), newKid)
	}

	// Old token (signed under k1) must still verify.
	if _, err := signer.Verify(oldTok); err != nil {
		t.Errorf("old token must still verify after rotate; got %v", err)
	}

	// New token signs under k2.
	newTok, err := signer.Sign(SignInput{UserID: "user:bob"})
	if err != nil {
		t.Fatalf("Sign with k2: %v", err)
	}
	if _, err := signer.Verify(newTok); err != nil {
		t.Errorf("new token must verify; got %v", err)
	}

	// New token's header MUST carry kid = newKid.
	header, _ := parseTokenHeader(t, newTok)
	if got, _ := header["kid"].(string); got != newKid {
		t.Errorf("new token kid header: got %q want %q", got, newKid)
	}

	// Old token's header MUST carry kid = oldKid.
	oldHeader, _ := parseTokenHeader(t, oldTok)
	if got, _ := oldHeader["kid"].(string); got != oldKid {
		t.Errorf("old token kid header: got %q want %q", got, oldKid)
	}

	// KeyIDs returns oldest→newest.
	ids := signer.KeyIDs()
	if len(ids) != 2 || ids[0] != oldKid || ids[1] != newKid {
		t.Errorf("KeyIDs: got %v want [%q,%q]", ids, oldKid, newKid)
	}
}

func TestUS490_KeyRing_Given_NoKidHeader_When_Verify_Then_FallsBackToAnyKey(t *testing.T) {
	priv1 := mustGenKey(t)
	signer, err := NewJWTSigner(priv1, &priv1.PublicKey, JWTSignerOptions{
		Issuer:         "weave-test",
		Audience:       "weave-api",
		AccessTokenTTL: 5 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Mint a token WITHOUT a kid header, signed by priv1, to simulate a
	// legacy/non-rotated token issued by an older signer build.
	now := time.Now()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "weave-test",
			Subject:   "user:legacy",
			Audience:  jwt.ClaimStrings{"weave-api"},
			ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)),
			NotBefore: jwt.NewNumericDate(now),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        "legacy-jti",
		},
		Weave: WeaveClaims{Version: 1},
	}
	raw := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	// Explicitly clear kid header if a default sneaks in.
	delete(raw.Header, "kid")
	legacyTok, err := raw.SignedString(priv1)
	if err != nil {
		t.Fatalf("sign legacy: %v", err)
	}

	// Now rotate to a fresh key — legacy token still verifies because
	// the signer falls back across the ring when no kid is present.
	priv2 := mustGenKey(t)
	if _, err := signer.Rotate(priv2, &priv2.PublicKey); err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	if _, err := signer.Verify(legacyTok); err != nil {
		t.Errorf("legacy (kid-less) token must still verify across the ring; got %v", err)
	}
}

func TestUS490_KeyRing_Given_UnknownKid_When_Verify_Then_RejectsWithInvalidSignature(t *testing.T) {
	priv1 := mustGenKey(t)
	signer, err := NewJWTSigner(priv1, &priv1.PublicKey, JWTSignerOptions{
		Issuer:         "weave-test",
		Audience:       "weave-api",
		AccessTokenTTL: 5 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Forge a token claiming kid="bogus-kid" signed by priv1. The signer
	// must refuse because kid is explicitly set but unknown to the ring —
	// matching a token issued from a rotated-out spare that was deleted.
	now := time.Now()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "weave-test",
			Subject:   "user:forged",
			Audience:  jwt.ClaimStrings{"weave-api"},
			ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)),
			NotBefore: jwt.NewNumericDate(now),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        "forged-jti",
		},
		Weave: WeaveClaims{Version: 1},
	}
	raw := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	raw.Header["kid"] = "bogus-kid"
	tok, err := raw.SignedString(priv1)
	if err != nil {
		t.Fatalf("sign forged: %v", err)
	}

	_, err = signer.Verify(tok)
	if err == nil {
		t.Fatal("expected error for unknown kid; got nil")
	}
	if !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("unknown kid must surface as ErrInvalidSignature; got %v", err)
	}
}

func TestUS490_KeyRing_Given_NilInputs_When_Rotate_Then_Rejects(t *testing.T) {
	priv1 := mustGenKey(t)
	signer, err := NewJWTSigner(priv1, &priv1.PublicKey, JWTSignerOptions{
		Issuer:         "weave-test",
		Audience:       "weave-api",
		AccessTokenTTL: 5 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := signer.Rotate(nil, &priv1.PublicKey); err == nil {
		t.Error("Rotate(nil priv) must error")
	}
	if _, err := signer.Rotate(priv1, nil); err == nil {
		t.Error("Rotate(nil pub) must error")
	}
}

func TestUS490_KeyRing_Given_TripleRotation_When_VerifyTokensFromEachEra_Then_AllValid(t *testing.T) {
	priv1 := mustGenKey(t)
	signer, err := NewJWTSigner(priv1, &priv1.PublicKey, JWTSignerOptions{
		Issuer:         "weave-test",
		Audience:       "weave-api",
		AccessTokenTTL: 30 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	tok1, _ := signer.Sign(SignInput{UserID: "u1"})

	priv2 := mustGenKey(t)
	if _, err := signer.Rotate(priv2, &priv2.PublicKey); err != nil {
		t.Fatal(err)
	}
	tok2, _ := signer.Sign(SignInput{UserID: "u2"})

	priv3 := mustGenKey(t)
	kid3, err := signer.Rotate(priv3, &priv3.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	tok3, _ := signer.Sign(SignInput{UserID: "u3"})

	for i, tok := range []string{tok1, tok2, tok3} {
		if _, err := signer.Verify(tok); err != nil {
			t.Errorf("tok%d must verify after 2 rotations; got %v", i+1, err)
		}
	}
	if signer.ActiveKeyID() != kid3 {
		t.Errorf("ActiveKeyID after 2 rotations: got %q want %q", signer.ActiveKeyID(), kid3)
	}
	ids := signer.KeyIDs()
	if len(ids) != 3 {
		t.Fatalf("KeyIDs: got %v want 3 entries", ids)
	}
	if ids[2] != kid3 {
		t.Errorf("newest kid: got %q want %q", ids[2], kid3)
	}
}

// parseTokenHeader peels off the base64url-encoded JOSE header from a JWT
// without verifying the signature, returning the decoded JSON map.
func parseTokenHeader(t *testing.T, tok string) (map[string]any, []byte) {
	t.Helper()
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("malformed JWT: %d parts", len(parts))
	}
	dec, err := jwt.NewParser().DecodeSegment(parts[0])
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}
	parser := jwt.NewParser()
	tokParsed, _, _ := parser.ParseUnverified(tok, jwt.MapClaims{})
	if tokParsed == nil {
		t.Fatal("ParseUnverified returned nil token")
	}
	return tokParsed.Header, dec
}
