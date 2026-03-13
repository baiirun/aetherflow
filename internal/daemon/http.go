package daemon

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// newHTTPHandler builds the HTTP handler (mux) for the daemon API.
// Each route delegates to the existing handler methods which return *Response.
func (d *Daemon) newHTTPHandler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/events", d.routeEvents)
	mux.HandleFunc("/api/v1/lifecycle", d.methodHandler(http.MethodGet, d.httpLifecycle))
	mux.HandleFunc("/api/v1/status", d.methodHandler(http.MethodGet, d.httpStatusFull))
	mux.HandleFunc("/api/v1/status/agents/", d.methodHandler(http.MethodGet, d.httpStatusAgent))
	mux.HandleFunc("/api/v1/pool/drain", d.methodHandler(http.MethodPost, d.httpPoolDrain))
	mux.HandleFunc("/api/v1/pool/pause", d.methodHandler(http.MethodPost, d.httpPoolPause))
	mux.HandleFunc("/api/v1/pool/resume", d.methodHandler(http.MethodPost, d.httpPoolResume))
	mux.HandleFunc("/api/v1/spawns", d.methodHandler(http.MethodPost, d.httpSpawnRegister))
	mux.HandleFunc("/api/v1/spawns/", d.methodHandler(http.MethodDelete, d.httpSpawnDeregister))
	mux.HandleFunc("/api/v1/shutdown", d.methodHandler(http.MethodPost, d.httpShutdown))

	return hostCheckMiddleware(browserBoundaryMiddleware(authTokenMiddleware(d.authToken, mux)))
}

func (d *Daemon) routeEvents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		d.httpEventsList(w, r)
	case http.MethodPost:
		d.httpSessionEvent(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, &Response{
			Success: false,
			Error:   fmt.Sprintf("method %s not allowed", r.Method),
		})
	}
}

func (d *Daemon) methodHandler(method string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			writeJSON(w, http.StatusMethodNotAllowed, &Response{
				Success: false,
				Error:   fmt.Sprintf("method %s not allowed", r.Method),
			})
			return
		}
		next(w, r)
	}
}

// hostCheckMiddleware rejects requests whose Host header is not a loopback
// address, defeating DNS-rebinding attacks at near-zero cost.
// A cross-origin browser request sends its target domain as the Host header;
// by requiring the Host to be a loopback address, we ensure only code running
// on the same machine can reach the daemon API.
func hostCheckMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqHost, _, _ := net.SplitHostPort(r.Host)
		if reqHost == "" {
			reqHost = r.Host // Host header without port
		}
		switch reqHost {
		case "127.0.0.1", "::1", "localhost", "":
			next.ServeHTTP(w, r)
		default:
			writeJSON(w, http.StatusForbidden, &Response{
				Success: false,
				Error:   "forbidden: requests must originate from localhost",
			})
		}
	})
}

