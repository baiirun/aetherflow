package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"
)

const (
	remoteSpawnSchemaVersion = 1
	remoteSpawnFileName      = "remote_spawns.json"
)

type RemoteSpawnState string

const (
	RemoteSpawnRequested  RemoteSpawnState = "requested"
	RemoteSpawnSpawning   RemoteSpawnState = "spawning"
	RemoteSpawnRunning    RemoteSpawnState = "running"
	RemoteSpawnFailed     RemoteSpawnState = "failed"
	RemoteSpawnTerminated RemoteSpawnState = "terminated"
	RemoteSpawnUnknown    RemoteSpawnState = "unknown"
)

type RemoteSpawnRecord struct {
	SpawnID           string           `json:"spawn_id"`
	Provider          string           `json:"provider"`
	RequestID         string           `json:"request_id"`
	ProviderSandboxID string           `json:"provider_sandbox_id,omitempty"`
	ProviderOperation string           `json:"provider_operation_id,omitempty"`
	ServerRef         string           `json:"server_ref,omitempty"`
	SessionID         string           `json:"session_id,omitempty"`
	State             RemoteSpawnState `json:"state"`
	LastError         string           `json:"last_error,omitempty"`
	CreatedAt         time.Time        `json:"created_at"`
	UpdatedAt         time.Time        `json:"updated_at"`
	LastReconciledAt  time.Time        `json:"last_reconciled_at,omitempty"`
}

type remoteSpawnDiskState struct {
	SchemaVersion int                 `json:"schema_version"`
	Records       []RemoteSpawnRecord `json:"records"`
}

type RemoteSpawnStore struct {
	dir  string
	path string
	mu   sync.Mutex
}

func OpenRemoteSpawnStore(dir string) (*RemoteSpawnStore, error) {
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return nil, fmt.Errorf("resolving user config dir: %w", err)
		}
		dir = filepath.Join(base, "aetherflow", "sessions")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating remote spawn dir %s: %w", dir, err)
	}
	return &RemoteSpawnStore{dir: dir, path: filepath.Join(dir, remoteSpawnFileName)}, nil
}

func (s *RemoteSpawnStore) Upsert(rec RemoteSpawnRecord) error {
	if rec.SpawnID == "" {
		return errors.New("spawn_id is required")
	}
	if rec.Provider == "" {
		return errors.New("provider is required")
	}
	if rec.RequestID == "" {
		return errors.New("request_id is required")
	}
	now := time.Now()
	if rec.State == "" {
		rec.State = RemoteSpawnRequested
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	unlock, err := s.lockFile()
	if err != nil {
		return err
	}
	defer unlock()

	state, err := s.readLocked()
	if err != nil {
		return err
	}

	// Idempotency uniqueness guardrail.
	for _, e := range state.Records {
		if e.Provider == rec.Provider && e.RequestID == rec.RequestID && e.SpawnID != rec.SpawnID {
			return fmt.Errorf("idempotency conflict: provider=%s request_id=%s already bound to spawn_id=%s", rec.Provider, rec.RequestID, e.SpawnID)
		}
	}

	updated := false
	for i := range state.Records {
		if state.Records[i].SpawnID != rec.SpawnID {
			continue
		}
		rec.CreatedAt = state.Records[i].CreatedAt
		rec.UpdatedAt = now
		state.Records[i] = rec
		updated = true
		break
	}
	if !updated {
		rec.CreatedAt = now
		rec.UpdatedAt = now
		state.Records = append(state.Records, rec)
	}

	return s.writeLocked(state)
}

func (s *RemoteSpawnStore) GetBySpawnID(spawnID string) (*RemoteSpawnRecord, error) {
	if spawnID == "" {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	unlock, err := s.lockFile()
	if err != nil {
		return nil, err
	}
	defer unlock()

	state, err := s.readLocked()
	if err != nil {
		return nil, err
	}
	for i := range state.Records {
		if state.Records[i].SpawnID == spawnID {
			cp := state.Records[i]
			return &cp, nil
		}
	}
	return nil, nil
}

func (s *RemoteSpawnStore) List() ([]RemoteSpawnRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	unlock, err := s.lockFile()
	if err != nil {
		return nil, err
	}
	defer unlock()

	state, err := s.readLocked()
	if err != nil {
		return nil, err
	}
	recs := append([]RemoteSpawnRecord(nil), state.Records...)
	sort.Slice(recs, func(i, j int) bool {
		return recs[i].UpdatedAt.After(recs[j].UpdatedAt)
	})
	return recs, nil
}

func (s *RemoteSpawnStore) readLocked() (remoteSpawnDiskState, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return remoteSpawnDiskState{SchemaVersion: remoteSpawnSchemaVersion}, nil
		}
		return remoteSpawnDiskState{}, fmt.Errorf("reading remote spawn store: %w", err)
	}
	var state remoteSpawnDiskState
	if err := json.Unmarshal(data, &state); err != nil {
		return remoteSpawnDiskState{}, fmt.Errorf("parsing remote spawn store: %w", err)
	}
	if state.SchemaVersion == 0 {
		state.SchemaVersion = remoteSpawnSchemaVersion
	}
	if state.SchemaVersion > remoteSpawnSchemaVersion {
		return remoteSpawnDiskState{}, fmt.Errorf("unsupported remote spawn schema version: %d", state.SchemaVersion)
	}
	return state, nil
}

func (s *RemoteSpawnStore) writeLocked(state remoteSpawnDiskState) error {
	state.SchemaVersion = remoteSpawnSchemaVersion
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling remote spawn store: %w", err)
	}
	tmp, err := os.CreateTemp(s.dir, ".remote-spawns-*.json")
	if err != nil {
		return fmt.Errorf("creating temp remote spawn file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("writing temp remote spawn file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod temp remote spawn file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("closing temp remote spawn file: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("renaming remote spawn file: %w", err)
	}
	return nil
}

func (s *RemoteSpawnStore) lockFile() (func(), error) {
	path := s.path + ".lock"
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening remote spawn lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("locking remote spawn store: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
