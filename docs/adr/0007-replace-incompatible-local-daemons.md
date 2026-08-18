# Replace incompatible local daemons

The desktop keeps a compatible `aetherflowd` alive after its last window closes
so detached turns and CLI clients can continue. Reinstalling or rebuilding the
daemon replaces its executable file but cannot update an already-running
process. Reusing that process after the actor protocol changes can route new
actions to an old runner and produce opaque HTTP 400 responses.

The desktop therefore treats legacy health responses and explicit protocol
mismatches as incompatible. Before launching its bundled or sibling helper, it
identifies the `aetherflowd` process listening on the configured loopback
attachment endpoint, sends it `TERM`, waits for the endpoint to be released,
and then waits for a newly registered Rivet runner. It refuses to terminate a
process that is not named `aetherflowd` or an endpoint that is not loopback.

Normal desktop exit still leaves a compatible daemon running. Package-version
differences alone do not cause replacement when the daemon protocol matches.
This keeps detached execution while making protocol upgrades self-healing.

This decision supersedes the protocol-mismatch behavior in
[ADR 0006](0006-ship-the-daemon-with-the-desktop.md).
