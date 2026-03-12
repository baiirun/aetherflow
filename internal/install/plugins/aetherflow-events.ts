import type { Plugin } from "@opencode-ai/plugin"

// Aetherflow event pipeline plugin.
//
// Forwards all opencode server events to the aetherflow daemon via its
// HTTP API. Configured via environment variable set by the daemon
// on the opencode serve process:
//
//   AETHERFLOW_URL  - daemon HTTP URL (absent = plugin is inert)
//
// The plugin is a dumb pipe: it forwards every event without filtering.
// The daemon decides what to keep, index, and expose. Events are keyed
// by session ID — the daemon correlates sessions to agents internally.

// Send a fire-and-forget POST to the daemon's HTTP API.
// Does not wait for the response — we don't want to block agent execution
// if the daemon is slow or unreachable.
function sendEvent(daemonURL: string, params: unknown): void {
  try {
    const url = `${daemonURL}/api/v1/events`
    fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(params),
      signal: AbortSignal.timeout(5000),
    }).catch(() => {
      // Silently ignore — daemon may be restarting or unreachable.
      // Events are best-effort; the daemon can backfill from the REST API.
    })
  } catch {
    // Don't crash the agent if the request fails.
  }
}

// Extract session ID from event properties.
// Location varies by event type — see spike 1 findings in the plan.
function extractSessionID(properties: any): string | undefined {
  if (!properties) return undefined
  return (
    properties.sessionID ??
    properties.info?.id ??
    properties.info?.sessionID ??
    properties.part?.sessionID ??
    undefined
  )
}

export const AetherflowEvents: Plugin = async () => {
  const daemonURL = process.env.AETHERFLOW_URL

  // If not running under aetherflow, return no hooks — completely inert.
  if (!daemonURL) return {}

  return {
    event: async ({ event }) => {
      const sessionId = extractSessionID(event.properties)
      if (!sessionId) return // Skip events without a session ID

      sendEvent(daemonURL, {
        event_type: event.type,
        session_id: sessionId,
        timestamp: Date.now(),
        data: event.properties,
      })
    },
  }
}
