package daemon

import "strings"

// EnsureAttachSpawnCmd returns spawnCmd with an attach target.
// If spawnCmd already includes --attach, it is returned unchanged.
func EnsureAttachSpawnCmd(spawnCmd, serverURL string) string {
	if spawnCmdHasAttach(spawnCmd) {
		return spawnCmd
	}
	return strings.TrimSpace(spawnCmd + " --attach " + serverURL)
}

// WithSessionFlag returns spawnCmd with --session <id> appended.
// If sessionID is empty or malformed, the command is returned unchanged.
// This enables session-aware respawn: a crashed agent reconnects to its
// existing opencode session instead of creating a new one.
//
// Session IDs must contain only alphanumeric characters, hyphens, and
// underscores. This prevents whitespace from splitting into extra argv
// tokens when ExecProcessStarter tokenizes via strings.Fields.
func WithSessionFlag(spawnCmd, sessionID string) string {
	if sessionID == "" {
		return spawnCmd
	}
	if !isValidSessionID(sessionID) {
		return spawnCmd
	}
	return strings.TrimSpace(spawnCmd + " --session " + sessionID)
}

// isValidSessionID checks that a session ID contains only safe characters.
// Session IDs from opencode follow the ses_<random> format (alphanumeric
// with underscores). This rejects whitespace, shell metacharacters, and
// path separators that could corrupt the spawn command or API URLs.
func isValidSessionID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func spawnCmdHasAttach(spawnCmd string) bool {
	for _, tok := range strings.Fields(spawnCmd) {
		if tok == "--attach" || strings.HasPrefix(tok, "--attach=") {
			return true
		}
	}
	return false
}
