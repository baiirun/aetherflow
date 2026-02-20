package daemon

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

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
