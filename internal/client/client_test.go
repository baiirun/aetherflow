package client

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/baiirun/aetherflow/internal/protocol"
)

func TestStopDaemonPreservesRefusedOutcome(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(Response{
			Success: true,
			Result: mustMarshal(t, protocol.StopDaemonResult{
				Outcome: protocol.StopOutcomeRefused,
				Status: protocol.DaemonLifecycleStatus{
					State:              protocol.LifecycleStateRunning,
					ActiveSessionCount: 2,
				},
				Message: "refusing stop with 2 active workload(s) across 2 attached session(s); retry with --force after confirmation",
			}),
		})
	}))
	defer server.Close()

	result, err := New(server.URL).StopDaemon(false)
	if result == nil {
		t.Fatal("StopDaemon result = nil, want refusal payload")
	}
	var refused *ShutdownRefusedError
	if err == nil || !errors.As(err, &refused) {
		t.Fatalf("StopDaemon error = %v, want ShutdownRefusedError", err)
	}
	if refused.Result.Outcome != protocol.StopOutcomeRefused {
		t.Fatalf("Outcome = %q, want %q", refused.Result.Outcome, protocol.StopOutcomeRefused)
	}
}

func TestClientSendsDaemonAuthTokenWhenPresent(t *testing.T) {
	var gotToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Aetherflow-Token")
		_ = json.NewEncoder(w).Encode(Response{
			Success: true,
			Result:  mustMarshal(t, protocol.DaemonLifecycleStatus{}),
		})
	}))
	defer server.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	path, err := authTokenPath(server.URL)
	if err != nil {
		t.Fatalf("authTokenPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("secret-token\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := New(server.URL).DaemonLifecycle(); err != nil {
		t.Fatalf("DaemonLifecycle: %v", err)
	}
	if gotToken != "secret-token" {
		t.Fatalf("X-Aetherflow-Token = %q, want %q", gotToken, "secret-token")
	}
}

func mustMarshal(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}
