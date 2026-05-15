package funcregistry

import (
	"fmt"
	"strings"

	"github.com/liyang/weave/pkg/oms"
)

// rangeOp identifies the comparison shape of a parsed SemverRange. The
// supported operators are the npm-flavoured subset that covers the
// VTX-048 spec ("^1.0.0") plus the obviously-useful tilde / range
// operators. Anything fancier (compound ranges with " || ", whitespace-
// separated "AND" lists) is rejected at parse time so the wire shape
// stays unambiguous.
type rangeOp int

const (
	opAny   rangeOp = iota // "" / "*" — match anything
	opEq                   // "1.2.3"  — exact
	opGT                   // ">1.2.3" — strictly greater
	opGTE                  // ">=1.2.3"
	opLT                   // "<1.2.3"
	opLTE                  // "<=1.2.3"
	opCaret                // "^1.2.3" — >=1.2.3 AND <(major+1).0.0
	opTilde                // "~1.2.3" — >=1.2.3 AND <(major).(minor+1).0
)

// SemverRange is a parsed semver-range constraint. Built by
// ParseSemverRange; tested with Matches. The struct is intentionally
// value-typed so callers can pass it around without allocation.
type SemverRange struct {
	op  rangeOp
	ref oms.Semver
}

// IsAny reports whether the range was parsed as "" or "*". The
// resolver uses this to short-circuit the per-candidate comparison.
func (r SemverRange) IsAny() bool { return r.op == opAny }

// ParseSemverRange parses a constraint string into a SemverRange. The
// supported shapes are documented on rangeOp. An empty string and "*"
// both parse to the always-match range so callers can pass an unset
// query param through without a nil-check.
func ParseSemverRange(s string) (SemverRange, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "*" {
		return SemverRange{op: opAny}, nil
	}

	// Order matters: ">=" must be probed before ">" so a leading "=" isn't
	// stripped by the shorter prefix.
	switch {
	case strings.HasPrefix(s, ">="):
		v, err := oms.ParseSemver(strings.TrimSpace(s[2:]))
		if err != nil {
			return SemverRange{}, fmt.Errorf("range %q: %w", s, err)
		}
		return SemverRange{op: opGTE, ref: v}, nil
	case strings.HasPrefix(s, "<="):
		v, err := oms.ParseSemver(strings.TrimSpace(s[2:]))
		if err != nil {
			return SemverRange{}, fmt.Errorf("range %q: %w", s, err)
		}
		return SemverRange{op: opLTE, ref: v}, nil
	case strings.HasPrefix(s, ">"):
		v, err := oms.ParseSemver(strings.TrimSpace(s[1:]))
		if err != nil {
			return SemverRange{}, fmt.Errorf("range %q: %w", s, err)
		}
		return SemverRange{op: opGT, ref: v}, nil
	case strings.HasPrefix(s, "<"):
		v, err := oms.ParseSemver(strings.TrimSpace(s[1:]))
		if err != nil {
			return SemverRange{}, fmt.Errorf("range %q: %w", s, err)
		}
		return SemverRange{op: opLT, ref: v}, nil
	case strings.HasPrefix(s, "^"):
		v, err := oms.ParseSemver(strings.TrimSpace(s[1:]))
		if err != nil {
			return SemverRange{}, fmt.Errorf("range %q: %w", s, err)
		}
		return SemverRange{op: opCaret, ref: v}, nil
	case strings.HasPrefix(s, "~"):
		v, err := oms.ParseSemver(strings.TrimSpace(s[1:]))
		if err != nil {
			return SemverRange{}, fmt.Errorf("range %q: %w", s, err)
		}
		return SemverRange{op: opTilde, ref: v}, nil
	case strings.HasPrefix(s, "="):
		v, err := oms.ParseSemver(strings.TrimSpace(s[1:]))
		if err != nil {
			return SemverRange{}, fmt.Errorf("range %q: %w", s, err)
		}
		return SemverRange{op: opEq, ref: v}, nil
	default:
		v, err := oms.ParseSemver(s)
		if err != nil {
			return SemverRange{}, fmt.Errorf("range %q: %w", s, err)
		}
		return SemverRange{op: opEq, ref: v}, nil
	}
}

// Matches reports whether v satisfies the range. The opAny short-circuit
// guarantees we never panic on a zero-value reference; every other op
// has a real ref from ParseSemverRange.
func (r SemverRange) Matches(v oms.Semver) bool {
	switch r.op {
	case opAny:
		return true
	case opEq:
		return oms.CompareSemver(v, r.ref) == 0
	case opGT:
		return oms.CompareSemver(v, r.ref) > 0
	case opGTE:
		return oms.CompareSemver(v, r.ref) >= 0
	case opLT:
		return oms.CompareSemver(v, r.ref) < 0
	case opLTE:
		return oms.CompareSemver(v, r.ref) <= 0
	case opCaret:
		// "^1.2.3" means >=1.2.3 AND <2.0.0. Major bumps are excluded;
		// minor/patch bumps within the same major are accepted.
		if oms.CompareSemver(v, r.ref) < 0 {
			return false
		}
		return v.Major == r.ref.Major
	case opTilde:
		// "~1.2.3" means >=1.2.3 AND <1.3.0. Same minor only.
		if oms.CompareSemver(v, r.ref) < 0 {
			return false
		}
		return v.Major == r.ref.Major && v.Minor == r.ref.Minor
	default:
		return false
	}
}

// ResolveLatestInRange returns the candidate with the highest version
// that satisfies r, plus true. When no candidate matches (or candidates
// is empty) the zero-value Function and false are returned. Candidates
// whose Version cannot be parsed as semver are skipped — a malformed
// row should never hide a valid one — but no error is reported because
// the registry's read path is forgiving by design (mirrors the same
// philosophy as oms.SortFunctionsByVersionDesc).
func ResolveLatestInRange(r SemverRange, candidates []oms.Function) (oms.Function, bool) {
	var (
		best    oms.Function
		bestVer oms.Semver
		matched bool
	)
	for _, fn := range candidates {
		v, err := oms.ParseSemver(fn.NormalisedVersion())
		if err != nil {
			continue
		}
		if !r.Matches(v) {
			continue
		}
		if !matched || oms.CompareSemver(v, bestVer) > 0 {
			best = fn
			bestVer = v
			matched = true
		}
	}
	return best, matched
}
