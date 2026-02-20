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
		State:     SpawnRunning,
		Prompt:    "refactor auth",
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
	if got.State != SpawnRunning {
		t.Errorf("State = %q, want %q", got.State, SpawnRunning)
	}
	if got.Prompt != "refactor auth" {
		t.Errorf("Prompt = %q, want %q", got.Prompt, "refactor auth")
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
		State:   SpawnRunning,
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

func TestSpawnRegistryList(t *testing.T) {
	r := NewSpawnRegistry()

	_ = r.Register(SpawnEntry{SpawnID: "spawn-a", PID: 1, State: SpawnRunning})
	_ = r.Register(SpawnEntry{SpawnID: "spawn-b", PID: 2, State: SpawnRunning})
	_ = r.Register(SpawnEntry{SpawnID: "spawn-c", PID: 3, State: SpawnRunning})

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

	_ = r.Register(SpawnEntry{SpawnID: "spawn-dup", PID: 100, State: SpawnRunning, Prompt: "first"})
	_ = r.Register(SpawnEntry{SpawnID: "spawn-dup", PID: 200, State: SpawnRunning, Prompt: "second"})

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

func TestSpawnRegistryRegisterFullCountsOnlyRunning(t *testing.T) {
	r := NewSpawnRegistry()

	// Fill the registry to capacity with running entries.
	for i := 0; i < maxSpawnEntries; i++ {
		err := r.Register(SpawnEntry{
			SpawnID: fmt.Sprintf("spawn-%d", i),
			PID:     i + 1,
			State:   SpawnRunning,
		})
		if err != nil {
			t.Fatalf("Register(%d) returned unexpected error: %v", i, err)
		}
	}

	// Next new entry should be rejected.
	err := r.Register(SpawnEntry{SpawnID: "spawn-overflow", PID: 9999, State: SpawnRunning})
	if err == nil {
		t.Fatal("expected error when registry is full, got nil")
	}

	// Re-registering an existing entry should still work (overwrite).
	err = r.Register(SpawnEntry{SpawnID: "spawn-0", PID: 42, State: SpawnRunning})
	if err != nil {
		t.Fatalf("re-registration should succeed even when full, got: %v", err)
	}
	if got := r.Get("spawn-0"); got.PID != 42 {
		t.Errorf("PID after re-register = %d, want 42", got.PID)
	}
}

func TestSpawnRegistryRegisterFullExitedDontCount(t *testing.T) {
	r := NewSpawnRegistry()

	// Fill registry with exited entries — should not block new running entries.
	for i := 0; i < maxSpawnEntries; i++ {
		err := r.Register(SpawnEntry{
			SpawnID:  fmt.Sprintf("spawn-exited-%d", i),
			PID:      i + 1,
			State:    SpawnExited,
			ExitedAt: time.Now(),
		})
		if err != nil {
			t.Fatalf("Register exited(%d) returned unexpected error: %v", i, err)
		}
	}

	// A new running entry should succeed — exited entries don't count toward the cap.
	err := r.Register(SpawnEntry{SpawnID: "spawn-new-running", PID: 9999, State: SpawnRunning})
	if err != nil {
		t.Fatalf("new running entry should succeed when all existing are exited, got: %v", err)
	}
}

func TestSpawnRegistryRegisterPanicsOnInvalidState(t *testing.T) {
	r := NewSpawnRegistry()

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for invalid state, got none")
		}
	}()

	_ = r.Register(SpawnEntry{SpawnID: "spawn-bad", PID: 1, State: "zombie"})
}

func TestSpawnRegistryRegisterPanicsOnExitedWithoutTimestamp(t *testing.T) {
	r := NewSpawnRegistry()

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for exited without ExitedAt, got none")
		}
	}()

	_ = r.Register(SpawnEntry{SpawnID: "spawn-bad", PID: 1, State: SpawnExited})
}

func TestSpawnRegistrySweepDeadMarksExited(t *testing.T) {
	r := NewSpawnRegistry()

	// Override pidAlive to control which PIDs are "alive".
	alive := map[int]bool{100: true, 200: false, 300: true}
	r.pidAlive = func(pid int) bool { return alive[pid] }

	_ = r.Register(SpawnEntry{SpawnID: "spawn-alive-1", PID: 100, State: SpawnRunning})
	_ = r.Register(SpawnEntry{SpawnID: "spawn-dead", PID: 200, State: SpawnRunning})
	_ = r.Register(SpawnEntry{SpawnID: "spawn-alive-2", PID: 300, State: SpawnRunning})

	result := r.SweepDead()
	if result.Marked != 1 {
		t.Errorf("SweepDead marked %d, want 1", result.Marked)
	}
	if result.Removed != 0 {
		t.Errorf("SweepDead removed %d, want 0", result.Removed)
	}

	// Dead entry should be marked exited, not removed.
	dead := r.Get("spawn-dead")
	if dead == nil {
		t.Fatal("dead entry should still exist (marked exited, not removed)")
	}
	if dead.State != SpawnExited {
		t.Errorf("State = %q, want %q", dead.State, SpawnExited)
	}
	if dead.ExitedAt.IsZero() {
		t.Error("ExitedAt should be set")
	}

	// Alive entries should still be running.
	if a := r.Get("spawn-alive-1"); a == nil || a.State != SpawnRunning {
		t.Error("alive entry spawn-alive-1 should still be running")
	}
	if a := r.Get("spawn-alive-2"); a == nil || a.State != SpawnRunning {
		t.Error("alive entry spawn-alive-2 should still be running")
	}
}

