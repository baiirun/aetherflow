package daemon

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// RenderPrompt reads a role prompt template and replaces {{task_id}} with the
// actual task ID.
//
// When promptDir is empty, prompts are read from the embedded filesystem
// compiled into the binary. When promptDir is set, prompts are read from
// that filesystem path instead (for development/customization).
//
// This is literal string replacement, not Go text/template. The only recognized
// variable is {{task_id}}.
//
// Returns the rendered prompt string ready to pass as the message argument
// to "opencode run".
func RenderPrompt(promptDir string, role Role, taskID string) (string, error) {
	// Allowlist roles to prevent path traversal if role ever becomes dynamic.
	switch role {
	case RoleWorker, RolePlanner:
	default:
		return "", fmt.Errorf("unknown role: %q", role)
	}

	filename := string(role) + ".md"

	var data []byte
	var err error

	if promptDir == "" {
		// Read from embedded filesystem.
		data, err = fs.ReadFile(promptsFS, "prompts/"+filename)
		if err != nil {
			return "", fmt.Errorf("reading embedded prompt %s: %w", filename, err)
		}
	} else {
		// Read from filesystem override.
		path := filepath.Join(promptDir, filename)
		data, err = os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("reading prompt %s: %w", path, err)
		}
	}

	rendered := strings.ReplaceAll(string(data), "{{task_id}}", taskID)

	// Catch template typos (e.g., "{{ task_id }}" with spaces) that would
	// leave unresolved variables in the prompt.
	if strings.Contains(rendered, "{{") {
		source := "embedded"
		if promptDir != "" {
			source = filepath.Join(promptDir, filename)
		}
		return "", fmt.Errorf("unresolved template variable in %s", source)
	}

	return rendered, nil
}
