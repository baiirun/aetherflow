package client

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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

func mustMarshal(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}