func TestSpawnRegistrySweepRemovesExpiredExited(t *testing.T) {
	r := NewSpawnRegistry()
	r.pidAlive = func(pid int) bool { return true }

	// Register an already-exited entry with an old ExitedAt.
	_ = r.Register(SpawnEntry{
		SpawnID:  "spawn-old-exit",
		PID:      100,
		State:    SpawnExited,
		ExitedAt: time.Now().Add(-2 * exitedSpawnTTL), // well past TTL
	})
	// Register a recently-exited entry.
	_ = r.Register(SpawnEntry{
		SpawnID:  "spawn-recent-exit",
		PID:      200,
		State:    SpawnExited,
		ExitedAt: time.Now().Add(-1 * time.Minute), // within TTL
	})
	// Register a running entry.
	_ = r.Register(SpawnEntry{SpawnID: "spawn-running", PID: 300, State: SpawnRunning})

	result := r.SweepDead()
	if result.Removed != 1 {
		t.Errorf("SweepDead removed %d, want 1 (removed expired exited)", result.Removed)
	}
	if result.Marked != 0 {
		t.Errorf("SweepDead marked %d, want 0", result.Marked)
	}

	if r.Get("spawn-old-exit") != nil {
		t.Error("expired exited entry should have been removed")
	}
	if r.Get("spawn-recent-exit") == nil {
		t.Error("recently exited entry should still exist")
	}
	if r.Get("spawn-running") == nil {
		t.Error("running entry should still exist")
	}
}

func TestSpawnRegistrySweepDoesNotRemoveReRegisteredEntry(t *testing.T) {
	// Regression test for TOCTOU race: if an entry is identified for removal
	// in phase 1 but re-registered (as running) before phase 2, the sweep
	// must not delete the new running entry.
	r := NewSpawnRegistry()
	r.pidAlive = func(pid int) bool { return true }

	// Register an exited entry past TTL.
	_ = r.Register(SpawnEntry{
		SpawnID:  "spawn-reused",
		PID:      100,
		State:    SpawnExited,
		ExitedAt: time.Now().Add(-2 * exitedSpawnTTL),
	})

	// Simulate the race: overwrite with a fresh running entry before sweep's
	// write phase. We can't truly race, but we can verify the re-check guard
	// by overwriting between the two phases. Since SweepDead is atomic from
	// the outside, we test the guard by overwriting before calling SweepDead
	// and verifying the running entry survives (the read phase won't see it
	// as exited). For a true TOCTOU test, we verify the guard code path by
	// checking that re-registered running entries aren't removed.
	_ = r.Register(SpawnEntry{
		SpawnID: "spawn-reused",
		PID:     200,
		State:   SpawnRunning,
	})

	result := r.SweepDead()
	// The entry is now running, so it should not be removed or marked.
	if result.Removed != 0 {
		t.Errorf("SweepDead removed %d, want 0 (re-registered entry should survive)", result.Removed)
	}

	got := r.Get("spawn-reused")
	if got == nil {
		t.Fatal("re-registered entry should still exist")
	}
	if got.State != SpawnRunning {
		t.Errorf("State = %q, want %q", got.State, SpawnRunning)
	}
	if got.PID != 200 {
		t.Errorf("PID = %d, want 200 (new registration)", got.PID)
	}
}

func TestSpawnRegistryMarkExited(t *testing.T) {
	r := NewSpawnRegistry()
	_ = r.Register(SpawnEntry{SpawnID: "spawn-test", PID: 100, State: SpawnRunning})

	if !r.MarkExited("spawn-test") {
		t.Error("MarkExited should return true for running entry")
	}

	got := r.Get("spawn-test")
	if got == nil {
		t.Fatal("entry should still exist after MarkExited")
	}
	if got.State != SpawnExited {
		t.Errorf("State = %q, want %q", got.State, SpawnExited)
	}
	if got.ExitedAt.IsZero() {
		t.Error("ExitedAt should be set")
	}
}

func TestSpawnRegistryMarkExitedNonexistent(t *testing.T) {
	r := NewSpawnRegistry()

	if r.MarkExited("nonexistent") {
		t.Error("MarkExited should return false for nonexistent entry")
	}
}

func TestSpawnRegistryMarkExitedIdempotent(t *testing.T) {
	r := NewSpawnRegistry()
	_ = r.Register(SpawnEntry{SpawnID: "spawn-test", PID: 100, State: SpawnRunning})

	// First call succeeds.
	if !r.MarkExited("spawn-test") {
		t.Error("first MarkExited should return true")
	}

	originalExitedAt := r.Get("spawn-test").ExitedAt

	// Second call returns false — already exited, TTL not reset.
	if r.MarkExited("spawn-test") {
		t.Error("second MarkExited should return false (already exited)")
	}

	got := r.Get("spawn-test")
	if got.ExitedAt != originalExitedAt {
		t.Error("ExitedAt should not change on double-exit")
	}
}

func TestSpawnRegistrySweepDeadAllAlive(t *testing.T) {
	r := NewSpawnRegistry()
	r.pidAlive = func(pid int) bool { return true }

	_ = r.Register(SpawnEntry{SpawnID: "spawn-a", PID: 1, State: SpawnRunning})
	_ = r.Register(SpawnEntry{SpawnID: "spawn-b", PID: 2, State: SpawnRunning})

	result := r.SweepDead()
	if result.Total() != 0 {
		t.Errorf("SweepDead total %d, want 0 (all alive)", result.Total())
	}
	if len(r.List()) != 2 {
		t.Errorf("List returned %d entries, want 2", len(r.List()))
	}
}

func TestSpawnRegistrySweepDeadEmpty(t *testing.T) {
	r := NewSpawnRegistry()

	result := r.SweepDead()
	if result.Total() != 0 {
		t.Errorf("SweepDead total %d from empty registry, want 0", result.Total())
	}
}
