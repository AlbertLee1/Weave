package formula

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEvaluate_ExpressionMatrix(t *testing.T) {
	t.Parallel()

	obj := map[string]interface{}{
		"firstName": "Ada",
		"lastName":  "Lovelace",
		"age":       int64(37),
		"salary":    float64(125000.5),
		"active":    true,
		"tags":      []interface{}{"engineer", "founder"},
		"address": map[string]interface{}{
			"city": "London",
			"zip":  "NW1",
		},
		"notes": nil,
	}

	tests := []struct {
		name    string
		formula string
		want    interface{}
	}{
		{
			name:    "string concatenation via this",
			formula: `this.firstName + " " + this.lastName`,
			want:    "Ada Lovelace",
		},
		{
			name:    "string concatenation via self",
			formula: `self.firstName + " " + self.lastName`,
			want:    "Ada Lovelace",
		},
		{
			name:    "integer arithmetic",
			formula: `this.age * 2 + 1`,
			want:    int64(75),
		},
		{
			name:    "float arithmetic",
			formula: `this.salary + 0.5`,
			want:    float64(125001.0),
		},
		{
			name:    "conditional (ternary)",
			formula: `this.age >= 18 ? "adult" : "minor"`,
			want:    "adult",
		},
		{
			name:    "boolean negation and equality",
			formula: `!this.active === false`,
			want:    true,
		},
		{
			name:    "string template",
			formula: "`${this.firstName}/${this.lastName}`",
			want:    "Ada/Lovelace",
		},
		{
			name:    "array indexing",
			formula: `this.tags[0]`,
			want:    "engineer",
		},
		{
			name:    "array length",
			formula: `this.tags.length`,
			want:    int64(2),
		},
		{
			name:    "nested object access",
			formula: `this.address.city + ", " + this.address.zip`,
			want:    "London, NW1",
		},
		{
			name:    "null coalescing via logical or",
			formula: `this.notes || "no notes"`,
			want:    "no notes",
		},
		{
			name:    "string methods (upper)",
			formula: `this.firstName.toUpperCase()`,
			want:    "ADA",
		},
		{
			name:    "Math builtin",
			formula: `Math.floor(this.salary / 1000)`,
			want:    int64(125),
		},
		{
			name:    "statement body with return",
			formula: "var n = this.firstName.length + this.lastName.length; return n;",
			want:    int64(11),
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ev, err := New(tc.formula)
			if err != nil {
				t.Fatalf("compile %q: %v", tc.formula, err)
			}
			got, err := ev.Evaluate(context.Background(), obj)
			if err != nil {
				t.Fatalf("evaluate %q: %v", tc.formula, err)
			}
			if !equalValues(got, tc.want) {
				t.Fatalf("formula %q: got %#v (%T), want %#v (%T)", tc.formula, got, got, tc.want, tc.want)
			}
		})
	}
}

func TestEvaluate_MissingPropertyIsUndefined(t *testing.T) {
	ev, err := New(`this.missing === undefined`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	got, err := ev.Evaluate(context.Background(), map[string]interface{}{"firstName": "Ada"})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if got != true {
		t.Fatalf("expected true, got %#v", got)
	}
}

func TestEvaluate_TimeoutInterruptsInfiniteLoop(t *testing.T) {
	ev, err := NewWithTimeout(`while (true) {} ; return 1;`, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	start := time.Now()
	_, err = ev.Evaluate(context.Background(), nil)
	elapsed := time.Since(start)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("expected ErrTimeout, got %v", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("interrupt too slow: %v", elapsed)
	}
}

func TestEvaluate_SandboxStripsHostGlobals(t *testing.T) {
	cases := []string{
		`typeof require`,
		`typeof fetch`,
		`typeof setTimeout`,
		`typeof process`,
	}
	for _, src := range cases {
		ev, err := New(src)
		if err != nil {
			t.Fatalf("compile %q: %v", src, err)
		}
		got, err := ev.Evaluate(context.Background(), nil)
		if err != nil {
			t.Fatalf("evaluate %q: %v", src, err)
		}
		if got != "undefined" {
			t.Fatalf("expected %q to be undefined, got %#v", src, got)
		}
	}
}

func TestEvaluate_WriteAttemptsFail(t *testing.T) {
	ev, err := New(`this.firstName = "Grace"; return this.firstName;`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	obj := map[string]interface{}{"firstName": "Ada"}
	_, err = ev.Evaluate(context.Background(), obj)
	if err == nil {
		t.Fatalf("expected write to be rejected")
	}
	if !errors.Is(err, ErrRuntime) {
		t.Fatalf("expected ErrRuntime, got %v", err)
	}
	if v := obj["firstName"]; v != "Ada" {
		t.Fatalf("underlying map was mutated: firstName=%v", v)
	}
}

func TestEvaluate_DeleteAttemptsFail(t *testing.T) {
	ev, err := New(`delete this.firstName; return this.firstName;`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	obj := map[string]interface{}{"firstName": "Ada"}
	_, err = ev.Evaluate(context.Background(), obj)
	if err == nil {
		t.Fatalf("expected delete to be rejected")
	}
	if v, ok := obj["firstName"]; !ok || v != "Ada" {
		t.Fatalf("underlying map was mutated: %v", obj)
	}
}

func TestNew_RejectsEmptySource(t *testing.T) {
	if _, err := New("   "); !errors.Is(err, ErrCompile) {
		t.Fatalf("expected ErrCompile for empty source, got %v", err)
	}
}

func TestNew_RejectsSyntaxError(t *testing.T) {
	if _, err := New(`this.foo +`); !errors.Is(err, ErrCompile) {
		t.Fatalf("expected ErrCompile for syntax error, got %v", err)
	}
}

func TestEvaluate_ConcurrentCallsAreIsolated(t *testing.T) {
	ev, err := New(`this.firstName + ":" + this.n`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := ev.Evaluate(context.Background(), map[string]interface{}{
				"firstName": "Ada",
				"n":         int64(i),
			})
			if err != nil {
				t.Errorf("goroutine %d: %v", i, err)
				return
			}
			s, ok := got.(string)
			if !ok || !strings.HasPrefix(s, "Ada:") {
				t.Errorf("goroutine %d: unexpected result %#v", i, got)
			}
		}()
	}
	wg.Wait()
}

// equalValues normalises numeric kinds from Goja (Export can yield int64 or
// float64 depending on the expression) before comparing.
func equalValues(got, want interface{}) bool {
	switch w := want.(type) {
	case int64:
		switch g := got.(type) {
		case int64:
			return g == w
		case float64:
			return g == float64(w)
		}
	case float64:
		switch g := got.(type) {
		case float64:
			return g == w
		case int64:
			return float64(g) == w
		}
	}
	return got == want
}
