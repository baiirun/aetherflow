# Ship the daemon with the desktop

Superseded in part by
[ADR 0007](0007-replace-incompatible-local-daemons.md): protocol or actor-build
mismatches are now replaced automatically rather than only rejected.

Cargo does not install dependency-package binaries, so a desktop package that
only depended on `aetherflow-pi` could run against an independently installed,
stale `aetherflowd`. The desktop package now owns both executable targets and the
macOS bundle carries the daemon as a signed helper, while the daemon
implementation remains behind the `aetherflow-pi` module interface. A
compatible daemon may remain alive across desktop exits so detached work
continues. Compatibility requires both the public daemon protocol and the actor
build fingerprint to match; package version text alone is not authoritative.
