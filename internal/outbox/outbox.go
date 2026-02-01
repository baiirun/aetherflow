// Package outbox implements per-agent message outboxes.
//
// Agents push messages to their outbox. Consumers (overseer, librarian, etc.)
// poll for messages addressed to them using the 'to' filter.
package outbox

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/geobrowser/aetherflow/internal/protocol"
)

// Config holds outbox configuration.
type Config struct {
	// StorePath is the directory for persistent storage.
	// If empty, persistence is disabled.
	StorePath string

	// Cap is the max messages per agent (0 = unlimited).
	Cap int
}

// Store manages outboxes for all agents.
type Store struct {
	config   Config
	messages map[protocol.AgentID][]*protocol.Message // per-agent outbox
	byID     map[string]*protocol.Message             // msgID -> message for quick lookup
	mu       sync.RWMutex
}

// New creates a new outbox store.
func New(cfg Config) (*Store, error) {
	s := &Store{
		config:   cfg,
		messages: make(map[protocol.AgentID][]*protocol.Message),
		byID:     make(map[string]*protocol.Message),
	}

	if cfg.StorePath != "" {
		if err := os.MkdirAll(cfg.StorePath, 0755); err != nil {
			return nil, fmt.Errorf("create store path: %w", err)
		}
		if err := s.replay(); err != nil {
			return nil, fmt.Errorf("replay: %w", err)
		}
	}

	return s, nil
}

// Push adds a message to an agent's outbox.
// Returns ErrQueueFull if the outbox is at capacity.
func (s *Store) Push(agentID protocol.AgentID, msg *protocol.Message) error {
	if err := msg.Validate(); err != nil {
		return fmt.Errorf("invalid message: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check capacity
	if s.config.Cap > 0 && len(s.messages[agentID]) >= s.config.Cap {
		return ErrQueueFull
	}

	s.messages[agentID] = append(s.messages[agentID], msg)
	s.byID[msg.ID] = msg

	// Persist
	if s.config.StorePath != "" {
		if err := s.appendToLog(agentID, msg); err != nil {
			// Roll back
			s.messages[agentID] = s.messages[agentID][:len(s.messages[agentID])-1]
			delete(s.byID, msg.ID)
			return fmt.Errorf("persist: %w", err)
		}
	}

	return nil
}

// Pop removes and returns the next message for a destination.
// Scans all agent outboxes for messages addressed to the given address.
// Returns nil if no messages are found.
func (s *Store) Pop(to protocol.Address) *protocol.Message {
	s.mu.Lock()
	defer s.mu.Unlock()

	for agentID, msgs := range s.messages {
		for i, msg := range msgs {
			if addressMatches(msg.To, to) {
				// Remove from queue
				s.messages[agentID] = append(msgs[:i], msgs[i+1:]...)
				delete(s.byID, msg.ID)

				if s.config.StorePath != "" {
					s.appendTombstone(agentID, msg.ID)
				}

				return msg
			}
		}
	}

	return nil
}

// PopFrom removes and returns the next message from a specific agent's outbox
// that matches the destination filter.
func (s *Store) PopFrom(agentID protocol.AgentID, to protocol.Address) *protocol.Message {
	s.mu.Lock()
	defer s.mu.Unlock()

	msgs, ok := s.messages[agentID]
	if !ok {
		return nil
	}

	for i, msg := range msgs {
		if addressMatches(msg.To, to) {
			s.messages[agentID] = append(msgs[:i], msgs[i+1:]...)
			delete(s.byID, msg.ID)

			if s.config.StorePath != "" {
				s.appendTombstone(agentID, msg.ID)
			}

			return msg
		}
	}

	return nil
}

// Peek returns up to limit messages for a destination without removing them.
// Pass limit=0 for all messages.
func (s *Store) Peek(to protocol.Address, limit int) []*protocol.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*protocol.Message

	for _, msgs := range s.messages {
		for _, msg := range msgs {
			if addressMatches(msg.To, to) {
				result = append(result, msg)
				if limit > 0 && len(result) >= limit {
					return result
				}
			}
		}
	}

	return result
}

// PeekFrom returns up to limit messages from a specific agent's outbox.
func (s *Store) PeekFrom(agentID protocol.AgentID, limit int) []*protocol.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	msgs, ok := s.messages[agentID]
	if !ok {
		return nil
	}

	if limit <= 0 || limit > len(msgs) {
		limit = len(msgs)
	}

	result := make([]*protocol.Message, limit)
	copy(result, msgs[:limit])
	return result
}

