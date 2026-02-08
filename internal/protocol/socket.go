package protocol

import (
	"fmt"
	"path/filepath"
)

// DefaultSocketPath is the fallback socket path when no project is known.
// Prefer SocketPathFor(project) to scope sockets per project.
const DefaultSocketPath = "/tmp/aetherd.sock"

// SocketPathFor returns a project-scoped socket path.
// Each project gets its own socket so daemons for different projects
// can't accidentally interfere with each other.
//
// The project name is sanitized with filepath.Base to prevent path
// traversal — a project name like "../../etc/evil" is reduced to "evil".
func SocketPathFor(project string) string {
	if project == "" {
		return DefaultSocketPath
	}
	safe := filepath.Base(project)
	if safe == "." || safe == "/" {
		return DefaultSocketPath
	}
	return fmt.Sprintf("/tmp/aetherd-%s.sock", safe)
}
