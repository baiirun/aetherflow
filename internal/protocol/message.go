// Package protocol defines the messaging protocol for aetherflow.
package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Lane represents the message lane (control or task).
type Lane string

const (
	LaneControl Lane = "control"
	LaneTask    Lane = "task"
)

// Priority represents message priority.
type Priority string

const (
	PriorityP0 Priority = "P0" // Critical, preempts other work
	PriorityP1 Priority = "P1" // Normal priority
	PriorityP2 Priority = "P2" // Low priority, can be deferred
)

// MessageType represents the semantic type of a message.
type MessageType string

const (
	// Task lifecycle
	TypeAssign    MessageType = "assign"    // Overseer -> Agent: work on this task
	TypeAck       MessageType = "ack"       // Agent -> Overseer: acknowledged receipt
	TypeDone      MessageType = "done"      // Agent -> Overseer: task complete
	TypeAbandoned MessageType = "abandoned" // Agent -> Overseer: task abandoned

	// Status updates
	TypeStatus   MessageType = "status"   // Agent -> Overseer: progress update
	TypeQuestion MessageType = "question" // Agent -> Overseer: need clarification
	TypeBlocker  MessageType = "blocker"  // Agent -> Overseer: blocked, need help

	// Review flow
	TypeReviewReady    MessageType = "review_ready"    // Agent -> Overseer: ready for review
	TypeReviewFeedback MessageType = "review_feedback" // Overseer -> Agent: review comments
)

// Address represents a message destination.
type Address struct {
	Type string `json:"type"` // "overseer", "agent", "team", "librarian", "company_chat"
	ID   string `json:"id"`   // Agent/team ID (empty for overseer/librarian/company_chat)
}

// ParseAddress parses an address string like "agent:worker-1" or "overseer".
func ParseAddress(s string) (Address, error) {
	switch s {
	case "overseer":
		return Address{Type: "overseer"}, nil
	case "librarian":
		return Address{Type: "librarian"}, nil
	case "company_chat":
		return Address{Type: "company_chat"}, nil
	}

	// Try agent:id or team:id format
	idx := strings.Index(s, ":")
	if idx == -1 || idx == 0 || idx == len(s)-1 {
		return Address{}, fmt.Errorf("invalid address format: %s (expected 'type:id' or 'overseer')", s)
	}

	addrType := s[:idx]
	id := s[idx+1:]

	// Check for extra colons (invalid)
	if strings.Contains(id, ":") {
		return Address{}, fmt.Errorf("invalid address format: %s (expected 'type:id' or 'overseer')", s)
	}

	if addrType != "agent" && addrType != "team" {
		return Address{}, fmt.Errorf("invalid address type: %s (expected 'agent' or 'team')", addrType)
	}

	return Address{Type: addrType, ID: id}, nil
}

func (a Address) String() string {
	if a.ID == "" {
		return a.Type
	}
	return fmt.Sprintf("%s:%s", a.Type, a.ID)
}

// Link represents an optional reference attached to a message.
type Link struct {
	Type string `json:"type"` // "task", "diff", "log", "doc"
	URL  string `json:"url"`
}

// Message is the core message envelope.
type Message struct {
	// Identity
	ID string `json:"id"` // UUIDv7 for time-ordering
	TS int64  `json:"ts"` // Unix milliseconds

	// Routing
	From Address `json:"from"`
	To   Address `json:"to"`
	Lane Lane    `json:"lane"`

	// Classification
	Priority Priority    `json:"priority"`
	Type     MessageType `json:"type"`

	// Content
	TaskID  string `json:"task_id,omitempty"` // Required for lane=task
	Summary string `json:"summary"`           // 1-2 sentences
	Links   []Link `json:"links,omitempty"`
}

// NewMessage creates a new message with a generated ID and current timestamp.
func NewMessage(from, to Address, lane Lane, priority Priority, msgType MessageType, summary string) *Message {
	return &Message{
		ID:       uuid.Must(uuid.NewV7()).String(),
		TS:       time.Now().UnixMilli(),
		From:     from,
		To:       to,
		Lane:     lane,
		Priority: priority,
		Type:     msgType,
		Summary:  summary,
	}
}

// Validate checks that the message is well-formed.
func (m *Message) Validate() error {
	if m.ID == "" {
		return fmt.Errorf("message ID is required")
	}
	if m.TS == 0 {
		return fmt.Errorf("message timestamp is required")
	}
	if m.From.Type == "" {
		return fmt.Errorf("message 'from' address is required")
	}
	if m.To.Type == "" {
		return fmt.Errorf("message 'to' address is required")
	}
	if m.Lane != LaneControl && m.Lane != LaneTask {
		return fmt.Errorf("invalid lane: %s", m.Lane)
	}
	if m.Lane == LaneTask && m.TaskID == "" {
		return fmt.Errorf("task_id is required for lane=task")
	}
	if m.Lane == LaneControl && m.TaskID != "" {
		return fmt.Errorf("task_id is forbidden for lane=control")
	}
	if m.Summary == "" {
		return fmt.Errorf("message summary is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (m *Message) MarshalJSON() ([]byte, error) {
	type Alias Message
	return json.Marshal((*Alias)(m))
}

// UnmarshalJSON implements json.Unmarshaler.
func (m *Message) UnmarshalJSON(data []byte) error {
	type Alias Message
	return json.Unmarshal(data, (*Alias)(m))
}