// Get retrieves a specific message by ID.
func (s *Store) Get(msgID string) *protocol.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.byID[msgID]
}

// Depth returns the number of messages in an agent's outbox.
func (s *Store) Depth(agentID protocol.AgentID) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.messages[agentID])
}

// DeleteAgent removes all messages from an agent's outbox.
func (s *Store) DeleteAgent(agentID protocol.AgentID) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove from byID index
	for _, msg := range s.messages[agentID] {
		delete(s.byID, msg.ID)
	}

	delete(s.messages, agentID)

	if s.config.StorePath != "" {
		os.Remove(s.logPath(agentID))
	}
}

// addressMatches returns true if msg.To matches the filter.
// Empty filter fields act as wildcards.
func addressMatches(msgTo, filter protocol.Address) bool {
	if filter.Type != "" && msgTo.Type != filter.Type {
		return false
	}
	if filter.ID != "" && msgTo.ID != filter.ID {
		return false
	}
	return true
}

// logPath returns the path to the JSONL log file for an agent.
func (s *Store) logPath(agentID protocol.AgentID) string {
	return filepath.Join(s.config.StorePath, string(agentID)+".jsonl")
}

// logEntry represents a single entry in the append-only log.
type logEntry struct {
	Op      string            `json:"op"`                // "push", "pop"
	Message *protocol.Message `json:"message,omitempty"` // for "push"
	MsgID   string            `json:"msg_id,omitempty"`  // for "pop"
}

func (s *Store) appendToLog(agentID protocol.AgentID, msg *protocol.Message) error {
	f, err := os.OpenFile(s.logPath(agentID), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	entry := logEntry{Op: "push", Message: msg}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}

	return f.Sync()
}

func (s *Store) appendTombstone(agentID protocol.AgentID, msgID string) error {
	f, err := os.OpenFile(s.logPath(agentID), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	entry := logEntry{Op: "pop", MsgID: msgID}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}

	return f.Sync()
}

// replay loads state from disk.
func (s *Store) replay() error {
	entries, err := os.ReadDir(s.config.StorePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}

		agentID := protocol.AgentID(entry.Name()[:len(entry.Name())-6]) // strip .jsonl
		if err := s.replayAgent(agentID); err != nil {
			return fmt.Errorf("replay %s: %w", agentID, err)
		}
	}

	return nil
}

func (s *Store) replayAgent(agentID protocol.AgentID) error {
	f, err := os.Open(s.logPath(agentID))
	if err != nil {
		return err
	}
	defer f.Close()

	// Track messages and their state
	messages := make(map[string]*protocol.Message)
	popped := make(map[string]bool)

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var entry logEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue // Skip malformed entries
		}

		switch entry.Op {
		case "push":
			if entry.Message != nil {
				messages[entry.Message.ID] = entry.Message
			}
		case "pop":
			popped[entry.MsgID] = true
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	// Reconstruct outbox state (preserve order by sorting by timestamp)
	var msgs []*protocol.Message
	for id, msg := range messages {
		if !popped[id] {
			msgs = append(msgs, msg)
			s.byID[id] = msg
		}
	}

	// Sort by timestamp to maintain order
	sortByTimestamp(msgs)

	if len(msgs) > 0 {
		s.messages[agentID] = msgs
	}

	return nil
}

// sortByTimestamp sorts messages by their timestamp (oldest first).
func sortByTimestamp(msgs []*protocol.Message) {
	// Simple insertion sort - outboxes are typically small
	for i := 1; i < len(msgs); i++ {
		j := i
		for j > 0 && msgs[j-1].TS > msgs[j].TS {
			msgs[j-1], msgs[j] = msgs[j], msgs[j-1]
			j--
		}
	}
}

// Errors
var (
	ErrQueueFull = fmt.Errorf("queue is full")
)
