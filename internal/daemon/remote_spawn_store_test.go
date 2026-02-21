package daemon

import "testing"

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
	if err := store.Upsert(RemoteSpawnRecord{SpawnID: "spn_2", Provider: "sprites", RequestID: "req_1"}); err == nil {
		t.Fatal("second Upsert() error = nil, want idempotency conflict")
	}
}
