package daemon

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

const trustedAttachHostsEnv = "AETHERFLOW_TRUSTED_ATTACH_HOSTS"

// ValidateServerURLLocal validates Phase A server URLs.
// Phase A is local-managed only, so host must be localhost/127.0.0.1.
func ValidateServerURLLocal(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("server-url must not be empty")
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid server-url %q: %w", raw, err)
	}
	if u.Scheme != "http" {
		return nil, fmt.Errorf("server-url must use http scheme, got %q", u.Scheme)
	}
	host := u.Hostname()
	if host != "127.0.0.1" && host != "localhost" {
		return nil, fmt.Errorf("server-url host must be localhost or 127.0.0.1, got %q", host)
	}
	if port := u.Port(); port == "" {
		return nil, fmt.Errorf("server-url must include port")
	}
	if _, err := net.ResolveTCPAddr("tcp", u.Host); err != nil {
		return nil, fmt.Errorf("invalid server-url address %q: %w", u.Host, err)
	}
	if strings.ContainsAny(raw, "\n\r\t ") {
		return nil, fmt.Errorf("server-url must not contain whitespace")
	}
	return u, nil
}

// ValidateServerURLAttachTarget validates a server_ref used by af session attach.
// It accepts local HTTP targets (localhost/127.0.0.1) and remote HTTPS targets.
func ValidateServerURLAttachTarget(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("server-url must not be empty")
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid server-url %q: %w", raw, err)
	}
	host := u.Hostname()
	scheme := strings.ToLower(u.Scheme)

	if scheme == "http" {
		if host != "127.0.0.1" && host != "localhost" {
			return nil, fmt.Errorf("http server-url host must be localhost or 127.0.0.1, got %q", host)
		}
	} else if scheme != "https" {
		return nil, fmt.Errorf("server-url must use http (local) or https (remote), got %q", u.Scheme)
	} else if !isTrustedRemoteAttachHost(host) {
		return nil, fmt.Errorf("https server-url host %q is not trusted (set %s to allow)", host, trustedAttachHostsEnv)
	}

	if port := u.Port(); port == "" {
		if scheme != "https" || host == "" {
			return nil, fmt.Errorf("server-url must include port for local http targets")
		}
	}
	if strings.ContainsAny(raw, "\n\r\t ") {
		return nil, fmt.Errorf("server-url must not contain whitespace")
	}

	if scheme == "http" {
		if _, err := net.ResolveTCPAddr("tcp", u.Host); err != nil {
			return nil, fmt.Errorf("invalid server-url address %q: %w", u.Host, err)
		}
	}

	return u, nil
}

func isTrustedRemoteAttachHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return false
	}

	trusted := []string{".sprites.app", "sprites.app", "api.sprites.dev"}
	if raw := strings.TrimSpace(os.Getenv(trustedAttachHostsEnv)); raw != "" {
		for _, item := range strings.Split(raw, ",") {
			v := strings.ToLower(strings.TrimSpace(item))
			if v == "" {
				continue
			}
			trusted = append(trusted, v)
		}
	}

	for _, t := range trusted {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "*.") {
			t = strings.TrimPrefix(t, "*")
		}
		if strings.HasPrefix(t, ".") {
			if strings.HasSuffix(host, t) {
				return true
			}
			continue
		}
		if host == t {
			return true
		}
	}
	return false
}
