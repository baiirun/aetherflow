// Package inbox implements message inboxes for agents and teams.
//
// Design: Pull-based model
//
// Agents poll their inbox to receive messages. Push/interrupt is not possible
// because MCP tools cannot invoke Claude - they can only respond when called.
// This means agents must check their inbox at natural breakpoints (after tool
// calls, between subtasks, etc.) to receive messages.
//
// Messages are never removed - they accumulate and agents use the --since
// parameter to only fetch new messages. Agent tracks last seen timestamp
// in its own context.
//
// Persistence: Inboxes are not persisted. On daemon restart, inboxes are empty.
// This is acceptable because agents re-raise blockers/questions when they hit
// the same issues again.
package inbox

import (
	"fmt"
	"sync"

	"github.com/geobrowser/aetherflow/internal/protocol"
)

// Config holds inbox configuration.
type Config struct {
	// Cap is the max messages per inbox (0 = unlimited).
	Cap int
}

// Store manages inboxes for agents and teams.
type Store struct {
	config  Config
	inboxes map[string][]*protocol.Message // inbox ID -> messages
	mu      sync.RWMutex
}

// New creates a new inbox store.
func New(cfg Config) *Store {
	return &Store{
		config:  cfg,
		inboxes: make(map[string][]*protocol.Message),
	}
}

// Push adds a message to an inbox.
// The inbox ID is derived from the message's To address.
// Returns ErrQueueFull if the inbox is at capacity.
func (s *Store) Push(msg *protocol.Message) error {
	if err := msg.Validate(); err != nil {
		return fmt.Errorf("invalid message: %w", err)
	}

	inboxID := msg.To.String()

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.config.Cap > 0 && len(s.inboxes[inboxID]) >= s.config.Cap {
		return ErrQueueFull
	}

	s.inboxes[inboxID] = append(s.inboxes[inboxID], msg)
	return nil
}

// Peek returns messages from an inbox without removing them.
// If since > 0, only returns messages with TS > since.
// If limit > 0, returns at most limit messages.
// Messages are returned in chronological order (oldest first).
func (s *Store) Peek(inboxID string, since int64, limit int) []*protocol.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	msgs, ok := s.inboxes[inboxID]
	if !ok {
		return nil
	}

	// Filter by since
	var filtered []*protocol.Message
	for _, msg := range msgs {
		if since <= 0 || msg.TS > since {
			filtered = append(filtered, msg)
		}
	}

	// Apply limit
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}

	return filtered
}

// Depth returns the number of messages in an inbox.
func (s *Store) Depth(inboxID string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.inboxes[inboxID])
}

// Delete removes an inbox entirely.
func (s *Store) Delete(inboxID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inboxes, inboxID)
}

// Errors
var (
	ErrQueueFull = fmt.Errorf("inbox is full")
)
