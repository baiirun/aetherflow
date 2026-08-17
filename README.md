# Aetherflow

Aetherflow is split into three Rust modules:

- `crates/aetherflow-storage`: durable `Channel`, `Agent`, and `Session` models.
- `crates/aetherflow-desktop`: the GPUI desktop application and its matching
  `aetherflowd` helper.
- `crates/aetherflow-pi`: Pi RPC JSONL transport, a Rivet `Session` actor,
  canonical typed stdout unions, and the `af` binary.

Install the desktop and its pinned daemon from this checkout with:

```sh
cargo install --path crates/aetherflow-desktop --force
```

Install the CLI separately when needed:

```sh
cargo install --path crates/aetherflow-pi --bin af --force
```

Run the checks with `cargo test --workspace`. Probe Pi directly without its TUI
with `af pi state`.

The desktop app connects to an existing compatible daemon or starts its pinned
`aetherflowd` itself. For development, build both binaries together before
launching:

```sh
cargo build -p aetherflow-desktop --bins
cargo run -p aetherflow-desktop
```

Build an Apple Silicon application bundle, including the daemon helper, with:

```sh
scripts/build-macos-app.sh
```

The bundle is written to `target/release/bundle/Aetherflow.app`. Install it in
`/Applications` with:

```sh
scripts/build-macos-app.sh --install
```

The local bundle is ad-hoc signed. It is suitable for development installs but
is not yet Developer ID signed or notarized for public distribution. When the
desktop app starts its daemon from Finder, it forwards the interactive login
shell's `PATH` so locally installed Pi and Node binaries remain discoverable.
Set `AETHERFLOWD_PATH` to use another daemon binary explicitly.

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
