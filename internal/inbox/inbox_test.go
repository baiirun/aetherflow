package inbox

import (
	"testing"

	"github.com/geobrowser/aetherflow/internal/protocol"
)

func makeMsg(id string, ts int64, to protocol.Address) *protocol.Message {
	return &protocol.Message{
		ID:      id,
		TS:      ts,
		From:    protocol.Address{Type: "agent", ID: "sender"},
		To:      to,
		Type:    protocol.TypeStatus,
		Summary: "Message " + id,
	}
}

func TestInbox_PushPeek(t *testing.T) {
	store := New(Config{})
	teamInbox := "team:alpha"
	to := protocol.Address{Type: "team", ID: "alpha"}

	// Push messages
	if err := store.Push(makeMsg("msg-1", 1000, to)); err != nil {
		t.Fatalf("Push error = %v", err)
	}
	if err := store.Push(makeMsg("msg-2", 2000, to)); err != nil {
		t.Fatalf("Push error = %v", err)
	}
	if err := store.Push(makeMsg("msg-3", 3000, to)); err != nil {
		t.Fatalf("Push error = %v", err)
	}

	// Peek all
	msgs := store.Peek(teamInbox, 0, 0)
	if len(msgs) != 3 {
		t.Fatalf("Peek(0, 0) len = %d, want 3", len(msgs))
	}

	// Peek doesn't remove - peek again
	msgs = store.Peek(teamInbox, 0, 0)
	if len(msgs) != 3 {
		t.Fatalf("Peek again len = %d, want 3", len(msgs))
	}
}

func TestInbox_PeekSince(t *testing.T) {
	store := New(Config{})
	teamInbox := "team:alpha"
	to := protocol.Address{Type: "team", ID: "alpha"}

	store.Push(makeMsg("msg-1", 1000, to))
	store.Push(makeMsg("msg-2", 2000, to))
	store.Push(makeMsg("msg-3", 3000, to))

	// Peek since timestamp 1000 (should get msg-2 and msg-3)
	msgs := store.Peek(teamInbox, 1000, 0)
	if len(msgs) != 2 {
		t.Fatalf("Peek(since=1000) len = %d, want 2", len(msgs))
	}
	if msgs[0].ID != "msg-2" || msgs[1].ID != "msg-3" {
		t.Errorf("Peek(since=1000) = [%s, %s], want [msg-2, msg-3]", msgs[0].ID, msgs[1].ID)
	}

	// Peek since timestamp 2000 (should get only msg-3)
	msgs = store.Peek(teamInbox, 2000, 0)
	if len(msgs) != 1 {
		t.Fatalf("Peek(since=2000) len = %d, want 1", len(msgs))
	}
	if msgs[0].ID != "msg-3" {
		t.Errorf("Peek(since=2000) = [%s], want [msg-3]", msgs[0].ID)
	}

	// Peek since timestamp 3000 (should get nothing)
	msgs = store.Peek(teamInbox, 3000, 0)
	if len(msgs) != 0 {
		t.Fatalf("Peek(since=3000) len = %d, want 0", len(msgs))
	}
}

func TestInbox_PeekLimit(t *testing.T) {
	store := New(Config{})
	teamInbox := "team:alpha"
	to := protocol.Address{Type: "team", ID: "alpha"}

	store.Push(makeMsg("msg-1", 1000, to))
	store.Push(makeMsg("msg-2", 2000, to))
	store.Push(makeMsg("msg-3", 3000, to))

	// Peek with limit
	msgs := store.Peek(teamInbox, 0, 2)
	if len(msgs) != 2 {
		t.Fatalf("Peek(limit=2) len = %d, want 2", len(msgs))
	}
	if msgs[0].ID != "msg-1" || msgs[1].ID != "msg-2" {
		t.Errorf("Peek(limit=2) = [%s, %s], want [msg-1, msg-2]", msgs[0].ID, msgs[1].ID)
	}
}

