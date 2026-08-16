use crate::{JsonlReader, PiMessage, PiOptions, PiRpc, PiSessionStorage, RpcCommand};
use aetherflow_storage::{AgentId, Session, SessionAssociation, SessionId, SessionStatus};
use anyhow::{Context, Result, bail};
use async_trait::async_trait;
use rivetkit::{BindParam, ColumnValue, prelude::*};
use serde::{Deserialize, Serialize};
use std::{future::Future, pin::Pin, sync::Arc};
use tokio::{
    io::{AsyncWriteExt, BufReader},
    process::{Child, ChildStdin, ChildStdout},
    sync::{Mutex, mpsc},
};

pub const SESSION_ACTOR_NAME: &str = "session";
pub const DEFAULT_SESSION_EVENT_PAGE_SIZE: u32 = 100;
pub const MAX_SESSION_EVENT_PAGE_SIZE: u32 = 1_000;
const COMMAND_BUFFER_CAPACITY: usize = 32;

type BoxActionFuture<T> = Pin<Box<dyn Future<Output = Result<T>> + Send>>;

#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct SessionEvent {
    pub sequence: u64,
    pub session_id: SessionId,
    pub agent_id: AgentId,
    pub association: SessionAssociation,
    #[serde(flatten)]
    pub payload: SessionEventPayload,
}

#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(tag = "event", rename_all = "snake_case")]
pub enum SessionEventPayload {
    Pi { message: Box<PiMessage> },
    Stopped { error: Option<String> },
}

#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct SessionActorConfig {
    pub session: Session,
    pub pi: PiOptions,
}

impl SessionActorConfig {
    fn validate(&self) -> Result<()> {
        if !self.pi.cwd.is_absolute() {
            bail!("Pi working directory must be absolute");
        }

        match &self.pi.storage {
            PiSessionStorage::Ephemeral => {
                bail!("Rivet Session actors require persistent Pi storage")
            }
            PiSessionStorage::Persistent {
                directory,
                session_id,
            } => {
                if !directory.is_absolute() {
                    bail!("Pi session directory must be absolute");
                }
                if *session_id != self.session.id {
                    bail!(
                        "Pi session ID {session_id} does not match Aetherflow session ID {}",
                        self.session.id
                    );
                }
            }
        }

        Ok(())
    }
}

#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct SessionActorState {
    pub session: Session,
    pub pi: PiOptions,
}

#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct SendSessionCommand {
    pub command: RpcCommand,
}

impl Action for SendSessionCommand {
    type Output = ();

    const NAME: &'static str = "send_command";
}

#[derive(Clone, Copy, Debug, Deserialize, Serialize)]
pub struct GetSessionState;

impl Action for GetSessionState {
    type Output = SessionActorState;

    const NAME: &'static str = "get_state";
}

#[derive(Clone, Copy, Debug, Deserialize, Serialize)]
pub struct ReadSessionEvents {
    pub after_sequence: u64,
    pub limit: u32,
}

impl ReadSessionEvents {
    pub fn validate(&self) -> Result<()> {
        if self.limit == 0 {
            bail!("session event limit must be greater than zero");
        }
        if self.limit > MAX_SESSION_EVENT_PAGE_SIZE {
            bail!(
                "session event limit {} exceeds maximum {MAX_SESSION_EVENT_PAGE_SIZE}",
                self.limit
            );
        }
        Ok(())
    }
}

impl Action for ReadSessionEvents {
    type Output = Vec<SessionEvent>;

    const NAME: &'static str = "read_events";
}

impl Event for SessionEvent {
    const NAME: &'static str = "session_event";
}

struct SessionRuntime {
    pi: PiRpc,
    commands: mpsc::Receiver<RpcCommand>,
}

pub struct SessionActor {
    commands: mpsc::Sender<RpcCommand>,
    runtime: Mutex<Option<SessionRuntime>>,
}

#[async_trait]
impl Actor for SessionActor {
    type State = SessionActorState;
    type Input = Option<SessionActorConfig>;
    type Actions = (SendSessionCommand, GetSessionState, ReadSessionEvents);
    type Events = (SessionEvent,);
    type Queue = ();
    type ConnParams = ();
    type ConnState = ();
    type Action = rivetkit::action::Raw;

    const HAS_DATABASE: bool = true;

