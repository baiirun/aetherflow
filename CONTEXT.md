# Aetherflow

Aetherflow coordinates agents, their private sessions, and the channels in which
they may participate.

## Language

**Agent**:
A stable agent identity whose private model interactions are represented by
Sessions. One Agent may have many Sessions and participate in many Channels.
_Avoid_: Worker, runner, model

**Channel**:
A collaboration space with its own identity. Associating a Session with a
Channel does not make that Session public or establish Agent participation.
_Avoid_: Room, thread, project

**Directory**:
Exactly one filesystem root available as working context for Sessions.
_Avoid_: Project, repository

**Workspace**:
A named, non-empty collection of Directories used together as filesystem
context. A Workspace is not a collaboration space.
_Avoid_: Project, channel

**Session**:
A private, ongoing interaction between one Agent and its model runtime. A Session
belongs to one Workspace, selects one of its Directories as its working
Directory, and may independently be associated with a Channel.

**Session Association**:
Whether a Session is standalone or associated with exactly one Channel. The
association does not make the Session public or imply Agent membership.
_Avoid_: Channel context, channel scope

**Session Event**:
A durable observation in one Session's ordered history. Event order is scoped
to that Session rather than shared across all Sessions.
_Avoid_: Pi event, global event

**Attachment**:
Media content supplied with or emitted by a Session message. Messages refer to
the Attachment without making its bytes part of Session identity.
_Avoid_: Blob, upload

**Session Promotion**:
The transfer of authoritative ownership for a Session from local execution to
hosted execution while preserving its identity and durable history.
_Avoid_: Actor migration, synchronization
