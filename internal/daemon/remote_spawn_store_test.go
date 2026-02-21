package daemon

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestRemoteSpawnStoreUpsertGet(t *testing.T) {
	t.Parallel()

	store, err := OpenRemoteSpawnStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenRemoteSpawnStore() error = %v", err)
	}
	rec := RemoteSpawnRecord{
		SpawnID:   "spn_1",
		Provider:  "sprites",
		RequestID: "req_1",
		State:     RemoteSpawnSpawning,
	}
	if err := store.Upsert(rec); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	got, err := store.GetBySpawnID("spn_1")
	if err != nil {
		t.Fatalf("GetBySpawnID() error = %v", err)
	}
	if got == nil {
		t.Fatal("GetBySpawnID() = nil, want record")
	}
	if got.SpawnID != "spn_1" || got.RequestID != "req_1" {
		t.Fatalf("GetBySpawnID() = %+v, want spawn_id spn_1 request_id req_1", got)
	}
}

func TestRemoteSpawnStoreIdempotencyConflict(t *testing.T) {
	t.Parallel()

	store, err := OpenRemoteSpawnStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenRemoteSpawnStore() error = %v", err)
	}
	if err := store.Upsert(RemoteSpawnRecord{SpawnID: "spn_1", Provider: "sprites", RequestID: "req_1"}); err != nil {
		t.Fatalf("first Upsert() error = %v", err)
	}
	err = store.Upsert(RemoteSpawnRecord{SpawnID: "spn_2", Provider: "sprites", RequestID: "req_1"})
	if err == nil {
		t.Fatal("second Upsert() error = nil, want idempotency conflict")
	}
	var conflict *IdempotencyConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected IdempotencyConflictError, got %T (%v)", err, err)
	}
	if !IsIdempotencyConflict(err) {
		t.Fatal("expected IsIdempotencyConflict(err) == true")
	}
}

func TestRemoteSpawnStoreGetByProviderRequest(t *testing.T) {
	t.Parallel()

	store, err := OpenRemoteSpawnStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenRemoteSpawnStore() error = %v", err)
	}
	if err := store.Upsert(RemoteSpawnRecord{SpawnID: "spn_abc", Provider: "sprites", RequestID: "req_xyz", State: RemoteSpawnSpawning}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	got, err := store.GetByProviderRequest("sprites", "req_xyz")
	if err != nil {
		t.Fatalf("GetByProviderRequest() error = %v", err)
	}
	if got == nil || got.SpawnID != "spn_abc" {
		t.Fatalf("GetByProviderRequest() = %+v, want spawn_id spn_abc", got)
	}
}

func TestPruneRemoteSpawnRecordsDropsOldTerminal(t *testing.T) {
	t.Parallel()
	now := time.Now()
	recs := []RemoteSpawnRecord{
		{SpawnID: "old-failed", State: RemoteSpawnFailed, UpdatedAt: now.Add(-retentionTTL - time.Minute)},
		{SpawnID: "recent-failed", State: RemoteSpawnFailed, UpdatedAt: now.Add(-time.Minute)},
		{SpawnID: "running", State: RemoteSpawnRunning, UpdatedAt: now.Add(-retentionTTL - time.Hour)},
	}
	got := pruneRemoteSpawnRecords(recs, now)
	if len(got) != 2 {
		t.Fatalf("len(pruned) = %d, want 2", len(got))
	}
	for _, r := range got {
		if r.SpawnID == "old-failed" {
			t.Fatal("expected old terminal record to be pruned")
		}
	}
}

func TestPruneRemoteSpawnRecordsNeverDropsNonTerminal(t *testing.T) {
	t.Parallel()
	now := time.Now()
	recs := make([]RemoteSpawnRecord, 0, remoteSpawnMaxRecords+10)
	for i := 0; i < remoteSpawnMaxRecords+10; i++ {
		recs = append(recs, RemoteSpawnRecord{
			SpawnID:   fmt.Sprintf("spn_%03d", i),
			State:     RemoteSpawnRunning,
			UpdatedAt: now.Add(-time.Duration(i) * time.Second),
		})
	}
	got := pruneRemoteSpawnRecords(recs, now)
	// Non-terminal records must never be pruned, even over the cap.
	if len(got) != remoteSpawnMaxRecords+10 {
		t.Fatalf("len(pruned) = %d, want %d (non-terminal records must not be dropped)", len(got), remoteSpawnMaxRecords+10)
	}
}

func TestPruneRemoteSpawnRecordsCapsTerminalRecords(t *testing.T) {
	t.Parallel()
	now := time.Now()
	recs := make([]RemoteSpawnRecord, 0, remoteSpawnMaxRecords+20)
	// 10 non-terminal (running) records.
	for i := 0; i < 10; i++ {
		recs = append(recs, RemoteSpawnRecord{
			SpawnID:   fmt.Sprintf("running_%03d", i),
			State:     RemoteSpawnRunning,
			UpdatedAt: now.Add(-time.Duration(i) * time.Second),
		})
	}
	// remoteSpawnMaxRecords + 10 terminal (failed) records — more than enough to exceed the cap.
	for i := 0; i < remoteSpawnMaxRecords+10; i++ {
		recs = append(recs, RemoteSpawnRecord{
			SpawnID:   fmt.Sprintf("failed_%03d", i),
			State:     RemoteSpawnFailed,
			UpdatedAt: now.Add(-time.Duration(i) * time.Second),
		})
	}
	got := pruneRemoteSpawnRecords(recs, now)
	// Expect: all 10 running + (512 - 10) = 502 terminal = 512 total
	if len(got) != remoteSpawnMaxRecords {
		t.Fatalf("len(pruned) = %d, want %d", len(got), remoteSpawnMaxRecords)
	}
	// Verify all running records survived.
	runningCount := 0
	for _, r := range got {
		if r.State == RemoteSpawnRunning {
			runningCount++
		}
	}
	if runningCount != 10 {
		t.Fatalf("running records = %d, want 10", runningCount)
	}
}
