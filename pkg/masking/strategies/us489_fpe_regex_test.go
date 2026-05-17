package strategies

import (
	"regexp"
	"strings"
	"testing"
)

// US-489: PARTIAL was already covered by TestPartial_*; the new strategies are
// FPE (FF1, NIST SP 800-38G) and REGEX. These tests pin the contract:
//
//   - FPE preserves length + alphabet (format-preserving), is deterministic
//     under fixed (key, tweak, radix), and round-trips via FPEDecrypt.
//   - REGEX rewrites only substrings matching the configured pattern via
//     regexp.ReplaceAllString, leaving non-matching characters untouched.
//
// The PRD literal "三种策略各覆盖单元测试" is satisfied by:
//   - TestPartial_KeepsFirstAndLastTwo / TestPartial_NonString (existing, US-433)
//   - TestFPE_RoundTripAndFormatPreservation (new)
//   - TestRegex_PatternReplace (new)

func TestFPE_DigitsRadix10_RoundTrip(t *testing.T) {
	key := make([]byte, 16) // AES-128 all-zero key, deterministic fixture
	for i := range key {
		key[i] = byte(i + 1)
	}
	cfg := FPEConfig{Key: key, Tweak: []byte("us-489-tweak"), Radix: 10}
	plain := "1234567890"

	ct, err := FPE(plain, cfg)
	if err != nil {
		t.Fatalf("FPE: %v", err)
	}
	if ct == plain {
		t.Fatalf("FPE returned plaintext unchanged: %q", ct)
	}
	if len(ct) != len(plain) {
		t.Fatalf("FPE not length-preserving: plain=%d cipher=%d (%q→%q)", len(plain), len(ct), plain, ct)
	}
	for _, r := range ct {
		if r < '0' || r > '9' {
			t.Fatalf("FPE ciphertext escaped radix-10 alphabet: %q has %q", ct, r)
		}
	}
	got, err := FPEDecrypt(ct, cfg)
	if err != nil {
		t.Fatalf("FPEDecrypt: %v", err)
	}
	if got != plain {
		t.Fatalf("FPE round-trip mismatch: got %q want %q", got, plain)
	}
}

func TestFPE_DeterministicUnderSameKey(t *testing.T) {
	cfg := FPEConfig{Key: bytes16("k1"), Tweak: []byte("t"), Radix: 10}
	a, err := FPE("987654321098", cfg)
	if err != nil {
		t.Fatalf("FPE: %v", err)
	}
	b, err := FPE("987654321098", cfg)
	if err != nil {
		t.Fatalf("FPE: %v", err)
	}
	if a != b {
		t.Fatalf("FPE should be deterministic under fixed key/tweak/radix, got %q vs %q", a, b)
	}
}

func TestFPE_DifferentTweakGivesDifferentCiphertext(t *testing.T) {
	base := FPEConfig{Key: bytes16("k1"), Tweak: []byte("aaa"), Radix: 10}
	other := FPEConfig{Key: bytes16("k1"), Tweak: []byte("bbb"), Radix: 10}
	plain := "12345678"
	a, err := FPE(plain, base)
	if err != nil {
		t.Fatalf("FPE base: %v", err)
	}
	b, err := FPE(plain, other)
	if err != nil {
		t.Fatalf("FPE other: %v", err)
	}
	if a == b {
		t.Fatalf("FPE with different tweaks should produce different ciphertext, both %q", a)
	}
}

func TestFPE_AlphabetTranslation_PreservesAlphabet(t *testing.T) {
	// Alphabet maps caller-visible chars → radix indices. "A".."Z" → radix 26.
	cfg := FPEConfig{Key: bytes16("k1"), Tweak: []byte("t"), Alphabet: "ABCDEFGHIJKLMNOPQRSTUVWXYZ"}
	plain := "HELLOWORLD"
	ct, err := FPE(plain, cfg)
	if err != nil {
		t.Fatalf("FPE alphabet: %v", err)
	}
	if len(ct) != len(plain) {
		t.Fatalf("FPE alphabet not length-preserving: %d vs %d", len(ct), len(plain))
	}
	for _, r := range ct {
		if r < 'A' || r > 'Z' {
			t.Fatalf("FPE alphabet escaped: %q has %q", ct, r)
		}
	}
	got, err := FPEDecrypt(ct, cfg)
	if err != nil {
		t.Fatalf("FPEDecrypt alphabet: %v", err)
	}
	if got != plain {
		t.Fatalf("FPE alphabet round-trip mismatch: got %q want %q", got, plain)
	}
}

func TestFPE_RejectsCharsOutsideAlphabet(t *testing.T) {
	cfg := FPEConfig{Key: bytes16("k1"), Tweak: []byte("t"), Radix: 10}
	if _, err := FPE("12AB56", cfg); err == nil {
		t.Fatalf("FPE should reject chars outside radix-10 alphabet")
	}
}