// browserBoundaryMiddleware rejects browser-originated requests to mutating
// endpoints. Local CLI, plugin, and app requests omit these headers.
func browserBoundaryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isMutatingMethod(r.Method) && hasBrowserRequestHeaders(r) {
			writeJSON(w, http.StatusForbidden, &Response{
				Success: false,
				Error:   "forbidden: browser-originated requests are not allowed",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func authTokenMiddleware(expectedToken string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if expectedToken == "" {
			writeJSON(w, http.StatusServiceUnavailable, &Response{
				Success: false,
				Error:   "daemon auth token is unavailable",
			})
			return
		}
		presented := r.Header.Get(daemonAuthHeader)
		if subtle.ConstantTimeCompare([]byte(presented), []byte(expectedToken)) != 1 {
			writeJSON(w, http.StatusUnauthorized, &Response{
				Success: false,
				Error:   "missing or invalid daemon auth token",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isMutatingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func hasBrowserRequestHeaders(r *http.Request) bool {
	if r.Header.Get("Origin") != "" || r.Header.Get("Referer") != "" {
		return true
	}
	site := strings.ToLower(r.Header.Get("Sec-Fetch-Site"))
	return site != "" && site != "none"
}

// writeJSON sends a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeResponse converts a legacy *Response to an HTTP response.
// Success → 200, failure → 400 (or caller overrides).
func writeResponse(w http.ResponseWriter, resp *Response) {
	if resp.Success {
		writeJSON(w, http.StatusOK, resp)
	} else {
		writeJSON(w, http.StatusBadRequest, resp)
	}
}

func (d *Daemon) httpSessionEvent(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 512<<10)
	var params SessionEventParams
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		writeJSON(w, http.StatusBadRequest, &Response{
			Success: false,
			Error:   fmt.Sprintf("invalid request body: %v", err),
		})
		return
	}
	writeResponse(w, d.handleSessionEvent(params))
}

func (d *Daemon) httpEventsList(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters into EventsListParams.
	params := EventsListParams{
		AgentName: r.URL.Query().Get("agent_name"),
	}
	if after := r.URL.Query().Get("after_timestamp"); after != "" {
		ts, err := strconv.ParseInt(after, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, &Response{Success: false, Error: "after_timestamp must be a valid int64"})
			return
		}
		params.AfterTimestamp = ts
	}
	if r.URL.Query().Get("raw") == "true" {
		params.Raw = true
	}
	writeResponse(w, d.handleEventsList(params))
}

func (d *Daemon) httpStatusFull(w http.ResponseWriter, r *http.Request) {
	writeResponse(w, d.handleStatusFull(r.Context()))
}

func (d *Daemon) httpStatusAgent(w http.ResponseWriter, r *http.Request) {
	agentID := strings.TrimPrefix(r.URL.Path, "/api/v1/status/agents/")
	agentID = strings.Trim(agentID, "/")
	if decoded, err := url.PathUnescape(agentID); err == nil {
		agentID = decoded
	}
	if agentID == "" {
		writeJSON(w, http.StatusBadRequest, &Response{
			Success: false,
			Error:   "agent id is required",
		})
		return
	}
	params := StatusAgentParams{AgentName: agentID}
	if limit := r.URL.Query().Get("limit"); limit != "" {
		l, err := strconv.Atoi(limit)
		if err != nil || l < 0 {
			writeJSON(w, http.StatusBadRequest, &Response{Success: false, Error: "limit must be a non-negative integer"})
			return
		}
		params.Limit = l
	}
	writeResponse(w, d.handleStatusAgent(r.Context(), params))
}

func (d *Daemon) httpPoolDrain(w http.ResponseWriter, r *http.Request) {
	writeResponse(w, d.handlePoolDrain())
}

func (d *Daemon) httpPoolPause(w http.ResponseWriter, r *http.Request) {
	writeResponse(w, d.handlePoolPause())
}

func (d *Daemon) httpPoolResume(w http.ResponseWriter, r *http.Request) {
	writeResponse(w, d.handlePoolResume())
}

func (d *Daemon) httpSpawnRegister(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 512<<10)
	var params SpawnRegisterParams
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		writeJSON(w, http.StatusBadRequest, &Response{
			Success: false,
			Error:   fmt.Sprintf("invalid request body: %v", err),
		})
		return
	}
	writeResponse(w, d.handleSpawnRegister(params))
}

func (d *Daemon) httpSpawnDeregister(w http.ResponseWriter, r *http.Request) {
	spawnID := strings.TrimPrefix(r.URL.Path, "/api/v1/spawns/")
	spawnID = strings.Trim(spawnID, "/")
	if decoded, err := url.PathUnescape(spawnID); err == nil {
		spawnID = decoded
	}
	if spawnID == "" {
		writeJSON(w, http.StatusBadRequest, &Response{
			Success: false,
			Error:   "spawn_id is required",
		})
		return
	}
	writeResponse(w, d.handleSpawnDeregister(SpawnDeregisterParams{SpawnID: spawnID}))
}

func (d *Daemon) httpShutdown(w http.ResponseWriter, r *http.Request) {
	force := r.URL.Query().Get("force") == "true"
	writeResponse(w, d.handleShutdown(force))
}

func (d *Daemon) httpLifecycle(w http.ResponseWriter, _ *http.Request) {
	status := d.lifecycleStatus()
	result, err := json.Marshal(status)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, &Response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, &Response{Success: true, Result: result})
}
