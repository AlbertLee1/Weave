package pii

import (
	"reflect"
	"sort"
	"testing"
)

func TestIsEmail(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"plain", "alice@example.com", true},
		{"with_dot", "alice.smith@example.co.uk", true},
		{"with_plus", "alice+test@example.com", true},
		{"embedded", "contact me at alice@example.com today", true},
		{"upper_case", "Alice@Example.COM", true},
		{"missing_at", "aliceexample.com", false},
		{"missing_tld", "alice@example", false},
		{"empty", "", false},
		{"just_text", "hello world", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsEmail(tc.in); got != tc.want {
				t.Errorf("IsEmail(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestIsSSN(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"canonical", "123-45-6789", true},
		{"embedded", "ssn: 123-45-6789 on file", true},
		{"no_dashes", "123456789", false}, // collides with order numbers — intentional
		{"wrong_groups", "12-345-6789", false},
		{"too_short", "123-45-678", false},
		{"empty", "", false},
		{"letters", "abc-de-fghi", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsSSN(tc.in); got != tc.want {
				t.Errorf("IsSSN(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestIsPhone(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"parens", "(415) 555-1234", true},
		{"dashes", "415-555-1234", true},
		{"dots", "415.555.1234", true},
		{"plus_one", "+1 415 555 1234", true},
		{"contiguous", "4155551234", true},
		{"too_short", "415-555", false},
		{"all_letters", "call me", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsPhone(tc.in); got != tc.want {
				t.Errorf("IsPhone(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestIsCreditCard(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"visa", "4111 1111 1111 1111", true},
		{"visa_no_space", "4111111111111111", true},
		{"mastercard", "5500 0000 0000 0004", true},
		{"amex", "3400 0000 0000 009", true}, // 15-digit Amex
		{"dashes", "4111-1111-1111-1111", true},
		{"luhn_invalid", "4111 1111 1111 1112", false},
		{"too_short", "4111 1111 11", false},
		{"random_id", "1234 5678 9012 3450", false}, // not Luhn-valid
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsCreditCard(tc.in); got != tc.want {
				t.Errorf("IsCreditCard(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestScanner_DetectPII(t *testing.T) {
	s := NewScanner()

	cases := []struct {
		name  string
		props map[string]interface{}
		want  bool
	}{
		{
			name:  "nil_props",
			props: nil,
			want:  false,
		},
		{
			name:  "empty_props",
			props: map[string]interface{}{},
			want:  false,
		},
		{
			name: "no_pii",
			props: map[string]interface{}{
				"id":   "emp-1",
				"name": "Alice",
				"age":  float64(30),
			},
			want: false,
		},
		{
			name: "email_field",
			props: map[string]interface{}{
				"id":    "emp-1",
				"email": "alice@example.com",
			},
			want: true,
		},
		{
			name: "ssn_field",
			props: map[string]interface{}{
				"id":  "emp-1",
				"ssn": "123-45-6789",
			},
			want: true,
		},
		{
			name: "phone_field",
			props: map[string]interface{}{
				"id":    "emp-1",
				"phone": "(415) 555-1234",
			},
			want: true,
		},
		{
			name: "credit_card_field",
			props: map[string]interface{}{
				"id":   "emp-1",
				"card": "4111 1111 1111 1111",
			},
			want: true,
		},
		{
			name: "non_string_skipped",
			props: map[string]interface{}{
				"id":  float64(123456789),
				"age": 42,
			},
			want: false,
		},
		{
			name: "pii_in_string_array",
			props: map[string]interface{}{
				"contacts": []interface{}{"Alice", "alice@example.com"},
			},
			want: true,
		},
		{
			name: "pii_in_typed_string_slice",
			props: map[string]interface{}{
				"emails": []string{"alice@example.com"},
			},
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := s.DetectPII(tc.props); got != tc.want {
				t.Errorf("DetectPII(%v) = %v, want %v", tc.props, got, tc.want)
			}
		})
	}
}

func TestScanner_Categories(t *testing.T) {
	s := NewScanner()

	props := map[string]interface{}{
		"name":  "Alice",
		"email": "alice@example.com",
		"phone": "415-555-1234",
		"ssn":   "123-45-6789",
		"card":  "4111 1111 1111 1111",
	}

	got := s.Categories(props)
	want := []string{
		CategoryCreditCard,
		CategoryEmail,
		CategoryPhone,
		CategorySSN,
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Categories() = %v, want %v", got, want)
	}
}

func TestScanner_Categories_Empty(t *testing.T) {
	s := NewScanner()
	if got := s.Categories(map[string]interface{}{"name": "Alice"}); got != nil {
		t.Errorf("Categories() = %v, want nil", got)
	}
}

func TestScanner_Categories_NilProps(t *testing.T) {
	s := NewScanner()
	if got := s.Categories(nil); got != nil {
		t.Errorf("Categories(nil) = %v, want nil", got)
	}
}

func TestPIIMarkingName_Constant(t *testing.T) {
	// Pinned to "PII" because the migrations seed this exact label and
	// the auth handlers reference it; renaming would break the wire
	// contract with every existing user_markings grant.
	if PIIMarkingName != "PII" {
		t.Errorf("PIIMarkingName = %q, want %q", PIIMarkingName, "PII")
	}
}
