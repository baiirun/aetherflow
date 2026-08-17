# Keep Pi as a headless subprocess

Aetherflow delegates the agent loop, model/tool execution, and continuation
format to canonical Pi rather than porting that behavior into Rust. Each active
Session actor owns a `pi --mode rpc` subprocess and communicates through strict
JSONL over stdio; Aetherflow owns orchestration, durable observation, and its
domain model around that seam. This keeps Aetherflow aligned with Pi behavior
without duplicating its rapidly changing runtime, at the cost of requiring a Pi
executable and maintaining a typed, forward-compatible RPC adapter.
