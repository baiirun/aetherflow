package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// reconcileRunner builds a CommandRunner for reconcile tests.
// reviewingTasks is the JSON response for `prog list --status reviewing`.
// branchExists maps branch names to whether git rev-parse should succeed.
// mergedBranches maps branch names to whether merge-base --is-ancestor should succeed.
// doneCalls records task IDs that received `prog done`.
type reconcileRunner struct {
	mu             sync.Mutex
	reviewingTasks []progListItem
	branchExists   map[string]bool
	mergedBranches map[string]bool
	doneCalls      []string
	listErr        error // if set, prog list returns this error
	doneErr        error // if set, prog done returns this error
}

func (r *reconcileRunner) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := strings.Join(append([]string{name}, args...), " ")

	// prog list --status reviewing --type task --json -p <project>
	if name == "prog" && len(args) >= 2 && args[0] == "list" && strings.Contains(cmd, "reviewing") {
		if r.listErr != nil {
			return nil, r.listErr
		}
		data, _ := json.Marshal(r.reviewingTasks)
		return data, nil
	}

	// git rev-parse --verify af/<taskID>
	if name == "git" && len(args) >= 2 && args[0] == "rev-parse" {
		branch := args[len(args)-1]
		if r.branchExists[branch] {
			return []byte("abc123\n"), nil
		}
		return nil, fmt.Errorf("fatal: Needed a single revision")
	}

	// git merge-base --is-ancestor af/<taskID> main
	if name == "git" && len(args) >= 3 && args[0] == "merge-base" {
		branch := args[2] // af/<taskID>
		if r.mergedBranches[branch] {
			return nil, nil // exit 0 = is ancestor
		}
		return nil, fmt.Errorf("exit status 1")
	}

	// prog done <taskID>
	if name == "prog" && len(args) >= 1 && args[0] == "done" {
		if r.doneErr != nil {
			return nil, r.doneErr
		}
		r.mu.Lock()
		r.doneCalls = append(r.doneCalls, args[1])
		r.mu.Unlock()
		return []byte("Completed " + args[1] + "\n"), nil
	}

	return nil, fmt.Errorf("unexpected command: %s", cmd)
}

func (r *reconcileRunner) getDoneCalls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]string, len(r.doneCalls))
	copy(cp, r.doneCalls)
	return cp
}

func testDaemonForReconcile(t *testing.T, runner CommandRunner) *Daemon {
	t.Helper()
	cfg := Config{
		Project:           "testproject",
		ReconcileInterval: 50 * time.Millisecond, // fast for tests
		Runner:            runner,
		Logger:            slog.Default(),
	}
	cfg.ApplyDefaults()
	return &Daemon{
		config:   cfg,
		shutdown: make(chan struct{}),
		log:      cfg.Logger,
	}
}

func TestReconcileOnce_MergedBranch(t *testing.T) {
	r := &reconcileRunner{
		reviewingTasks: []progListItem{
			{ID: "ts-abc123", Title: "Some task", Status: "reviewing"},
		},
		branchExists:   map[string]bool{"af/ts-abc123": true},
		mergedBranches: map[string]bool{"af/ts-abc123": true},
	}

	d := testDaemonForReconcile(t, r.run)
	d.reconcileOnce(context.Background())

	calls := r.getDoneCalls()
	if len(calls) != 1 || calls[0] != "ts-abc123" {
		t.Errorf("expected prog done ts-abc123, got %v", calls)
	}
}

func TestReconcileOnce_UnmergedBranch(t *testing.T) {
	r := &reconcileRunner{
		reviewingTasks: []progListItem{
			{ID: "ts-abc123", Title: "Some task", Status: "reviewing"},
		},
		branchExists:   map[string]bool{"af/ts-abc123": true},
		mergedBranches: map[string]bool{}, // not merged
	}

	d := testDaemonForReconcile(t, r.run)
	d.reconcileOnce(context.Background())

	calls := r.getDoneCalls()
	if len(calls) != 0 {
		t.Errorf("expected no prog done calls for unmerged branch, got %v", calls)
	}
}

func TestReconcileOnce_MissingBranch(t *testing.T) {
	// Branch doesn't exist — treat as merged (already cleaned up).
	r := &reconcileRunner{
		reviewingTasks: []progListItem{
			{ID: "ts-abc123", Title: "Some task", Status: "reviewing"},
		},
		branchExists:   map[string]bool{}, // branch gone
		mergedBranches: map[string]bool{},
	}

	d := testDaemonForReconcile(t, r.run)
	d.reconcileOnce(context.Background())

	calls := r.getDoneCalls()
	if len(calls) != 1 || calls[0] != "ts-abc123" {
		t.Errorf("expected prog done for missing branch (treated as merged), got %v", calls)
	}
}

