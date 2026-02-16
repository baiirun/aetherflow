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
)

// SpawnEntry tracks a spawned agent registered with the daemon.
// Spawned agents are outside the pool — they don't consume pool slots
// and aren't managed by the daemon's scheduler. Registration is purely
// for observability (af status, af logs, af status <agent>).
type SpawnEntry struct {
	SpawnID   string    `json:"spawn_id"`
	PID       int       `json:"pid"`
	Prompt    string    `json:"prompt"`
	LogPath   string    `json:"log_path"`
	SpawnTime time.Time `json:"spawn_time"`
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
// Returns an error if the registry is full and this is a new entry.
func (r *SpawnRegistry) Register(entry SpawnEntry) error {
	if entry.SpawnID == "" {
		panic("spawn registry: Register called with empty SpawnID")
	}
	if entry.PID <= 0 {
		panic("spawn registry: Register called with non-positive PID")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Allow re-registration of an existing spawn (same ID), but reject
	// new entries when at capacity.
	if _, exists := r.entries[entry.SpawnID]; !exists && len(r.entries) >= maxSpawnEntries {
		return fmt.Errorf("spawn registry full (%d entries)", maxSpawnEntries)
	}

	r.entries[entry.SpawnID] = &entry
	return nil
}

// Deregister removes a spawned agent from the registry.
// Returns true if the entry existed and was removed.
func (r *SpawnRegistry) Deregister(spawnID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, existed := r.entries[spawnID]
	delete(r.entries, spawnID)
	return existed
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

// SweepDead removes entries whose PID is no longer alive.
// Called periodically by the daemon alongside the pool sweep.
//
// Uses a two-phase approach: collect dead PIDs under read lock (so
// pidAlive syscalls don't block concurrent Get/List/Register), then
// delete under write lock.
func (r *SpawnRegistry) SweepDead() int {
	// Phase 1: identify dead PIDs under read lock.
	r.mu.RLock()
	var dead []string
	for id, entry := range r.entries {
		if !r.pidAlive(entry.PID) {
			dead = append(dead, id)
		}
	}
	r.mu.RUnlock()

	if len(dead) == 0 {
		return 0
	}

	// Phase 2: remove dead entries under write lock.
	// Re-check existence: entry may have been deregistered between phases.
	r.mu.Lock()
	defer r.mu.Unlock()
	removed := 0
	for _, id := range dead {
		if _, exists := r.entries[id]; exists {
			delete(r.entries, id)
			removed++
		}
	}
	return removed
}
