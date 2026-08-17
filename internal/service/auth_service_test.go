package service

import (
	"testing"
)

func TestRegisterValidation(t *testing.T) {
	tests := []struct {
		name    string
		req     [4]string // name, email, password, confirm
		wantErr bool
	}{
		{"empty name", [4]string{"", "a@b.com", "abc12345", "abc12345"}, true},
		{"bad email", [4]string{"A", "not-an-email", "abc12345", "abc12345"}, true},
		{"short password", [4]string{"A", "a@b.com", "abc123", "abc123"}, true},
		{"password no digit", [4]string{"A", "a@b.com", "abcdefgh", "abcdefgh"}, true},
		{"password no letter", [4]string{"A", "a@b.com", "12345678", "12345678"}, true},
		{"confirm mismatch", [4]string{"A", "a@b.com", "abc12345", "abc12346"}, true},
		{"valid", [4]string{"Budi", "a@b.com", "abc12345", "abc12345"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			verrs := validateRegister(tt.req[0], tt.req[1], tt.req[2], tt.req[3])
			if tt.wantErr {
				if len(verrs) == 0 {
					t.Errorf("validateRegister() = no errors, want validation errors for %v", tt.req)
				}
			} else if len(verrs) > 0 {
				t.Errorf("validateRegister() = %v, want no errors", verrs)
			}
		})
	}
}
