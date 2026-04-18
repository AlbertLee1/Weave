package auth

import (
	"testing"
	"time"
)

func TestServiceAccount_IsDisabled(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		sa   *ServiceAccount
		want bool
	}{
		{"nil", nil, false},
		{"active", &ServiceAccount{}, false},
		{"disabled", &ServiceAccount{DisabledAt: &now}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.sa.IsDisabled(); got != tt.want {
				t.Errorf("IsDisabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestServiceAccount_IsExpired(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)
	tests := []struct {
		name string
		sa   *ServiceAccount
		now  time.Time
		want bool
	}{
		{"nil", nil, time.Now(), false},
		{"no expiry", &ServiceAccount{}, time.Now(), false},
		{"future expiry", &ServiceAccount{ExpiresAt: &future}, time.Now(), false},
		{"past expiry", &ServiceAccount{ExpiresAt: &past}, time.Now(), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.sa.IsExpired(tt.now); got != tt.want {
				t.Errorf("IsExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestServiceAccount_IsActive(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	tests := []struct {
		name string
		sa   *ServiceAccount
		want bool
	}{
		{"nil", nil, false},
		{"fully active", &ServiceAccount{ExpiresAt: &future}, true},
		{"no expiry still active", &ServiceAccount{}, true},
		{"disabled", &ServiceAccount{DisabledAt: &now}, false},
		{"expired", &ServiceAccount{ExpiresAt: &past}, false},
		{"disabled AND expired", &ServiceAccount{DisabledAt: &now, ExpiresAt: &past}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.sa.IsActive(now); got != tt.want {
				t.Errorf("IsActive() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateServiceAccountName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"empty rejected", "", true},
		{"simple ok", "ci-bot", false},
		{"snake ok", "build_bot", false},
		{"namespaced ok", "deploy.prod", false},
		{"mixed case ok", "Deploy-Bot", false},
		{"digits ok", "bot42", false},
		{"with space rejected", "bot name", true},
		{"with slash rejected", "bot/name", true},
		{"too long rejected", string(make([]byte, 129)), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateServiceAccountName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateServiceAccountName(%q) err=%v wantErr=%v", tt.input, err, tt.wantErr)
			}
		})
	}
}
