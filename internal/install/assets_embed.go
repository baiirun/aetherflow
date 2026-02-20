package install

import "embed"

// assetsFS holds the skill, agent, and plugin definitions compiled into the
// binary. These are installed to the user's opencode configuration directory
// by the "af install" command.
//
//go:embed skills agents plugins
var assetsFS embed.FS
