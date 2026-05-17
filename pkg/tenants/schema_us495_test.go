package tenants

import (
	"errors"
	"strings"
	"testing"
)

// US-495 — Tenants PG schema 强隔离. Unit-level guard rails for the
// tenant→schema-name mapping; the real isolation contract lives in the
// integration BDD (schema_us495_bdd_test.go) which exercises a real PG
// container.

func TestUS495_SchemaName_AcceptsLegalTenantID(t *testing.T) {
	cases := map[string]string{
		"acme":             "tenant_acme",
		"acme-1":           "tenant_acme_1",
		"acme.corp":        "tenant_acme_corp",
		"team_blue":        "tenant_team_blue",
		"ABC.123-xyz":      "tenant_ABC_123_xyz",
		"a":                "tenant_a",
		strings.Repeat("a", 128): "tenant_" + strings.Repeat("a", 128),
	}
	for tenant, want := range cases {
		got, err := SchemaName(tenant)
		if err != nil {
			t.Errorf("SchemaName(%q): unexpected error %v", tenant, err)
			continue
		}
		if got != want {
			t.Errorf("SchemaName(%q): want %q, got %q", tenant, want, got)
		}
	}
}

func TestUS495_SchemaName_RejectsIllegalTenantID(t *testing.T) {
	cases := []string{
		"",                          // empty
		" leading-space",            // leading whitespace
		"trailing ",                 // trailing whitespace
		"semi;colon",                // injection vector
		"quote'd",                   // injection vector
		`back"tick`,                 // injection vector
		strings.Repeat("a", 129),    // too long
		"tenant with spaces",        // spaces
		"unicode_∞",                 // non-ASCII
	}
	for _, tenant := range cases {
		got, err := SchemaName(tenant)
		if !errors.Is(err, ErrInvalidTenantID) {
			t.Errorf("SchemaName(%q): expected ErrInvalidTenantID, got %q / err=%v", tenant, got, err)
		}
	}
}

func TestUS495_SchemaName_Prefixed(t *testing.T) {
	// Every output must start with the reserved "tenant_" prefix so the
	// per-tenant search_path can never collide with the shared "public"
	// schema or the migrations ledger.
	got, err := SchemaName("acme")
	if err != nil {
		t.Fatalf("SchemaName: %v", err)
	}
	if !strings.HasPrefix(got, "tenant_") {
		t.Errorf("SchemaName(%q) = %q; expected `tenant_` prefix", "acme", got)
	}
}
