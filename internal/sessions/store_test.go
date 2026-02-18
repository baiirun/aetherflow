package sessions

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreUpsertAndList(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	rec := Record{
		ServerRef: "http://127.0.0.1:4096",
		SessionID: "ses_abc",
		Origin:    OriginPool,
		WorkRef:   "ts-123",
	}
	if err := store.Upsert(rec); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	recs, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(recs))
	}
	if recs[0].SessionID != "ses_abc" {
		t.Fatalf("SessionID = %q, want ses_abc", recs[0].SessionID)
	}
	if recs[0].Status != StatusActive {
		t.Fatalf("Status = %q, want %q", recs[0].Status, StatusActive)
	}
	if recs[0].CreatedAt.IsZero() || recs[0].UpdatedAt.IsZero() {
		t.Fatalf("timestamps were not set: %+v", recs[0])
	}

	oldCreated := recs[0].CreatedAt
	time.Sleep(10 * time.Millisecond)
	rec.AgentID = "worker-1"
	rec.Status = StatusIdle
	if err := store.Upsert(rec); err != nil {
		t.Fatalf("Upsert(update) error = %v", err)
	}

	recs, err = store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(recs))
	}
	if recs[0].CreatedAt != oldCreated {
		t.Fatalf("CreatedAt changed: got %v want %v", recs[0].CreatedAt, oldCreated)
	}
	if recs[0].AgentID != "worker-1" {
		t.Fatalf("AgentID = %q, want worker-1", recs[0].AgentID)
	}
	if recs[0].Status != StatusIdle {
		t.Fatalf("Status = %q, want %q", recs[0].Status, StatusIdle)
	}
	if !recs[0].UpdatedAt.After(oldCreated) {
		t.Fatalf("UpdatedAt = %v, want > %v", recs[0].UpdatedAt, oldCreated)
	}
}

func TestStoreSetStatusByWorkRef(t *testing.T) {
	t.Parallel()

	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	_ = store.Upsert(Record{ServerRef: "http://127.0.0.1:4096", SessionID: "ses_1", Origin: OriginPool, WorkRef: "ts-1"})
	_ = store.Upsert(Record{ServerRef: "http://127.0.0.1:4096", SessionID: "ses_2", Origin: OriginPool, WorkRef: "ts-2"})

	if _, err := store.SetStatusByWorkRef(OriginPool, "ts-1", StatusTerminated); err != nil {
		t.Fatalf("SetStatusByWorkRef() error = %v", err)
	}

	recs, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	var status1, status2 Status
	for _, r := range recs {
		switch r.SessionID {
		case "ses_1":
			status1 = r.Status
		case "ses_2":
			status2 = r.Status
		}
	}
	if status1 != StatusTerminated {
		t.Fatalf("ses_1 status = %q, want %q", status1, StatusTerminated)
	}
	if status2 != StatusActive {
		t.Fatalf("ses_2 status = %q, want %q", status2, StatusActive)
	}
}

func TestStoreSetStatusBySession(t *testing.T) {
	t.Parallel()

	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if err := store.Upsert(Record{ServerRef: "http://127.0.0.1:4096", SessionID: "ses_1", Origin: OriginPool, WorkRef: "ts-1"}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	changed, err := store.SetStatusBySession("http://127.0.0.1:4096", "ses_1", StatusIdle)
	if err != nil {
		t.Fatalf("SetStatusBySession() error = %v", err)
	}
	if !changed {
		t.Fatal("SetStatusBySession() changed = false, want true")
	}

	recs, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(recs) != 1 || recs[0].Status != StatusIdle {
		t.Fatalf("status = %q, want %q", recs[0].Status, StatusIdle)
	}
}

func TestStoreWritesExpectedFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if err := store.Upsert(Record{ServerRef: "http://127.0.0.1:4096", SessionID: "ses_x"}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	path := filepath.Join(dir, fileName)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s) error = %v", path, err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("file mode = %o, want 600", got)
	}
}
