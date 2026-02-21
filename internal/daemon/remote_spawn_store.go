package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	remoteSpawnSchemaVersion = 1
	remoteSpawnFileName      = "remote_spawns.json"
	remoteSpawnMaxRecords    = 512
)

type IdempotencyConflictError struct {
	Provider      string
	RequestID     string
	ExistingSpawn string
}

func (e *IdempotencyConflictError) Error() string {
	return fmt.Sprintf("idempotency conflict: provider=%s request_id=%s already bound to spawn_id=%s", e.Provider, e.RequestID, e.ExistingSpawn)
}

func IsIdempotencyConflict(err error) bool {
	var target *IdempotencyConflictError
	return errors.As(err, &target)
}

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
			return &IdempotencyConflictError{
				Provider:      rec.Provider,
				RequestID:     rec.RequestID,
				ExistingSpawn: e.SpawnID,
			}
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

	state.Records = pruneRemoteSpawnRecords(state.Records, now)

	return s.writeLocked(state)
}

func (s *RemoteSpawnStore) GetByProviderRequest(provider, requestID string) (*RemoteSpawnRecord, error) {
	provider = strings.TrimSpace(provider)
	requestID = strings.TrimSpace(requestID)
	if provider == "" || requestID == "" {
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
		r := state.Records[i]
		if r.Provider == provider && r.RequestID == requestID {
			cp := r
			return &cp, nil
		}
	}
	return nil, nil
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
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("sync temp remote spawn file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("closing temp remote spawn file: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("renaming remote spawn file: %w", err)
	}
	if err := syncDir(s.dir); err != nil {
		return err
	}
	return nil
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open dir for sync: %w", err)
	}
	defer func() { _ = d.Close() }()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("sync dir: %w", err)
	}
	return nil
}

// pruneRemoteSpawnRecords evicts terminal records to keep store size bounded.
// Uses the shared retentionTTL (48h, defined in pool.go) so all daemon data
// expires on the same cadence. Non-terminal records are never pruned.
func pruneRemoteSpawnRecords(records []RemoteSpawnRecord, now time.Time) []RemoteSpawnRecord {
	if len(records) == 0 {
		return records
	}

	// Partition into non-terminal (never pruned) and terminal (eligible for eviction).
	var nonTerminal, terminal []RemoteSpawnRecord
	for _, r := range records {
		if isTerminalRemoteSpawnState(r.State) {
			terminal = append(terminal, r)
		} else {
			nonTerminal = append(nonTerminal, r)
		}
	}

	// TTL-based eviction: drop terminal records older than retention window.
	keptTerminal := terminal[:0]
	for _, r := range terminal {
		if !r.UpdatedAt.IsZero() && now.Sub(r.UpdatedAt) > retentionTTL {
			continue
		}
		keptTerminal = append(keptTerminal, r)
	}

	// Cap-based eviction: if we're still over the limit, drop oldest terminal records.
	total := len(nonTerminal) + len(keptTerminal)
	if total > remoteSpawnMaxRecords && len(keptTerminal) > 0 {
		sort.Slice(keptTerminal, func(i, j int) bool {
			return keptTerminal[i].UpdatedAt.After(keptTerminal[j].UpdatedAt)
		})
		budget := remoteSpawnMaxRecords - len(nonTerminal)
		if budget < 0 {
			budget = 0
		}
		if len(keptTerminal) > budget {
			keptTerminal = keptTerminal[:budget]
		}
	}

	result := make([]RemoteSpawnRecord, 0, len(nonTerminal)+len(keptTerminal))
	result = append(result, nonTerminal...)
	result = append(result, keptTerminal...)
	return result
}

func isTerminalRemoteSpawnState(state RemoteSpawnState) bool {
	return state == RemoteSpawnFailed || state == RemoteSpawnTerminated
}

const fileLockTimeout = 5 * time.Second

func (s *RemoteSpawnStore) lockFile() (func(), error) {
	path := s.path + ".lock"
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening remote spawn lock file: %w", err)
	}

	deadline := time.Now().Add(fileLockTimeout)
	backoff := 5 * time.Millisecond
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			_ = f.Close()
			return nil, fmt.Errorf("locking remote spawn store: %w", err)
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("timed out acquiring remote spawn store lock after %s (another process may be stuck)", fileLockTimeout)
		}
		time.Sleep(backoff)
		backoff = min(backoff*2, 200*time.Millisecond)
	}

	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
