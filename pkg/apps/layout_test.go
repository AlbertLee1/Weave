package apps

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestValidateLayout_AcceptsCanonicalShape verifies the AC example from
// US-391 round-trips without error.
func TestValidateLayout_AcceptsCanonicalShape(t *testing.T) {
	raw := json.RawMessage(`{
		"type": "row",
		"children": [
			{
				"type": "col",
				"width": 6,
				"child": { "type": "component", "componentType": "table", "props": { "objectSet": "ri.x" } }
			},
			{
				"type": "col",
				"width": 6,
				"child": { "type": "component", "componentType": "chart" }
			}
		]
	}`)
	if err := ValidateLayout(raw); err != nil {
		t.Fatalf("canonical shape should validate, got %v", err)
	}
}

func TestValidateLayout_RejectsEmpty(t *testing.T) {
	for name, in := range map[string]json.RawMessage{
		"nil":        nil,
		"empty":      json.RawMessage(``),
		"whitespace": json.RawMessage(`   `),
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateLayout(in); err == nil {
				t.Fatalf("expected error for empty layout %q", name)
			}
		})
	}
}

func TestValidateLayout_RejectsInvalidJSON(t *testing.T) {
	if err := ValidateLayout(json.RawMessage(`{not-json`)); err == nil {
		t.Fatal("expected JSON parse error")
	}
}

func TestValidateLayout_RejectsUnknownType(t *testing.T) {
	cases := map[string]string{
		"unknown":          `{"type":"banana"}`,
		"missing":          `{"foo":"bar"}`,
		"empty type":       `{"type":""}`,
		"type not string":  `{"type":7}`,
		"layout not object": `[{"type":"row"}]`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			err := ValidateLayout(json.RawMessage(body))
			if err == nil {
				t.Fatalf("expected error for %s", name)
			}
		})
	}
}

func TestValidateLayout_RowRequiresChildrenArray(t *testing.T) {
	cases := map[string]string{
		"missing children":      `{"type":"row"}`,
		"children not array":    `{"type":"row","children":{}}`,
		"empty children array":  `{"type":"row","children":[]}`,
		"children entry not obj": `{"type":"row","children":[1,2,3]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateLayout(json.RawMessage(body)); err == nil {
				t.Fatalf("expected error for %s", name)
			}
		})
	}
}

func TestValidateLayout_ColRequiresWidthAndChild(t *testing.T) {
	cases := map[string]string{
		"width below range": `{"type":"row","children":[{"type":"col","width":0,"child":{"type":"component","componentType":"x"}}]}`,
		"width above range": `{"type":"row","children":[{"type":"col","width":13,"child":{"type":"component","componentType":"x"}}]}`,
		"width missing":     `{"type":"row","children":[{"type":"col","child":{"type":"component","componentType":"x"}}]}`,
		"child missing":     `{"type":"row","children":[{"type":"col","width":6}]}`,
		"width float":       `{"type":"row","children":[{"type":"col","width":6.5,"child":{"type":"component","componentType":"x"}}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateLayout(json.RawMessage(body)); err == nil {
				t.Fatalf("expected error for %s", name)
			}
		})
	}
}

func TestValidateLayout_RowChildWidthsCannotExceed12(t *testing.T) {
	// Two cols at width 8 each = 16, > 12 grid.
	body := `{"type":"row","children":[
		{"type":"col","width":8,"child":{"type":"component","componentType":"x"}},
		{"type":"col","width":8,"child":{"type":"component","componentType":"x"}}
	]}`
	err := ValidateLayout(json.RawMessage(body))
	if err == nil {
		t.Fatal("expected error for widths > 12")
	}
	if !strings.Contains(err.Error(), "width") {
		t.Fatalf("expected error to mention width, got %v", err)
	}
}

func TestValidateLayout_RowChildrenMustBeColumns(t *testing.T) {
	// PRD wire shape: row's direct children are cols, never plain components.
	body := `{"type":"row","children":[{"type":"component","componentType":"x"}]}`
	if err := ValidateLayout(json.RawMessage(body)); err == nil {
		t.Fatal("row children must be col nodes")
	}
}

func TestValidateLayout_ComponentRequiresComponentType(t *testing.T) {
	cases := map[string]string{
		"missing":   `{"type":"row","children":[{"type":"col","width":6,"child":{"type":"component"}}]}`,
		"empty":     `{"type":"row","children":[{"type":"col","width":6,"child":{"type":"component","componentType":""}}]}`,
		"not string": `{"type":"row","children":[{"type":"col","width":6,"child":{"type":"component","componentType":7}}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateLayout(json.RawMessage(body)); err == nil {
				t.Fatalf("expected error for %s", name)
			}
		})
	}
}

func TestValidateLayout_NestedRowsAllowed(t *testing.T) {
	// A col's child can itself be another row — nested layouts are valid.
	body := `{
		"type": "row",
		"children": [
			{"type":"col","width":12,"child":{
				"type":"row",
				"children":[
					{"type":"col","width":4,"child":{"type":"component","componentType":"a"}},
					{"type":"col","width":4,"child":{"type":"component","componentType":"b"}},
					{"type":"col","width":4,"child":{"type":"component","componentType":"c"}}
				]
			}}
		]
	}`
	if err := ValidateLayout(json.RawMessage(body)); err != nil {
		t.Fatalf("nested row should validate, got %v", err)
	}
}

func TestValidateLayout_DepthLimit(t *testing.T) {
	// Deeply nested layouts should be rejected past MaxLayoutDepth so
	// pathological client payloads can't blow the stack. Each iteration
	// here adds one row→col pair = 2 depth steps, so we test at half the
	// limit (success) and full limit + 5 (failure).
	build := func(rowPairs int) string {
		var b strings.Builder
		for i := 0; i < rowPairs; i++ {
			b.WriteString(`{"type":"row","children":[{"type":"col","width":12,"child":`)
		}
		b.WriteString(`{"type":"component","componentType":"x"}`)
		for i := 0; i < rowPairs; i++ {
			b.WriteString(`}]}`)
		}
		return b.String()
	}
	if err := ValidateLayout(json.RawMessage(build(MaxLayoutDepth / 2))); err != nil {
		t.Fatalf("layout at MaxLayoutDepth/2 row-pairs should validate, got %v", err)
	}
	if err := ValidateLayout(json.RawMessage(build(MaxLayoutDepth + 5))); err == nil {
		t.Fatal("layout exceeding MaxLayoutDepth should be rejected")
	}
}

func TestValidateLayout_RejectsTrailingNoise(t *testing.T) {
	// Two top-level objects in one document — strict decode rejects this.
	body := `{"type":"row","children":[{"type":"col","width":6,"child":{"type":"component","componentType":"x"}}]}{"extra":1}`
	if err := ValidateLayout(json.RawMessage(body)); err == nil {
		t.Fatal("trailing JSON should be rejected")
	}
}
