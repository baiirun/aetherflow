package daemon

import (
	"fmt"
	"sync"
	"time"
)

const (
	// maxSpawnEntries caps the registry to prevent memory exhaustion from
	// runaway or malicious spawn.register RPCs. 128 is generous — even a
	// busy team won't run 128 concurrent ad-hoc agents.
	maxSpawnEntries = 128

	// maxSpawnPromptLen caps the stored prompt to prevent large payloads
	// from inflating daemon memory. The prompt is only used for display
	// (truncated to 80 runes in status views), so 8 KiB is generous.
	maxSpawnPromptLen = 8192

	// exitedSpawnTTL is how long an exited spawn entry is kept in the
	// registry before being swept. This preserves the agent→session mapping
	// so af status <agent> works after the agent process exits.
	exitedSpawnTTL = 1 * time.Hour
)

// SpawnState is the lifecycle state of a spawn entry.
type SpawnState string

const (
	SpawnRunning SpawnState = "running"
	SpawnExited  SpawnState = "exited"
)

// SpawnEntry tracks a spawned agent registered with the daemon.
// Spawned agents are outside the pool — they don't consume pool slots
// and aren't managed by the daemon's scheduler. Registration is purely
// for observability (af status, af logs, af status <agent>).
//
// Entries transition from running → exited when the agent process exits.
// Exited entries are kept for exitedSpawnTTL so af status <agent> works
// after exit. The periodic sweep removes them after the TTL expires.
type SpawnEntry struct {
	SpawnID   string     `json:"spawn_id"`
	PID       int        `json:"pid"`
	SessionID string     `json:"session_id,omitempty"`
	State     SpawnState `json:"state"`
	Prompt    string     `json:"prompt"`
	SpawnTime time.Time  `json:"spawn_time"`
	ExitedAt  time.Time  `json:"exited_at,omitempty"`
}

// SpawnRegistry tracks spawned agents for observability.
// All methods are safe for concurrent use.
type SpawnRegistry struct {
	mu       sync.RWMutex
	entries  map[string]*SpawnEntry // keyed by spawn ID
	pidAlive func(int) bool
}

// NewSpawnRegistry creates an empty registry.
func NewSpawnRegistry() *SpawnRegistry {
	return &SpawnRegistry{
		entries:  make(map[string]*SpawnEntry),
		pidAlive: defaultPIDAlive,
	}
}

// Register adds a spawned agent to the registry.
// If a spawn with the same ID already exists, it is overwritten (re-registration).
// Returns an error if the registry is full of running entries and this is a new entry.
// The caller must set entry.State explicitly (typically SpawnRunning).
func (r *SpawnRegistry) Register(entry SpawnEntry) error {
	if entry.SpawnID == "" {
		panic("spawn registry: Register called with empty SpawnID")
	}
	if entry.PID <= 0 {
		panic("spawn registry: Register called with non-positive PID")
	}
	if entry.State != SpawnRunning && entry.State != SpawnExited {
		panic(fmt.Sprintf("spawn registry: Register called with invalid State %q", entry.State))
	}
	if entry.State == SpawnExited && entry.ExitedAt.IsZero() {
		panic("spawn registry: Register called with SpawnExited but zero ExitedAt")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Allow re-registration of an existing spawn (same ID), but reject
	// new entries when running entries are at capacity. Exited entries don't
	// count — they're observability artifacts bounded by TTL, not resources.
	if _, exists := r.entries[entry.SpawnID]; !exists {
		running := 0
		for _, e := range r.entries {
			if e.State == SpawnRunning {
				running++
			}
		}
		if running >= maxSpawnEntries {
			return fmt.Errorf("spawn registry full (%d running entries)", maxSpawnEntries)
		}
	}

	r.entries[entry.SpawnID] = &entry
	return nil
}

// MarkExited transitions a spawn entry from running to exited.
// The entry remains in the registry (preserving the agent→session mapping)
// until the periodic sweep removes it after exitedSpawnTTL.
// Returns false when the spawn is not registered or already exited.
// Idempotent for already-exited entries — does not reset the TTL clock.
func (r *SpawnRegistry) MarkExited(spawnID string) bool {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.entries[spawnID]
	if !ok {
		return false
	}
	if entry.State != SpawnRunning {
		return false // already exited — preserve original ExitedAt
	}
	entry.State = SpawnExited
	entry.ExitedAt = now
	return true
}

// Get returns a spawn entry by ID, or nil if not found.
func (r *SpawnRegistry) Get(spawnID string) *SpawnEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if e, ok := r.entries[spawnID]; ok {
		cp := *e
		return &cp
	}
	return nil
}

// SetSessionID updates the session ID for an existing spawn entry.
// Returns false when the spawn is not registered.
func (r *SpawnRegistry) SetSessionID(spawnID, sessionID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.entries[spawnID]
	if !ok {
		return false
	}
	entry.SessionID = sessionID
	return true
}

// List returns all registered spawn entries.
func (r *SpawnRegistry) List() []SpawnEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]SpawnEntry, 0, len(r.entries))
	for _, e := range r.entries {
		result = append(result, *e)
	}
	return result
}

// SweepResult is the outcome of a SweepDead call.
type SweepResult struct {
	Marked  int // running entries transitioned to exited
	Removed int // exited entries deleted past TTL
}

// Total returns the number of entries affected by the sweep.
func (r SweepResult) Total() int { return r.Marked + r.Removed }

// SweepDead marks running entries whose PID is no longer alive as exited,
// and removes exited entries that have exceeded exitedSpawnTTL.
// Called periodically by the daemon.
//
// Uses a two-phase approach: collect candidates under read lock (so
// pidAlive syscalls don't block concurrent Get/List/Register), then
// mutate under write lock. Both phases re-verify state to handle races
// between phases (e.g., a re-registered entry must not be swept).
func (r *SpawnRegistry) SweepDead() SweepResult {
	now := time.Now()

	// Phase 1: identify candidates under read lock.
	r.mu.RLock()
	var toMark []string   // running entries with dead PIDs → mark exited
	var toRemove []string // exited entries past TTL → remove
	for id, entry := range r.entries {
		switch entry.State {
		case SpawnRunning:
			if !r.pidAlive(entry.PID) {
				toMark = append(toMark, id)
			}
		case SpawnExited:
			if now.Sub(entry.ExitedAt) > exitedSpawnTTL {
				toRemove = append(toRemove, id)
			}
		default:
			// Unknown state is a bug — treat as dead to prevent immortal entries.
			toMark = append(toMark, id)
		}
	}
	r.mu.RUnlock()

	if len(toMark) == 0 && len(toRemove) == 0 {
		return SweepResult{}
	}

	// Phase 2: mutate under write lock.
	// Re-check state: entries may have been re-registered or marked exited
	// by another goroutine between the two lock phases.
	r.mu.Lock()
	defer r.mu.Unlock()
	var result SweepResult
	for _, id := range toMark {
		if entry, exists := r.entries[id]; exists && entry.State == SpawnRunning {
			entry.State = SpawnExited
			entry.ExitedAt = now
			result.Marked++
		}
	}
	for _, id := range toRemove {
		if entry, exists := r.entries[id]; exists && entry.State == SpawnExited {
			delete(r.entries, id)
			result.Removed++
		}
	}
	return result
}