func TestInbox_PeekSinceWithLimit(t *testing.T) {
	store := New(Config{})
	teamInbox := "team:alpha"
	to := protocol.Address{Type: "team", ID: "alpha"}

	store.Push(makeMsg("msg-1", 1000, to))
	store.Push(makeMsg("msg-2", 2000, to))
	store.Push(makeMsg("msg-3", 3000, to))
	store.Push(makeMsg("msg-4", 4000, to))

	// Peek since 1000 with limit 2
	msgs := store.Peek(teamInbox, 1000, 2)
	if len(msgs) != 2 {
		t.Fatalf("Peek(since=1000, limit=2) len = %d, want 2", len(msgs))
	}
	if msgs[0].ID != "msg-2" || msgs[1].ID != "msg-3" {
		t.Errorf("Peek(since=1000, limit=2) = [%s, %s], want [msg-2, msg-3]", msgs[0].ID, msgs[1].ID)
	}
}

func TestInbox_QueueCap(t *testing.T) {
	store := New(Config{Cap: 2})
	to := protocol.Address{Type: "team", ID: "alpha"}

	store.Push(makeMsg("msg-1", 1000, to))
	store.Push(makeMsg("msg-2", 2000, to))

	if err := store.Push(makeMsg("msg-3", 3000, to)); err != ErrQueueFull {
		t.Errorf("Push(msg-3) = %v, want ErrQueueFull", err)
	}
}

func TestInbox_Delete(t *testing.T) {
	store := New(Config{})
	teamInbox := "team:alpha"
	to := protocol.Address{Type: "team", ID: "alpha"}

	store.Push(makeMsg("msg-1", 1000, to))
	store.Push(makeMsg("msg-2", 2000, to))

	store.Delete(teamInbox)

	if store.Depth(teamInbox) != 0 {
		t.Errorf("Depth after delete = %d, want 0", store.Depth(teamInbox))
	}
}

func TestInbox_Depth(t *testing.T) {
	store := New(Config{})
	teamInbox := "team:alpha"
	to := protocol.Address{Type: "team", ID: "alpha"}

	if store.Depth(teamInbox) != 0 {
		t.Errorf("Depth of empty inbox = %d, want 0", store.Depth(teamInbox))
	}

	store.Push(makeMsg("msg-1", 1000, to))
	store.Push(makeMsg("msg-2", 2000, to))

	if store.Depth(teamInbox) != 2 {
		t.Errorf("Depth = %d, want 2", store.Depth(teamInbox))
	}
}

func TestInbox_ValidationError(t *testing.T) {
	store := New(Config{})

	// Invalid message (missing summary)
	badMsg := &protocol.Message{
		ID:   "bad-1",
		TS:   1000,
		From: protocol.Address{Type: "agent", ID: "test"},
		To:   protocol.Address{Type: "team", ID: "alpha"},
		Type: protocol.TypeStatus,
	}

	if err := store.Push(badMsg); err == nil {
		t.Error("Push(invalid) should fail")
	}
}

func TestInbox_EmptyInboxReturnsNil(t *testing.T) {
	store := New(Config{})

	msgs := store.Peek("nonexistent", 0, 10)
	if msgs != nil {
		t.Errorf("Peek(nonexistent) = %v, want nil", msgs)
	}
}

func TestInbox_MultipleInboxes(t *testing.T) {
	store := New(Config{})

	toAlpha := protocol.Address{Type: "team", ID: "alpha"}
	toBeta := protocol.Address{Type: "team", ID: "beta"}

	store.Push(makeMsg("msg-1", 1000, toAlpha))
	store.Push(makeMsg("msg-2", 2000, toBeta))
	store.Push(makeMsg("msg-3", 3000, toAlpha))

	// Alpha inbox should have 2 messages
	if store.Depth("team:alpha") != 2 {
		t.Errorf("Alpha depth = %d, want 2", store.Depth("team:alpha"))
	}

	// Beta inbox should have 1 message
	if store.Depth("team:beta") != 1 {
		t.Errorf("Beta depth = %d, want 1", store.Depth("team:beta"))
	}
}
