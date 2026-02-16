package install

import "embed"

// assetsFS holds the skill and agent definitions compiled into the binary.
// These are installed to the user's opencode configuration directory by the
// "af install" command.
//
//go:embed skills agents
var assetsFS embed.FS

// claudeAssetsFS holds Claude Code slash command definitions compiled into
// the binary. These are installed to the project's .claude/commands/
// directory by "af install --runtime claude".
//
//go:embed claude-commands
var claudeAssetsFS embed.FS
