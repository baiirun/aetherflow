package inbox

import (
	"testing"

	"github.com/geobrowser/aetherflow/internal/protocol"
)

func makeControlMsg(id string) *protocol.Message {
	return &protocol.Message{
		ID:       id,
		TS:       1000,
		From:     protocol.Address{Type: "overseer"},
		To:       protocol.Address{Type: "agent", ID: "test_agent"},
		Lane:     protocol.LaneControl,
		Priority: protocol.PriorityP0,
		Type:     protocol.TypeAssign,
		Summary:  "Control message " + id,
	}
}

func makeTaskMsg(id, taskID string) *protocol.Message {
	return &protocol.Message{
		ID:       id,
		TS:       1000,
		From:     protocol.Address{Type: "overseer"},
		To:       protocol.Address{Type: "agent", ID: "test_agent"},
		Lane:     protocol.LaneTask,
		Priority: protocol.PriorityP1,
		Type:     protocol.TypeAssign,
		TaskID:   taskID,
		Summary:  "Task message " + id,
	}
}

func TestInbox_PushPop(t *testing.T) {
	store := New(Config{})
	agentID := protocol.AgentID("test_agent")

	// Push control and task messages
	if err := store.Push(agentID, makeControlMsg("ctrl-1")); err != nil {
		t.Fatalf("Push control error = %v", err)
	}
	if err := store.Push(agentID, makeTaskMsg("task-1", "ts-001")); err != nil {
		t.Fatalf("Push task error = %v", err)
	}
	if err := store.Push(agentID, makeControlMsg("ctrl-2")); err != nil {
		t.Fatalf("Push control error = %v", err)
	}
	if err := store.Push(agentID, makeTaskMsg("task-2", "ts-002")); err != nil {
		t.Fatalf("Push task error = %v", err)
	}

	// Control lane should drain first
	msg := store.Pop(agentID)
	if msg == nil || msg.ID != "ctrl-1" {
		t.Errorf("Pop() = %v, want ctrl-1", msg)
	}

	msg = store.Pop(agentID)
	if msg == nil || msg.ID != "ctrl-2" {
		t.Errorf("Pop() = %v, want ctrl-2", msg)
	}

	// Now task lane
	msg = store.Pop(agentID)
	if msg == nil || msg.ID != "task-1" {
		t.Errorf("Pop() = %v, want task-1", msg)
	}

	msg = store.Pop(agentID)
	if msg == nil || msg.ID != "task-2" {
		t.Errorf("Pop() = %v, want task-2", msg)
	}

	// Empty
	msg = store.Pop(agentID)
	if msg != nil {
		t.Errorf("Pop() = %v, want nil", msg)
	}
}

func TestInbox_PopLane(t *testing.T) {
	store := New(Config{})
	agentID := protocol.AgentID("test_agent")

	store.Push(agentID, makeControlMsg("ctrl-1"))
	store.Push(agentID, makeTaskMsg("task-1", "ts-001"))

	// Pop specifically from task lane
	msg := store.PopLane(agentID, protocol.LaneTask)
	if msg == nil || msg.ID != "task-1" {
		t.Errorf("PopLane(task) = %v, want task-1", msg)
	}

	// Control still there
	msg = store.PopLane(agentID, protocol.LaneControl)
	if msg == nil || msg.ID != "ctrl-1" {
		t.Errorf("PopLane(control) = %v, want ctrl-1", msg)
	}
}

