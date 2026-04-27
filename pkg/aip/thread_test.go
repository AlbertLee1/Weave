package aip

import "testing"

func TestValidateThreadID(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"thr_abc123", false},
		{"thr.with-dots", false},
		{"a", false},
		{"", true},
		{" ", true},
		{"thread id with spaces", true},
		{"id/with/slash", true},
		{"id*", true},
	}
	for _, tc := range cases {
		err := ValidateThreadID(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("ValidateThreadID(%q): err=%v wantErr=%v", tc.in, err, tc.wantErr)
		}
	}
}

func TestValidateProvider(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"mock", false},
		{"openai", false},
		{"anthropic", false},
		{"custom-1", false},
		{"a", false},
		{"", true},
		{"Mock", true},  // uppercase rejected
		{"1abc", true},  // must start with a letter
		{"-abc", true},  // must start with a letter
		{"abc!", true},  // punctuation rejected
	}
	for _, tc := range cases {
		err := ValidateProvider(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("ValidateProvider(%q): err=%v wantErr=%v", tc.in, err, tc.wantErr)
		}
	}
}

func TestIsKnownProvider(t *testing.T) {
	for _, name := range KnownProviders() {
		if !IsKnownProvider(name) {
			t.Errorf("IsKnownProvider(%q) = false, want true", name)
		}
	}
	if IsKnownProvider("nope") {
		t.Errorf("IsKnownProvider(%q) = true, want false", "nope")
	}
	if IsKnownProvider("") {
		t.Errorf("IsKnownProvider(%q) = true, want false", "")
	}
}

func TestValidateRole(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"system", false},
		{"user", false},
		{"assistant", false},
		{"tool", false},
		{"", true},
		{"USER", true},
	}
	for _, tc := range cases {
		err := ValidateRole(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("ValidateRole(%q): err=%v wantErr=%v", tc.in, err, tc.wantErr)
		}
	}
}
