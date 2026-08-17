# Separate Session discovery from Session runtime

Session listing and sidebar metadata live in one Session Directory actor rather
than being derived by waking or scanning every Session actor. The Directory
stores only identity, title, archive state, and activity time, while each Session
actor retains its own runtime state and history. This makes many-Session listing
cheap and independent of Pi lifecycles, at the cost of a cross-actor registration
step that is not transactionally atomic with Session creation.
