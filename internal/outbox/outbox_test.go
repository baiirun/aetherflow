package outbox

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/geobrowser/aetherflow/internal/protocol"
)

func makeMsg(id string, to protocol.Address) *protocol.Message {
	return &protocol.Message{
		ID:       id,
		TS:       1000,
		From:     protocol.Address{Type: "agent", ID: "test_agent"},
		To:       to,
		Lane:     protocol.LaneControl,
		Priority: protocol.PriorityP1,
		Type:     protocol.TypeStatus,
		Summary:  "Message " + id,
	}
}

func TestOutbox_PushPop(t *testing.T) {
	store, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	agentID := protocol.AgentID("test_agent")
	overseer := protocol.Address{Type: "overseer"}

	// Push messages
	store.Push(agentID, makeMsg("msg-1", overseer))
	store.Push(agentID, makeMsg("msg-2", overseer))

	// Pop for overseer
	msg := store.Pop(overseer)
	if msg == nil || msg.ID != "msg-1" {
		t.Errorf("Pop(overseer) = %v, want msg-1", msg)
	}

	msg = store.Pop(overseer)
	if msg == nil || msg.ID != "msg-2" {
		t.Errorf("Pop(overseer) = %v, want msg-2", msg)
	}

	// Empty
	msg = store.Pop(overseer)
	if msg != nil {
		t.Errorf("Pop(overseer) = %v, want nil", msg)
	}
}

func TestOutbox_PopFiltersDestination(t *testing.T) {
	store, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	agentID := protocol.AgentID("test_agent")
	overseer := protocol.Address{Type: "overseer"}
	librarian := protocol.Address{Type: "librarian"}

	store.Push(agentID, makeMsg("msg-1", overseer))
	store.Push(agentID, makeMsg("msg-2", librarian))
	store.Push(agentID, makeMsg("msg-3", overseer))

	// Pop for librarian
	msg := store.Pop(librarian)
	if msg == nil || msg.ID != "msg-2" {
		t.Errorf("Pop(librarian) = %v, want msg-2", msg)
	}

	// No more for librarian
	msg = store.Pop(librarian)
	if msg != nil {
		t.Errorf("Pop(librarian) = %v, want nil", msg)
	}

	// Overseer messages still there
	if store.Depth(agentID) != 2 {
		t.Errorf("Depth = %d, want 2", store.Depth(agentID))
	}
}

func TestOutbox_PopFrom(t *testing.T) {
	store, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	agent1 := protocol.AgentID("agent_1")
	agent2 := protocol.AgentID("agent_2")
	overseer := protocol.Address{Type: "overseer"}

	store.Push(agent1, makeMsg("msg-1", overseer))
	store.Push(agent2, makeMsg("msg-2", overseer))

	// Pop from specific agent
	msg := store.PopFrom(agent1, overseer)
	if msg == nil || msg.ID != "msg-1" {
		t.Errorf("PopFrom(agent1) = %v, want msg-1", msg)
	}

	// agent2's message still there
	msg = store.PopFrom(agent2, overseer)
	if msg == nil || msg.ID != "msg-2" {
		t.Errorf("PopFrom(agent2) = %v, want msg-2", msg)
	}
}

func TestOutbox_Peek(t *testing.T) {
	store, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	agentID := protocol.AgentID("test_agent")
	overseer := protocol.Address{Type: "overseer"}
	librarian := protocol.Address{Type: "librarian"}

	store.Push(agentID, makeMsg("msg-1", overseer))
	store.Push(agentID, makeMsg("msg-2", librarian))
	store.Push(agentID, makeMsg("msg-3", overseer))

	// Peek for overseer
	msgs := store.Peek(overseer, 0)
	if len(msgs) != 2 {
		t.Fatalf("Peek(overseer) len = %d, want 2", len(msgs))
	}

	// With limit
	msgs = store.Peek(overseer, 1)
	if len(msgs) != 1 {
		t.Fatalf("Peek(overseer, 1) len = %d, want 1", len(msgs))
	}

	// Peek doesn't remove
	if store.Depth(agentID) != 3 {
		t.Errorf("Depth = %d, want 3", store.Depth(agentID))
	}
}

func TestOutbox_PeekFrom(t *testing.T) {
	store, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	agentID := protocol.AgentID("test_agent")
	overseer := protocol.Address{Type: "overseer"}

	store.Push(agentID, makeMsg("msg-1", overseer))
	store.Push(agentID, makeMsg("msg-2", overseer))

	msgs := store.PeekFrom(agentID, 0)
	if len(msgs) != 2 {
		t.Fatalf("PeekFrom len = %d, want 2", len(msgs))
	}

	msgs = store.PeekFrom(agentID, 1)
	if len(msgs) != 1 {
		t.Fatalf("PeekFrom(1) len = %d, want 1", len(msgs))
	}
}

func TestOutbox_Get(t *testing.T) {
	store, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	agentID := protocol.AgentID("test_agent")
	overseer := protocol.Address{Type: "overseer"}

	store.Push(agentID, makeMsg("msg-1", overseer))

	msg := store.Get("msg-1")
	if msg == nil || msg.ID != "msg-1" {
		t.Errorf("Get(msg-1) = %v, want msg-1", msg)
	}

	msg = store.Get("nonexistent")
	if msg != nil {
		t.Errorf("Get(nonexistent) = %v, want nil", msg)
	}
}

func TestOutbox_QueueCap(t *testing.T) {
	store, err := New(Config{Cap: 2})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	agentID := protocol.AgentID("test_agent")
	overseer := protocol.Address{Type: "overseer"}

	store.Push(agentID, makeMsg("msg-1", overseer))
	store.Push(agentID, makeMsg("msg-2", overseer))

	if err := store.Push(agentID, makeMsg("msg-3", overseer)); err != ErrQueueFull {
		t.Errorf("Push(msg-3) = %v, want ErrQueueFull", err)
	}
}