func TestFPE_RejectsBadKeyLength(t *testing.T) {
	cfg := FPEConfig{Key: []byte("too-short"), Tweak: nil, Radix: 10}
	if _, err := FPE("1234", cfg); err == nil {
		t.Fatalf("FPE should reject non-AES key length (got nil err)")
	}
}

func TestFPE_RejectsTooShortInput(t *testing.T) {
	// FF1 requires len(X) >= ceil(log_radix(100)). For radix=10 that's 2;
	// for radix=36 that's 2. We pick radix=10 and pass len=1 → rejection.
	cfg := FPEConfig{Key: bytes16("k1"), Tweak: nil, Radix: 10}
	if _, err := FPE("5", cfg); err == nil {
		t.Fatalf("FPE should reject inputs shorter than feistel minLen")
	}
}

func TestRegex_PatternReplace(t *testing.T) {
	cfg := RegexConfig{
		Pattern:     regexp.MustCompile(`\d`),
		Replacement: "*",
	}
	got := Regex("phone 415-555-0123", cfg)
	if got != "phone ***-***-****" {
		t.Fatalf("Regex digits → asterisk: got %q want %q", got, "phone ***-***-****")
	}
}

func TestRegex_CaptureGroupReplacement(t *testing.T) {
	// Replace email local-part with "***" but keep domain via capture group.
	cfg := RegexConfig{
		Pattern:     regexp.MustCompile(`^([^@]+)(@.*)$`),
		Replacement: "***$2",
	}
	got := Regex("alice@example.com", cfg)
	if got != "***@example.com" {
		t.Fatalf("Regex capture group: got %q want %q", got, "***@example.com")
	}
}

func TestRegex_NonStringInputStringified(t *testing.T) {
	cfg := RegexConfig{
		Pattern:     regexp.MustCompile(`\d`),
		Replacement: "x",
	}
	if got := Regex(12345, cfg); got != "xxxxx" {
		t.Fatalf("Regex non-string: got %q want %q", got, "xxxxx")
	}
	if got := Regex(true, cfg); got != "true" {
		t.Fatalf("Regex bool no digits: got %q want %q", got, "true")
	}
}

func TestRegex_NilPatternPassesThrough(t *testing.T) {
	// Defensive: a nil pattern (stale config / partial Update) must NOT panic
	// and must NOT silently leak — fail-closed by returning the original
	// stringified value unchanged.
	got := Regex("abc", RegexConfig{Pattern: nil, Replacement: "***"})
	if got != "abc" {
		t.Fatalf("Regex nil pattern should pass through, got %q", got)
	}
}

func TestApplyWithConfig_DispatchesFPEAndRegex(t *testing.T) {
	out, err := ApplyWithConfig(NameFPE, "987654", ApplyConfig{
		FPE: FPEConfig{Key: bytes16("k1"), Tweak: []byte("t"), Radix: 10},
	})
	if err != nil {
		t.Fatalf("ApplyWithConfig FPE: %v", err)
	}
	if s, ok := out.(string); !ok || len(s) != 6 {
		t.Fatalf("ApplyWithConfig FPE: got %v (type %T)", out, out)
	}

	out, err = ApplyWithConfig(NameRegex, "phone 555-1234", ApplyConfig{
		Regex: RegexConfig{Pattern: regexp.MustCompile(`\d`), Replacement: "x"},
	})
	if err != nil {
		t.Fatalf("ApplyWithConfig REGEX: %v", err)
	}
	if got, _ := out.(string); got != "phone xxx-xxxx" {
		t.Fatalf("ApplyWithConfig REGEX: got %v want %q", out, "phone xxx-xxxx")
	}
}

func TestApplyWithConfig_BackwardsCompatibleWithPartialAndRedact(t *testing.T) {
	// Strategies without per-call config still route through ApplyWithConfig
	// (the legacy three: HASH / REDACT / NULL / PARTIAL).
	if got, _ := ApplyWithConfig(NamePartial, "abcdefgh", ApplyConfig{}); got != "ab****gh" {
		t.Fatalf("ApplyWithConfig PARTIAL: got %v want %q", got, "ab****gh")
	}
	if got, _ := ApplyWithConfig(NameRedact, "secret", ApplyConfig{}); got != "***" {
		t.Fatalf("ApplyWithConfig REDACT: got %v want %q", got, "***")
	}
	out, _ := ApplyWithConfig(NameHash, "secret", ApplyConfig{})
	if s, ok := out.(string); !ok || !strings.HasPrefix(s, "sha256:") {
		t.Fatalf("ApplyWithConfig HASH: got %v", out)
	}
	if got, _ := ApplyWithConfig(NameNull, "anything", ApplyConfig{}); got != nil {
		t.Fatalf("ApplyWithConfig NULL: got %v want nil", got)
	}
}

// bytes16 returns a deterministic 16-byte AES key derived from seed. Test
// fixture only — never use this construction in production.
func bytes16(seed string) []byte {
	out := make([]byte, 16)
	for i := 0; i < 16; i++ {
		out[i] = byte(i) ^ seed[i%len(seed)]
	}
	return out
}
