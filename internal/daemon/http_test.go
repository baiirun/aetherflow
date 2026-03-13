package daemon

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/baiirun/aetherflow/internal/protocol"
)

func TestHTTPEventsListRejectsInvalidAfterTimestamp(t *testing.T) {
	cfg := Config{
		ListenAddr:        "127.0.0.1:7070",
		Project:           "test",
		PollInterval:      time.Second,
		PoolSize:          1,
		SpawnCmd:          "echo test",
		SpawnPolicy:       SpawnPolicyManual,
		ReconcileInterval: DefaultReconcileInterval,
	}
	d := New(cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events?agent_name=a&after_timestamp=abc", nil)
	rec := httptest.NewRecorder()
	d.httpEventsList(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHTTPStatusAgentRejectsInvalidLimit(t *testing.T) {
	cfg := Config{
		ListenAddr:        "127.0.0.1:7070",
		Project:           "test",
		PollInterval:      time.Second,
		PoolSize:          1,
		SpawnCmd:          "echo test",
		SpawnPolicy:       SpawnPolicyManual,
		ReconcileInterval: DefaultReconcileInterval,
	}
	d := New(cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/status/agents/ghost?limit=-1", nil)
	req.SetPathValue("id", "ghost")
	rec := httptest.NewRecorder()
	d.httpStatusAgent(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestBrowserBoundaryMiddlewareRejectsMutatingBrowserRequests(t *testing.T) {
	handler := browserBoundaryMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/shutdown", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestHostCheckMiddlewareRejectsRemoteHostHeader(t *testing.T) {
	handler := hostCheckMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	req.Host = "evil.example:7070"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestDaemonURLDefaultsToNormalizedLoopback(t *testing.T) {
	cfg := Config{
		ListenAddr:        ":7070",
		Project:           "test",
		PollInterval:      time.Second,
		PoolSize:          1,
		SpawnCmd:          "echo test",
		SpawnPolicy:       SpawnPolicyManual,
		ReconcileInterval: DefaultReconcileInterval,
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	d := New(cfg)
	d.setLifecycleState(protocol.LifecycleStateRunning, "")
	if got := d.lifecycleStatus().DaemonURL; got != "http://127.0.0.1:7070" {
		t.Fatalf("DaemonURL = %q, want %q", got, "http://127.0.0.1:7070")
	}
}

func TestHandleShutdownForcedSignalsShutdown(t *testing.T) {
	cfg := Config{
		ListenAddr:        "127.0.0.1:7070",
		Project:           "test",
		PollInterval:      time.Second,
		PoolSize:          1,
		SpawnCmd:          "echo test",
		SpawnPolicy:       SpawnPolicyManual,
		ReconcileInterval: DefaultReconcileInterval,
	}
	d := New(cfg)
	resp := d.handleShutdown(true)
	if !resp.Success {
		t.Fatalf("handleShutdown error: %s", resp.Error)
	}
	select {
	case <-d.shutdown:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("shutdown channel was not closed")
	}
}
