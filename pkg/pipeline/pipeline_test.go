package pipeline

import (
	"strings"
	"testing"
)

func TestValidatePipelineID(t *testing.T) {
	cases := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{"empty", "", true},
		{"valid simple", "etl-daily", false},
		{"valid dots", "team.etl.daily", false},
		{"valid underscores", "etl_daily_v2", false},
		{"too long", strings.Repeat("a", 129), true},
		{"max length", strings.Repeat("a", 128), false},
		{"invalid space", "etl daily", true},
		{"invalid slash", "etl/daily", true},
		{"invalid colon", "etl:daily", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePipelineID(tc.id)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidatePipelineID(%q): err=%v wantErr=%v", tc.id, err, tc.wantErr)
			}
		})
	}
}

func TestValidateNodeName(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"empty", "", true},
		{"letters", "alice", false},
		{"underscore start", "_data", false},
		{"alphanumeric", "table_v2", false},
		{"digit start rejected", "1table", true},
		{"hyphen rejected", "my-table", true},
		{"too long", strings.Repeat("a", 65), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateNodeName(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateNodeName(%q): err=%v wantErr=%v", tc.input, err, tc.wantErr)
			}
		})
	}
}

func TestValidateSchedule(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"empty allowed", "", false},
		{"5-field", "0 9 * * 1", false},
		{"6-field with seconds", "0 0 9 * * 1", false},
		{"3-field rejected", "0 9 *", true},
		{"7-field rejected", "0 0 9 * * 1 2026", true},
		{"tabs accepted as separator", "0\t9\t*\t*\t1", false},
		{"only whitespace rejected", "   ", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSchedule(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateSchedule(%q): err=%v wantErr=%v", tc.input, err, tc.wantErr)
			}
		})
	}
}

func validPipeline() *Pipeline {
	return &Pipeline{
		ID:   "demo",
		Name: "Demo",
		Inputs: []Input{
			{Name: "src", Type: "objectset", Config: map[string]any{"objectType": "Customer"}},
		},
		Transforms: []Transform{
			{Name: "filter_active", Type: "filter", Inputs: []string{"src"}, Config: map[string]any{"where": "active = true"}},
		},
		Outputs: []Output{
			{Name: "warehouse", Type: "jdbc", Input: "filter_active", Config: map[string]any{"table": "active_customers"}},
		},
		Schedule: "0 9 * * *",
	}
}

func TestPipeline_Validate(t *testing.T) {
	if err := validPipeline().Validate(); err != nil {
		t.Fatalf("valid pipeline returned err: %v", err)
	}
}

func TestPipeline_Validate_Errors(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Pipeline)
		want string
	}{
		{
			name: "nil id",
			mut:  func(p *Pipeline) { p.ID = "" },
			want: "pipeline id must not be empty",
		},
		{
			name: "no inputs",
			mut:  func(p *Pipeline) { p.Inputs = nil },
			want: "at least one input",
		},
		{
			name: "no outputs",
			mut:  func(p *Pipeline) { p.Outputs = nil },
			want: "at least one output",
		},
		{
			name: "duplicate input + transform name",
			mut:  func(p *Pipeline) { p.Transforms[0].Name = "src" },
			want: "duplicate node name",
		},
		{
			name: "input missing type",
			mut:  func(p *Pipeline) { p.Inputs[0].Type = "" },
			want: "type must not be empty",
		},
		{
			name: "transform refs unknown input",
			mut:  func(p *Pipeline) { p.Transforms[0].Inputs = []string{"missing"} },
			want: "is not a known upstream node",
		},
		{
			name: "transform self-reference",
			mut:  func(p *Pipeline) { p.Transforms[0].Inputs = []string{"filter_active"} },
			want: "self-reference",
		},
		{
			name: "output refs unknown input",
			mut:  func(p *Pipeline) { p.Outputs[0].Input = "missing" },
			want: "is not a known upstream node",
		},
		{
			name: "bad schedule",
			mut:  func(p *Pipeline) { p.Schedule = "bad" },
			want: "schedule",
		},
		{
			name: "transform refs later transform",
			mut: func(p *Pipeline) {
				p.Transforms = []Transform{
					{Name: "first", Type: "filter", Inputs: []string{"second"}},
					{Name: "second", Type: "filter", Inputs: []string{"src"}},
				}
				p.Outputs[0].Input = "second"
			},
			want: "is not a known upstream node",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := validPipeline()
			tc.mut(p)
			err := p.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestClonePipeline_DeepCopy(t *testing.T) {
	p := validPipeline()
	cp := ClonePipeline(p)
	cp.Inputs[0].Name = "mutated"
	cp.Inputs[0].Config["objectType"] = "Order"
	cp.Transforms[0].Inputs[0] = "mutated"
	cp.Outputs[0].Config["table"] = "all_customers"
	if p.Inputs[0].Name == "mutated" {
		t.Fatal("ClonePipeline did not deep-copy Inputs[].Name")
	}
	if p.Inputs[0].Config["objectType"] != "Customer" {
		t.Fatal("ClonePipeline did not deep-copy Inputs[].Config")
	}
	if p.Transforms[0].Inputs[0] != "src" {
		t.Fatal("ClonePipeline did not deep-copy Transforms[].Inputs slice")
	}
	if p.Outputs[0].Config["table"] != "active_customers" {
		t.Fatal("ClonePipeline did not deep-copy Outputs[].Config")
	}
}

func TestClonePipeline_NilSafe(t *testing.T) {
	if got := ClonePipeline(nil); got != nil {
		t.Fatalf("ClonePipeline(nil) = %v, want nil", got)
	}
}
