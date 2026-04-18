// Package pii provides a pure-Go regex-based scanner that recognises
// common Personally Identifiable Information (PII) patterns — email
// addresses, US Social Security Numbers, North-American phone numbers
// and credit-card numbers (with Luhn checksum) — inside the property
// values of an object edit.
//
// The scanner is intentionally minimal: each detector returns a boolean
// for "this single string carries this kind of PII" and the top-level
// Scanner walks a map of properties to surface the union of categories
// detected. Callers (today: pkg/funnel.Consumer) layer the marking
// decision on top — when any category fires, the consumer auto-tags
// the indexed document with the well-known "PII" marking so the
// existing marking-based mandatory access control (US-051 et al.)
// gates visibility automatically.
//
// Patterns are calibrated for the common case rather than RFC-perfect
// coverage. The scanner is fail-open at the boundary — values it can't
// classify are simply not flagged — so a false negative degrades to
// "no auto marking" and an admin can still apply an explicit grant.
package pii

import (
	"regexp"
	"sort"
	"strings"
)

// PIIMarkingName is the well-known marking applied to every object
// whose property values trigger a positive PII detection. Operators
// can grant this marking via the existing /admin/users/{id}/markings
// endpoints; the funnel consumer attaches it on write.
const PIIMarkingName = "PII"

// Category names returned by Scanner.Categories. Stable strings —
// downstream tooling (audit, dashboards) keys on them.
const (
	CategoryEmail      = "email"
	CategorySSN        = "ssn"
	CategoryPhone      = "phone"
	CategoryCreditCard = "credit_card"
)

// emailPattern matches the common "<local>@<domain>.<tld>" shape.
// Anchored with word boundaries so substrings of larger tokens (URLs,
// path fragments) don't bleed false positives.
var emailPattern = regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b`)

// ssnPattern matches the canonical US SSN format `XXX-XX-XXXX`. The
// dash-separated form is conventional in PII corpora; the all-digit
// 9-character variant collides with too many account numbers to be
// reliable, so we leave it out here. A custom detector can wrap this
// scanner and add organisation-specific regexes if needed.
var ssnPattern = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)

// phonePattern matches North-American 10-digit phone numbers in the
// common formats: `(NNN) NNN-NNNN`, `NNN-NNN-NNNN`, `NNN.NNN.NNNN`,
// `+1 NNN NNN NNNN` and contiguous `NNNNNNNNNN`. The international
// `+CC` prefix is optional. Word boundaries keep the match from
// chewing into adjacent digits (e.g. order numbers).
var phonePattern = regexp.MustCompile(`(?:\+?\d{1,3}[\s\-.])?(?:\(\d{3}\)|\d{3})[\s\-.]?\d{3}[\s\-.]?\d{4}`)

// creditCardCandidate matches a 13–19 digit run optionally separated
// by spaces or hyphens — the candidate is then validated by Luhn so
// the regex's permissiveness doesn't generate false positives.
var creditCardCandidate = regexp.MustCompile(`\b(?:\d[ \-]?){12,18}\d\b`)

// IsEmail reports whether s contains an email address.
func IsEmail(s string) bool {
	return emailPattern.MatchString(s)
}

// IsSSN reports whether s contains a US Social Security Number in the
// canonical XXX-XX-XXXX form.
func IsSSN(s string) bool {
	return ssnPattern.MatchString(s)
}

// IsPhone reports whether s contains a North-American phone number.
// The detector matches the common formatted shapes; bare 10-digit
// runs (which collide with order numbers / IDs) are rejected unless
// they're properly separated.
func IsPhone(s string) bool {
	return phonePattern.MatchString(s)
}

// IsCreditCard reports whether s contains a sequence of 13–19 digits
// that passes the Luhn checksum. The Luhn check is what keeps the
// detector from flagging arbitrary numeric IDs of similar length.
func IsCreditCard(s string) bool {
	for _, m := range creditCardCandidate.FindAllString(s, -1) {
		digits := stripNonDigits(m)
		if len(digits) < 13 || len(digits) > 19 {
			continue
		}
		if luhnValid(digits) {
			return true
		}
	}
	return false
}

// stripNonDigits returns s with every non-digit byte removed.
func stripNonDigits(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// luhnValid implements the Luhn (mod-10) checksum used by every major
// payment-card scheme. Returns true when the supplied all-digit
// string is a valid Luhn-checksummed card number.
func luhnValid(digits string) bool {
	if len(digits) == 0 {
		return false
	}
	sum := 0
	double := false
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum%10 == 0
}

// Scanner is the top-level PII detector. It is cheap to construct
// (the regexes are package-level pre-compiled) and safe for
// concurrent use; methods don't mutate any state.
type Scanner struct{}

// NewScanner returns a Scanner with default detectors. Reserved as a
// constructor so a future config-driven scanner (custom regexes,
// allow-listed fields) can replace the zero-value implementation
// without breaking call sites.
func NewScanner() *Scanner {
	return &Scanner{}
}

// DetectPII reports whether any string value inside properties matches
// any of the built-in PII patterns. Non-string values are skipped —
// the detector is intentionally narrow about its input shape so the
// caller (funnel consumer) can keep the hot path allocation-free.
func (s *Scanner) DetectPII(properties map[string]interface{}) bool {
	if len(properties) == 0 {
		return false
	}
	for _, v := range properties {
		if matches := categoriesForValue(v); len(matches) > 0 {
			return true
		}
	}
	return false
}

// Categories returns the deduplicated, lexicographically sorted set
// of PII categories detected across every string value in properties.
// The empty slice means "no PII detected"; downstream callers that
// only care about the boolean can use DetectPII for a faster path.
func (s *Scanner) Categories(properties map[string]interface{}) []string {
	if len(properties) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	for _, v := range properties {
		for _, c := range categoriesForValue(v) {
			seen[c] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// categoriesForValue is the per-value classifier shared by DetectPII
// and Categories. Slices and string-array shapes (Bleve hands arrays
// back as []interface{}, []string, or — for length-1 — bare string)
// are walked element-wise so values stored as multi-valued props get
// scanned correctly. Non-string elements are silently skipped.
func categoriesForValue(v interface{}) []string {
	switch t := v.(type) {
	case string:
		return categoriesForString(t)
	case []string:
		return categoriesForStrings(t)
	case []interface{}:
		out := []string{}
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, categoriesForString(s)...)
			}
		}
		return out
	}
	return nil
}

func categoriesForStrings(values []string) []string {
	out := []string{}
	for _, v := range values {
		out = append(out, categoriesForString(v)...)
	}
	return out
}

func categoriesForString(s string) []string {
	if s == "" {
		return nil
	}
	out := make([]string, 0, 4)
	if IsEmail(s) {
		out = append(out, CategoryEmail)
	}
	if IsSSN(s) {
		out = append(out, CategorySSN)
	}
	if IsPhone(s) {
		out = append(out, CategoryPhone)
	}
	if IsCreditCard(s) {
		out = append(out, CategoryCreditCard)
	}
	return out
}
