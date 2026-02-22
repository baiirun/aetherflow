package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
)

const (
	// maxRequestBodyBytes limits POST request bodies to prevent memory
	// exhaustion from oversized payloads. Generous for the expected
	// envelope sizes; the event data limit (256KB) is checked separately.
	maxRequestBodyBytes = 1 << 20 // 1 MiB
)

// newHTTPHandler builds the HTTP handler (mux) for the daemon API.
// Each route delegates to the existing handler methods which return *Response.
func (d *Daemon) newHTTPHandler() http.Handler {
	mux := http.NewServeMux()

	// POST /api/v1/events — plugin event ingestion (was session.event)
	mux.HandleFunc("POST /api/v1/events", d.httpSessionEvent)

	// GET /api/v1/events — list events for an agent (was events.list)
	mux.HandleFunc("GET /api/v1/events", d.httpEventsList)

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
// Success → 200, failure → 422 (Unprocessable Entity).
// 422 is used over 400 because the request was syntactically valid but the
// server couldn't process it (e.g., unknown agent, invalid state). This
// distinguishes business logic errors from malformed request errors (400).
func writeResponse(w http.ResponseWriter, resp *Response) {
	if resp.Success {
		writeJSON(w, http.StatusOK, resp)
	} else {
		writeJSON(w, http.StatusUnprocessableEntity, resp)
	}
}

func (d *Daemon) httpSessionEvent(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
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
		var ts int64
		if _, err := fmt.Sscanf(after, "%d", &ts); err == nil {
			params.AfterTimestamp = ts
		}
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
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
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
	spawnID := r.PathValue("id")
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
	writeResponse(w, d.handleShutdown())
}
