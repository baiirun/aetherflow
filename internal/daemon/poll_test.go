package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"
)

func TestParseProgReady(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []Task
		wantErr bool
	}{
		{
			name:  "typical output with multiple tasks",
			input: "ID           PRI  TITLE\nts-1450cd    1    [DoD] Daemon: poll loop\nep-82985b    2    [DoD] Daemon — process supervisor\n",
			want: []Task{
				{ID: "ts-1450cd", Priority: 1, Title: "[DoD] Daemon: poll loop"},
				{ID: "ep-82985b", Priority: 2, Title: "[DoD] Daemon — process supervisor"},
			},
		},
		{
			name:  "single task",
			input: "ID           PRI  TITLE\nts-abc123    3    Fix the thing\n",
			want: []Task{
				{ID: "ts-abc123", Priority: 3, Title: "Fix the thing"},
			},
		},
		{
			name:  "empty output",
			input: "",
			want:  nil,
		},
		{
			name:  "header only no tasks",
			input: "ID           PRI  TITLE\n",
			want:  nil,
		},
		{
			name:  "blank lines between tasks",
			input: "ID           PRI  TITLE\nts-aaa    1    Task A\n\nts-bbb    2    Task B\n",
			want: []Task{
				{ID: "ts-aaa", Priority: 1, Title: "Task A"},
				{ID: "ts-bbb", Priority: 2, Title: "Task B"},
			},
		},
		{
			name:    "invalid priority",
			input:   "ID           PRI  TITLE\nts-aaa    X    Bad priority\n",
			wantErr: true,
		},
		{
			name:    "too few fields",
			input:   "ID           PRI  TITLE\nts-aaa\n",
			wantErr: true,
		},
		{
			name:  "title with many spaces",
			input: "ID           PRI  TITLE\nts-aaa    1    A title with   many   spaces\n",
			want: []Task{
				{ID: "ts-aaa", Priority: 1, Title: "A title with many spaces"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseProgReady(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d tasks, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i].ID != tt.want[i].ID {
					t.Errorf("task[%d].ID = %q, want %q", i, got[i].ID, tt.want[i].ID)
				}
				if got[i].Priority != tt.want[i].Priority {
					t.Errorf("task[%d].Priority = %d, want %d", i, got[i].Priority, tt.want[i].Priority)
				}
				if got[i].Title != tt.want[i].Title {
					t.Errorf("task[%d].Title = %q, want %q", i, got[i].Title, tt.want[i].Title)
				}
			}
		})
	}
}

// fakeRunner returns a CommandRunner that returns canned output.
func fakeRunner(output string, err error) CommandRunner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte(output), err
	}
}

func TestPollerPoll(t *testing.T) {
	output := "ID           PRI  TITLE\nts-abc    1    Do the thing\nts-def    2    Other thing\n"
	p := NewPoller("myproject", time.Second, fakeRunner(output, nil), slog.Default())

	tasks, err := p.Poll(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("got %d tasks, want 2", len(tasks))
	}
	if tasks[0].ID != "ts-abc" {
		t.Errorf("tasks[0].ID = %q, want %q", tasks[0].ID, "ts-abc")
	}
	if tasks[1].ID != "ts-def" {
		t.Errorf("tasks[1].ID = %q, want %q", tasks[1].ID, "ts-def")
	}
}

func TestPollerPollCommandError(t *testing.T) {
	p := NewPoller("myproject", time.Second, fakeRunner("", fmt.Errorf("exit status 1")), slog.Default())

	_, err := p.Poll(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestPollerPollEmptyOutput(t *testing.T) {
	p := NewPoller("myproject", time.Second, fakeRunner("ID           PRI  TITLE\n", nil), slog.Default())

	tasks, err := p.Poll(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("got %d tasks, want 0", len(tasks))
	}
}

func TestPollerStartSendsTasksAndStopsOnCancel(t *testing.T) {
	output := "ID           PRI  TITLE\nts-abc    1    Task\n"

	calls := 0
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		calls++
		return []byte(output), nil
	}

	p := NewPoller("myproject", 10*time.Millisecond, runner, slog.Default())
	ctx, cancel := context.WithCancel(context.Background())

	ch := p.Start(ctx)

	// Should receive at least one batch (the immediate poll).
	select {
	case tasks := <-ch:
		if len(tasks) != 1 {
			t.Fatalf("got %d tasks, want 1", len(tasks))
		}
		if tasks[0].ID != "ts-abc" {
			t.Errorf("tasks[0].ID = %q, want %q", tasks[0].ID, "ts-abc")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for tasks")
	}

	// Cancel and verify channel closes.
	cancel()

	select {
	case _, ok := <-ch:
		if ok {
			// Got another batch before close, that's fine — drain it.
			// But the channel must eventually close.
			select {
			case _, ok := <-ch:
				if ok {
					t.Fatal("channel should be closed after context cancel")
				}
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for channel close")
			}
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for channel close")
	}
}

func TestPollerStartMultipleTicks(t *testing.T) {
	output := "ID           PRI  TITLE\nts-abc    1    Task\n"
	p := NewPoller("myproject", 10*time.Millisecond, fakeRunner(output, nil), slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := p.Start(ctx)

	// Drain at least 3 batches to verify the ticker is working.
	for i := 0; i < 3; i++ {
		select {
		case tasks := <-ch:
			if len(tasks) != 1 {
				t.Fatalf("batch %d: got %d tasks, want 1", i, len(tasks))
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for batch %d", i)
		}
	}
}

func TestPollerStartHandlesErrors(t *testing.T) {
	// Runner that fails every time. The poller should not send anything
	// on the channel, but should keep running (not panic or close the channel).
	p := NewPoller("myproject", 10*time.Millisecond, fakeRunner("", fmt.Errorf("broken")), slog.Default())

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	ch := p.Start(ctx)

	// Should not receive any tasks since every poll errors.
	select {
	case tasks, ok := <-ch:
		if ok {
			t.Fatalf("expected no tasks on error, got %d", len(tasks))
		}
		// Channel closed due to context timeout — that's correct.
	case <-time.After(time.Second):
		t.Fatal("timed out — channel should close when context expires")
	}
}

func TestPollerStartNoTasksNothingSent(t *testing.T) {
	// Runner returns header only (no tasks).
	p := NewPoller("myproject", 10*time.Millisecond, fakeRunner("ID           PRI  TITLE\n", nil), slog.Default())

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	ch := p.Start(ctx)

	// Should not receive anything since there are no tasks.
	select {
	case tasks, ok := <-ch:
		if ok {
			t.Fatalf("expected no tasks, got %d", len(tasks))
		}
	case <-time.After(time.Second):
		t.Fatal("timed out — channel should close when context expires")
	}
}