    async fn create_state(_ctx: &Ctx<Self>, input: Self::Input) -> Result<Self::State> {
        let config = input.context("Session actor creation input is required")?;
        config.validate()?;
        Ok(SessionActorState {
            session: config.session,
            pi: config.pi,
        })
    }

    async fn create(ctx: &Ctx<Self>) -> Result<Self> {
        SessionActorConfig {
            session: ctx.state().session.clone(),
            pi: ctx.state().pi.clone(),
        }
        .validate()?;
        ctx.sql()
            .execute(
                "CREATE TABLE IF NOT EXISTS session_events (\
                    sequence INTEGER PRIMARY KEY AUTOINCREMENT, \
                    payload TEXT NOT NULL\
                )",
                None,
            )
            .await?;
        let pi = PiRpc::spawn(ctx.state().pi.clone()).context("start persistent Pi session")?;
        let (commands, command_rx) = mpsc::channel(COMMAND_BUFFER_CAPACITY);
        ctx.state_mut().session.status = SessionStatus::Running;

        Ok(Self {
            commands,
            runtime: Mutex::new(Some(SessionRuntime {
                pi,
                commands: command_rx,
            })),
        })
    }

    async fn run(self: Arc<Self>, ctx: Ctx<Self>) -> Result<()> {
        let runtime = self
            .runtime
            .lock()
            .await
            .take()
            .context("Session actor runtime already started")?;
        let session = ctx.state().session.clone();
        let error = run_session_loop(runtime, &session, &ctx)
            .await
            .err()
            .map(|error| format!("{error:#}"));

        ctx.state_mut().session.status = SessionStatus::Stopped;
        append_session_event(
            &ctx,
            &session,
            SessionEventPayload::Stopped {
                error: error.clone(),
            },
        )
        .await?;

        if let Some(error) = error {
            bail!(error);
        }
        Ok(())
    }

    async fn on_sleep(self: Arc<Self>, ctx: Ctx<Self>) -> Result<()> {
        ctx.state_mut().session.status = SessionStatus::Idle;
        Ok(())
    }

    async fn on_destroy(self: Arc<Self>, ctx: Ctx<Self>) -> Result<()> {
        ctx.state_mut().session.status = SessionStatus::Stopped;
        Ok(())
    }
}

impl Handles<SendSessionCommand> for SessionActor {
    type Future = BoxActionFuture<()>;

    fn handle(self: Arc<Self>, _ctx: Ctx<Self>, action: SendSessionCommand) -> Self::Future {
        Box::pin(async move {
            self.commands
                .send(action.command)
                .await
                .context("Pi session is not accepting commands")
        })
    }
}

impl Handles<GetSessionState> for SessionActor {
    type Future = BoxActionFuture<SessionActorState>;

    fn handle(self: Arc<Self>, ctx: Ctx<Self>, _action: GetSessionState) -> Self::Future {
        Box::pin(async move { Ok(ctx.state().clone()) })
    }
}

impl Handles<ReadSessionEvents> for SessionActor {
    type Future = BoxActionFuture<Vec<SessionEvent>>;

    fn handle(self: Arc<Self>, ctx: Ctx<Self>, action: ReadSessionEvents) -> Self::Future {
        Box::pin(async move {
            action.validate()?;
            let after_sequence = i64::try_from(action.after_sequence)
                .context("session event cursor exceeds SQLite integer range")?;
            let result = ctx
                .sql()
                .query(
                    "SELECT sequence, payload FROM session_events \
                     WHERE sequence > ?1 ORDER BY sequence ASC LIMIT ?2",
                    Some(vec![
                        BindParam::Integer(after_sequence),
                        BindParam::Integer(i64::from(action.limit)),
                    ]),
                )
                .await?;

            result
                .rows
                .into_iter()
                .map(|row| decode_session_event_row(&ctx.state().session, row))
                .collect()
        })
    }
}

async fn run_session_loop(
    runtime: SessionRuntime,
    session: &Session,
    ctx: &Ctx<SessionActor>,
) -> Result<()> {
    let PiRpc {
        _child: child,
        stdin,
        stdout,
    } = runtime.pi;
    run_io_loop(child, stdin, stdout, runtime.commands, session, ctx).await
}

