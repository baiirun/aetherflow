package daemon

import (
	"fmt"
	"testing"
	"time"
)

func TestSpawnRegistryRegisterAndGet(t *testing.T) {
	r := NewSpawnRegistry()

	entry := SpawnEntry{
		SpawnID:   "spawn-ghost_wolf",
		PID:       1234,
		Prompt:    "refactor auth",
		LogPath:   "/tmp/logs/spawn-ghost_wolf.jsonl",
		SpawnTime: time.Now(),
	}

	if err := r.Register(entry); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	got := r.Get("spawn-ghost_wolf")
	if got == nil {
		t.Fatal("expected entry, got nil")
	}
	if got.SpawnID != "spawn-ghost_wolf" {
		t.Errorf("SpawnID = %q, want %q", got.SpawnID, "spawn-ghost_wolf")
	}
	if got.PID != 1234 {
		t.Errorf("PID = %d, want 1234", got.PID)
	}
	if got.Prompt != "refactor auth" {
		t.Errorf("Prompt = %q, want %q", got.Prompt, "refactor auth")
	}
	if got.LogPath != "/tmp/logs/spawn-ghost_wolf.jsonl" {
		t.Errorf("LogPath = %q, want %q", got.LogPath, "/tmp/logs/spawn-ghost_wolf.jsonl")
	}
}

func TestSpawnRegistryGetNotFound(t *testing.T) {
	r := NewSpawnRegistry()

	got := r.Get("nonexistent")
	if got != nil {
		t.Errorf("expected nil for nonexistent entry, got %+v", got)
	}
}

func TestSpawnRegistryGetReturnsCopy(t *testing.T) {
	r := NewSpawnRegistry()
	if err := r.Register(SpawnEntry{
		SpawnID: "spawn-test",
		PID:     100,
		Prompt:  "original",
	}); err != nil {
		t.Fatal(err)
	}

	// Modifying the returned entry should not affect the registry.
	got := r.Get("spawn-test")
	got.Prompt = "modified"

	original := r.Get("spawn-test")
	if original.Prompt != "original" {
		t.Error("Get should return a copy, but modification affected the original")
	}
}

func TestSpawnRegistryDeregister(t *testing.T) {
	r := NewSpawnRegistry()
	_ = r.Register(SpawnEntry{SpawnID: "spawn-to-remove", PID: 1})

	existed := r.Deregister("spawn-to-remove")
	if !existed {
		t.Error("Deregister should return true for existing entry")
	}

	if r.Get("spawn-to-remove") != nil {
		t.Error("entry should be removed after Deregister")
	}
}

func TestSpawnRegistryDeregisterNonexistent(t *testing.T) {
	r := NewSpawnRegistry()

	existed := r.Deregister("nonexistent")
	if existed {
		t.Error("Deregister should return false for nonexistent entry")
	}
}

func TestSpawnRegistryList(t *testing.T) {
	r := NewSpawnRegistry()

	_ = r.Register(SpawnEntry{SpawnID: "spawn-a", PID: 1})
	_ = r.Register(SpawnEntry{SpawnID: "spawn-b", PID: 2})
	_ = r.Register(SpawnEntry{SpawnID: "spawn-c", PID: 3})

	entries := r.List()
	if len(entries) != 3 {
		t.Fatalf("List returned %d entries, want 3", len(entries))
	}

	// Verify all entries are present (order is not guaranteed).
	ids := make(map[string]bool)
	for _, e := range entries {
		ids[e.SpawnID] = true
	}
	for _, want := range []string{"spawn-a", "spawn-b", "spawn-c"} {
		if !ids[want] {
			t.Errorf("List missing entry %q", want)
		}
	}
}

func TestSpawnRegistryListEmpty(t *testing.T) {
	r := NewSpawnRegistry()

	entries := r.List()
	if len(entries) != 0 {
		t.Errorf("List returned %d entries for empty registry, want 0", len(entries))
	}
}