func TestInbox_Peek(t *testing.T) {
	store := New(Config{})
	agentID := protocol.AgentID("test_agent")

	store.Push(agentID, makeControlMsg("ctrl-1"))
	store.Push(agentID, makeTaskMsg("task-1", "ts-001"))
	store.Push(agentID, makeControlMsg("ctrl-2"))

	// Peek all - control first
	msgs := store.Peek(agentID, 0)
	if len(msgs) != 3 {
		t.Fatalf("Peek(0) len = %d, want 3", len(msgs))
	}
	if msgs[0].ID != "ctrl-1" || msgs[1].ID != "ctrl-2" || msgs[2].ID != "task-1" {
		t.Errorf("Peek order wrong: %v, %v, %v", msgs[0].ID, msgs[1].ID, msgs[2].ID)
	}

	// Peek with limit
	msgs = store.Peek(agentID, 2)
	if len(msgs) != 2 {
		t.Fatalf("Peek(2) len = %d, want 2", len(msgs))
	}

	// Peek doesn't remove
	control, task := store.Depth(agentID)
	if control != 2 || task != 1 {
		t.Errorf("Depth = (%d, %d), want (2, 1)", control, task)
	}
}

func TestInbox_PeekLane(t *testing.T) {
	store := New(Config{})
	agentID := protocol.AgentID("test_agent")

	store.Push(agentID, makeControlMsg("ctrl-1"))
	store.Push(agentID, makeTaskMsg("task-1", "ts-001"))
	store.Push(agentID, makeTaskMsg("task-2", "ts-002"))

	msgs := store.PeekLane(agentID, protocol.LaneTask, 0)
	if len(msgs) != 2 {
		t.Fatalf("PeekLane(task) len = %d, want 2", len(msgs))
	}

	msgs = store.PeekLane(agentID, protocol.LaneControl, 1)
	if len(msgs) != 1 {
		t.Fatalf("PeekLane(control, 1) len = %d, want 1", len(msgs))
	}
}

func TestInbox_QueueCap(t *testing.T) {
	store := New(Config{
		ControlCap: 2,
		TaskCap:    1,
	})
	agentID := protocol.AgentID("test_agent")

	// Control lane cap
	store.Push(agentID, makeControlMsg("ctrl-1"))
	store.Push(agentID, makeControlMsg("ctrl-2"))
	if err := store.Push(agentID, makeControlMsg("ctrl-3")); err != ErrQueueFull {
		t.Errorf("Push(ctrl-3) = %v, want ErrQueueFull", err)
	}

	// Task lane cap
	store.Push(agentID, makeTaskMsg("task-1", "ts-001"))
	if err := store.Push(agentID, makeTaskMsg("task-2", "ts-002")); err != ErrQueueFull {
		t.Errorf("Push(task-2) = %v, want ErrQueueFull", err)
	}
}

func TestInbox_DeleteAgent(t *testing.T) {
	store := New(Config{})
	agentID := protocol.AgentID("test_agent")

	store.Push(agentID, makeControlMsg("ctrl-1"))
	store.Push(agentID, makeTaskMsg("task-1", "ts-001"))

	store.DeleteAgent(agentID)

	// Should be empty
	control, task := store.Depth(agentID)
	if control != 0 || task != 0 {
		t.Errorf("Depth after delete = (%d, %d), want (0, 0)", control, task)
	}
}

func TestInbox_ValidationError(t *testing.T) {
	store := New(Config{})
	agentID := protocol.AgentID("test_agent")

	// Invalid message (missing summary)
	badMsg := &protocol.Message{
		ID:   "bad-1",
		TS:   1000,
		From: protocol.Address{Type: "overseer"},
		To:   protocol.Address{Type: "agent", ID: "test"},
		Lane: protocol.LaneControl,
	}

	if err := store.Push(agentID, badMsg); err == nil {
		t.Error("Push(invalid) should fail")
	}
}

func TestInbox_EmptyAgentReturnsNil(t *testing.T) {
	store := New(Config{})
	agentID := protocol.AgentID("nonexistent")

	if msg := store.Pop(agentID); msg != nil {
		t.Errorf("Pop(nonexistent) = %v, want nil", msg)
	}

	if msgs := store.Peek(agentID, 10); msgs != nil {
		t.Errorf("Peek(nonexistent) = %v, want nil", msgs)
	}

	control, task := store.Depth(agentID)
	if control != 0 || task != 0 {
		t.Errorf("Depth(nonexistent) = (%d, %d), want (0, 0)", control, task)
	}
}
