# Codex Skill Installation Locations

**Task**: ts-0e0711  
**Date**: 2026-02-10  
**Status**: Complete

## Summary

Researched OpenAI Codex CLI to determine skill installation paths and formats. Codex supports skills but does NOT support agents (as defined in aetherflow's architecture) or plugins.

## Key Findings

### Skills

Codex **DOES** support skills using the [Agent Skills standard](https://agentskills.io).

**Format**: `SKILL.md` files with YAML frontmatter
```markdown
---
name: skill-name
description: When this skill should trigger
---

Skill instructions for Codex to follow.
```

**Installation locations** (in precedence order):
1. `$CWD/.agents/skills/` - Current working directory
2. `$CWD/../.agents/skills/` - Parent directories up to repo root
3. `$REPO_ROOT/.agents/skills/` - Repository root (when inside a Git repo)
4. `$HOME/.agents/skills/` - User's home directory
5. `/etc/codex/skills/` - System-wide admin location
6. System-bundled skills (shipped with Codex)

Codex scans **all** these locations and makes all discovered skills available. Skills with duplicate names don't merge - both appear in skill selectors.

**Progressive disclosure**: Codex initially loads only skill metadata (name, description, path), then loads full `SKILL.md` content when the skill is activated.

**Optional metadata**: Skills can include `agents/openai.yaml` for UI customization and MCP dependencies (used by Codex app, not CLI):
```yaml
interface:
  display_name: "User-facing name"
  short_description: "Short description"
  icon_small: "./assets/small-logo.svg"
  icon_large: "./assets/large-logo.png"
  brand_color: "#3B82F6"
  default_prompt: "Optional surrounding prompt"

dependencies:
  tools:
    - type: "mcp"
      value: "openaiDeveloperDocs"
      description: "OpenAI Docs MCP server"
      transport: "streamable_http"
      url: "https://developers.openai.com/mcp"
```

### Agents

Codex **DOES NOT** support agents as a separate installation category. The term "agents" in Codex docs refers to their system prompts and AGENTS.md files (custom instructions), not subagent definitions that can be spawned via Task() calls like in opencode/claude.

**What Codex calls "agents"**:
- `AGENTS.md` files: Custom instructions that modify Codex's behavior for a project
- Located at: `~/.codex/AGENTS.md` (global), project root, or nested directories
- NOT the same as aetherflow's agent definitions

### Plugins

Codex **DOES NOT** support plugins as defined in aetherflow's architecture. There is no plugin installation system, no TypeScript plugin files, and no registration mechanism.

**What exists instead**:
- `.rules` files: Control which commands Codex can run outside the sandbox (in `~/.codex/rules/`)
- MCP (Model Context Protocol): Tool integration system, not a plugin system
- Skills can declare MCP dependencies, but MCP servers are not "plugins" in the aetherflow sense

## Compatibility Assessment

**Can aetherflow support Codex?**

| Category | Supported? | Notes |
|----------|-----------|-------|
| Skills | ✅ YES | Full support - can install to `.agents/skills/` |
| Agents | ❌ NO | Codex doesn't have this concept |
| Plugins | ❌ NO | Codex doesn't have this concept |

## Implementation Recommendation

Update `codexHarness` to:

1. **Skills**: Return `.agents/skills/{name}/SKILL.md` for skill paths
2. **Agents**: Return `ErrNotSupported` with explanation that Codex uses AGENTS.md for instructions, not separate agent definitions
3. **Plugins**: Return `ErrNotSupported` - Codex has no plugin system

The harness should support partial installation: users can install skills to Codex even though agents and plugins aren't supported.

## References

- [Codex Skills Documentation](https://developers.openai.com/codex/skills)
- [Codex Rules Documentation](https://developers.openai.com/codex/rules)
- [Codex AGENTS.md Documentation](https://developers.openai.com/codex/guides/agents-md)
- [Agent Skills Standard](https://agentskills.io)
- [OpenAI Codex GitHub](https://github.com/openai/codex)

## Example Codex Skill Structure

```
my-skill/
├── SKILL.md              # Required: name, description, instructions
├── scripts/              # Optional: executable code
├── references/           # Optional: documentation
├── assets/              # Optional: templates, resources
└── agents/
    └── openai.yaml      # Optional: UI metadata and MCP deps
```

## Detection Logic

Codex is considered "installed" if:
- The `codex` command is in `$PATH` OR
- The config directory `~/.codex/` exists

This matches the detection pattern used for opencode and claude harnesses.
