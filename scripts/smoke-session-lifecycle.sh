#!/bin/sh
set -eu

workspace_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
smoke_dir=$(mktemp -d)
smoke_key="aetherflow-smoke-$$-$(date +%s)"
attachment_port=$((20000 + ($$ % 20000)))
attachment_address="127.0.0.1:$attachment_port"
attachment_endpoint="http://$attachment_address"
daemon_pid=""

cleanup() {
    if [ -n "$daemon_pid" ]; then
        kill -TERM "$daemon_pid" 2>/dev/null || true
        wait "$daemon_pid" 2>/dev/null || true
    fi
    case "$smoke_dir" in
        /tmp/*|/var/folders/*) rm -rf -- "$smoke_dir" ;;
    esac
}
trap cleanup EXIT INT TERM

cd "$workspace_dir"
RUSTUP_TOOLCHAIN=stable cargo build -q \
    -p aetherflow-pi --bin af \
    -p aetherflow-desktop --bin aetherflowd

start_daemon() {
    RIVET_POOL_NAME="$smoke_key" \
        AETHERFLOW_ATTACHMENT_ADDRESS="$attachment_address" \
        target/debug/aetherflowd >"$smoke_dir/daemon.log" 2>&1 &
    daemon_pid=$!

    attempt=0
    until target/debug/af --pool "$smoke_key" --session-directory-key "$smoke_key" session list >/dev/null 2>&1; do
        attempt=$((attempt + 1))
        if ! kill -0 "$daemon_pid" 2>/dev/null; then
            wait "$daemon_pid" 2>/dev/null || true
            echo "aetherflowd exited before becoming ready:" >&2
            cat "$smoke_dir/daemon.log" >&2
            exit 1
        fi
        if [ "$attempt" -ge 120 ]; then
            echo "aetherflowd did not become ready:" >&2
            cat "$smoke_dir/daemon.log" >&2
            exit 1
        fi
        sleep 0.25
    done
}

af() {
    target/debug/af --pool "$smoke_key" --attachment-endpoint "$attachment_endpoint" \
        --session-directory-key "$smoke_key" \
        --workspace-catalog-key "$smoke_key" "$@"
}

start_daemon
secondary_directory="$smoke_dir/secondary-directory"
mkdir -p "$secondary_directory"
secondary_directory=$(CDPATH= cd -- "$secondary_directory" && pwd -P)
workspace_json=$(af workspace create \
    --name "Smoke workspace" \
    --directory "$workspace_dir" \
    --directory "$secondary_directory")
workspace_ids=$(printf '%s\n' "$workspace_json" \
    | sed -n 's/^[[:space:]]*"id": "\([^"]*\)".*/\1/p')
workspace_id=$(printf '%s\n' "$workspace_ids" | sed -n '1p')
secondary_directory_id=$(printf '%s\n' "$workspace_ids" | sed -n '3p')
if [ -z "$workspace_id" ] || [ -z "$secondary_directory_id" ]; then
    echo "multi-directory workspace creation did not return expected ids" >&2
    exit 1
fi
if [ "$#" -gt 0 ]; then
    session_id=$(af session create "$1" --workspace "$workspace_id" \
        --directory "$secondary_directory_id" --session-dir "$smoke_dir/pi-sessions")
else
    session_id=$(af session create --workspace "$workspace_id" \
        --directory "$secondary_directory_id" --session-dir "$smoke_dir/pi-sessions")
fi
af session list | grep -F "\"id\": \"$session_id\"" >/dev/null

kill -TERM "$daemon_pid"
wait "$daemon_pid"
daemon_pid=""
start_daemon

af session list | grep -F "\"id\": \"$session_id\"" >/dev/null
af workspace list | grep -F "\"id\": \"$workspace_id\"" >/dev/null
session_state=$(af session state "$session_id")
printf '%s\n' "$session_state" | grep -F "\"id\": \"$session_id\"" >/dev/null
printf '%s\n' "$session_state" | grep -F "\"workspace_id\": \"$workspace_id\"" >/dev/null
printf '%s\n' "$session_state" | grep -F "\"working_directory_id\": \"$secondary_directory_id\"" >/dev/null
printf '%s\n' "$session_state" | grep -F "\"cwd\": \"$secondary_directory\"" >/dev/null

echo "session lifecycle smoke test passed: $session_id"
