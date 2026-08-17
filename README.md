# Aetherflow

Aetherflow is split into three Rust modules:

- `crates/aetherflow-storage`: durable `Channel`, `Agent`, and `Session` models.
- `crates/aetherflow-desktop`: the GPUI desktop application.
- `crates/aetherflow-pi`: Pi RPC JSONL transport, a Rivet `Session` actor,
  canonical typed stdout unions, and the `aetherflowd` and `af` binaries.

Install the daemon and CLI from this checkout with:

```sh
cargo install --path crates/aetherflow-pi --bins --force
```

Run the checks with `cargo test --workspace`. Probe Pi directly without its TUI
with `af pi state`.

The desktop app connects to an existing daemon or starts `aetherflowd` itself.
For development, build the daemon beside the desktop binary before launching:

```sh
cargo build -p aetherflow-pi --bin aetherflowd
cargo run -p aetherflow-desktop
```

An application bundle may place the daemon at
`Aetherflow.app/Contents/Helpers/aetherflowd` or beside the desktop executable.
Set `AETHERFLOWD_PATH` to use another binary explicitly.

Run the current session lifecycle acceptance test directly from the checkout:

```sh
scripts/smoke-session-lifecycle.sh
```

The smoke test builds the current binaries, starts an isolated daemon, creates
and lists a real headless Pi session, restarts the daemon, and verifies that the
same session is still listed and resumable. Pass a prompt to include a real
model turn:

```sh
scripts/smoke-session-lifecycle.sh "Reply with the word aubergine"
```

Start the Aetherflow daemon. It installs and starts the bundled Rivet Engine
automatically:

```sh
aetherflowd
```

Startup and shutdown events include the daemon version, process ID, Rivet
endpoint, namespace, pool, Engine source and path, actor types, outcome, and
duration. Set `RUST_LOG` to adjust verbosity, for example
`RUST_LOG=aetherflowd=debug,rivetkit=info aetherflowd`.

In another terminal, create and prompt a persistent session:

```sh
af session create "Hello"
af session list
af session prompt <SESSION_ID> "Hello"
af session state <SESSION_ID>
af session events <SESSION_ID>
```

The prompt on `session create` is optional. Creation prints only the new session
ID and lets the turn continue in the daemon. Pass `--attach` to print the same
unrendered Pi event stream as `session prompt`:

```sh
af session create "Hello" --attach
```

Every daemon-backed session event has a durable, monotonically increasing
`sequence`. Read a bounded snapshot, resume after the last sequence you saw, or
catch up and continue following the live stream:

```sh
af session events <SESSION_ID> --limit 100
af session events <SESSION_ID> --after 42
af session events <SESSION_ID> --after 42 --follow
```

`--after` is exclusive. While following, `--limit` is the catch-up page size;
without `--follow`, it is the maximum number of events returned.

Session actors persist their Pi JSONL under `~/.aetherflow/pi-sessions` by
default. Override it with `--session-dir` during creation or by setting
`AETHERFLOW_DATA_DIR`.

On macOS, GPUI's first build requires Xcode's Metal Toolchain. Install it with
`xcodebuild -downloadComponent MetalToolchain` if the `metal` compiler is absent.

The future local-to-hosted Session Promotion contract is documented in
`docs/session-promotion.md`. Current development remains focused on local Rivet
actors and filesystem persistence.
