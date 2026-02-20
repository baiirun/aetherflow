import type { Plugin } from "@opencode-ai/plugin"

// Aetherflow event pipeline plugin.
//
// Forwards all opencode server events to the aetherflow daemon via its
// Unix socket RPC. Configured via environment variable set by the daemon
// on the opencode serve process:
//
//   AETHERFLOW_SOCKET  - daemon Unix socket path (absent = plugin is inert)
//
// The plugin is a dumb pipe: it forwards every event without filtering.
// The daemon decides what to keep, index, and expose. Events are keyed
// by session ID — the daemon correlates sessions to agents internally.

// Send a fire-and-forget RPC call to the daemon's Unix socket.
// Does not wait for the response — we don't want to block agent execution
// if the daemon is slow or unreachable.
function sendEvent(socketPath: string, method: string, params: unknown): void {
  try {
    const net = require("net")
    const socket = net.createConnection(socketPath, () => {
      socket.write(JSON.stringify({ method, params }) + "\n")
      // Don't call socket.end() immediately — let the server read first.
      // Set a short timeout to auto-close after the write drains.
      setTimeout(() => socket.destroy(), 500)
    })
    socket.on("error", () => {
      // Silently ignore — daemon may be restarting or unreachable.
      // Events are best-effort; the daemon can backfill from the REST API.
    })
  } catch {
    // Don't crash the agent if socket operations fail.
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
  const socketPath = process.env.AETHERFLOW_SOCKET

  // If not running under aetherflow, return no hooks — completely inert.
  if (!socketPath) return {}

  return {
    event: async ({ event }) => {
      const sessionId = extractSessionID(event.properties)
      if (!sessionId) return // Skip events without a session ID

      sendEvent(socketPath, "session.event", {
        event_type: event.type,
        session_id: sessionId,
        timestamp: Date.now(),
        data: event.properties,
      })
    },
  }
}
