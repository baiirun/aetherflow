package cmd

import (
	"errors"
	"testing"

	"github.com/baiirun/aetherflow/internal/sessions"
)

func TestLoadOpencodeSessionIndex(t *testing.T) {
	original := runCommandOutput
	t.Cleanup(func() { runCommandOutput = original })

	runCommandOutput = func(name string, args ...string) ([]byte, error) {
		if name != "opencode" {
			t.Fatalf("name = %q, want opencode", name)
		}
		return []byte(`[
  {"id":"ses_1","title":"Fix race in daemon","directory":"/tmp/proj"},
  {"id":"ses_2","title":"","directory":"/tmp/other"}
]`), nil
	}

	idx := loadOpencodeSessionIndex()
	if len(idx) != 2 {
		t.Fatalf("len(index) = %d, want 2", len(idx))
	}
	if idx["ses_1"].Title != "Fix race in daemon" {
		t.Fatalf("index[ses_1].Title = %q", idx["ses_1"].Title)
	}
}

func TestLoadOpencodeSessionIndexError(t *testing.T) {
	original := runCommandOutput
	t.Cleanup(func() { runCommandOutput = original })

	runCommandOutput = func(name string, args ...string) ([]byte, error) {
		return nil, errors.New("boom")
	}

	idx := loadOpencodeSessionIndex()
	if len(idx) != 0 {
		t.Fatalf("len(index) = %d, want 0", len(idx))
	}
}

func TestSessionWhatForRecord(t *testing.T) {
	tests := []struct {
		name     string
		rec      sessions.Record
		index    map[string]opencodeSessionSummary
		semantic map[string]string
		want     string
	}{
		{
			name: "prefers semantic objective",
			rec:  sessions.Record{SessionID: "ses_1", ServerRef: "http://127.0.0.1:4096", WorkRef: "ts-123"},
			semantic: map[string]string{
				recordKey("http://127.0.0.1:4096", "ses_1"): "Run regression tests and report failures",
			},
			index: map[string]opencodeSessionSummary{
				"ses_1": {ID: "ses_1", Title: "Session purpose text"},
			},
			want: "Run regression tests and report failures",
		},
		{
			name: "prefers opencode title",
			rec:  sessions.Record{SessionID: "ses_1", WorkRef: "ts-123"},
			index: map[string]opencodeSessionSummary{
				"ses_1": {ID: "ses_1", Title: "Session purpose text"},
			},
			want: "Session purpose text",
		},
		{
			name: "falls back to work ref",
			rec:  sessions.Record{SessionID: "ses_missing", WorkRef: "ts-123"},
			want: "ts-123",
		},
		{
			name: "falls back to directory basename",
			rec:  sessions.Record{SessionID: "ses_2"},
			index: map[string]opencodeSessionSummary{
				"ses_2": {ID: "ses_2", Directory: "/Users/dev/repo-name"},
			},
			want: "dir: repo-name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sessionWhatForRecord(tt.rec, tt.index, tt.semantic)
			if got != tt.want {
				t.Fatalf("sessionWhatForRecord() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestObjectiveFromPromptText(t *testing.T) {
	text := `"# Spawn Agent

## Objective

Run go test ./... and then summarize failures.

## Output

Done
"`
	got := objectiveFromPromptText(text)
	want := "Run go test ./... and then summarize failures."
	if got != want {
		t.Fatalf("objectiveFromPromptText() = %q, want %q", got, want)
	}
}

func TestShouldEnrichSessionTitle(t *testing.T) {
	tests := []struct {
		title string
		want  bool
	}{
		{title: "Autonomous Spawn Agent: setup", want: true},
		{title: "Spawn-ice_fox objective", want: true},
		{title: "New session - 2026-02-17T04:16:43.591Z", want: true},
		{title: "Implement websocket retries", want: false},
	}
	for _, tt := range tests {
		if got := shouldEnrichSessionTitle(tt.title); got != tt.want {
			t.Fatalf("shouldEnrichSessionTitle(%q) = %v, want %v", tt.title, got, tt.want)
		}
	}
}
