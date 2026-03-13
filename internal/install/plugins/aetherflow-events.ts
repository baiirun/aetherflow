import type { Plugin } from "@opencode-ai/plugin"

// Aetherflow event pipeline plugin.
//
// Forwards all opencode server events to the aetherflow daemon via its
// HTTP API. Configured via environment variable set by the daemon
// on the opencode serve process:
//
//   AETHERFLOW_URL  - daemon HTTP URL (absent = plugin is inert)
//   AETHERFLOW_AUTH_TOKEN - daemon API token for local authorization
//
// The plugin is a dumb pipe: it forwards every event without filtering.
// The daemon decides what to keep, index, and expose. Events are keyed
// by session ID — the daemon correlates sessions to agents internally.

// Send a fire-and-forget POST to the daemon's HTTP API.
// Does not wait for the response — we don't want to block agent execution
// if the daemon is slow or unreachable.
function sendEvent(daemonURL: string, authToken: string | undefined, params: unknown): void {
  try {
    const url = `${daemonURL}/api/v1/events`
    const headers: Record<string, string> = { "Content-Type": "application/json" }
    if (authToken) {
      headers["X-Aetherflow-Token"] = authToken
    }
    fetch(url, {
      method: "POST",
      headers,
      body: JSON.stringify(params),
      signal: AbortSignal.timeout(5000),
    }).catch(() => {
      console.warn("[aetherflow-events] event delivery failed")
      // Daemon may be restarting or unreachable.
      // Events are best-effort; the daemon can backfill from the REST API.
    })
  } catch (error) {
    console.warn("[aetherflow-events] event delivery setup failed", error)
    // Don't crash the agent if the request fails.
  }
}

function isLoopbackDaemonURL(rawURL: string): boolean {
  try {
    const parsed = new URL(rawURL)
    if (parsed.protocol !== "http:") return false
    return parsed.hostname === "127.0.0.1" || parsed.hostname === "localhost" || parsed.hostname === "::1" || parsed.hostname === "[::1]"
  } catch {
    return false
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
  const authToken = process.env.AETHERFLOW_AUTH_TOKEN

  // If not running under aetherflow, return no hooks — completely inert.
  if (!daemonURL) return {}
  if (!isLoopbackDaemonURL(daemonURL)) {
    console.warn("[aetherflow-events] ignoring non-loopback AETHERFLOW_URL")
    return {}
  }

  return {
    event: async ({ event }) => {
      const sessionId = extractSessionID(event.properties)
      if (!sessionId) return // Skip events without a session ID

      sendEvent(daemonURL, authToken, {
        event_type: event.type,
        session_id: sessionId,
        timestamp: Date.now(),
        data: event.properties,
      })
    },
  }
}
