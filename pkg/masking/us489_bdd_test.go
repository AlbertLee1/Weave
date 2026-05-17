package masking_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/masking"
	"github.com/liyang/weave/pkg/masking/strategies"
)

// TestBDD_US489_PartialFPERegex_AppliedTogether is the US-489 BDD acceptance
// scenario. It exercises the canonical "row of properties + transform plan"
// contract callers (cellsec engine, column-mask engine, OSS handler shim)
// rely on, and pins down the externally observable behaviour of the three
// new strategies through ApplyStrategyTransformsWithConfig.
//
// Given an in-memory row representing a customer record with name, SSN,
//   phone, and email properties.
// When the operator authors a transform plan combining PARTIAL (US-433),
//   FPE (US-489), and REGEX (US-489) policies on different fields.
// Then the masked row leaks no clear PII, FPE preserves the SSN format
//   AND decrypts back to the original under the same key, REGEX masks only
//   the digits in the phone number, and PARTIAL keeps the email's first /
//   last two characters.
func TestBDD_US489_PartialFPERegex_AppliedTogether(t *testing.T) {
	// --- Given ---
	row := map[string]interface{}{
		"name":  "Alice Anderson",
		"ssn":   "123456789",
		"phone": "415-555-0123",
		"email": "alice@example.com",
	}

	fpeKey := make([]byte, 16)
	for i := range fpeKey {
		fpeKey[i] = byte(i*7 + 3) // deterministic AES-128 test fixture
	}

	plan := map[string]masking.StrategyApplication{
		// PARTIAL: keep first/last two letters of the email.
		"email": {
			Strategy: masking.MaskStrategyPartial,
		},
		// FPE: format-preserving encryption of the 9-digit SSN. The wire
		// shape must remain "ddddddddd" so downstream consumers expecting
		// SSN-shaped values do not break.
		"ssn": {
			Strategy: masking.MaskStrategyFPE,
			Config: strategies.ApplyConfig{
				FPE: strategies.FPEConfig{
					Key:   fpeKey,
					Tweak: []byte("customer:ssn"),
					Radix: 10,
				},
			},
		},
		// REGEX: mask every digit in the phone number; keep the dashes.
		"phone": {
			Strategy: masking.MaskStrategyRegex,
			Config: strategies.ApplyConfig{
				Regex: strategies.RegexConfig{
					Pattern:     regexp.MustCompile(`\d`),
					Replacement: "x",
				},
			},
		},
	}

	// --- When ---
	errs := masking.ApplyStrategyTransformsWithConfig(row, plan)

	// --- Then ---
	if errs != nil {
		t.Fatalf("expected no per-key errors, got: %v", errs)
	}
	if got := row["name"].(string); got != "Alice Anderson" {
		t.Fatalf("name should be untouched (not in plan), got %q", got)
	}
	// PARTIAL acceptance: keeps first and last 2 chars.
	if got := row["email"].(string); got != "al*************om" {
		t.Fatalf("email PARTIAL: got %q want %q", got, "al*************om")
	}
	// REGEX acceptance: digits masked, dashes intact.
	if got := row["phone"].(string); got != "xxx-xxx-xxxx" {
		t.Fatalf("phone REGEX: got %q want %q", got, "xxx-xxx-xxxx")
	}
	// FPE acceptance: ciphertext is digits only, length 9, NOT the plaintext.
	cipherSSN, ok := row["ssn"].(string)
	if !ok {
		t.Fatalf("ssn should remain a string after FPE, got %T", row["ssn"])
	}
	if cipherSSN == "123456789" {
		t.Fatalf("FPE returned plaintext unchanged for ssn")
	}
	if len(cipherSSN) != 9 {
		t.Fatalf("FPE ssn not length-preserving: got len=%d (%q)", len(cipherSSN), cipherSSN)
	}
	for _, r := range cipherSSN {
		if r < '0' || r > '9' {
			t.Fatalf("FPE ssn escaped radix-10 alphabet: %q", cipherSSN)
		}
	}
	// FPE round-trip via the strategies primitive — the contract guarantees
	// the operator can unmask later given the same key/tweak.
	recovered, err := strategies.FPEDecrypt(cipherSSN, strategies.FPEConfig{
		Key: fpeKey, Tweak: []byte("customer:ssn"), Radix: 10,
	})
	if err != nil {
		t.Fatalf("FPEDecrypt: %v", err)
	}
	if recovered != "123456789" {
		t.Fatalf("FPE round-trip mismatch: got %q want %q", recovered, "123456789")
	}
}

// TestBDD_US489_FailClosed_OnMisconfiguredFPE pins the negative control: when
// an FPE config carries a bad-length key (operator error, key rotation drift)
// the transform must NOT leak the clear value. The row's masked cell becomes
// nil and the per-key error map carries the underlying cipher error so the
// caller (admin handler / log line) can surface the misconfiguration.
//
// Without this guarantee, a transient operator slip would silently turn a
// REDACT-equivalent into a clear-text passthrough — a classic fail-open
// security regression we explicitly prevent.
func TestBDD_US489_FailClosed_OnMisconfiguredFPE(t *testing.T) {
	row := map[string]interface{}{
		"ssn": "123456789",
	}
	plan := map[string]masking.StrategyApplication{
		"ssn": {
			Strategy: masking.MaskStrategyFPE,
			Config: strategies.ApplyConfig{
				FPE: strategies.FPEConfig{
					Key: []byte("too-short"), // AES rejects: not 16/24/32 bytes
				},
			},
		},
	}
	errs := masking.ApplyStrategyTransformsWithConfig(row, plan)
	if errs == nil || errs["ssn"] == nil {
		t.Fatalf("expected fail-closed error for misconfigured FPE, got nil")
	}
	if row["ssn"] != nil {
		t.Fatalf("fail-closed: ssn should be nil, got %v", row["ssn"])
	}
	if !strings.Contains(errs["ssn"].Error(), "fpe") {
		t.Fatalf("error should mention fpe: %v", errs["ssn"])
	}
}

// TestBDD_US489_TaxonomyAcceptsNewStrategies verifies the canonical
// NormalizeStrategy / IsKnownStrategy entry points learn the new FPE and
// REGEX names so the admin CRUD surface accepts them — without this, posts
// like `{"maskStrategy":"FPE"}` would hit InvalidCellMask (ErrUnknownMaskStrategy)
// even though the engine could otherwise execute the transform.
func TestBDD_US489_TaxonomyAcceptsNewStrategies(t *testing.T) {
	for _, s := range []masking.MaskStrategy{masking.MaskStrategyFPE, masking.MaskStrategyRegex} {
		if !masking.IsKnownStrategy(s) {
			t.Fatalf("IsKnownStrategy(%q) should be true after US-489", s)
		}
		lower := masking.MaskStrategy(strings.ToLower(string(s)))
		if got := masking.NormalizeStrategy(lower); got != s {
			t.Fatalf("NormalizeStrategy(%q): got %q want %q", lower, got, s)
		}
	}
}
