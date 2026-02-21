package daemon

import "testing"

func TestValidateServerURLAttachTarget(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		valid bool
	}{
		{name: "local http localhost", in: "http://localhost:4096", valid: true},
		{name: "local http loopback", in: "http://127.0.0.1:4096", valid: true},
		{name: "remote https trusted default", in: "https://agent-a.sprites.app", valid: true},
		{name: "remote https untrusted host", in: "https://agent.example.com", valid: false},
		{name: "remote http rejected", in: "http://agent.example.com", valid: false},
		{name: "invalid scheme", in: "ftp://example.com", valid: false},
	}

	t.Setenv(trustedAttachHostsEnv, "")

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

func TestValidateServerURLAttachTargetWithConfiguredTrustedHost(t *testing.T) {
	t.Setenv(trustedAttachHostsEnv, "example.com,*.internal.example")

	if _, err := ValidateServerURLAttachTarget("https://example.com"); err != nil {
		t.Fatalf("expected configured host to be trusted, got error: %v", err)
	}
	if _, err := ValidateServerURLAttachTarget("https://svc.internal.example"); err != nil {
		t.Fatalf("expected configured wildcard host to be trusted, got error: %v", err)
	}
}

func TestIsTrustedRemoteAttachHostRejectsIP(t *testing.T) {
	t.Setenv(trustedAttachHostsEnv, "127.0.0.1")
	if isTrustedRemoteAttachHost("127.0.0.1") {
		t.Fatal("expected IP literal to be rejected")
	}
}
