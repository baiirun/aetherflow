package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// newHTTPHandler builds the HTTP handler (mux) for the daemon API.
// Each route delegates to the existing handler methods which return *Response.
func (d *Daemon) newHTTPHandler() http.Handler {
	mux := http.NewServeMux()

	// POST /api/v1/events — plugin event ingestion (was session.event)
	mux.HandleFunc("POST /api/v1/events", d.httpSessionEvent)

	// GET /api/v1/events — list events for an agent (was events.list)
	mux.HandleFunc("GET /api/v1/events", d.httpEventsList)

	// GET /api/v1/lifecycle — daemon lifecycle state
	mux.HandleFunc("GET /api/v1/lifecycle", d.httpLifecycle)

	// GET /api/v1/status — full swarm status (was status.full)
	mux.HandleFunc("GET /api/v1/status", d.httpStatusFull)

	// GET /api/v1/status/agents/{id} — single agent detail (was status.agent)
	mux.HandleFunc("GET /api/v1/status/agents/{id}", d.httpStatusAgent)

	// POST /api/v1/pool/drain — drain the pool (was pool.drain)
	mux.HandleFunc("POST /api/v1/pool/drain", d.httpPoolDrain)

	// POST /api/v1/pool/pause — pause the pool (was pool.pause)
	mux.HandleFunc("POST /api/v1/pool/pause", d.httpPoolPause)

	// POST /api/v1/pool/resume — resume the pool (was pool.resume)
	mux.HandleFunc("POST /api/v1/pool/resume", d.httpPoolResume)

	// POST /api/v1/spawns — register a spawn (was spawn.register)
	mux.HandleFunc("POST /api/v1/spawns", d.httpSpawnRegister)

	// DELETE /api/v1/spawns/{id} — deregister a spawn (was spawn.deregister)
	mux.HandleFunc("DELETE /api/v1/spawns/{id}", d.httpSpawnDeregister)

	// POST /api/v1/shutdown — shut down the daemon (was shutdown)
	mux.HandleFunc("POST /api/v1/shutdown", d.httpShutdown)

	return mux
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
	var params SessionEventParams
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		writeJSON(w, http.StatusBadRequest, &Response{
			Success: false,
			Error:   fmt.Sprintf("invalid request body: %v", err),
		})
		return
	}
	raw, _ := json.Marshal(params)
	writeResponse(w, d.handleSessionEvent(raw))
}

func (d *Daemon) httpEventsList(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters into EventsListParams.
	params := EventsListParams{
		AgentName: r.URL.Query().Get("agent_name"),
	}
	if after := r.URL.Query().Get("after_timestamp"); after != "" {
		var ts int64
		if _, err := fmt.Sscanf(after, "%d", &ts); err == nil {
			params.AfterTimestamp = ts
		}
	}
	if r.URL.Query().Get("raw") == "true" {
		params.Raw = true
	}
	raw, _ := json.Marshal(params)
	writeResponse(w, d.handleEventsList(raw))
}

func (d *Daemon) httpStatusFull(w http.ResponseWriter, r *http.Request) {
	writeResponse(w, d.handleStatusFull(r.Context()))
}

func (d *Daemon) httpStatusAgent(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	if agentID == "" {
		writeJSON(w, http.StatusBadRequest, &Response{
			Success: false,
			Error:   "agent id is required",
		})
		return
	}
	params := StatusAgentParams{AgentName: agentID}
	if limit := r.URL.Query().Get("limit"); limit != "" {
		var l int
		if _, err := fmt.Sscanf(limit, "%d", &l); err == nil {
			params.Limit = l
		}
	}
	raw, _ := json.Marshal(params)
	writeResponse(w, d.handleStatusAgent(r.Context(), raw))
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
	var params SpawnRegisterParams
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		writeJSON(w, http.StatusBadRequest, &Response{
			Success: false,
			Error:   fmt.Sprintf("invalid request body: %v", err),
		})
		return
	}
	raw, _ := json.Marshal(params)
	writeResponse(w, d.handleSpawnRegister(raw))
}

func (d *Daemon) httpSpawnDeregister(w http.ResponseWriter, r *http.Request) {
	spawnID := r.PathValue("id")
	if spawnID == "" {
		writeJSON(w, http.StatusBadRequest, &Response{
			Success: false,
			Error:   "spawn_id is required",
		})
		return
	}
	params := SpawnDeregisterParams{SpawnID: spawnID}
	raw, _ := json.Marshal(params)
	writeResponse(w, d.handleSpawnDeregister(raw))
}

func (d *Daemon) httpShutdown(w http.ResponseWriter, r *http.Request) {
	writeResponse(w, d.handleShutdown())
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

// daemonURLToListenAddr extracts the host:port from a daemon URL string.
// For example, "http://127.0.0.1:7070" returns "127.0.0.1:7070".
// This is exported for use by tests that need to construct listener addresses.
func daemonURLToListenAddr(daemonURL string) string {
	// Strip scheme prefix for simpler parsing.
	addr := daemonURL
	addr = strings.TrimPrefix(addr, "http://")
	addr = strings.TrimPrefix(addr, "https://")
	// Remove any trailing path.
	if i := strings.Index(addr, "/"); i != -1 {
		addr = addr[:i]
	}
	return addr
}
