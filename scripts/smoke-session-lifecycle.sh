#!/bin/sh
set -eu

workspace_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
smoke_dir=$(mktemp -d)
smoke_key="aetherflow-smoke-$$-$(date +%s)"
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
RUSTUP_TOOLCHAIN=stable cargo build -q -p aetherflow-pi --bins

start_daemon() {
    RIVET_POOL_NAME="$smoke_key" target/debug/aetherflowd >"$smoke_dir/daemon.log" 2>&1 &
    daemon_pid=$!

    attempt=0
    until target/debug/af --pool "$smoke_key" --session-directory-key "$smoke_key" session list >/dev/null 2>&1; do
        attempt=$((attempt + 1))
        if [ "$attempt" -ge 120 ]; then
            echo "aetherflowd did not become ready; see $smoke_dir/daemon.log" >&2
            exit 1
        fi
        sleep 0.25
    done
}

af() {
    target/debug/af --pool "$smoke_key" --session-directory-key "$smoke_key" "$@"
}

start_daemon
if [ "$#" -gt 0 ]; then
    session_id=$(af session create "$1" --session-dir "$smoke_dir/pi-sessions")
else
    session_id=$(af session create --session-dir "$smoke_dir/pi-sessions")
fi
af session list | grep -F "\"id\": \"$session_id\"" >/dev/null

kill -TERM "$daemon_pid"
wait "$daemon_pid"
daemon_pid=""
start_daemon

af session list | grep -F "\"id\": \"$session_id\"" >/dev/null
af session state "$session_id" | grep -F "\"id\": \"$session_id\"" >/dev/null

echo "session lifecycle smoke test passed: $session_id"
