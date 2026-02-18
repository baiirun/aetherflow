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

func spawnCmdHasAttach(spawnCmd string) bool {
	for _, tok := range strings.Fields(spawnCmd) {
		if tok == "--attach" || strings.HasPrefix(tok, "--attach=") {
			return true
		}
	}
	return false
}