func TestReconcileOnce_NoReviewingTasks(t *testing.T) {
	r := &reconcileRunner{
		reviewingTasks: []progListItem{},
	}

	d := testDaemonForReconcile(t, r.run)
	d.reconcileOnce(context.Background())

	calls := r.getDoneCalls()
	if len(calls) != 0 {
		t.Errorf("expected no prog done calls when no reviewing tasks, got %v", calls)
	}
}

func TestReconcileOnce_ProgListError(t *testing.T) {
	r := &reconcileRunner{
		listErr: fmt.Errorf("prog not found"),
	}

	d := testDaemonForReconcile(t, r.run)
	// Should not panic — errors are logged and swallowed.
	d.reconcileOnce(context.Background())

	calls := r.getDoneCalls()
	if len(calls) != 0 {
		t.Errorf("expected no prog done calls on list error, got %v", calls)
	}
}

func TestReconcileOnce_ProgDoneError(t *testing.T) {
	r := &reconcileRunner{
		reviewingTasks: []progListItem{
			{ID: "ts-abc123", Title: "Task A", Status: "reviewing"},
			{ID: "ts-def456", Title: "Task B", Status: "reviewing"},
		},
		branchExists:   map[string]bool{}, // both missing = merged
		mergedBranches: map[string]bool{},
		doneErr:        fmt.Errorf("database locked"),
	}

	d := testDaemonForReconcile(t, r.run)
	d.reconcileOnce(context.Background())

	// Neither should succeed — prog done fails for both.
	calls := r.getDoneCalls()
	if len(calls) != 0 {
		t.Errorf("expected no successful done calls when prog done errors, got %v", calls)
	}
}

func TestReconcileOnce_MultipleTasks_MixedState(t *testing.T) {
	r := &reconcileRunner{
		reviewingTasks: []progListItem{
			{ID: "ts-merged", Title: "Merged one", Status: "reviewing"},
			{ID: "ts-pending", Title: "Still pending", Status: "reviewing"},
			{ID: "ts-cleaned", Title: "Branch cleaned", Status: "reviewing"},
		},
		branchExists:   map[string]bool{"af/ts-merged": true, "af/ts-pending": true},
		mergedBranches: map[string]bool{"af/ts-merged": true}, // only ts-merged is merged
	}

	d := testDaemonForReconcile(t, r.run)
	d.reconcileOnce(context.Background())

	calls := r.getDoneCalls()
	// ts-merged: merged, should be done
	// ts-pending: exists but not merged, skip
	// ts-cleaned: branch gone, treat as merged
	if len(calls) != 2 {
		t.Fatalf("expected 2 done calls, got %d: %v", len(calls), calls)
	}

	got := map[string]bool{}
	for _, c := range calls {
		got[c] = true
	}
	if !got["ts-merged"] || !got["ts-cleaned"] {
		t.Errorf("expected done calls for ts-merged and ts-cleaned, got %v", calls)
	}
	if got["ts-pending"] {
		t.Errorf("ts-pending should NOT have been marked done")
	}
}

func TestReconcileOnce_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	r := &reconcileRunner{
		reviewingTasks: []progListItem{
			{ID: "ts-abc123", Title: "Some task", Status: "reviewing"},
		},
		branchExists:   map[string]bool{},
		mergedBranches: map[string]bool{},
	}

	d := testDaemonForReconcile(t, r.run)
	d.reconcileOnce(ctx)

	// With context already cancelled, fetchReviewingTasks may fail or
	// the loop may exit early. Either way, no done calls expected.
	calls := r.getDoneCalls()
	if len(calls) != 0 {
		t.Errorf("expected no done calls with cancelled context, got %v", calls)
	}
}

func TestReconcileReviewing_StopsOnContextCancel(t *testing.T) {
	r := &reconcileRunner{
		reviewingTasks: []progListItem{},
	}

	d := testDaemonForReconcile(t, r.run)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		d.reconcileReviewing(ctx)
		close(done)
	}()

	// Let it tick once.
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Good — reconciler exited.
	case <-time.After(2 * time.Second):
		t.Fatal("reconcileReviewing did not stop after context cancellation")
	}
}
