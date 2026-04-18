package oms

import (
	"fmt"
	"strconv"
	"strings"
)

// DefaultFunctionVersion is the semver string used when CreateFunction is
// called without an explicit version. Matches the migration 000041 default.
const DefaultFunctionVersion = "1.0.0"

// Semver is the parsed view of a Function.Version string. The minimal subset
// needed by the registry — major.minor.patch with an optional pre-release
// segment ("1.2.3-beta"). Build metadata ("+build") is preserved verbatim in
// Pre but ignored for ordering.
type Semver struct {
	Major int
	Minor int
	Patch int
	Pre   string
}

// ParseSemver parses a semver string in the MAJOR.MINOR.PATCH[-PRE] form.
// Returns a typed error when any segment is missing or non-numeric. Empty
// strings are rejected — callers wanting "the default" should use
// DefaultFunctionVersion explicitly.
func ParseSemver(v string) (Semver, error) {
	if v == "" {
		return Semver{}, fmt.Errorf("version is required")
	}
	core, pre, _ := strings.Cut(v, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return Semver{}, fmt.Errorf("version %q is not semver (expected MAJOR.MINOR.PATCH)", v)
	}
	out := Semver{Pre: pre}
	for i, p := range parts {
		if p == "" {
			return Semver{}, fmt.Errorf("version %q has empty segment", v)
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return Semver{}, fmt.Errorf("version %q segment %d is not a non-negative integer", v, i)
		}
		switch i {
		case 0:
			out.Major = n
		case 1:
			out.Minor = n
		case 2:
			out.Patch = n
		}
	}
	return out, nil
}

// CompareSemver returns -1 / 0 / 1 if a is lower / equal / higher than b.
// Pre-release strings make the version LOWER than the same core without a
// pre tag (per semver §11). Build metadata is ignored.
func CompareSemver(a, b Semver) int {
	switch {
	case a.Major != b.Major:
		return cmpInt(a.Major, b.Major)
	case a.Minor != b.Minor:
		return cmpInt(a.Minor, b.Minor)
	case a.Patch != b.Patch:
		return cmpInt(a.Patch, b.Patch)
	}
	switch {
	case a.Pre == "" && b.Pre == "":
		return 0
	case a.Pre == "" && b.Pre != "":
		return 1
	case a.Pre != "" && b.Pre == "":
		return -1
	}
	return strings.Compare(a.Pre, b.Pre)
}

func cmpInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// validateFunctionVersion is the Function.Validate helper. Empty version
// substitutes to the default; otherwise the string must parse as semver.
func validateFunctionVersion(v string) error {
	if v == "" {
		return nil
	}
	if _, err := ParseSemver(v); err != nil {
		return err
	}
	return nil
}

// SortFunctionsByVersionDesc sorts the slice in-place: name ascending,
// version descending (latest first per name). Stable on the rare equal-version
// duplicates that can show up in mock repos. Used by ListFunctions so the
// HTTP wire format keeps the per-name latest at the top of each group.
func SortFunctionsByVersionDesc(fns []Function) {
	// Tiny helper — insertion sort is fine for the registry's typical size
	// (< 1k rows per ontology). Keeps the impl free of "import sort" coupling
	// and easy to audit for the ordering tests.
	for i := 1; i < len(fns); i++ {
		j := i
		for j > 0 && functionLess(fns[j], fns[j-1]) {
			fns[j], fns[j-1] = fns[j-1], fns[j]
			j--
		}
	}
}

// functionLess implements the ordering used by SortFunctionsByVersionDesc:
// primary key is Name ascending; secondary key is parsed Version descending.
// Unparseable versions sort to the bottom of their name group so a malformed
// row never hides a valid one at the top.
func functionLess(a, b Function) bool {
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	av, aerr := ParseSemver(a.Version)
	bv, berr := ParseSemver(b.Version)
	switch {
	case aerr != nil && berr != nil:
		return a.Version < b.Version
	case aerr != nil:
		return false
	case berr != nil:
		return true
	}
	return CompareSemver(av, bv) > 0
}