async fn run_io_loop(
    _child: Child,
    mut stdin: ChildStdin,
    mut stdout: JsonlReader<BufReader<ChildStdout>>,
    mut commands: mpsc::Receiver<RpcCommand>,
    session: &Session,
    ctx: &Ctx<SessionActor>,
) -> Result<()> {
    loop {
        tokio::select! {
            command = commands.recv() => {
                let command = command.context("Session actor command channel closed")?;
                let mut record = serde_json::to_vec(&command)?;
                record.push(b'\n');
                stdin.write_all(&record).await?;
                stdin.flush().await?;
            }
            message = stdout.next() => {
                let message: PiMessage = message?.context("Pi closed its RPC stream")?;
                append_session_event(
                    ctx,
                    session,
                    SessionEventPayload::Pi { message: Box::new(message) },
                )
                .await?;
            }
        }
    }
}

async fn append_session_event(
    ctx: &Ctx<SessionActor>,
    session: &Session,
    payload: SessionEventPayload,
) -> Result<SessionEvent> {
    let result = ctx
        .sql()
        .query(
            "INSERT INTO session_events (payload) VALUES (?1) RETURNING sequence",
            Some(vec![BindParam::Text(serde_json::to_string(&payload)?)]),
        )
        .await?;
    let sequence = result
        .rows
        .first()
        .and_then(|row| row.first())
        .context("session event insert did not return a sequence")?;
    let ColumnValue::Integer(sequence) = sequence else {
        bail!("session event insert returned a non-integer sequence");
    };
    let sequence = u64::try_from(*sequence).context("session event sequence was negative")?;
    let event = SessionEvent {
        sequence,
        session_id: session.id,
        agent_id: session.agent_id,
        association: session.association,
        payload,
    };
    ctx.emit(event.clone())?;
    Ok(event)
}

fn decode_session_event_row(session: &Session, row: Vec<ColumnValue>) -> Result<SessionEvent> {
    let [ColumnValue::Integer(sequence), ColumnValue::Text(payload)] = row.as_slice() else {
        bail!("session event row has an invalid shape");
    };
    Ok(SessionEvent {
        sequence: u64::try_from(*sequence).context("session event sequence was negative")?,
        session_id: session.id,
        agent_id: session.agent_id,
        association: session.association,
        payload: serde_json::from_str(payload)?,
    })
}

pub fn rivet_registry() -> Registry {
    let mut registry = Registry::new();
    registry.register_actor_with::<SessionActor>(
        SESSION_ACTOR_NAME,
        rivetkit::ActorConfig {
            has_state: true,
            has_database: true,
            ..rivetkit::ActorConfig::default()
        },
    );
    registry.register_actor_with::<crate::SessionDirectoryActor>(
        crate::SESSION_DIRECTORY_ACTOR_NAME,
        rivetkit::ActorConfig {
            has_database: true,
            ..rivetkit::ActorConfig::default()
        },
    );
    registry
}

#[cfg(test)]
mod tests {
    use super::*;
    use aetherflow_storage::Agent;
    use rivetkit::client::GetOrCreateOptions;
    use std::path::PathBuf;
    #[cfg(unix)]
    use std::{fs, os::unix::fs::PermissionsExt, time::Duration};

    #[test]
    fn config_rejects_a_different_pi_session_identity() {
        let agent = Agent::new("test");
        let session = Session::new(agent.id, SessionAssociation::Standalone);
        let config = SessionActorConfig {
            session: session.clone(),
            pi: PiOptions::persistent(
                PathBuf::from("/worktree"),
                PathBuf::from("/sessions"),
                aetherflow_storage::SessionId::new(),
            ),
        };

        let error = config.validate().unwrap_err();

        assert!(error.to_string().contains("does not match"));
    }

    #[test]
    fn config_requires_persistent_pi_storage() {
        let agent = Agent::new("test");
        let session = Session::new(agent.id, SessionAssociation::Standalone);
        let config = SessionActorConfig {
            session,
            pi: PiOptions::ephemeral("/worktree"),
        };

        let error = config.validate().unwrap_err();

        assert!(error.to_string().contains("require persistent"));
    }

    #[test]
    fn event_page_limit_is_bounded() {
        assert!(
            ReadSessionEvents {
                after_sequence: 0,
                limit: 1,
            }
            .validate()
            .is_ok()
        );
        assert!(
            ReadSessionEvents {
                after_sequence: 0,
                limit: 0,
            }
            .validate()
            .is_err()
        );
        assert!(
            ReadSessionEvents {
                after_sequence: 0,
                limit: MAX_SESSION_EVENT_PAGE_SIZE + 1,
            }
            .validate()
            .is_err()
        );
    }

