# Vendored Rivet Engine

Aetherflow vendors the Rivet Engine as a runtime sidecar so local users only
need to start `aetherflowd`.

- Version: `2.3.10`
- Upstream: `https://github.com/rivet-dev/rivet/tree/v2.3.10`
- License: Apache-2.0; see `vendor/rivet-engine/LICENSE`

| Target | Artifact | SHA-256 |
| --- | --- | --- |
| `aarch64-apple-darwin` | `2.3.10/aarch64-apple-darwin/rivet-engine` | `6413c7278648cb69c989dca804ba135d9110ef3068e95170a10bf835cc13dc45` |

The daemon embeds the target-specific artifact, installs it into Aetherflow's
runtime directory atomically, and passes its path to RivetKit. Add and verify a
new artifact before enabling another build target.