func TestSpawnRegistryRegisterOverwrites(t *testing.T) {
	r := NewSpawnRegistry()

	_ = r.Register(SpawnEntry{SpawnID: "spawn-dup", PID: 100, Prompt: "first"})
	_ = r.Register(SpawnEntry{SpawnID: "spawn-dup", PID: 200, Prompt: "second"})

	got := r.Get("spawn-dup")
	if got.PID != 200 {
		t.Errorf("PID = %d, want 200 (overwritten)", got.PID)
	}
	if got.Prompt != "second" {
		t.Errorf("Prompt = %q, want %q (overwritten)", got.Prompt, "second")
	}

	// Should still be only one entry.
	if len(r.List()) != 1 {
		t.Errorf("List returned %d entries, want 1 after overwrite", len(r.List()))
	}
}

func TestSpawnRegistryRegisterFull(t *testing.T) {
	r := NewSpawnRegistry()

	// Fill the registry to capacity.
	for i := 0; i < maxSpawnEntries; i++ {
		err := r.Register(SpawnEntry{
			SpawnID: fmt.Sprintf("spawn-%d", i),
			PID:     i + 1,
		})
		if err != nil {
			t.Fatalf("Register(%d) returned unexpected error: %v", i, err)
		}
	}

	// Next new entry should be rejected.
	err := r.Register(SpawnEntry{SpawnID: "spawn-overflow", PID: 9999})
	if err == nil {
		t.Fatal("expected error when registry is full, got nil")
	}

	// Re-registering an existing entry should still work (overwrite).
	err = r.Register(SpawnEntry{SpawnID: "spawn-0", PID: 42})
	if err != nil {
		t.Fatalf("re-registration should succeed even when full, got: %v", err)
	}
	if got := r.Get("spawn-0"); got.PID != 42 {
		t.Errorf("PID after re-register = %d, want 42", got.PID)
	}
}

func TestSpawnRegistrySweepDead(t *testing.T) {
	r := NewSpawnRegistry()

	// Override pidAlive to control which PIDs are "alive".
	alive := map[int]bool{100: true, 200: false, 300: true}
	r.pidAlive = func(pid int) bool { return alive[pid] }

	_ = r.Register(SpawnEntry{SpawnID: "spawn-alive-1", PID: 100})
	_ = r.Register(SpawnEntry{SpawnID: "spawn-dead", PID: 200})
	_ = r.Register(SpawnEntry{SpawnID: "spawn-alive-2", PID: 300})

	removed := r.SweepDead()
	if removed != 1 {
		t.Errorf("SweepDead removed %d, want 1", removed)
	}

	if r.Get("spawn-dead") != nil {
		t.Error("dead entry should have been swept")
	}
	if r.Get("spawn-alive-1") == nil {
		t.Error("alive entry spawn-alive-1 should still exist")
	}
	if r.Get("spawn-alive-2") == nil {
		t.Error("alive entry spawn-alive-2 should still exist")
	}
}

func TestSpawnRegistrySweepDeadAllAlive(t *testing.T) {
	r := NewSpawnRegistry()
	r.pidAlive = func(pid int) bool { return true }

	_ = r.Register(SpawnEntry{SpawnID: "spawn-a", PID: 1})
	_ = r.Register(SpawnEntry{SpawnID: "spawn-b", PID: 2})

	removed := r.SweepDead()
	if removed != 0 {
		t.Errorf("SweepDead removed %d, want 0 (all alive)", removed)
	}
	if len(r.List()) != 2 {
		t.Errorf("List returned %d entries, want 2", len(r.List()))
	}
}

func TestSpawnRegistrySweepDeadEmpty(t *testing.T) {
	r := NewSpawnRegistry()

	removed := r.SweepDead()
	if removed != 0 {
		t.Errorf("SweepDead removed %d from empty registry, want 0", removed)
	}
}
