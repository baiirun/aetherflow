package daemon

import (
	"encoding/json"
	"fmt"
)

// PoolModeResult is the response for pool control RPC methods.
type PoolModeResult struct {
	Mode    PoolMode `json:"mode"`
	Running int      `json:"running"`
}

// poolModeResponse builds a response with the current pool mode and running count.
// Callers must ensure d.pool is non-nil before calling.
func (d *Daemon) poolModeResponse() *Response {
	result, err := json.Marshal(PoolModeResult{
		Mode:    d.pool.Mode(),
		Running: len(d.pool.Status()),
	})
	if err != nil {
		return &Response{Success: false, Error: fmt.Sprintf("marshal pool mode: %v", err)}
	}
	return &Response{Success: true, Result: result}
}

// handlePoolDrain transitions the pool to draining mode.
// New tasks from the queue are not scheduled, but current agents
// run to completion and crash respawns are still allowed.
func (d *Daemon) handlePoolDrain() *Response {
	if d.pool == nil {
		return &Response{Success: false, Error: "no pool configured"}
	}
	d.pool.Drain()
	return d.poolModeResponse()
}

// handlePoolPause transitions the pool to paused mode.
// No new scheduling and no crash respawns.
func (d *Daemon) handlePoolPause() *Response {
	if d.pool == nil {
		return &Response{Success: false, Error: "no pool configured"}
	}
	d.pool.Pause()
	return d.poolModeResponse()
}

// handlePoolResume transitions the pool back to active mode.
func (d *Daemon) handlePoolResume() *Response {
	if d.pool == nil {
		return &Response{Success: false, Error: "no pool configured"}
	}
	d.pool.Resume()
	return d.poolModeResponse()
}
