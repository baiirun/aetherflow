package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/baiirun/aetherflow/internal/sessions"
)

// backfillEvents populates the in-memory event buffer from the opencode
// REST API for sessions that existed before the daemon started. This runs
// once on daemon startup after the managed server is ready.
//
// The approach:
//  1. Read the session registry for sessions with known session IDs.
//  2. For each session, fetch messages from GET /session/:id/message.
//  3. Convert each message part into a SessionEvent (same shape as plugin events).
//  4. Push events into the buffer so af status / af logs work immediately.
//
// This is best-effort: failures are logged but don't prevent the daemon
// from starting. The plugin will deliver future events regardless.
func backfillEvents(ctx context.Context, api *opencodeClient, store *sessions.Store, events *EventBuffer, log *slog.Logger) {
	if store == nil || api == nil || events == nil {
		return
	}

	records, err := store.List()
	if err != nil {
		log.Warn("backfill: failed to list sessions", "error", err)
		return
	}

	if len(records) == 0 {
		return
	}

	var backfilled, skipped, errored int
	for _, rec := range records {
		if rec.SessionID == "" {
			continue
		}
		// Only backfill sessions that are still relevant (active or idle).
		// Terminated/stale sessions aren't useful for observability.
		if rec.Status != sessions.StatusActive && rec.Status != sessions.StatusIdle {
			skipped++
			continue
		}
		// Skip sessions that already have events (plugin delivered them).
		if events.Len(rec.SessionID) > 0 {
			skipped++
			continue
		}

		n, err := backfillSession(ctx, api, events, rec.SessionID)
		if err != nil {
			log.Warn("backfill: failed to fetch session",
				"session_id", rec.SessionID,
				"error", err,
			)
			errored++
			continue
		}

		if n > 0 {
			backfilled++
			log.Debug("backfill: populated session",
				"session_id", rec.SessionID,
				"events", n,
			)
		}
	}

	if backfilled > 0 || errored > 0 {
		log.Info("backfill complete",
			"sessions_backfilled", backfilled,
			"sessions_skipped", skipped,
			"sessions_errored", errored,
		)
	}
}

// backfillSession fetches messages for a single session from the REST API
// and pushes them into the event buffer as synthetic SessionEvents.
// Returns the number of events pushed.
func backfillSession(ctx context.Context, api *opencodeClient, events *EventBuffer, sessionID string) (int, error) {
	messages, err := api.fetchSessionMessages(ctx, sessionID)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, msg := range messages {
		for _, rawPart := range msg.Parts {
			ev, err := partToEvent(sessionID, rawPart)
			if err != nil {
				continue // skip malformed parts
			}
			events.Push(ev)
			count++
		}
	}
	return count, nil
}

// partToEvent converts a raw part JSON object from the REST API into a
// SessionEvent that matches the shape delivered by the plugin.
//
// The plugin sends: {event_type: "message.part.updated", data: {"part": <part>}}
// The REST API returns parts directly. We wrap each part in the {"part": ...}
// envelope to match the plugin format so ToolCallsFromEvents works unchanged.
func partToEvent(sessionID string, rawPart json.RawMessage) (SessionEvent, error) {
	// Extract the part's time_created for the event timestamp.
	// Parts have a time field at various locations depending on type.
	// For tool parts: state.time.start. For text: no time.
	// Fall back to extracting timeCreated from the DB-level wrapper if present,
	// otherwise use zero (backfilled events are ordered by insertion).
	ts := extractPartTimestamp(rawPart)

	// Wrap in {"part": <raw>} envelope to match plugin event shape.
	data, err := json.Marshal(struct {
		Part json.RawMessage `json:"part"`
	}{Part: rawPart})
	if err != nil {
		return SessionEvent{}, err
	}

	return SessionEvent{
		EventType: "message.part.updated",
		SessionID: sessionID,
		Timestamp: ts,
		Data:      data,
	}, nil
}

// extractPartTimestamp extracts a timestamp from a part's JSON payload.
// Tries state.time.start (tool parts), then time.start (reasoning parts),
// then falls back to 0 (text parts have no time). Returns Unix millis.
func extractPartTimestamp(raw json.RawMessage) int64 {
	var probe struct {
		State struct {
			Time struct {
				Start int64 `json:"start"`
			} `json:"time"`
		} `json:"state"`
		Time struct {
			Start int64 `json:"start"`
		} `json:"time"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return 0
	}
	if probe.State.Time.Start > 0 {
		return probe.State.Time.Start
	}
	if probe.Time.Start > 0 {
		return probe.Time.Start
	}
	return 0
}

// backfillTimeout is the maximum time spent on the entire backfill operation.
// Individual session fetches may be faster. This bounds total startup delay.
const backfillTimeout = 30 * time.Second
