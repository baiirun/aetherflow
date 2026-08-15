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

Run the checks with `cargo test --workspace`. Launch the desktop shell with
`cargo run -p aetherflow-desktop`. Probe Pi directly without its TUI with
`af pi state`.

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
af session create
af session list
af session prompt <SESSION_ID> "Hello"
af session state <SESSION_ID>
```

Session actors persist their Pi JSONL under `~/.aetherflow/pi-sessions` by
default. Override it with `--session-dir` during creation or by setting
`AETHERFLOW_DATA_DIR`.

On macOS, GPUI's first build requires Xcode's Metal Toolchain. Install it with
`xcodebuild -downloadComponent MetalToolchain` if the `metal` compiler is absent.

The future local-to-hosted Session Promotion contract is documented in
`docs/session-promotion.md`. Current development remains focused on local Rivet
actors and filesystem persistence.
