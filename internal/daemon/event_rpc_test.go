package daemon

import (
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/baiirun/aetherflow/internal/sessions"
)

func newTestDaemonForEvents() *Daemon {
	return &Daemon{
		events: NewEventBuffer(DefaultEventBufSize),
		spawns: NewSpawnRegistry(),
		log:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}
}

// testPoolForClaim creates a pool with one running agent that has no session ID,
// for testing session claim logic.
func testPoolForClaim(t *testing.T) *Pool {
	t.Helper()
	cfg := Config{Project: "test", PoolSize: 2}
	cfg.ApplyDefaults()
	pool := NewPool(cfg, nil, nil, slog.Default())
	pool.mu.Lock()
	pool.agents["ts-abc"] = &Agent{
		ID:        "ghost_wolf",
		TaskID:    "ts-abc",
		Role:      RoleWorker,
		PID:       1234,
		SpawnTime: time.Now(),
		State:     AgentRunning,
	}
	pool.mu.Unlock()
	return pool
}

func TestHandleSessionEventSuccess(t *testing.T) {
	d := newTestDaemonForEvents()

	params, _ := json.Marshal(SessionEventParams{
		EventType: "session.created",
		SessionID: "ses-abc",
		Timestamp: 1000,
		Data:      json.RawMessage(`{"info":{"id":"ses-abc"}}`),
	})

	resp := d.handleSessionEvent(params)
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	events := d.events.Events("ses-abc")
	if len(events) != 1 {
		t.Fatalf("expected 1 event in buffer, got %d", len(events))
	}
	ev := events[0]
	if ev.EventType != "session.created" {
		t.Errorf("EventType = %q, want %q", ev.EventType, "session.created")
	}
	if ev.SessionID != "ses-abc" {
		t.Errorf("SessionID = %q, want %q", ev.SessionID, "ses-abc")
	}
	if ev.Timestamp != 1000 {
		t.Errorf("Timestamp = %d, want 1000", ev.Timestamp)
	}
	if string(ev.Data) != `{"info":{"id":"ses-abc"}}` {
		t.Errorf("Data = %s, want %s", ev.Data, `{"info":{"id":"ses-abc"}}`)
	}
}

func TestHandleSessionEventMissingSessionID(t *testing.T) {
	d := newTestDaemonForEvents()

	params, _ := json.Marshal(SessionEventParams{
		EventType: "session.created",
		Timestamp: 1000,
	})

	resp := d.handleSessionEvent(params)
	if resp.Success {
		t.Fatal("expected failure for missing session_id")
	}
	if resp.Error != "session_id is required" {
		t.Errorf("Error = %q, want %q", resp.Error, "session_id is required")
	}
}

func TestHandleSessionEventMissingEventType(t *testing.T) {
	d := newTestDaemonForEvents()

	params, _ := json.Marshal(SessionEventParams{
		SessionID: "ses-abc",
		Timestamp: 1000,
	})

	resp := d.handleSessionEvent(params)
	if resp.Success {
		t.Fatal("expected failure for missing event_type")
	}
	if resp.Error != "event_type is required" {
		t.Errorf("Error = %q, want %q", resp.Error, "event_type is required")
	}
}

func TestHandleSessionEventInvalidJSON(t *testing.T) {
	d := newTestDaemonForEvents()

	resp := d.handleSessionEvent(json.RawMessage(`{invalid json`))
	if resp.Success {
		t.Fatal("expected failure for invalid JSON")
	}
}

func TestHandleSessionEventMultipleEvents(t *testing.T) {
	d := newTestDaemonForEvents()

	types := []string{"session.created", "message.updated", "message.part.updated", "session.idle"}
	for i, et := range types {
		params, _ := json.Marshal(SessionEventParams{
			EventType: et,
			SessionID: "ses-abc",
			Timestamp: int64(i + 1),
		})
		resp := d.handleSessionEvent(params)
		if !resp.Success {
			t.Fatalf("event %d (%s) failed: %s", i, et, resp.Error)
		}
	}

	if d.events.Len("ses-abc") != 4 {
		t.Errorf("Len = %d, want 4", d.events.Len("ses-abc"))
	}

	events := d.events.Events("ses-abc")
	for i, et := range types {
		if events[i].EventType != et {
			t.Errorf("events[%d].EventType = %q, want %q", i, events[i].EventType, et)
		}
	}
}

func TestHandleSessionEventIsolatesSessions(t *testing.T) {
	d := newTestDaemonForEvents()

	for _, sid := range []string{"ses-1", "ses-2"} {
		params, _ := json.Marshal(SessionEventParams{
			EventType: "session.created",
			SessionID: sid,
			Timestamp: 1,
		})
		resp := d.handleSessionEvent(params)
		if !resp.Success {
			t.Fatalf("event for %s failed: %s", sid, resp.Error)
		}
	}

	if d.events.Len("ses-1") != 1 {
		t.Errorf("ses-1 Len = %d, want 1", d.events.Len("ses-1"))
	}
	if d.events.Len("ses-2") != 1 {
		t.Errorf("ses-2 Len = %d, want 1", d.events.Len("ses-2"))
	}
}

