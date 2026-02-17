package daemon

import "strings"

// EnsureAttachSpawnCmd returns spawnCmd with an attach target.
// If spawnCmd already includes --attach, it is returned unchanged.
func EnsureAttachSpawnCmd(spawnCmd, serverURL string) string {
	if strings.Contains(spawnCmd, "--attach") {
		return spawnCmd
	}
	return strings.TrimSpace(spawnCmd + " --attach " + serverURL)
}