func TestOutbox_DeleteAgent(t *testing.T) {
	store, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	agentID := protocol.AgentID("test_agent")
	overseer := protocol.Address{Type: "overseer"}

	store.Push(agentID, makeMsg("msg-1", overseer))
	store.Push(agentID, makeMsg("msg-2", overseer))

	store.DeleteAgent(agentID)

	if store.Depth(agentID) != 0 {
		t.Errorf("Depth after delete = %d, want 0", store.Depth(agentID))
	}

	// Get should also fail
	if msg := store.Get("msg-1"); msg != nil {
		t.Errorf("Get after delete = %v, want nil", msg)
	}
}

func TestOutbox_ValidationError(t *testing.T) {
	store, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	agentID := protocol.AgentID("test_agent")

	// Invalid message (missing summary)
	badMsg := &protocol.Message{
		ID:   "bad-1",
		TS:   1000,
		From: protocol.Address{Type: "agent", ID: "test"},
		To:   protocol.Address{Type: "overseer"},
		Lane: protocol.LaneControl,
	}

	if err := store.Push(agentID, badMsg); err == nil {
		t.Error("Push(invalid) should fail")
	}
}

func TestOutbox_Persistence(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "outbox")

	agentID := protocol.AgentID("test_agent")
	overseer := protocol.Address{Type: "overseer"}

	// Create store and push messages
	store1, err := New(Config{StorePath: storePath})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	store1.Push(agentID, makeMsg("msg-1", overseer))
	store1.Push(agentID, makeMsg("msg-2", overseer))

	// Pop one message
	msg := store1.Pop(overseer)
	if msg.ID != "msg-1" {
		t.Fatalf("Pop() = %s, want msg-1", msg.ID)
	}

	// Create new store from same path (simulate restart)
	store2, err := New(Config{StorePath: storePath})
	if err != nil {
		t.Fatalf("New() replay error = %v", err)
	}

	// msg-2 should still be there
	if store2.Depth(agentID) != 1 {
		t.Errorf("Depth after replay = %d, want 1", store2.Depth(agentID))
	}

	msg = store2.Pop(overseer)
	if msg == nil || msg.ID != "msg-2" {
		t.Errorf("Pop() = %v, want msg-2", msg)
	}
}

func TestOutbox_PersistenceOrdering(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "outbox")

	agentID := protocol.AgentID("test_agent")
	overseer := protocol.Address{Type: "overseer"}

	// Create store with messages at different timestamps
	store1, err := New(Config{StorePath: storePath})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	msg1 := makeMsg("msg-1", overseer)
	msg1.TS = 1000
	msg2 := makeMsg("msg-2", overseer)
	msg2.TS = 2000
	msg3 := makeMsg("msg-3", overseer)
	msg3.TS = 1500

	store1.Push(agentID, msg1)
	store1.Push(agentID, msg2)
	store1.Push(agentID, msg3)

	// Replay and check order (should be sorted by timestamp)
	store2, err := New(Config{StorePath: storePath})
	if err != nil {
		t.Fatalf("New() replay error = %v", err)
	}

	msgs := store2.PeekFrom(agentID, 0)
	if len(msgs) != 3 {
		t.Fatalf("PeekFrom len = %d, want 3", len(msgs))
	}

	// Should be sorted by timestamp: msg-1 (1000), msg-3 (1500), msg-2 (2000)
	if msgs[0].ID != "msg-1" || msgs[1].ID != "msg-3" || msgs[2].ID != "msg-2" {
		t.Errorf("Order after replay = [%s, %s, %s], want [msg-1, msg-3, msg-2]",
			msgs[0].ID, msgs[1].ID, msgs[2].ID)
	}
}

func TestOutbox_EmptyAgentReturnsNil(t *testing.T) {
	store, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	agentID := protocol.AgentID("nonexistent")
	overseer := protocol.Address{Type: "overseer"}

	if msg := store.PopFrom(agentID, overseer); msg != nil {
		t.Errorf("PopFrom(nonexistent) = %v, want nil", msg)
	}

	if msgs := store.PeekFrom(agentID, 10); msgs != nil {
		t.Errorf("PeekFrom(nonexistent) = %v, want nil", msgs)
	}

	if depth := store.Depth(agentID); depth != 0 {
		t.Errorf("Depth(nonexistent) = %d, want 0", depth)
	}
}

func TestOutbox_WildcardFilter(t *testing.T) {
	store, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	agentID := protocol.AgentID("test_agent")
	overseer := protocol.Address{Type: "overseer"}
	librarian := protocol.Address{Type: "librarian"}

	store.Push(agentID, makeMsg("msg-1", overseer))
	store.Push(agentID, makeMsg("msg-2", librarian))

	// Empty filter matches all
	msgs := store.Peek(protocol.Address{}, 0)
	if len(msgs) != 2 {
		t.Fatalf("Peek(empty) len = %d, want 2", len(msgs))
	}
}

func TestOutbox_DeleteAgentWithPersistence(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "outbox")

	store, err := New(Config{StorePath: storePath})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	agentID := protocol.AgentID("test_agent")
	overseer := protocol.Address{Type: "overseer"}

	store.Push(agentID, makeMsg("msg-1", overseer))

	// Delete should remove log file
	store.DeleteAgent(agentID)

	logFile := filepath.Join(storePath, "test_agent.jsonl")
	if _, err := os.Stat(logFile); !os.IsNotExist(err) {
		t.Error("Log file should be deleted")
	}
}
