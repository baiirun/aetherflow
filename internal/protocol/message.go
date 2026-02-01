// Package protocol defines the messaging protocol for aetherflow.
package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MessageType represents the semantic type of a message.
type MessageType string

const (
	// Coordination
	TypeStatus   MessageType = "status"   // Progress update
	TypeQuestion MessageType = "question" // Need input from others
	TypeBlocker  MessageType = "blocker"  // Blocked, need help
	TypeProposal MessageType = "proposal" // Proposing an approach
	TypeAgree    MessageType = "agree"    // Agreeing with a proposal
	TypeDisagree MessageType = "disagree" // Disagreeing with a proposal

	// Review flow
	TypeReviewReady    MessageType = "review_ready"    // Ready for review
	TypeReviewFeedback MessageType = "review_feedback" // Review comments

	// Lifecycle
	TypeDone      MessageType = "done"      // Task/work complete
	TypeAbandoned MessageType = "abandoned" // Task abandoned
)

// Address represents a message destination.
type Address struct {
	Type string `json:"type"` // "agent", "team", "human", "librarian"
	ID   string `json:"id"`   // Agent/team ID (empty for human/librarian)
}

// ParseAddress parses an address string like "agent:ghost_wolf" or "human".
func ParseAddress(s string) (Address, error) {
	switch s {
	case "human":
		return Address{Type: "human"}, nil
	case "librarian":
		return Address{Type: "librarian"}, nil
	}

	// Try agent:id or team:id format
	idx := strings.Index(s, ":")
	if idx == -1 || idx == 0 || idx == len(s)-1 {
		return Address{}, fmt.Errorf("invalid address format: %s (expected 'type:id' or 'human')", s)
	}

	addrType := s[:idx]
	id := s[idx+1:]

	// Check for extra colons (invalid)
	if strings.Contains(id, ":") {
		return Address{}, fmt.Errorf("invalid address format: %s (expected 'type:id' or 'human')", s)
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

	// Classification
	Type MessageType `json:"type"`

	// Content
	TaskID  string `json:"task_id,omitempty"` // Optional task reference
	Summary string `json:"summary"`           // Message content
	Links   []Link `json:"links,omitempty"`
}

// NewMessage creates a new message with a generated ID and current timestamp.
func NewMessage(from, to Address, msgType MessageType, summary string) *Message {
	return &Message{
		ID:      uuid.Must(uuid.NewV7()).String(),
		TS:      time.Now().UnixMilli(),
		From:    from,
		To:      to,
		Type:    msgType,
		Summary: summary,
	}
}

// WithTaskID sets the task ID and returns the message for chaining.
func (m *Message) WithTaskID(taskID string) *Message {
	m.TaskID = taskID
	return m
}

// WithLinks sets the links and returns the message for chaining.
func (m *Message) WithLinks(links []Link) *Message {
	m.Links = links
	return m
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
