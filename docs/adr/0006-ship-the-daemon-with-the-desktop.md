# Ship the daemon with the desktop

Superseded in part by
[ADR 0007](0007-replace-incompatible-local-daemons.md): protocol mismatches
are now replaced automatically rather than only rejected.

Cargo does not install dependency-package binaries, so a desktop package that
only depended on `aetherflow-pi` could run against an independently installed,
stale `aetherflowd`. The desktop package now owns both executable targets and the
macOS bundle carries the daemon as a signed helper, while the daemon
implementation remains behind the `aetherflow-pi` module interface. A
protocol-compatible daemon may remain alive across desktop exits and package
upgrades so detached work continues; package version alone does not force a
restart, while a protocol mismatch is rejected.
