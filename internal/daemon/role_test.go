package daemon

import (
	"context"
	"fmt"
	"testing"
)

func TestRoleInference(t *testing.T) {
	// MVP: all tasks are workers. When planner role is added,
	// expand this table with the distinguishing cases.
	tests := []struct {
		name string
		meta TaskMeta
		want Role
	}{
		{
			name: "task with DoD is worker",
			meta: TaskMeta{ID: "ts-abc", Type: "task", DefinitionOfDone: "Tests pass"},
			want: RoleWorker,
		},
		{
			name: "epic is worker",
			meta: TaskMeta{ID: "ep-abc", Type: "epic"},
			want: RoleWorker,
		},
		{
			name: "task without DoD is worker",
			meta: TaskMeta{ID: "ts-abc", Type: "task"},
			want: RoleWorker,
		},
		{
			name: "task with labels is worker",
			meta: TaskMeta{ID: "ts-abc", Type: "task", Labels: []string{"plan"}},
			want: RoleWorker,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferRole(tt.meta)
			if got != tt.want {
				t.Errorf("InferRole(%+v) = %q, want %q", tt.meta, got, tt.want)
			}
		})
	}
}

func TestFetchTaskMeta(t *testing.T) {
	json := `{
		"id": "ts-abc",
		"title": "Do the thing",
		"type": "task",
		"status": "open",
		"priority": 1,
		"project": "myproject",
		"parent": "ep-123",
		"definition_of_done": "It works. Run: go test",
		"labels": ["daemon"],
		"dependencies": [],
		"logs": []
	}`

	runner := fakeRunner(json, nil)
	meta, err := FetchTaskMeta(context.Background(), "ts-abc", "myproject", runner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if meta.ID != "ts-abc" {
		t.Errorf("ID = %q, want %q", meta.ID, "ts-abc")
	}
	if meta.Type != "task" {
		t.Errorf("Type = %q, want %q", meta.Type, "task")
	}
	if meta.DefinitionOfDone != "It works. Run: go test" {
		t.Errorf("DefinitionOfDone = %q, want %q", meta.DefinitionOfDone, "It works. Run: go test")
	}
	if len(meta.Labels) != 1 || meta.Labels[0] != "daemon" {
		t.Errorf("Labels = %v, want [daemon]", meta.Labels)
	}
}

func TestFetchTaskMetaCommandError(t *testing.T) {
	runner := fakeRunner("not found", fmt.Errorf("exit status 1"))
	_, err := FetchTaskMeta(context.Background(), "ts-nope", "myproject", runner)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestFetchTaskMetaInvalidJSON(t *testing.T) {
	runner := fakeRunner("not json at all", nil)
	_, err := FetchTaskMeta(context.Background(), "ts-abc", "myproject", runner)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestFetchTaskMetaNullDoD(t *testing.T) {
	json := `{
		"id": "ep-abc",
		"type": "epic",
		"definition_of_done": null,
		"labels": []
	}`

	runner := fakeRunner(json, nil)
	meta, err := FetchTaskMeta(context.Background(), "ep-abc", "myproject", runner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if meta.DefinitionOfDone != "" {
		t.Errorf("DefinitionOfDone = %q, want empty string for null", meta.DefinitionOfDone)
	}
}
