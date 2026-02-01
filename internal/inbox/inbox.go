// Package inbox implements per-agent message inboxes with control and task lanes.
//
// Design: Pull-based model
//
// Agents poll their inbox to receive messages. Push/interrupt is not possible
// because MCP tools cannot invoke Claude - they can only respond when called.
// This means agents must check their inbox at natural breakpoints (after tool
// calls, between subtasks, etc.) to receive priority messages.
//
// The control lane is drained first, so when an agent polls, P0 preemption
// signals take precedence over queued task work. However, if an agent is deep
// in a long operation without polling, messages will queue until the next check.
//
// Intelligent polling is an agent-side concern - this package only provides
// the storage and priority ordering.
//
// Persistence: Inboxes are not persisted. On daemon restart, inboxes are empty
// and agents re-register. Any in-flight messages are lost, but agents will
// re-raise blockers/questions when they hit the same issues again.
package inbox

import (
	"fmt"
	"sync"

	"github.com/geobrowser/aetherflow/internal/protocol"
)

// Config holds inbox configuration.
type Config struct {
	// ControlCap is the max messages in the control lane (0 = unlimited).
	ControlCap int

	// TaskCap is the max messages in the task lane (0 = unlimited).
	TaskCap int
}

// agentInbox holds the queues for a single agent.
type agentInbox struct {
	control []*protocol.Message // control lane (drained first)
	task    []*protocol.Message // task lane
}

// Store manages inboxes for all agents.
type Store struct {
	config  Config
	inboxes map[protocol.AgentID]*agentInbox
	mu      sync.RWMutex
}

// New creates a new inbox store.
func New(cfg Config) *Store {
	return &Store{
		config:  cfg,
		inboxes: make(map[protocol.AgentID]*agentInbox),
	}
}

// Push adds a message to an agent's inbox.
// Returns ErrQueueFull if the lane is at capacity.
func (s *Store) Push(agentID protocol.AgentID, msg *protocol.Message) error {
	if err := msg.Validate(); err != nil {
		return fmt.Errorf("invalid message: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	inbox := s.getOrCreateInbox(agentID)

	switch msg.Lane {
	case protocol.LaneControl:
		if s.config.ControlCap > 0 && len(inbox.control) >= s.config.ControlCap {
			return ErrQueueFull
		}
		inbox.control = append(inbox.control, msg)
	case protocol.LaneTask:
		if s.config.TaskCap > 0 && len(inbox.task) >= s.config.TaskCap {
			return ErrQueueFull
		}
		inbox.task = append(inbox.task, msg)
	default:
		return fmt.Errorf("unknown lane: %s", msg.Lane)
	}

	return nil
}

// Pop removes and returns the next message from an agent's inbox.
// Control lane is drained first, then task lane.
// Returns nil if the inbox is empty.
func (s *Store) Pop(agentID protocol.AgentID) *protocol.Message {
	s.mu.Lock()
	defer s.mu.Unlock()

	inbox, ok := s.inboxes[agentID]
	if !ok {
		return nil
	}

	// Control lane has priority
	if len(inbox.control) > 0 {
		msg := inbox.control[0]
		inbox.control = inbox.control[1:]
		return msg
	}

	if len(inbox.task) > 0 {
		msg := inbox.task[0]
		inbox.task = inbox.task[1:]
		return msg
	}

	return nil
}

// PopLane removes and returns the next message from a specific lane.
// Returns nil if the lane is empty.
func (s *Store) PopLane(agentID protocol.AgentID, lane protocol.Lane) *protocol.Message {
	s.mu.Lock()
	defer s.mu.Unlock()

	inbox, ok := s.inboxes[agentID]
	if !ok {
		return nil
	}

	switch lane {
	case protocol.LaneControl:
		if len(inbox.control) > 0 {
			msg := inbox.control[0]
			inbox.control = inbox.control[1:]
			return msg
		}
	case protocol.LaneTask:
		if len(inbox.task) > 0 {
			msg := inbox.task[0]
			inbox.task = inbox.task[1:]
			return msg
		}
	}

	return nil
}

// Peek returns up to limit messages from an agent's inbox without removing them.
// Control lane messages come first. Pass limit=0 for all messages.
func (s *Store) Peek(agentID protocol.AgentID, limit int) []*protocol.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	inbox, ok := s.inboxes[agentID]
	if !ok {
		return nil
	}

	total := len(inbox.control) + len(inbox.task)
	if limit <= 0 || limit > total {
		limit = total
	}

	result := make([]*protocol.Message, 0, limit)

	// Control first
	for i := 0; i < len(inbox.control) && len(result) < limit; i++ {
		result = append(result, inbox.control[i])
	}

	// Then task
	for i := 0; i < len(inbox.task) && len(result) < limit; i++ {
		result = append(result, inbox.task[i])
	}

	return result
}

// PeekLane returns up to limit messages from a specific lane without removing them.
func (s *Store) PeekLane(agentID protocol.AgentID, lane protocol.Lane, limit int) []*protocol.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	inbox, ok := s.inboxes[agentID]
	if !ok {
		return nil
	}

	var source []*protocol.Message
	switch lane {
	case protocol.LaneControl:
		source = inbox.control
	case protocol.LaneTask:
		source = inbox.task
	default:
		return nil
	}

	if limit <= 0 || limit > len(source) {
		limit = len(source)
	}

	result := make([]*protocol.Message, limit)
	copy(result, source[:limit])
	return result
}

// Depth returns the number of messages in an agent's inbox.
func (s *Store) Depth(agentID protocol.AgentID) (control, task int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	inbox, ok := s.inboxes[agentID]
	if !ok {
		return 0, 0
	}

	return len(inbox.control), len(inbox.task)
}

// DeleteAgent removes all messages for an agent.
func (s *Store) DeleteAgent(agentID protocol.AgentID) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.inboxes, agentID)
}

func (s *Store) getOrCreateInbox(agentID protocol.AgentID) *agentInbox {
	inbox, ok := s.inboxes[agentID]
	if !ok {
		inbox = &agentInbox{}
		s.inboxes[agentID] = inbox
	}
	return inbox
}

// Errors
var (
	ErrQueueFull = fmt.Errorf("queue is full")
)
