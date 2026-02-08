package tui

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/charmbracelet/glamour"
)

// TaskDetail holds the parsed output from `prog show --json <task_id>`.
type TaskDetail struct {
	ID               string    `json:"id"`
	Title            string    `json:"title"`
	Type             string    `json:"type"`
	Status           string    `json:"status"`
	Priority         int       `json:"priority"`
	Project          string    `json:"project"`
	Parent           string    `json:"parent"`
	Description      string    `json:"description"`
	DefinitionOfDone *string   `json:"definition_of_done"`
	Labels           []string  `json:"labels"`
	Dependencies     []string  `json:"dependencies"`
	Logs             []TaskLog `json:"logs"`
}

// TaskLog is a single log entry from prog.
type TaskLog struct {
	Message   string `json:"message"`
	CreatedAt string `json:"created_at"`
}

// fetchTaskDetail runs `prog show --json <taskID>` and parses the result.
// Returns nil with no error if prog is not available or the command fails
// (non-critical — the panel still renders without task detail).
func fetchTaskDetail(taskID string) (*TaskDetail, error) {
	cmd := exec.Command("prog", "show", "--json", taskID)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("prog show: %w", err)
	}

	var detail TaskDetail
	if err := json.Unmarshal(out, &detail); err != nil {
		return nil, fmt.Errorf("parsing prog output: %w", err)
	}

	return &detail, nil
}

// renderMarkdown renders a markdown string using glamour, falling back
// to plain word-wrapped text if glamour fails.
func renderMarkdown(md string, width int) string {
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return wrapText(md, width)
	}
	out, err := r.Render(md)
	if err != nil {
		return wrapText(md, width)
	}
	return strings.TrimRight(out, "\n")
}

// renderTaskInfo formats the task detail into styled text for display
// in the task info pane. Returns a string ready for a viewport.
func renderTaskInfo(td *TaskDetail, width int) string {
	if td == nil {
		return dimStyle.Render("Loading task info...")
	}

	var b strings.Builder

	// Title
	b.WriteString(paneHeaderStyle.Render(td.Title))
	b.WriteString("\n\n")

	// Status / Priority / ID
	statusColor := dimStyle
	switch td.Status {
	case "in_progress":
		statusColor = greenStyle
	case "open":
		statusColor = blueStyle
	case "blocked":
		statusColor = redStyle
	case "done":
		statusColor = dimStyle
	}

	b.WriteString(fmt.Sprintf("%s %s  %s %s  %s %s",
		dimStyle.Render("Status:"),
		statusColor.Render(td.Status),
		dimStyle.Render("Priority:"),
		fmt.Sprintf("P%d", td.Priority),
		dimStyle.Render("ID:"),
		td.ID,
	))
	b.WriteString("\n")

	// Dependencies
	if len(td.Dependencies) > 0 {
		b.WriteString(fmt.Sprintf("%s %s",
			dimStyle.Render("Deps:"),
			blueStyle.Render(strings.Join(td.Dependencies, ", ")),
		))
		b.WriteString("\n")
	}

	// Description (rendered as markdown)
	if td.Description != "" {
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("── Description ──"))
		b.WriteString("\n")
		b.WriteString(renderMarkdown(td.Description, width))
		b.WriteString("\n")
	}

	// Definition of Done (rendered as markdown)
	if td.DefinitionOfDone != nil && *td.DefinitionOfDone != "" {
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("── Definition of Done ──"))
		b.WriteString("\n")
		b.WriteString(renderMarkdown(*td.DefinitionOfDone, width))
		b.WriteString("\n")
	}

	return b.String()
}

// wrapText does simple word wrapping at the given width.
// Preserves existing newlines. Used as fallback when glamour fails.
func wrapText(s string, width int) string {
	if width <= 0 {
		return s
	}

	var result strings.Builder
	for _, paragraph := range strings.Split(s, "\n") {
		if result.Len() > 0 {
			result.WriteString("\n")
		}

		if len([]rune(paragraph)) <= width {
			result.WriteString(paragraph)
			continue
		}

		words := strings.Fields(paragraph)
		lineLen := 0
		for i, word := range words {
			wordLen := len([]rune(word))
			if i > 0 && lineLen+1+wordLen > width {
				result.WriteString("\n")
				lineLen = 0
			} else if i > 0 {
				result.WriteString(" ")
				lineLen++
			}
			result.WriteString(word)
			lineLen += wordLen
		}
	}

	return result.String()
}
