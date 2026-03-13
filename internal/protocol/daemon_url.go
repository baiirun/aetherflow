package protocol

import (
	"fmt"
	"net"
)

const (
	// DefaultDaemonPort is the default HTTP port for the daemon API.
	DefaultDaemonPort = 7070

	// DefaultDaemonURL is the fallback daemon URL when no project is known.
	DefaultDaemonURL = "http://127.0.0.1:7070"
)

// DaemonURLFor returns a project-scoped daemon URL.
// Each project gets a unique port offset from the default so daemons
// for different projects don't collide.
//
// Port allocation: hash the project name to a port in the range
// [7071, 7170] (100 slots). Collisions are possible but unlikely
// for typical usage (1-5 projects).
func DaemonURLFor(project string) string {
	if project == "" {
		return DefaultDaemonURL
	}
	port := DefaultDaemonPort + 1 + int(simpleHash(project)%100)
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

// NormalizeListenAddr canonicalizes daemon listen addresses into explicit
// host:port pairs suitable for binding and later publishing as client URLs.
// Empty hosts are normalized to 127.0.0.1 so ":7070" never escapes as
// "http://:7070".
func NormalizeListenAddr(listenAddr string) (string, error) {
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return "", err
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port), nil
}

// DaemonURLFromListenAddr converts a daemon listen address into the published
// loopback URL clients should use.
func DaemonURLFromListenAddr(listenAddr string) (string, error) {
	addr, err := NormalizeListenAddr(listenAddr)
	if err != nil {
		return "", err
	}
	return "http://" + addr, nil
}

// simpleHash is a basic FNV-1a-style hash for port allocation.
func simpleHash(s string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}
