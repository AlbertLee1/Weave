package strategies

import (
	"errors"
	"fmt"
	"strings"

	"github.com/capitalone/fpe/ff1"
)

// FPEConfig configures the FF1 format-preserving cipher (NIST SP 800-38G).
//
// Key must be 16, 24, or 32 bytes (AES-128 / AES-192 / AES-256). Tweak is
// caller-supplied additional input bound into the cipher state — same plain-
// text under different tweaks yields different ciphertext, letting callers
// derive per-record randomness without a per-record key.
//
// Exactly one of Radix or Alphabet selects the format-preserving alphabet:
//
//   - Radix in [2,36]: ciphertext is over the canonical base-N digits the
//     stdlib's math/big uses ('0'..'9' then 'a'..'z'). Default when both are
//     zero values is Radix=10 (decimal digits). Input must already be a
//     string of those digits.
//   - Alphabet (non-empty): every input character must appear in Alphabet
//     and Alphabet's characters must be unique. The wrapper internally maps
//     characters to base-N digits (N = len(Alphabet)), runs FF1, then maps
//     the cipher digits back so the on-wire format matches the caller's
//     alphabet exactly. This is how we support hex IDs, uppercase letters,
//     custom token charsets, etc.
//
// MaxTweakLen bounds the tweak the underlying ff1.NewCipher will accept; the
// wrapper defaults to a generous 32 if the caller leaves it zero.
type FPEConfig struct {
	Key         []byte
	Tweak       []byte
	Radix       int
	Alphabet    string
	MaxTweakLen int
}

// ErrEmptyFPEInput is returned when callers pass an empty string into FPE.
// FF1 requires |X| >= ceil(log_radix(100)) >= 2, so empty is always invalid.
var ErrEmptyFPEInput = errors.New("fpe: empty input")

// FPE encrypts value using FF1 with cfg and returns the ciphertext as a
// length- and alphabet-preserving string. The transform is deterministic
// under fixed (Key, Tweak, Radix/Alphabet). Inputs containing characters
// outside the active alphabet are rejected with a descriptive error so the
// caller can distinguish "wrong alphabet" from "key/tweak misconfigured".
func FPE(value string, cfg FPEConfig) (string, error) {
	if value == "" {
		return "", ErrEmptyFPEInput
	}
	if cfg.Alphabet != "" {
		return fpeAlphabet(value, cfg, true)
	}
	return fpeRadix(value, cfg, true)
}

// FPEDecrypt is the inverse of FPE. Decrypting a value not produced by FPE
// under the same cfg returns a (length-preserving) garbage string — callers
// MUST treat the ciphertext source as authoritative.
func FPEDecrypt(value string, cfg FPEConfig) (string, error) {
	if value == "" {
		return "", ErrEmptyFPEInput
	}
	if cfg.Alphabet != "" {
		return fpeAlphabet(value, cfg, false)
	}
	return fpeRadix(value, cfg, false)
}

func effectiveRadix(cfg FPEConfig) int {
	if cfg.Radix > 0 {
		return cfg.Radix
	}
	return 10
}

func effectiveMaxTLen(cfg FPEConfig) int {
	if cfg.MaxTweakLen > 0 {
		return cfg.MaxTweakLen
	}
	// 32 is generous for typical tweaks (resource RIDs, table+pk pairs);
	// the underlying ff1 library accepts an int.
	return 32
}

func fpeRadix(value string, cfg FPEConfig, encrypt bool) (string, error) {
	radix := effectiveRadix(cfg)
	cipher, err := ff1.NewCipher(radix, effectiveMaxTLen(cfg), cfg.Key, cfg.Tweak)
	if err != nil {
		return "", fmt.Errorf("fpe: %w", err)
	}
	if encrypt {
		out, err := cipher.Encrypt(value)
		if err != nil {
			return "", fmt.Errorf("fpe encrypt: %w", err)
		}
		return out, nil
	}
	out, err := cipher.Decrypt(value)
	if err != nil {
		return "", fmt.Errorf("fpe decrypt: %w", err)
	}
	return out, nil
}

// fpeAlphabet runs FF1 over an arbitrary character alphabet by translating
// each input character to its index in cfg.Alphabet (a base-N digit), feeding
// the resulting numeric string into FF1 with radix N, then mapping each
// output digit back to the caller's alphabet.
func fpeAlphabet(value string, cfg FPEConfig, encrypt bool) (string, error) {
	alphabet := cfg.Alphabet
	radix := len(alphabet)
	if radix < 2 {
		return "", fmt.Errorf("fpe: alphabet must have at least 2 characters")
	}
	index := make(map[rune]int, radix)
	for i, r := range alphabet {
		if _, dup := index[r]; dup {
			return "", fmt.Errorf("fpe: alphabet contains duplicate character %q", r)
		}
		index[r] = i
	}
	// Translate value → numeric string in radix N (using stdlib math/big's
	// canonical digit set so we can hand it to ff1 as-is).
	digits := []rune(canonicalDigits(radix))
	if len(digits) < radix {
		return "", fmt.Errorf("fpe: radix %d exceeds 36; use Radix= form instead", radix)
	}
	encoded := make([]byte, 0, len(value))
	for _, r := range value {
		idx, ok := index[r]
		if !ok {
			return "", fmt.Errorf("fpe: character %q not in alphabet", r)
		}
		encoded = append(encoded, byte(digits[idx]))
	}
	cipher, err := ff1.NewCipher(radix, effectiveMaxTLen(cfg), cfg.Key, cfg.Tweak)
	if err != nil {
		return "", fmt.Errorf("fpe: %w", err)
	}
	var outDigits string
	if encrypt {
		outDigits, err = cipher.Encrypt(string(encoded))
		if err != nil {
			return "", fmt.Errorf("fpe encrypt: %w", err)
		}
	} else {
		outDigits, err = cipher.Decrypt(string(encoded))
		if err != nil {
			return "", fmt.Errorf("fpe decrypt: %w", err)
		}
	}
	// Map each ciphertext digit back to the caller's alphabet.
	digitIdx := make(map[rune]int, radix)
	for i, r := range digits[:radix] {
		digitIdx[r] = i
	}
	var b strings.Builder
	b.Grow(len(outDigits))
	for _, r := range outDigits {
		idx, ok := digitIdx[r]
		if !ok {
			return "", fmt.Errorf("fpe: unexpected ciphertext digit %q", r)
		}
		b.WriteRune(rune(alphabet[idx]))
	}
	return b.String(), nil
}

// canonicalDigits returns the digit alphabet math/big uses for radix N:
// '0'..'9' then 'a'..'z'. Only valid for N <= 36.
func canonicalDigits(radix int) string {
	const all = "0123456789abcdefghijklmnopqrstuvwxyz"
	if radix > len(all) {
		return ""
	}
	return all[:radix]
}
