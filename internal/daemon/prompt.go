package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RenderPrompt reads a role prompt template and replaces {{task_id}} with the
// actual task ID. The prompt files live in prompts/ relative to the project root.
//
// Returns the rendered prompt string ready to pass as the message argument
// to "opencode run".
func RenderPrompt(promptDir string, role Role, taskID string) (string, error) {
	filename := string(role) + ".md"
	path := filepath.Join(promptDir, filename)

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading prompt %s: %w", path, err)
	}

	rendered := strings.ReplaceAll(string(data), "{{task_id}}", taskID)
	return rendered, nil
}
