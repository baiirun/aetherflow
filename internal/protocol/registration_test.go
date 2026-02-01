package protocol

import (
	"encoding/json"
	"testing"
	"time"
)

func TestAgentStates(t *testing.T) {
	states := []AgentState{
		StateIdle, StateActive, StateQuestion,
		StateBlocked, StateReadyForReview, StateDone,
	}

	seen := make(map[AgentState]bool)
	for _, s := range states {
		if seen[s] {
			t.Errorf("Duplicate state: %s", s)
		}
		seen[s] = true
		if s == "" {
			t.Error("Empty state found")
		}
	}
}

func TestRegistrationResponseJSON(t *testing.T) {
	resp := RegistrationResponse{
		AgentID: "ghost_wolf",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var decoded RegistrationResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if decoded.AgentID != resp.AgentID {
		t.Errorf("AgentID = %q, want %q", decoded.AgentID, resp.AgentID)
	}
}

func TestAgentInfoJSON(t *testing.T) {
	now := time.Now().UnixMilli()
	info := AgentInfo{
		ID:           "ghost_wolf",
		State:        StateActive,
		RegisteredAt: now,
		CurrentTask:  "ts-abc123",
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var decoded AgentInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if decoded.ID != info.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, info.ID)
	}
	if decoded.State != info.State {
		t.Errorf("State = %q, want %q", decoded.State, info.State)
	}
	if decoded.RegisteredAt != info.RegisteredAt {
		t.Errorf("RegisteredAt = %d, want %d", decoded.RegisteredAt, info.RegisteredAt)
	}
	if decoded.CurrentTask != info.CurrentTask {
		t.Errorf("CurrentTask = %q, want %q", decoded.CurrentTask, info.CurrentTask)
	}
}

func TestAgentInfoIsExpired(t *testing.T) {
	timeout := time.Minute

	tests := []struct {
		name         string
		registeredAt int64
		wantExpired  bool
	}{
		{"not expired", time.Now().UnixMilli(), false},
		{"expired", time.Now().Add(-2 * time.Minute).UnixMilli(), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &AgentInfo{RegisteredAt: tt.registeredAt}
			if got := info.IsExpired(timeout); got != tt.wantExpired {
				t.Errorf("IsExpired() = %v, want %v", got, tt.wantExpired)
			}
		})
	}
}
