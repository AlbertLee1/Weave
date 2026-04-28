package oms

import "testing"

func TestActionLogLineageRID(t *testing.T) {
	tests := []struct {
		name string
		id   int64
		want string
	}{
		{"zero is empty", 0, ""},
		{"negative is empty", -1, ""},
		{"positive id", 42, "ri.actions.main.action-log.42"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ActionLogLineageRID(tt.id)
			if got != tt.want {
				t.Errorf("ActionLogLineageRID(%d) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}
}

func TestObjectLineageRID(t *testing.T) {
	tests := []struct {
		name       string
		objectType string
		primaryKey string
		want       string
	}{
		{"empty primary key returns empty", "Employee", "", ""},
		{"primary key only mirrors FormatObject", "", "EMP-001", "ri.phonograph2-objects.main.object.EMP-001"},
		{"both segments", "Employee", "EMP-001", "ri.phonograph2-objects.main.object.Employee.EMP-001"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ObjectLineageRID(tt.objectType, tt.primaryKey)
			if got != tt.want {
				t.Errorf("ObjectLineageRID(%q, %q) = %q, want %q",
					tt.objectType, tt.primaryKey, got, tt.want)
			}
		})
	}
}
