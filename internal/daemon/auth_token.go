package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const daemonAuthHeader = "X-Aetherflow-Token"

func daemonAuthTokenPath(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse daemon url: %w", err)
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		host = "127.0.0.1"
	}
	host = strings.NewReplacer(":", "_", "[", "", "]", "").Replace(host)
	port := parsed.Port()
	if port == "" {
		return "", fmt.Errorf("daemon url missing port")
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(configDir, "aetherflow", "auth", fmt.Sprintf("%s_%s.token", host, port)), nil
}

func ensureDaemonAuthToken(rawURL string) (string, error) {
	path, err := daemonAuthTokenPath(rawURL)
	if err != nil {
		return "", err
	}
	if data, err := os.ReadFile(path); err == nil {
		token := strings.TrimSpace(string(data))
		if token != "" {
			return token, nil
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("read auth token: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create auth dir: %w", err)
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate auth token: %w", err)
	}
	token := hex.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write auth token: %w", err)
	}
	return token, nil
}