    #[test]
    fn persisted_event_rows_recover_their_sequence() -> Result<()> {
        let agent = Agent::new("test");
        let session = Session::new(agent.id, SessionAssociation::Standalone);
        let payload = SessionEventPayload::Stopped { error: None };

        let event = decode_session_event_row(
            &session,
            vec![
                ColumnValue::Integer(42),
                ColumnValue::Text(serde_json::to_string(&payload)?),
            ],
        )?;

        assert_eq!(event.sequence, 42);
        assert_eq!(event.session_id, session.id);
        assert!(matches!(
            event.payload,
            SessionEventPayload::Stopped { error: None }
        ));
        Ok(())
    }

    #[cfg(unix)]
    #[tokio::test]
    #[ignore = "requires a local Rivet Engine"]
    async fn actor_routes_commands_and_events_through_rivet() -> Result<()> {
        let temp = tempfile::tempdir()?;
        let executable = temp.path().join("fake-pi");
        fs::write(
            &executable,
            r#"#!/bin/sh
session_dir=""
session_id=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --session-dir) session_dir="$2"; shift 2 ;;
    --session-id) session_id="$2"; shift 2 ;;
    *) shift ;;
  esac
done
mkdir -p "$session_dir"
record="$session_dir/$session_id.jsonl"
while IFS= read -r line; do
  printf '%s\n' "$line" >> "$record"
  printf '%s\n' '{"type":"agent_start"}'
  printf '%s\n' '{"type":"agent_end","messages":[],"willRetry":false}'
done
"#,
        )?;
        fs::set_permissions(&executable, fs::Permissions::from_mode(0o755))?;

        let agent = Agent::new("test");
        let session = Session::new(agent.id, SessionAssociation::Standalone);
        let session_id = session.id;
        let mut pi = PiOptions::persistent(temp.path(), temp.path().join("sessions"), session.id);
        pi.executable = executable;
        let config = SessionActorConfig { session, pi };
        let test = rivetkit::test::setup(rivet_registry()).await?;
        let actor = test.actor_with_options::<SessionActor>(
            SESSION_ACTOR_NAME,
            [session_id.to_string()],
            GetOrCreateOptions {
                create_with_input: Some(serde_json::to_value(config)?),
                ..GetOrCreateOptions::default()
            },
        );
        let connection = actor.connect();
        let (events, mut event_rx) = mpsc::unbounded_channel();
        let _subscription = connection
            .on::<SessionEvent>(move |event| {
                let _ = events.send(event);
            })
            .await;

        connection
            .send(SendSessionCommand {
                command: RpcCommand::prompt("test", "hello"),
            })
            .await?;

        let end = tokio::time::timeout(Duration::from_secs(10), async {
            loop {
                let event = event_rx
                    .recv()
                    .await
                    .context("session event stream closed")?;
                if matches!(
                    event.payload,
                    SessionEventPayload::Pi { ref message }
                        if matches!(message.event(), crate::PiEvent::AgentEnd(_))
                ) {
                    return Ok::<_, anyhow::Error>(event);
                }
            }
        })
        .await??;

        assert_eq!(end.session_id, session_id);
        assert!(end.sequence > 0);
        assert_eq!(actor.send(GetSessionState).await?.session.id, session_id);
        let all_events = actor
            .send(ReadSessionEvents {
                after_sequence: 0,
                limit: DEFAULT_SESSION_EVENT_PAGE_SIZE,
            })
            .await?;
        assert_eq!(all_events.len(), 2);
        assert_eq!(all_events[0].sequence + 1, all_events[1].sequence);
        let after_first = actor
            .send(ReadSessionEvents {
                after_sequence: all_events[0].sequence,
                limit: DEFAULT_SESSION_EVENT_PAGE_SIZE,
            })
            .await?;
        assert_eq!(after_first.len(), 1);
        assert_eq!(after_first[0].sequence, all_events[1].sequence);
        let persisted = fs::read_to_string(
            temp.path()
                .join("sessions")
                .join(format!("{session_id}.jsonl")),
        )?;
        assert!(persisted.contains("hello"));
        _subscription.unsubscribe().await;
        connection.disconnect().await;
        tokio::time::sleep(Duration::from_millis(100)).await;
        test.shutdown().await;
        Ok(())
    }
}
