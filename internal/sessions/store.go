package sessions

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
	schemaVersion = 1
	fileName      = "sessions.json"
)

// Status is the lifecycle state tracked by aetherflow's session registry.
type Status string

const (
	StatusActive     Status = "active"
	StatusIdle       Status = "idle"
	StatusTerminated Status = "terminated"
	StatusStale      Status = "stale"
)

// OriginType identifies how a session was created.
type OriginType string

const (
	OriginPool   OriginType = "pool"
	OriginSpawn  OriginType = "spawn"
	OriginManual OriginType = "manual"
)

// Record is one routing/enrichment entry in the global session registry.
type Record struct {
	ServerRef string     `json:"server_ref"`
	SessionID string     `json:"session_id"`
	Directory string     `json:"directory,omitempty"`
	Project   string     `json:"project,omitempty"`
	Origin    OriginType `json:"origin_type,omitempty"`
	WorkRef   string     `json:"work_ref,omitempty"`
	AgentID   string     `json:"agent_id,omitempty"`
	Status    Status     `json:"status"`

	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (r Record) key() string {
	return r.ServerRef + "\x00" + r.SessionID
}

type diskState struct {
	SchemaVersion int      `json:"schema_version"`
	Records       []Record `json:"records"`
}

// Store persists session routing metadata in ~/.config/aetherflow/sessions.
type Store struct {
	dir  string
	path string
	mu   sync.Mutex
}

// DefaultDir returns the default session registry directory.
func DefaultDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolving user config dir: %w", err)
	}
	return filepath.Join(base, "aetherflow", "sessions"), nil
}

// Open returns a Store at dir. Empty dir uses the default config location.
func Open(dir string) (*Store, error) {
	if dir == "" {
		var err error
		dir, err = DefaultDir()
		if err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating sessions dir %s: %w", dir, err)
	}
	return &Store{dir: dir, path: filepath.Join(dir, fileName)}, nil
}

// Path returns the registry file path.
func (s *Store) Path() string { return s.path }

// List returns all records sorted by UpdatedAt descending.
func (s *Store) List() ([]Record, error) {
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
	recs := append([]Record(nil), state.Records...)
	sort.Slice(recs, func(i, j int) bool {
		return recs[i].UpdatedAt.After(recs[j].UpdatedAt)
	})
	return recs, nil
}

// Upsert inserts or updates a session record keyed by {server_ref, session_id}.
func (s *Store) Upsert(rec Record) error {
	if rec.ServerRef == "" {
		return errors.New("server_ref is required")
	}
	if rec.SessionID == "" {
		return errors.New("session_id is required")
	}
	now := time.Now()
	if rec.Status == "" {
		rec.Status = StatusActive
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

	key := rec.key()
	updated := false
	for i := range state.Records {
		if state.Records[i].key() != key {
			continue
		}
		existing := state.Records[i]
		rec.CreatedAt = existing.CreatedAt
		if rec.LastSeenAt.IsZero() {
			rec.LastSeenAt = now
		}
		rec.UpdatedAt = now
		state.Records[i] = rec
		updated = true
		break
	}
	if !updated {
		if rec.CreatedAt.IsZero() {
			rec.CreatedAt = now
		}
		if rec.LastSeenAt.IsZero() {
			rec.LastSeenAt = now
		}
		rec.UpdatedAt = now
		state.Records = append(state.Records, rec)
	}

	return s.writeLocked(state)
}

// SetStatusByWorkRef updates status for matching records.
func (s *Store) SetStatusByWorkRef(origin OriginType, workRef string, status Status) (bool, error) {
	if workRef == "" {
		return false, nil
	}
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	unlock, err := s.lockFile()
	if err != nil {
		return false, err
	}
	defer unlock()

	state, err := s.readLocked()
	if err != nil {
		return false, err
	}
	changed := false
	for i := range state.Records {
		r := &state.Records[i]
		if r.Origin != origin || r.WorkRef != workRef {
			continue
		}
		r.Status = status
		r.UpdatedAt = now
		if status != StatusTerminated {
			r.LastSeenAt = now
		}
		changed = true
	}
	if !changed {
		return false, nil
	}
	if err := s.writeLocked(state); err != nil {
		return false, err
	}
	return true, nil
}

// SetStatusBySession updates a record by canonical {server_ref, session_id}.
func (s *Store) SetStatusBySession(serverRef, sessionID string, status Status) (bool, error) {
	if serverRef == "" || sessionID == "" {
		return false, nil
	}
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	unlock, err := s.lockFile()
	if err != nil {
		return false, err
	}
	defer unlock()

	state, err := s.readLocked()
	if err != nil {
		return false, err
	}
	for i := range state.Records {
		r := &state.Records[i]
		if r.ServerRef != serverRef || r.SessionID != sessionID {
			continue
		}
		r.Status = status
		r.UpdatedAt = now
		if status != StatusTerminated {
			r.LastSeenAt = now
		}
		if err := s.writeLocked(state); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func (s *Store) readLocked() (diskState, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return diskState{SchemaVersion: schemaVersion, Records: nil}, nil
		}
		return diskState{}, fmt.Errorf("reading sessions registry: %w", err)
	}

	var state diskState
	if err := json.Unmarshal(data, &state); err != nil {
		return diskState{}, fmt.Errorf("parsing sessions registry: %w", err)
	}
	if state.SchemaVersion == 0 {
		state.SchemaVersion = schemaVersion
	}
	if state.SchemaVersion > schemaVersion {
		return diskState{}, fmt.Errorf("unsupported sessions schema version: %d", state.SchemaVersion)
	}
	return state, nil
}

func (s *Store) writeLocked(state diskState) error {
	state.SchemaVersion = schemaVersion
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling sessions registry: %w", err)
	}

	tmp, err := os.CreateTemp(s.dir, ".sessions-*.json")
	if err != nil {
		return fmt.Errorf("creating temp registry file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("writing temp registry file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod temp registry file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("closing temp registry file: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("renaming sessions registry: %w", err)
	}
	return nil
}

func (s *Store) lockFile() (func(), error) {
	path := s.path + ".lock"
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("locking sessions registry: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
