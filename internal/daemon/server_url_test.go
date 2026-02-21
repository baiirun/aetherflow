package daemon

import "testing"

func TestValidateServerURLAttachTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		in    string
		valid bool
	}{
		{name: "local http localhost", in: "http://localhost:4096", valid: true},
		{name: "local http loopback", in: "http://127.0.0.1:4096", valid: true},
		{name: "remote https", in: "https://agent.example.com", valid: true},
		{name: "remote http rejected", in: "http://agent.example.com", valid: false},
		{name: "invalid scheme", in: "ftp://example.com", valid: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateServerURLAttachTarget(tc.in)
			if tc.valid && err != nil {
				t.Fatalf("ValidateServerURLAttachTarget(%q) error = %v, want nil", tc.in, err)
			}
			if !tc.valid && err == nil {
				t.Fatalf("ValidateServerURLAttachTarget(%q) error = nil, want error", tc.in)
			}
		})
	}
}
