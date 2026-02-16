package install

import "embed"

// assetsFS holds the skill and agent definitions compiled into the binary.
// These are installed to the user's opencode configuration directory by the
// "af install" command.
//
//go:embed skills agents
var assetsFS embed.FS
