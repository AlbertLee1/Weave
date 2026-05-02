package celmask

import (
	"errors"
	"strings"
	"testing"
)

func TestCompile_RejectsEmpty(t *testing.T) {
	if _, err := Compile(""); !errors.Is(err, ErrEmptyExpression) {
		t.Fatalf("expected ErrEmptyExpression, got %v", err)
	}
	if _, err := Compile("   \t\n  "); !errors.Is(err, ErrEmptyExpression) {
		t.Fatalf("expected ErrEmptyExpression for whitespace, got %v", err)
	}
}

func TestCompile_RejectsNonBoolReturn(t *testing.T) {
	if _, err := Compile(`row.country`); err == nil {
		t.Fatalf("expected error for non-bool expression")
	}
}

func TestCompile_RejectsSyntaxError(t *testing.T) {
	cases := []string{
		`user.markings has 'PII'`, // 'has' is a macro, not infix; this is invalid
		`row.country == `,
		`unknown.symbol == 1`,
	}
	for _, src := range cases {
		if _, err := Compile(src); err == nil {
			t.Fatalf("expected error for %q", src)
		}
	}
}

func TestEval_PRDExample_PIIOrCountry(t *testing.T) {
	prog, err := Compile(`("PII" in user.markings) || row.country == "CN"`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	cases := []struct {
		name     string
		user     UserView
		row      map[string]any
		expected bool
	}{
		{"matches PII marking", UserView{Markings: []string{"PII"}}, map[string]any{"country": "US"}, true},
		{"matches CN row", UserView{Markings: []string{"PUBLIC"}}, map[string]any{"country": "CN"}, true},
		{"matches both", UserView{Markings: []string{"PII"}}, map[string]any{"country": "CN"}, true},
		{"matches neither", UserView{Markings: []string{"PUBLIC"}}, map[string]any{"country": "DE"}, false},
		{"empty user markings + non-CN row", UserView{}, map[string]any{"country": "US"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := prog.Eval(tc.user, tc.row)
			if err != nil {
				t.Fatalf("eval: %v", err)
			}
			if got != tc.expected {
				t.Fatalf("expected %v, got %v", tc.expected, got)
			}
		})
	}
}

func TestEval_RoleBasedAndUserMatching(t *testing.T) {
	cases := []struct {
		name     string
		expr     string
		user     UserView
		row      map[string]any
		expected bool
	}{
		{
			"role gate trigger",
			`!("finance" in user.roles)`,
			UserView{Roles: []string{"viewer"}},
			nil,
			true,
		},
		{
			"role gate pass",
			`!("finance" in user.roles)`,
			UserView{Roles: []string{"finance", "viewer"}},
			nil,
			false,
		},
		{
			"specific user bypass",
			`user.email != "trusted@ex.com"`,
			UserView{Email: "alice@ex.com"},
			nil,
			true,
		},
		{
			"row attribute string equality",
			`row.classification == "SECRET"`,
			UserView{},
			map[string]any{"classification": "SECRET"},
			true,
		},
		{
			"numeric row threshold",
			`row.amount > 10000.0`,
			UserView{},
			map[string]any{"amount": 25000.0},
			true,
		},
		{
			"compound role + row attribute",
			`!("admin" in user.roles) && row.region == "EU"`,
			UserView{Roles: []string{"viewer"}},
			map[string]any{"region": "EU"},
			true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := Compile(tc.expr)
			if err != nil {
				t.Fatalf("compile %q: %v", tc.expr, err)
			}
			got, err := prog.Eval(tc.user, tc.row)
			if err != nil {
				t.Fatalf("eval %q: %v", tc.expr, err)
			}
			if got != tc.expected {
				t.Fatalf("expr %q with user=%+v row=%v expected %v got %v", tc.expr, tc.user, tc.row, tc.expected, got)
			}
		})
	}
}

func TestEval_NilRowSubstitutedWithEmptyMap(t *testing.T) {
	prog, err := Compile(`size(user.markings) > 0`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	got, err := prog.Eval(UserView{Markings: []string{"PII"}}, nil)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !got {
		t.Fatalf("expected true, got false")
	}
}

func TestEval_MissingRowField_FailsClosed(t *testing.T) {
	prog, err := Compile(`row.unknown_field == "x"`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := prog.Eval(UserView{}, map[string]any{}); err == nil {
		t.Fatalf("expected eval error for missing field")
	}
}

func TestProgram_SourcePreserved(t *testing.T) {
	prog, err := Compile(`("PII" in user.markings)`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(prog.Source(), "PII") {
		t.Fatalf("expected source preserved, got %q", prog.Source())
	}
}

func TestValidate_AllowsValid_RejectsInvalid(t *testing.T) {
	if err := Validate(`true`); err != nil {
		t.Fatalf("expected nil for trivially-true expression, got %v", err)
	}
	if err := Validate(`row.country ===`); err == nil {
		t.Fatalf("expected error for syntactically invalid expression")
	}
}

func TestEval_ConcurrentSafe(t *testing.T) {
	prog, err := Compile(`("PII" in user.markings) || row.country == "CN"`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	const workers = 16
	done := make(chan struct{}, workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 200; j++ {
				_, _ = prog.Eval(UserView{Markings: []string{"PII"}}, map[string]any{"country": "US"})
			}
		}()
	}
	for i := 0; i < workers; i++ {
		<-done
	}
}
