# Aetherflow

Aetherflow is split into three Rust modules:

- `crates/aetherflow-storage`: durable `Channel`, `Agent`, and `Session` models.
- `crates/aetherflow-desktop`: the GPUI desktop application.
- `crates/aetherflow-pi`: Pi RPC JSONL transport, a plural daemon runtime registry,
  canonical typed stdout unions, and the `aetherflowd` and `af` binaries.

Run the checks with `cargo test --workspace`. Launch the desktop shell with
`cargo run -p aetherflow-desktop`. Probe a headless Pi session with
`cargo run -p aetherflow-pi --bin af -- state`.

On macOS, GPUI's first build requires Xcode's Metal Toolchain. Install it with
`xcodebuild -downloadComponent MetalToolchain` if the `metal` compiler is absent.
