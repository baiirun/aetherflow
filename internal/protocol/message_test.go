package protocol

import (
	"encoding/json"
	"testing"
)

func TestParseAddress(t *testing.T) {
	tests := []struct {
		input   string
		want    Address
		wantErr bool
	}{
		// Singleton addresses
		{"overseer", Address{Type: "overseer"}, false},
		{"librarian", Address{Type: "librarian"}, false},
		{"company_chat", Address{Type: "company_chat"}, false},

		// Agent addresses
		{"agent:ghost_wolf", Address{Type: "agent", ID: "ghost_wolf"}, false},
		{"agent:cyber_phoenix", Address{Type: "agent", ID: "cyber_phoenix"}, false},

		// Team addresses
		{"team:frontend", Address{Type: "team", ID: "frontend"}, false},
		{"team:tiger_01", Address{Type: "team", ID: "tiger_01"}, false},

		// Invalid addresses
		{"", Address{}, true},
		{"unknown", Address{}, true},
		{"agent:", Address{}, true},
		{"agent", Address{}, true},
		{"foo:bar", Address{}, true},
		{"agent:ghost:extra", Address{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseAddress(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseAddress(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseAddress(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestAddressString(t *testing.T) {
	tests := []struct {
		addr Address
		want string
	}{
		{Address{Type: "overseer"}, "overseer"},
		{Address{Type: "librarian"}, "librarian"},
		{Address{Type: "agent", ID: "ghost_wolf"}, "agent:ghost_wolf"},
		{Address{Type: "team", ID: "frontend"}, "team:frontend"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.addr.String(); got != tt.want {
				t.Errorf("Address.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewMessage(t *testing.T) {
	from := Address{Type: "agent", ID: "ghost_wolf"}
	to := Address{Type: "overseer"}

	msg := NewMessage(from, to, LaneTask, PriorityP1, TypeStatus, "Working on feature X")

	if msg.ID == "" {
		t.Error("Message ID should not be empty")
	}
	if msg.TS == 0 {
		t.Error("Message timestamp should not be zero")
	}
	if msg.From != from {
		t.Errorf("Message.From = %v, want %v", msg.From, from)
	}
	if msg.To != to {
		t.Errorf("Message.To = %v, want %v", msg.To, to)
	}
	if msg.Lane != LaneTask {
		t.Errorf("Message.Lane = %v, want %v", msg.Lane, LaneTask)
	}
	if msg.Priority != PriorityP1 {
		t.Errorf("Message.Priority = %v, want %v", msg.Priority, PriorityP1)
	}
	if msg.Type != TypeStatus {
		t.Errorf("Message.Type = %v, want %v", msg.Type, TypeStatus)
	}
	if msg.Summary != "Working on feature X" {
		t.Errorf("Message.Summary = %q, want %q", msg.Summary, "Working on feature X")
	}
}

func TestMessageValidate(t *testing.T) {
	validTaskMsg := &Message{
		ID:       "msg-123",
		TS:       1234567890,
		From:     Address{Type: "agent", ID: "ghost_wolf"},
		To:       Address{Type: "overseer"},
		Lane:     LaneTask,
		Priority: PriorityP1,
		Type:     TypeStatus,
		TaskID:   "ts-abc123",
		Summary:  "Progress update",
	}

	validControlMsg := &Message{
		ID:       "msg-456",
		TS:       1234567890,
		From:     Address{Type: "overseer"},
		To:       Address{Type: "agent", ID: "ghost_wolf"},
		Lane:     LaneControl,
		Priority: PriorityP0,
		Type:     TypeAssign,
		Summary:  "New task assigned",
	}

	tests := []struct {
		name    string
		modify  func(*Message)
		wantErr bool
	}{
		{"valid task message", func(m *Message) { *m = *validTaskMsg }, false},
		{"valid control message", func(m *Message) { *m = *validControlMsg }, false},
		{"missing ID", func(m *Message) { *m = *validTaskMsg; m.ID = "" }, true},
		{"missing timestamp", func(m *Message) { *m = *validTaskMsg; m.TS = 0 }, true},
		{"missing from", func(m *Message) { *m = *validTaskMsg; m.From = Address{} }, true},
		{"missing to", func(m *Message) { *m = *validTaskMsg; m.To = Address{} }, true},
		{"invalid lane", func(m *Message) { *m = *validTaskMsg; m.Lane = "invalid" }, true},
		{"missing summary", func(m *Message) { *m = *validTaskMsg; m.Summary = "" }, true},
		{"task lane without task_id", func(m *Message) { *m = *validTaskMsg; m.TaskID = "" }, true},
		{"control lane with task_id", func(m *Message) { *m = *validControlMsg; m.TaskID = "ts-123" }, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &Message{}
			tt.modify(msg)
			err := msg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMessageJSON(t *testing.T) {
	original := &Message{
		ID:       "msg-123",
		TS:       1234567890000,
		From:     Address{Type: "agent", ID: "ghost_wolf"},
		To:       Address{Type: "overseer"},
		Lane:     LaneTask,
		Priority: PriorityP1,
		Type:     TypeStatus,
		TaskID:   "ts-abc123",
		Summary:  "Working on feature X",
		Links: []Link{
			{Type: "diff", URL: "https://github.com/org/repo/pull/123"},
		},
	}

	// Marshal
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	// Unmarshal
	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	// Compare
	if decoded.ID != original.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, original.ID)
	}
	if decoded.TS != original.TS {
		t.Errorf("TS = %d, want %d", decoded.TS, original.TS)
	}
	if decoded.From != original.From {
		t.Errorf("From = %v, want %v", decoded.From, original.From)
	}
	if decoded.To != original.To {
		t.Errorf("To = %v, want %v", decoded.To, original.To)
	}
	if decoded.Lane != original.Lane {
		t.Errorf("Lane = %v, want %v", decoded.Lane, original.Lane)
	}
	if decoded.Priority != original.Priority {
		t.Errorf("Priority = %v, want %v", decoded.Priority, original.Priority)
	}
	if decoded.Type != original.Type {
		t.Errorf("Type = %v, want %v", decoded.Type, original.Type)
	}
	if decoded.TaskID != original.TaskID {
		t.Errorf("TaskID = %q, want %q", decoded.TaskID, original.TaskID)
	}
	if decoded.Summary != original.Summary {
		t.Errorf("Summary = %q, want %q", decoded.Summary, original.Summary)
	}
	if len(decoded.Links) != len(original.Links) {
		t.Errorf("Links length = %d, want %d", len(decoded.Links), len(original.Links))
	}
}

func TestMessageTypes(t *testing.T) {
	// Verify all message types are distinct
	types := []MessageType{
		TypeAssign, TypeAck, TypeDone, TypeAbandoned,
		TypeStatus, TypeQuestion, TypeBlocker,
		TypeReviewReady, TypeReviewFeedback,
	}

	seen := make(map[MessageType]bool)
	for _, mt := range types {
		if seen[mt] {
			t.Errorf("Duplicate message type: %s", mt)
		}
		seen[mt] = true
		if mt == "" {
			t.Error("Empty message type found")
		}
	}
}

func TestPriorities(t *testing.T) {
	// Verify all priorities are distinct
	priorities := []Priority{PriorityP0, PriorityP1, PriorityP2}

	seen := make(map[Priority]bool)
	for _, p := range priorities {
		if seen[p] {
			t.Errorf("Duplicate priority: %s", p)
		}
		seen[p] = true
	}
}

func TestLanes(t *testing.T) {
	// Verify lanes are distinct
	if LaneControl == LaneTask {
		t.Error("LaneControl and LaneTask should be different")
	}
	if LaneControl == "" || LaneTask == "" {
		t.Error("Lanes should not be empty")
	}
}
