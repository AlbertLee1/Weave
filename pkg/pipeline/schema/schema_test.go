package schema

import (
	"strings"
	"testing"

	weavetypes "github.com/liyang/weave/pkg/types"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		raw  string
		want candidateType
	}{
		{"42", candidateInteger},
		{"-7", candidateInteger},
		{"+7", candidateInteger},
		{"3000000000", candidateLong},
		{"3.14", candidateDouble},
		{"-0.5", candidateDouble},
		{"1e5", candidateDouble},
		{"true", candidateBoolean},
		{"FALSE", candidateBoolean},
		{"2025-04-28", candidateDate},
		{"2025-04-28T12:00:00Z", candidateTimestamp},
		{"2025-04-28 12:00:00", candidateTimestamp},
		{"hello", candidateString},
		{"", candidateString},
		{"   ", candidateString},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			got := classify(tc.raw)
			if got != tc.want {
				t.Fatalf("classify(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestNarrow_Promotion(t *testing.T) {
	// integer + long → long
	if got := narrow(candidateInteger, "3000000000"); got != candidateLong {
		t.Errorf("integer→long promote = %v", got)
	}
	// integer + double → double
	if got := narrow(candidateInteger, "3.14"); got != candidateDouble {
		t.Errorf("integer→double promote = %v", got)
	}
	// long + double → double
	if got := narrow(candidateLong, "3.14"); got != candidateDouble {
		t.Errorf("long→double promote = %v", got)
	}
	// boolean + integer → string
	if got := narrow(candidateBoolean, "42"); got != candidateString {
		t.Errorf("boolean+integer should widen to string, got %v", got)
	}
	// date + timestamp → string
	if got := narrow(candidateDate, "2025-04-28T12:00:00Z"); got != candidateString {
		t.Errorf("date+timestamp should widen to string, got %v", got)
	}
	// once string, stays string
	if got := narrow(candidateString, "42"); got != candidateString {
		t.Errorf("string is sticky, got %v", got)
	}
}

func TestInferCSV_Basic(t *testing.T) {
	csv := strings.NewReader("id,name,active,price,birthday\n" +
		"1,alice,true,1.50,2025-04-01\n" +
		"2,bob,false,3.00,2025-04-02\n" +
		"3,carol,true,7.99,2025-04-03\n")

	res, err := InferCSV(csv, Options{HasHeader: true})
	if err != nil {
		t.Fatalf("InferCSV: %v", err)
	}
	if res.RowsScanned != 3 {
		t.Errorf("RowsScanned=%d want 3", res.RowsScanned)
	}
	if len(res.Fields) != 5 {
		t.Fatalf("Fields=%d want 5", len(res.Fields))
	}
	expectations := map[string]weavetypes.BaseType{
		"id":       weavetypes.Integer,
		"name":     weavetypes.String,
		"active":   weavetypes.Boolean,
		"price":    weavetypes.Double,
		"birthday": weavetypes.Date,
	}
	for _, f := range res.Fields {
		want, ok := expectations[f.Name]
		if !ok {
			t.Errorf("unexpected field %q", f.Name)
			continue
		}
		if f.BaseType != want {
			t.Errorf("Fields[%s].BaseType = %v want %v", f.Name, f.BaseType, want)
		}
	}
}

func TestInferCSV_NullsAndNullable(t *testing.T) {
	csv := strings.NewReader("id,note\n1,hello\n2,\n3,NULL\n")
	res, err := InferCSV(csv, Options{HasHeader: true})
	if err != nil {
		t.Fatalf("InferCSV: %v", err)
	}
	if len(res.Fields) != 2 {
		t.Fatalf("Fields=%d", len(res.Fields))
	}
	id := findField(res.Fields, "id")
	note := findField(res.Fields, "note")
	if id == nil || note == nil {
		t.Fatalf("missing fields: %#v", res.Fields)
	}
	if id.Nullable {
		t.Error("id should not be Nullable")
	}
	if !note.Nullable {
		t.Error("note should be Nullable (empty + NULL sentinel)")
	}
	if note.NullCount != 2 {
		t.Errorf("note NullCount=%d want 2", note.NullCount)
	}
	if note.NonNullCount != 1 {
		t.Errorf("note NonNullCount=%d want 1", note.NonNullCount)
	}
}

func TestInferCSV_NoHeader(t *testing.T) {
	csv := strings.NewReader("1,foo\n2,bar\n")
	res, err := InferCSV(csv, Options{HasHeader: false})
	if err != nil {
		t.Fatalf("InferCSV: %v", err)
	}
	if len(res.Fields) != 2 {
		t.Fatalf("Fields=%d", len(res.Fields))
	}
	if res.Fields[0].Name != "col1" || res.Fields[1].Name != "col2" {
		t.Errorf("synthetic names = %q,%q", res.Fields[0].Name, res.Fields[1].Name)
	}
}

func TestInferCSV_TabDelimiter(t *testing.T) {
	csv := strings.NewReader("a\tb\n1\tfoo\n2\tbar\n")
	res, err := InferCSV(csv, Options{HasHeader: true, Delimiter: '\t'})
	if err != nil {
		t.Fatalf("InferCSV: %v", err)
	}
	if len(res.Fields) != 2 {
		t.Fatalf("got %d fields", len(res.Fields))
	}
	if res.Fields[0].BaseType != weavetypes.Integer {
		t.Errorf("a should infer integer, got %v", res.Fields[0].BaseType)
	}
}

func TestInferCSV_RaggedRows(t *testing.T) {
	csv := strings.NewReader("a,b,c\n1,2,3\n4,5\n6,7,8,9\n")
	res, err := InferCSV(csv, Options{HasHeader: true})
	if err != nil {
		t.Fatalf("InferCSV: %v", err)
	}
	if res.WarningCount != 2 {
		t.Errorf("WarningCount=%d want 2", res.WarningCount)
	}
	// Column b has two ints and a 5; column c has two ints and one null
	c := findField(res.Fields, "c")
	if c == nil {
		t.Fatal("c missing")
	}
	if c.NullCount != 1 {
		t.Errorf("c NullCount=%d want 1", c.NullCount)
	}
}

func TestInferCSV_SampleRowsLimit(t *testing.T) {
	var b strings.Builder
	b.WriteString("a\n")
	for i := 0; i < 50; i++ {
		b.WriteString("hello\n")
	}
	res, err := InferCSV(strings.NewReader(b.String()), Options{HasHeader: true, SampleRows: 10})
	if err != nil {
		t.Fatalf("InferCSV: %v", err)
	}
	if res.RowsScanned != 10 {
		t.Errorf("RowsScanned=%d want 10", res.RowsScanned)
	}
	if !res.Truncated {
		t.Error("Truncated should be true")
	}
}

func TestInferCSV_MixedTypesWidensToString(t *testing.T) {
	csv := strings.NewReader("v\n1\n2\nthree\n")
	res, err := InferCSV(csv, Options{HasHeader: true})
	if err != nil {
		t.Fatalf("InferCSV: %v", err)
	}
	if res.Fields[0].BaseType != weavetypes.String {
		t.Errorf("v should widen to string, got %v", res.Fields[0].BaseType)
	}
}

func TestInferCSV_AllNullColumn(t *testing.T) {
	csv := strings.NewReader("a,b\n1,\n2,\n")
	res, err := InferCSV(csv, Options{HasHeader: true})
	if err != nil {
		t.Fatalf("InferCSV: %v", err)
	}
	b := findField(res.Fields, "b")
	if b == nil {
		t.Fatal("b missing")
	}
	if b.BaseType != weavetypes.String {
		t.Errorf("all-null column should fall through to string, got %v", b.BaseType)
	}
	if !b.Nullable {
		t.Error("all-null column should be Nullable")
	}
}

func TestInferCSV_EmptyInput(t *testing.T) {
	res, err := InferCSV(strings.NewReader(""), Options{HasHeader: true})
	if err != nil {
		t.Fatalf("InferCSV: %v", err)
	}
	if res.RowsScanned != 0 {
		t.Errorf("RowsScanned=%d", res.RowsScanned)
	}
	if len(res.Fields) != 0 {
		t.Errorf("Fields=%d", len(res.Fields))
	}
}

func TestInferCSV_SampleValueLimit(t *testing.T) {
	var b strings.Builder
	b.WriteString("a\n")
	for i := 0; i < 20; i++ {
		b.WriteString("hello\n")
	}
	res, err := InferCSV(strings.NewReader(b.String()), Options{HasHeader: true})
	if err != nil {
		t.Fatalf("InferCSV: %v", err)
	}
	if got := len(res.Fields[0].Samples); got != SampleValueLimit {
		t.Errorf("Samples len = %d, want %d", got, SampleValueLimit)
	}
}

func TestInferCSV_DateTimestampSiblings(t *testing.T) {
	// A column that mixes pure dates and timestamps should widen to
	// string — there's no common Weave base type for "either".
	csv := strings.NewReader("ts\n2025-04-28\n2025-04-28T12:00:00Z\n")
	res, err := InferCSV(csv, Options{HasHeader: true})
	if err != nil {
		t.Fatalf("InferCSV: %v", err)
	}
	if res.Fields[0].BaseType != weavetypes.String {
		t.Errorf("date+timestamp should widen to string, got %v", res.Fields[0].BaseType)
	}
}

func TestInferJSON_ArrayOfObjects(t *testing.T) {
	src := strings.NewReader(`[
		{"id": 1, "name": "alice", "active": true},
		{"id": 2, "name": "bob",   "active": false},
		{"id": 3, "name": "carol", "active": true}
	]`)
	res, err := InferJSON(src, Options{})
	if err != nil {
		t.Fatalf("InferJSON: %v", err)
	}
	if res.RowsScanned != 3 {
		t.Errorf("RowsScanned=%d", res.RowsScanned)
	}
	got := map[string]weavetypes.BaseType{}
	for _, f := range res.Fields {
		got[f.Name] = f.BaseType
	}
	if got["id"] != weavetypes.Integer {
		t.Errorf("id=%v", got["id"])
	}
	if got["name"] != weavetypes.String {
		t.Errorf("name=%v", got["name"])
	}
	if got["active"] != weavetypes.Boolean {
		t.Errorf("active=%v", got["active"])
	}
}

func TestInferJSON_SingleObject(t *testing.T) {
	src := strings.NewReader(`{"id": 7, "label": "x"}`)
	res, err := InferJSON(src, Options{})
	if err != nil {
		t.Fatalf("InferJSON: %v", err)
	}
	if res.RowsScanned != 1 {
		t.Errorf("RowsScanned=%d", res.RowsScanned)
	}
	if len(res.Fields) != 2 {
		t.Errorf("Fields=%d", len(res.Fields))
	}
}

func TestInferJSON_MissingKeysAreNull(t *testing.T) {
	src := strings.NewReader(`[{"a": 1}, {"a": 2, "b": "x"}]`)
	res, err := InferJSON(src, Options{})
	if err != nil {
		t.Fatalf("InferJSON: %v", err)
	}
	b := findField(res.Fields, "b")
	if b == nil {
		t.Fatal("b missing")
	}
	if !b.Nullable {
		t.Error("b should be Nullable (missing in row 1)")
	}
	if b.NullCount != 1 {
		t.Errorf("b NullCount=%d want 1", b.NullCount)
	}
}

func TestInferJSON_ExplicitNull(t *testing.T) {
	src := strings.NewReader(`[{"a": 1, "b": null}, {"a": 2, "b": "x"}]`)
	res, err := InferJSON(src, Options{})
	if err != nil {
		t.Fatalf("InferJSON: %v", err)
	}
	b := findField(res.Fields, "b")
	if b == nil {
		t.Fatal("b missing")
	}
	if b.NullCount != 1 {
		t.Errorf("b NullCount=%d", b.NullCount)
	}
}

func TestInferJSON_PreservesFirstSeenOrder(t *testing.T) {
	src := strings.NewReader(`[{"z": 1, "a": 2}, {"a": 3, "m": 4}]`)
	res, err := InferJSON(src, Options{})
	if err != nil {
		t.Fatalf("InferJSON: %v", err)
	}
	got := []string{}
	for _, f := range res.Fields {
		got = append(got, f.Name)
	}
	// First row introduced z then a; second row introduced m. Note
	// map iteration in Go is non-deterministic, so the only stable
	// guarantee from the test is "m sorts after z and a".
	if len(got) != 3 {
		t.Fatalf("Fields=%v", got)
	}
	if got[2] != "m" {
		t.Errorf("expected m last, got order %v", got)
	}
}

func TestInferJSON_NumberPromotion(t *testing.T) {
	src := strings.NewReader(`[{"v": 1}, {"v": 2}, {"v": 3.14}]`)
	res, err := InferJSON(src, Options{})
	if err != nil {
		t.Fatalf("InferJSON: %v", err)
	}
	if res.Fields[0].BaseType != weavetypes.Double {
		t.Errorf("v should be double, got %v", res.Fields[0].BaseType)
	}
}

func TestInferJSON_NestedAsString(t *testing.T) {
	src := strings.NewReader(`[{"v": {"nested": 1}}, {"v": {"nested": 2}}]`)
	res, err := InferJSON(src, Options{})
	if err != nil {
		t.Fatalf("InferJSON: %v", err)
	}
	if res.Fields[0].BaseType != weavetypes.String {
		t.Errorf("nested object should infer string, got %v", res.Fields[0].BaseType)
	}
}

func TestInferNDJSON(t *testing.T) {
	src := strings.NewReader(`{"a": 1, "b": "x"}` + "\n" +
		`{"a": 2, "b": "y"}` + "\n" +
		`` + "\n" +
		`{"a": 3, "b": "z"}` + "\n")
	res, err := InferNDJSON(src, Options{})
	if err != nil {
		t.Fatalf("InferNDJSON: %v", err)
	}
	if res.RowsScanned != 3 {
		t.Errorf("RowsScanned=%d want 3", res.RowsScanned)
	}
	if res.Format != FormatNDJSON {
		t.Errorf("Format=%v want %v", res.Format, FormatNDJSON)
	}
}

func TestInferNDJSON_MalformedLineSkipped(t *testing.T) {
	src := strings.NewReader(`{"a": 1}` + "\n" + `not json` + "\n" + `{"a": 2}` + "\n")
	res, err := InferNDJSON(src, Options{})
	if err != nil {
		t.Fatalf("InferNDJSON: %v", err)
	}
	if res.RowsScanned != 2 {
		t.Errorf("RowsScanned=%d want 2", res.RowsScanned)
	}
	if res.WarningCount != 1 {
		t.Errorf("WarningCount=%d want 1", res.WarningCount)
	}
}

func TestEffectiveSampleRows(t *testing.T) {
	if got := effectiveSampleRows(0); got != DefaultSampleRows {
		t.Errorf("zero -> %d, want %d", got, DefaultSampleRows)
	}
	if got := effectiveSampleRows(-5); got != DefaultSampleRows {
		t.Errorf("neg -> %d", got)
	}
	if got := effectiveSampleRows(10); got != 10 {
		t.Errorf("10 -> %d", got)
	}
	if got := effectiveSampleRows(MaxSampleRows + 1); got != MaxSampleRows {
		t.Errorf("over-cap -> %d", got)
	}
}

func TestValidateFormat(t *testing.T) {
	if err := validateFormat(FormatCSV); err != nil {
		t.Errorf("csv rejected: %v", err)
	}
	if err := validateFormat(Format("xml")); err == nil {
		t.Errorf("xml accepted")
	}
}

func findField(fields []Field, name string) *Field {
	for i := range fields {
		if fields[i].Name == name {
			return &fields[i]
		}
	}
	return nil
}
