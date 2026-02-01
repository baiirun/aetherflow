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
		{"human", Address{Type: "human"}, false},
		{"librarian", Address{Type: "librarian"}, false},

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
		{Address{Type: "human"}, "human"},
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
	to := Address{Type: "team", ID: "alpha"}

	msg := NewMessage(from, to, TypeStatus, "Working on feature X")

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
	if msg.Type != TypeStatus {
		t.Errorf("Message.Type = %v, want %v", msg.Type, TypeStatus)
	}
	if msg.Summary != "Working on feature X" {
		t.Errorf("Message.Summary = %q, want %q", msg.Summary, "Working on feature X")
	}
}

func TestMessageWithTaskID(t *testing.T) {
	from := Address{Type: "agent", ID: "ghost_wolf"}
	to := Address{Type: "human"}

	msg := NewMessage(from, to, TypeBlocker, "Blocked on API credentials").WithTaskID("ts-123")

	if msg.TaskID != "ts-123" {
		t.Errorf("Message.TaskID = %q, want %q", msg.TaskID, "ts-123")
	}
}

func TestMessageValidate(t *testing.T) {
	validMsg := &Message{
		ID:      "msg-123",
		TS:      1234567890,
		From:    Address{Type: "agent", ID: "ghost_wolf"},
		To:      Address{Type: "team", ID: "alpha"},
		Type:    TypeStatus,
		Summary: "Progress update",
	}

	tests := []struct {
		name    string
		modify  func(*Message)
		wantErr bool
	}{
		{"valid message", func(m *Message) { *m = *validMsg }, false},
		{"missing ID", func(m *Message) { *m = *validMsg; m.ID = "" }, true},
		{"missing timestamp", func(m *Message) { *m = *validMsg; m.TS = 0 }, true},
		{"missing from", func(m *Message) { *m = *validMsg; m.From = Address{} }, true},
		{"missing to", func(m *Message) { *m = *validMsg; m.To = Address{} }, true},
		{"missing summary", func(m *Message) { *m = *validMsg; m.Summary = "" }, true},
		{"with task ID", func(m *Message) { *m = *validMsg; m.TaskID = "ts-123" }, false},
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
		ID:      "msg-123",
		TS:      1234567890000,
		From:    Address{Type: "agent", ID: "ghost_wolf"},
		To:      Address{Type: "team", ID: "alpha"},
		Type:    TypeStatus,
		TaskID:  "ts-abc123",
		Summary: "Working on feature X",
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
		TypeStatus, TypeQuestion, TypeBlocker,
		TypeProposal, TypeAgree, TypeDisagree,
		TypeReviewReady, TypeReviewFeedback,
		TypeDone, TypeAbandoned,
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
