package aip

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestValidateToolName(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"empty", "", true},
		{"ok-letter-start", "echo", false},
		{"ok-snake", "lookup_object", false},
		{"ok-camel", "lookupObject", false},
		{"reject-digit-start", "1tool", true},
		{"reject-hyphen", "echo-tool", true},
		{"reject-too-long", strings.Repeat("a", 65), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateToolName(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateToolName(%q) err=%v, wantErr=%v", tc.input, err, tc.wantErr)
			}
		})
	}
}

func TestMemoryToolCatalog_CreateAndGet(t *testing.T) {
	cat := NewMemoryToolCatalog()
	tool := &ToolRecord{
		Name:               "lookup",
		Description:        "Look something up",
		Parameters:         json.RawMessage(`{"type":"object"}`),
		HandlerFunctionRID: "ri.functions.main.fn.abc",
		Enabled:            true,
	}
	if err := cat.CreateTool(context.Background(), tool); err != nil {
		t.Fatalf("CreateTool: %v", err)
	}
	got, err := cat.GetTool(context.Background(), "lookup")
	if err != nil {
		t.Fatalf("GetTool: %v", err)
	}
	if got.Name != "lookup" || got.HandlerFunctionRID != "ri.functions.main.fn.abc" {
		t.Errorf("GetTool = %+v", got)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Errorf("expected CreatedAt/UpdatedAt to be stamped, got %+v", got)
	}
}

func TestMemoryToolCatalog_CreateDuplicate(t *testing.T) {
	cat := NewMemoryToolCatalog()
	if err := cat.CreateTool(context.Background(), &ToolRecord{Name: "echo"}); err != nil {
		t.Fatalf("first CreateTool: %v", err)
	}
	err := cat.CreateTool(context.Background(), &ToolRecord{Name: "echo"})
	if !errors.Is(err, ErrToolAlreadyExists) {
		t.Fatalf("second CreateTool err=%v want ErrToolAlreadyExists", err)
	}
}

func TestMemoryToolCatalog_GetMissing(t *testing.T) {
	cat := NewMemoryToolCatalog()
	if _, err := cat.GetTool(context.Background(), "nope"); !errors.Is(err, ErrToolRecordNotFound) {
		t.Fatalf("GetTool missing err=%v want ErrToolRecordNotFound", err)
	}
}

func TestMemoryToolCatalog_List(t *testing.T) {
	cat := NewMemoryToolCatalog()
	for _, n := range []string{"zeta", "alpha", "mike"} {
		_ = cat.CreateTool(context.Background(), &ToolRecord{Name: n, Enabled: true})
	}
	out, err := cat.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	want := []string{"alpha", "mike", "zeta"}
	if len(out) != len(want) {
		t.Fatalf("ListTools len=%d want %d", len(out), len(want))
	}
	for i, w := range want {
		if out[i].Name != w {
			t.Errorf("ListTools[%d]=%q want %q", i, out[i].Name, w)
		}
	}
}

func TestMemoryToolCatalog_Update(t *testing.T) {
	cat := NewMemoryToolCatalog()
	if err := cat.CreateTool(context.Background(), &ToolRecord{Name: "echo", Enabled: true}); err != nil {
		t.Fatalf("CreateTool: %v", err)
	}
	desc := "echoes input"
	disabled := false
	upd := ToolUpdate{Description: &desc, Enabled: &disabled}
	if err := cat.UpdateTool(context.Background(), "echo", upd); err != nil {
		t.Fatalf("UpdateTool: %v", err)
	}
	got, err := cat.GetTool(context.Background(), "echo")
	if err != nil {
		t.Fatalf("GetTool: %v", err)
	}
	if got.Description != "echoes input" || got.Enabled {
		t.Errorf("UpdateTool: got %+v", got)
	}
}

func TestMemoryToolCatalog_UpdateMissing(t *testing.T) {
	cat := NewMemoryToolCatalog()
	if err := cat.UpdateTool(context.Background(), "nope", ToolUpdate{}); !errors.Is(err, ErrToolRecordNotFound) {
		t.Fatalf("UpdateTool missing err=%v want ErrToolRecordNotFound", err)
	}
}

func TestMemoryToolCatalog_Delete(t *testing.T) {
	cat := NewMemoryToolCatalog()
	_ = cat.CreateTool(context.Background(), &ToolRecord{Name: "echo"})
	if err := cat.DeleteTool(context.Background(), "echo"); err != nil {
		t.Fatalf("DeleteTool: %v", err)
	}
	if _, err := cat.GetTool(context.Background(), "echo"); !errors.Is(err, ErrToolRecordNotFound) {
		t.Fatalf("after delete err=%v want ErrToolRecordNotFound", err)
	}
}

func TestMemoryToolCatalog_DeleteMissing(t *testing.T) {
	cat := NewMemoryToolCatalog()
	if err := cat.DeleteTool(context.Background(), "nope"); !errors.Is(err, ErrToolRecordNotFound) {
		t.Fatalf("DeleteTool missing err=%v want ErrToolRecordNotFound", err)
	}
}