// --- claimSession tests ---

func TestClaimSessionPoolAgent(t *testing.T) {
	dir := t.TempDir()
	store, err := sessions.Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	pool := testPoolForClaim(t)
	d := &Daemon{
		events: NewEventBuffer(DefaultEventBufSize),
		pool:   pool,
		spawns: NewSpawnRegistry(),
		sstore: store,
		config: Config{ServerURL: "http://127.0.0.1:4096", Project: "test"},
		log:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	// Send session.created — pool has one unclaimed agent.
	params, _ := json.Marshal(SessionEventParams{
		EventType: "session.created",
		SessionID: "ses-claimed",
		Timestamp: 1000,
	})
	resp := d.handleSessionEvent(params)
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	// Verify pool agent got the session ID.
	agents := pool.Status()
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if agents[0].SessionID != "ses-claimed" {
		t.Errorf("agent SessionID = %q, want %q", agents[0].SessionID, "ses-claimed")
	}

	// Verify session was persisted.
	records, err := store.List()
	if err != nil {
		t.Fatalf("store.List error: %v", err)
	}
	found := false
	for _, r := range records {
		if r.SessionID == "ses-claimed" {
			found = true
			if r.Origin != sessions.OriginPool {
				t.Errorf("Origin = %q, want %q", r.Origin, sessions.OriginPool)
			}
		}
	}
	if !found {
		t.Error("expected session record to be persisted")
	}
}

func TestClaimSessionSpawnEntry(t *testing.T) {
	dir := t.TempDir()
	store, err := sessions.Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	spawns := NewSpawnRegistry()
	_ = spawns.Register(SpawnEntry{
		SpawnID:   "spawn-test",
		PID:       12345,
		State:     SpawnRunning,
		Prompt:    "test prompt",
		SpawnTime: time.Now(),
	})

	d := &Daemon{
		events: NewEventBuffer(DefaultEventBufSize),
		spawns: spawns,
		sstore: store,
		config: Config{ServerURL: "http://127.0.0.1:4096", Project: "test"},
		log:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	params, _ := json.Marshal(SessionEventParams{
		EventType: "session.created",
		SessionID: "ses-spawn",
		Timestamp: 1000,
	})
	resp := d.handleSessionEvent(params)
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	// Verify spawn entry got the session ID.
	entry := spawns.Get("spawn-test")
	if entry == nil {
		t.Fatal("spawn entry not found")
	}
	if entry.SessionID != "ses-spawn" {
		t.Errorf("spawn SessionID = %q, want %q", entry.SessionID, "ses-spawn")
	}

	// Verify session was persisted.
	records, err := store.List()
	if err != nil {
		t.Fatalf("store.List error: %v", err)
	}
	found := false
	for _, r := range records {
		if r.SessionID == "ses-spawn" {
			found = true
			if r.Origin != sessions.OriginSpawn {
				t.Errorf("Origin = %q, want %q", r.Origin, sessions.OriginSpawn)
			}
		}
	}
	if !found {
		t.Error("expected session record to be persisted")
	}
}

func TestClaimSessionNoCandidates(t *testing.T) {
	d := &Daemon{
		events: NewEventBuffer(DefaultEventBufSize),
		spawns: NewSpawnRegistry(),
		log:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	// No pool, no spawns — should not panic, just log.
	params, _ := json.Marshal(SessionEventParams{
		EventType: "session.created",
		SessionID: "ses-orphan",
		Timestamp: 1000,
	})
	resp := d.handleSessionEvent(params)
	if !resp.Success {
		t.Fatalf("expected success even with no candidates, got error: %s", resp.Error)
	}
}

func TestClaimSessionMultipleCandidatesSkips(t *testing.T) {
	spawns := NewSpawnRegistry()
	_ = spawns.Register(SpawnEntry{SpawnID: "spawn-a", PID: 100, State: SpawnRunning, SpawnTime: time.Now()})
	_ = spawns.Register(SpawnEntry{SpawnID: "spawn-b", PID: 200, State: SpawnRunning, SpawnTime: time.Now()})

	d := &Daemon{
		events: NewEventBuffer(DefaultEventBufSize),
		spawns: spawns,
		log:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	params, _ := json.Marshal(SessionEventParams{
		EventType: "session.created",
		SessionID: "ses-ambiguous",
		Timestamp: 1000,
	})
	resp := d.handleSessionEvent(params)
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	// Neither spawn should get the session ID.
	a := spawns.Get("spawn-a")
	b := spawns.Get("spawn-b")
	if a.SessionID != "" {
		t.Errorf("spawn-a SessionID = %q, want empty", a.SessionID)
	}
	if b.SessionID != "" {
		t.Errorf("spawn-b SessionID = %q, want empty", b.SessionID)
	}
}
