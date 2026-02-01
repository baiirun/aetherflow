package protocol

import (
	"encoding/json"
	"testing"
	"time"
)

func TestAgentStates(t *testing.T) {
	// Verify all states are distinct and non-empty
	states := []AgentState{
		StateIdle, StateQueued, StateActive,
		StateQuestion, StateBlocked, StateReadyForReview,
		StateReview, StateDone, StateAbandoned,
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

func TestRegistrationRequestJSON(t *testing.T) {
	req := RegistrationRequest{
		Name:                  "my-worker",
		Labels:                []string{"frontend", "senior"},
		Capacity:              10,
		HeartbeatIntervalSecs: 60,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var decoded RegistrationRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if decoded.Name != req.Name {
		t.Errorf("Name = %q, want %q", decoded.Name, req.Name)
	}
	if len(decoded.Labels) != len(req.Labels) {
		t.Errorf("Labels length = %d, want %d", len(decoded.Labels), len(req.Labels))
	}
	if decoded.Capacity != req.Capacity {
		t.Errorf("Capacity = %d, want %d", decoded.Capacity, req.Capacity)
	}
	if decoded.HeartbeatIntervalSecs != req.HeartbeatIntervalSecs {
		t.Errorf("HeartbeatIntervalSecs = %d, want %d", decoded.HeartbeatIntervalSecs, req.HeartbeatIntervalSecs)
	}
}

func TestRegistrationRequestDefaults(t *testing.T) {
	// Minimal request with only required fields
	req := RegistrationRequest{}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	// Should omit empty fields
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	// Empty strings and zero values with omitempty should not appear
	if _, ok := m["name"]; ok {
		t.Error("Empty name should be omitted")
	}
	if _, ok := m["labels"]; ok {
		t.Error("Empty labels should be omitted")
	}
}

func TestRegistrationResponseJSON(t *testing.T) {
	resp := RegistrationResponse{
		AgentID:               "ghost_wolf",
		Accepted:              true,
		HeartbeatIntervalSecs: 30,
		LeaseExpiresAt:        time.Now().Add(90 * time.Second).UnixMilli(),
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
	if decoded.Accepted != resp.Accepted {
		t.Errorf("Accepted = %v, want %v", decoded.Accepted, resp.Accepted)
	}
	if decoded.HeartbeatIntervalSecs != resp.HeartbeatIntervalSecs {
		t.Errorf("HeartbeatIntervalSecs = %d, want %d", decoded.HeartbeatIntervalSecs, resp.HeartbeatIntervalSecs)
	}
	if decoded.LeaseExpiresAt != resp.LeaseExpiresAt {
		t.Errorf("LeaseExpiresAt = %d, want %d", decoded.LeaseExpiresAt, resp.LeaseExpiresAt)
	}
}

func TestRegistrationResponseRejected(t *testing.T) {
	resp := RegistrationResponse{
		Accepted: false,
		Reason:   "Agent pool at capacity",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var decoded RegistrationResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if decoded.Accepted {
		t.Error("Accepted should be false")
	}
	if decoded.Reason != resp.Reason {
		t.Errorf("Reason = %q, want %q", decoded.Reason, resp.Reason)
	}
}

func TestHeartbeatJSON(t *testing.T) {
	hb := Heartbeat{
		AgentID:     "ghost_wolf",
		TS:          time.Now().UnixMilli(),
		State:       StateActive,
		QueueDepth:  3,
		CurrentTask: "ts-abc123",
	}

	data, err := json.Marshal(hb)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var decoded Heartbeat
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if decoded.AgentID != hb.AgentID {
		t.Errorf("AgentID = %q, want %q", decoded.AgentID, hb.AgentID)
	}
	if decoded.TS != hb.TS {
		t.Errorf("TS = %d, want %d", decoded.TS, hb.TS)
	}
	if decoded.State != hb.State {
		t.Errorf("State = %q, want %q", decoded.State, hb.State)
	}
	if decoded.QueueDepth != hb.QueueDepth {
		t.Errorf("QueueDepth = %d, want %d", decoded.QueueDepth, hb.QueueDepth)
	}
	if decoded.CurrentTask != hb.CurrentTask {
		t.Errorf("CurrentTask = %q, want %q", decoded.CurrentTask, hb.CurrentTask)
	}
}

func TestAgentInfoIsExpired(t *testing.T) {
	now := time.Now().UnixMilli()

	tests := []struct {
		name           string
		leaseExpiresAt int64
		wantExpired    bool
	}{
		{"not expired (future)", now + 10000, false},
		{"expired (past)", now - 10000, true},
		{"just expired", now - 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &AgentInfo{
				LeaseExpiresAt: tt.leaseExpiresAt,
			}
			if got := info.IsExpired(); got != tt.wantExpired {
				t.Errorf("IsExpired() = %v, want %v", got, tt.wantExpired)
			}
		})
	}
}

func TestAgentInfoJSON(t *testing.T) {
	now := time.Now().UnixMilli()
	info := AgentInfo{
		ID:             "ghost_wolf",
		Name:           "my-worker",
		Labels:         []string{"frontend"},
		Capacity:       5,
		State:          StateActive,
		RegisteredAt:   now - 60000,
		LastHeartbeat:  now - 5000,
		LeaseExpiresAt: now + 25000,
		QueueDepth:     2,
		CurrentTask:    "ts-abc123",
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
	if decoded.CurrentTask != info.CurrentTask {
		t.Errorf("CurrentTask = %q, want %q", decoded.CurrentTask, info.CurrentTask)
	}
}

func TestUnregisterRequestJSON(t *testing.T) {
	req := UnregisterRequest{
		AgentID: "ghost_wolf",
		Reason:  "Shutting down gracefully",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var decoded UnregisterRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if decoded.AgentID != req.AgentID {
		t.Errorf("AgentID = %q, want %q", decoded.AgentID, req.AgentID)
	}
	if decoded.Reason != req.Reason {
		t.Errorf("Reason = %q, want %q", decoded.Reason, req.Reason)
	}
}

func TestUnregisterResponseJSON(t *testing.T) {
	resp := UnregisterResponse{
		Success:         true,
		PendingMessages: 3,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var decoded UnregisterResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if decoded.Success != resp.Success {
		t.Errorf("Success = %v, want %v", decoded.Success, resp.Success)
	}
	if decoded.PendingMessages != resp.PendingMessages {
		t.Errorf("PendingMessages = %d, want %d", decoded.PendingMessages, resp.PendingMessages)
	}
}
