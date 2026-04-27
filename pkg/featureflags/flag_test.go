package featureflags

import (
	"testing"

	"github.com/liyang/weave/pkg/auth"
)

func TestFlag_EnabledFor(t *testing.T) {
	cases := []struct {
		name string
		flag Flag
		user *auth.User
		want bool
	}{
		{
			name: "disabled globally",
			flag: Flag{Name: "x", Enabled: false},
			user: &auth.User{ID: "u1"},
			want: false,
		},
		{
			name: "enabled globally no scopes",
			flag: Flag{Name: "x", Enabled: true},
			user: &auth.User{ID: "u1"},
			want: true,
		},
		{
			name: "enabled globally with nil user",
			flag: Flag{Name: "x", Enabled: true},
			user: nil,
			want: true,
		},
		{
			name: "enabled with user scope match",
			flag: Flag{Name: "x", Enabled: true, Users: []string{"u1", "u2"}},
			user: &auth.User{ID: "u1"},
			want: true,
		},
		{
			name: "enabled with user scope miss",
			flag: Flag{Name: "x", Enabled: true, Users: []string{"u1", "u2"}},
			user: &auth.User{ID: "u3"},
			want: false,
		},
		{
			name: "enabled with user scope but nil user",
			flag: Flag{Name: "x", Enabled: true, Users: []string{"u1"}},
			user: nil,
			want: false,
		},
		{
			name: "enabled with realm scope match",
			flag: Flag{Name: "x", Enabled: true, Realms: []string{"main", "dev"}},
			user: &auth.User{ID: "u1", Attributes: map[string]any{"realm": "main"}},
			want: true,
		},
		{
			name: "enabled with realm scope miss",
			flag: Flag{Name: "x", Enabled: true, Realms: []string{"main"}},
			user: &auth.User{ID: "u1", Attributes: map[string]any{"realm": "other"}},
			want: false,
		},
		{
			name: "enabled with realm scope but no realm attr",
			flag: Flag{Name: "x", Enabled: true, Realms: []string{"main"}},
			user: &auth.User{ID: "u1"},
			want: false,
		},
		{
			name: "enabled with both scopes — user match wins",
			flag: Flag{Name: "x", Enabled: true, Users: []string{"u1"}, Realms: []string{"other"}},
			user: &auth.User{ID: "u1", Attributes: map[string]any{"realm": "main"}},
			want: true,
		},
		{
			name: "enabled with both scopes — realm match wins",
			flag: Flag{Name: "x", Enabled: true, Users: []string{"u2"}, Realms: []string{"main"}},
			user: &auth.User{ID: "u1", Attributes: map[string]any{"realm": "main"}},
			want: true,
		},
		{
			name: "enabled with both scopes — neither matches",
			flag: Flag{Name: "x", Enabled: true, Users: []string{"u2"}, Realms: []string{"other"}},
			user: &auth.User{ID: "u1", Attributes: map[string]any{"realm": "main"}},
			want: false,
		},
		{
			name: "disabled — scopes ignored",
			flag: Flag{Name: "x", Enabled: false, Users: []string{"u1"}, Realms: []string{"main"}},
			user: &auth.User{ID: "u1", Attributes: map[string]any{"realm": "main"}},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.flag.EnabledFor(tc.user); got != tc.want {
				t.Fatalf("EnabledFor(%+v) = %v; want %v", tc.user, got, tc.want)
			}
		})
	}
}

func TestFlag_ValidateName(t *testing.T) {
	cases := []struct {
		name    string
		flagKey string
		wantErr bool
	}{
		{"simple", "dark_mode", false},
		{"hyphens", "new-ui", false},
		{"dots allowed", "billing.v2", false},
		{"alphanumerics", "abc123", false},
		{"empty", "", true},
		{"spaces", "dark mode", true},
		{"slashes", "a/b", true},
		{"too long", stringOfLen(129), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateFlagName(tc.flagKey)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateFlagName(%q) err=%v wantErr=%v", tc.flagKey, err, tc.wantErr)
			}
		})
	}
}

func stringOfLen(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}
