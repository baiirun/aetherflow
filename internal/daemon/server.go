package daemon

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"time"
)

// StartManagedServer ensures a local opencode server is available at serverURL.
// If one is already listening, it is reused.
// Extra env vars (e.g. AETHERFLOW_SOCKET, AETHERFLOW_AGENT_ID) are added to
// the server process environment alongside the inherited parent env.
func StartManagedServer(ctx context.Context, serverURL string, env []string, logf func(msg string, args ...any)) (*exec.Cmd, error) {
	u, err := ValidateServerURLLocal(serverURL)
	if err != nil {
		return nil, err
	}
	host := u.Hostname()
	portStr := u.Port()
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 {
		return nil, fmt.Errorf("invalid server port in %q", serverURL)
	}

	addr := net.JoinHostPort(host, portStr)
	if conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond); err == nil {
		_ = conn.Close()
		if logf != nil {
			logf("opencode server already running", "url", serverURL)
		}
		return nil, nil
	}

	cmd := exec.CommandContext(ctx, "opencode", "serve", "--port", strconv.Itoa(port))
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting opencode server on %s: %w", serverURL, err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
			_ = conn.Close()
			if logf != nil {
				logf("managed opencode server started", "url", serverURL, "pid", cmd.Process.Pid)
			}
			return cmd, nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()
	return nil, fmt.Errorf("managed opencode server did not become ready at %s", serverURL)
}
