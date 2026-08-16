use crate::{
    GetSessionState, PiEvent, PiOptions, ReadSessionEvents, RpcCommand, SESSION_ACTOR_NAME,
    SESSION_DIRECTORY_ACTOR_NAME, SendSessionCommand, SessionActor, SessionActorConfig,
    SessionActorState, SessionDescriptor, SessionDirectoryActor, SessionEvent, SessionEventPayload,
    session_directory::{DEFAULT_SESSION_DIRECTORY_KEY, ListSessions, RegisterSession},
};
use aetherflow_storage::{Agent, AgentId, Session, SessionAssociation, SessionId};
use anyhow::{Context, Result, bail};
use rivetkit::{
    TypedActorConnection, TypedClientExt,
    client::{Client, ClientConfig, GetOrCreateOptions, connection::SubscriptionHandle},
};
use std::{
    collections::VecDeque,
    path::{Path, PathBuf},
};
use tokio::sync::mpsc;

pub const DEFAULT_ENDPOINT: &str = "http://127.0.0.1:6420";
pub const DEFAULT_TOKEN: &str = "dev";
pub const DEFAULT_NAMESPACE: &str = "default";
pub const DEFAULT_POOL: &str = "rivetkit-rust";

#[derive(Clone, Debug)]
pub struct AetherflowClientOptions {
    pub endpoint: String,
    pub token: String,
    pub namespace: String,
    pub pool: String,
    pub session_directory_key: String,
}

impl Default for AetherflowClientOptions {
    fn default() -> Self {
        Self {
            endpoint: DEFAULT_ENDPOINT.to_owned(),
            token: DEFAULT_TOKEN.to_owned(),
            namespace: DEFAULT_NAMESPACE.to_owned(),
            pool: DEFAULT_POOL.to_owned(),
            session_directory_key: DEFAULT_SESSION_DIRECTORY_KEY.to_owned(),
        }
    }
}

#[derive(Clone, Debug)]
pub struct CreateSessionOptions {
    pub agent_id: Option<AgentId>,
    pub association: SessionAssociation,
    pub cwd: PathBuf,
    pub pi_session_directory: Option<PathBuf>,
    pub pi_executable: PathBuf,
    pub initial_prompt: Option<String>,
}

impl Default for CreateSessionOptions {
    fn default() -> Self {
        Self {
            agent_id: None,
            association: SessionAssociation::Standalone,
            cwd: PathBuf::from("."),
            pi_session_directory: None,
            pi_executable: PathBuf::from("pi"),
            initial_prompt: None,
        }
    }
}

#[derive(Clone)]
pub struct AetherflowClient {
    client: Client,
    session_directory_key: String,
}

impl AetherflowClient {
    pub fn connect(options: AetherflowClientOptions) -> Self {
        Self {
            client: Client::new(
                ClientConfig::new(options.endpoint)
                    .token(options.token)
                    .namespace(options.namespace)
                    .pool_name(options.pool),
            ),
            session_directory_key: options.session_directory_key,
        }
    }

    pub fn from_rivet_client(client: Client) -> Self {
        Self {
            client,
            session_directory_key: DEFAULT_SESSION_DIRECTORY_KEY.to_owned(),
        }
    }

    pub async fn create_session(&self, options: CreateSessionOptions) -> Result<SessionId> {
        let CreateSessionOptions {
            agent_id,
            association,
            cwd,
            pi_session_directory,
            pi_executable,
            initial_prompt,
        } = options;
        let cwd = absolute_existing_directory(&cwd)?;
        let session_directory =
            absolute_path(pi_session_directory.unwrap_or(default_pi_session_directory()?))?;
        let executable = resolve_executable(pi_executable)?;
        let agent_id = agent_id.unwrap_or_else(|| Agent::new("local").id);
        let session = Session::new(agent_id, association);
        let mut pi = PiOptions::persistent(cwd, session_directory, session.id);
        pi.executable = executable;

        self.client
            .get_or_create_typed::<SessionActor>(
                SESSION_ACTOR_NAME,
                [session.id.to_string()],
                GetOrCreateOptions {
                    create_with_input: Some(serde_json::to_value(SessionActorConfig {
                        session: session.clone(),
                        pi,
                    })?),
                    ..GetOrCreateOptions::default()
                },
            )?
            .send(GetSessionState)
            .await
            .context("create session actor")?;

        self.session_directory()
            .send(RegisterSession {
                session: SessionDescriptor::from(&session),
            })
            .await
            .context("register session")?;

        if let Some(prompt) = initial_prompt {
            self.session_handle(session.id)
                .send(SendSessionCommand {
                    command: RpcCommand::prompt("prompt", prompt),
                })
                .await
                .with_context(|| format!("prompt new session {}", session.id))?;
        }

        Ok(session.id)
    }

    pub async fn list_sessions(&self) -> Result<Vec<SessionDescriptor>> {
        self.session_directory()
            .send(ListSessions)
            .await
            .context("list sessions")
    }

    pub async fn session_state(&self, session_id: SessionId) -> Result<SessionActorState> {
        self.session_handle(session_id)
            .send(GetSessionState)
            .await
            .with_context(|| format!("read session {session_id}"))
    }

    pub async fn session_events(
        &self,
        session_id: SessionId,
        after_sequence: u64,
        limit: u32,
    ) -> Result<Vec<SessionEvent>> {
        self.session_handle(session_id)
            .send(ReadSessionEvents {
                after_sequence,
                limit,
            })
            .await
            .with_context(|| format!("read events for session {session_id}"))
    }

    pub async fn follow_session_events(
        &self,
        session_id: SessionId,
        after_sequence: u64,
        limit: u32,
    ) -> Result<SessionEventSubscription> {
        let connection = self.session_handle(session_id).connect();
        let (events, event_rx) = mpsc::unbounded_channel();
        let subscription = connection
            .on::<SessionEvent>(move |event| {
                let _ = events.send(event);
            })
            .await;
        let mut replay = Vec::new();
        let mut replay_after = after_sequence;
        loop {
            let page = connection
                .send(ReadSessionEvents {
                    after_sequence: replay_after,
                    limit,
                })
                .await
                .with_context(|| format!("read events for session {session_id}"))?;
            let page_len = page.len();
            if let Some(event) = page.last() {
                replay_after = event.sequence;
            }
            replay.extend(page);
            if page_len < limit as usize {
                break;
            }
        }

        Ok(SessionEventSubscription {
            session_id,
            connection,
            subscription,
            replay: replay.into(),
            live: event_rx,
            last_sequence: after_sequence,
        })
    }

    pub async fn prompt_session(
        &self,
        session_id: SessionId,
        message: impl Into<String>,
    ) -> Result<SessionEventStream> {
        let connection = self.session_handle(session_id).connect();
        let (events, event_rx) = mpsc::unbounded_channel();
        let subscription = connection
            .on::<SessionEvent>(move |event| {
                let _ = events.send(event);
            })
            .await;

        connection
            .send(SendSessionCommand {
                command: RpcCommand::prompt("prompt", message),
            })
            .await
            .with_context(|| format!("prompt session {session_id}"))?;

        Ok(SessionEventStream {
            session_id,
            connection,
            subscription,
            events: event_rx,
            finished: false,
        })
    }

    fn session_directory(&self) -> rivetkit::TypedActorHandle<SessionDirectoryActor> {
        self.client
            .get_or_create_typed_default::<SessionDirectoryActor>(
                SESSION_DIRECTORY_ACTOR_NAME,
                [self.session_directory_key.clone()],
            )
            .expect("session directory actor key is valid")
    }

    fn session_handle(&self, session_id: SessionId) -> rivetkit::TypedActorHandle<SessionActor> {
        self.client
            .get_typed_default::<SessionActor>(SESSION_ACTOR_NAME, [session_id.to_string()])
            .expect("session actor key is valid")
    }
}

pub struct SessionEventStream {
    session_id: SessionId,
    connection: TypedActorConnection<SessionActor>,
    subscription: SubscriptionHandle,
    events: mpsc::UnboundedReceiver<SessionEvent>,
    finished: bool,
}

pub struct SessionEventSubscription {
    session_id: SessionId,
    connection: TypedActorConnection<SessionActor>,
    subscription: SubscriptionHandle,
    replay: VecDeque<SessionEvent>,
    live: mpsc::UnboundedReceiver<SessionEvent>,
    last_sequence: u64,
}

impl SessionEventSubscription {
    pub async fn next(&mut self) -> Result<Option<SessionEvent>> {
        if let Some(event) = self.replay.pop_front() {
            self.last_sequence = event.sequence;
            return Ok(Some(event));
        }

        while let Some(event) = self.live.recv().await {
            if event.sequence <= self.last_sequence {
                continue;
            }
            self.last_sequence = event.sequence;
            return Ok(Some(event));
        }

        bail!(
            "session {} event subscription closed after sequence {}",
            self.session_id,
            self.last_sequence
        )
    }

    pub async fn close(self) {
        self.subscription.unsubscribe().await;
        self.connection.disconnect().await;
    }
}

impl SessionEventStream {
    pub async fn next(&mut self) -> Result<Option<SessionEvent>> {
        if self.finished {
            return Ok(None);
        }

        let event = self.events.recv().await.with_context(|| {
            format!(
                "session {} event stream closed before the prompt completed",
                self.session_id
            )
        })?;
        if is_terminal_event(&event) {
            self.finished = true;
            self.subscription.unsubscribe().await;
            self.connection.disconnect().await;
        }
        Ok(Some(event))
    }
}

fn is_terminal_event(event: &SessionEvent) -> bool {
    match &event.payload {
        SessionEventPayload::Pi { message } => matches!(message.event(), PiEvent::AgentEnd(_)),
        SessionEventPayload::Stopped { .. } => true,
    }
}

fn default_pi_session_directory() -> Result<PathBuf> {
    if let Some(path) = std::env::var_os("AETHERFLOW_DATA_DIR") {
        return Ok(PathBuf::from(path).join("pi-sessions"));
    }
    let home = std::env::var_os("HOME")
        .context("HOME is not set; pass --session-dir or set AETHERFLOW_DATA_DIR")?;
    Ok(PathBuf::from(home).join(".aetherflow/pi-sessions"))
}

fn absolute_existing_directory(path: &Path) -> Result<PathBuf> {
    let path = absolute_path(path)?;
    let metadata = path
        .metadata()
        .with_context(|| format!("read working directory {}", path.display()))?;
    if !metadata.is_dir() {
        bail!("working directory {} is not a directory", path.display());
    }
    path.canonicalize()
        .with_context(|| format!("resolve working directory {}", path.display()))
}

fn resolve_executable(path: PathBuf) -> Result<PathBuf> {
    let has_directory = path
        .parent()
        .is_some_and(|parent| !parent.as_os_str().is_empty());
    if !has_directory {
        return Ok(path);
    }

    let path = absolute_path(path)?;
    let metadata = path
        .metadata()
        .with_context(|| format!("read Pi executable {}", path.display()))?;
    if !metadata.is_file() {
        bail!("Pi executable {} is not a file", path.display());
    }
    path.canonicalize()
        .with_context(|| format!("resolve Pi executable {}", path.display()))
}

fn absolute_path(path: impl AsRef<Path>) -> Result<PathBuf> {
    let path = path.as_ref();
    if path.is_absolute() {
        return Ok(path.to_owned());
    }
    Ok(std::env::current_dir()
        .context("read current working directory")?
        .join(path))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::rivet_registry;
    use rivetkit::ServeConfig;
    use std::time::{Duration, SystemTime, UNIX_EPOCH};
    #[cfg(unix)]
    use std::{fs, os::unix::fs::PermissionsExt};
    use tempfile::TempDir;
    use tokio::task::JoinHandle;
    use tokio_util::sync::CancellationToken;

    #[cfg(unix)]
    #[tokio::test]
    #[ignore = "requires a local Rivet Engine"]
    async fn client_creates_lists_prompts_and_resumes_a_session() -> Result<()> {
        let temp = TempDir::new()?;
        let executable = fake_pi(&temp)?;
        let directory_key = format!(
            "test-{}",
            SystemTime::now().duration_since(UNIX_EPOCH)?.as_nanos()
        );
        let pool = format!("aetherflow-restart-test-{directory_key}");
        let (runner, client) = TestRunner::start(&pool, &directory_key).await?;

        let first = client
            .create_session(CreateSessionOptions {
                cwd: temp.path().to_owned(),
                pi_session_directory: Some(temp.path().join("sessions")),
                pi_executable: executable.clone(),
                initial_prompt: Some("during creation".to_owned()),
                ..CreateSessionOptions::default()
            })
            .await?;
        let second = client
            .create_session(CreateSessionOptions {
                cwd: temp.path().to_owned(),
                pi_session_directory: Some(temp.path().join("sessions")),
                pi_executable: executable,
                ..CreateSessionOptions::default()
            })
            .await?;

        wait_for_persisted_prompt(&temp.path().join("sessions"), first, "during creation").await?;

        assert_eq!(
            client
                .list_sessions()
                .await?
                .iter()
                .map(|session| session.id)
                .collect::<Vec<_>>(),
            [first, second]
        );
        runner.crash().await;

        let (restarted, client) = TestRunner::start(&pool, &directory_key).await?;
        assert_eq!(client.list_sessions().await?.len(), 2);
        assert_eq!(client.session_state(first).await?.session.id, first);
        prompt_to_completion(&client, first, "after restart").await?;

        let persisted =
            fs::read_to_string(temp.path().join("sessions").join(format!("{first}.jsonl")))?;
        assert!(persisted.contains("during creation"));
        assert!(persisted.contains("after restart"));
        restarted.shutdown().await?;
        Ok(())
    }

    struct TestRunner {
        shutdown: CancellationToken,
        serve: JoinHandle<Result<()>>,
    }

    impl TestRunner {
        async fn start(pool: &str, directory_key: &str) -> Result<(Self, AetherflowClient)> {
            let shutdown = CancellationToken::new();
            let config = ServeConfig {
                version: 1,
                endpoint: DEFAULT_ENDPOINT.to_owned(),
                token: Some(DEFAULT_TOKEN.to_owned()),
                namespace: DEFAULT_NAMESPACE.to_owned(),
                pool_name: pool.to_owned(),
                ..ServeConfig::default()
            };
            let serve = tokio::spawn({
                let shutdown = shutdown.clone();
                async move { rivet_registry().serve_with_config(config, shutdown).await }
            });
            let client = AetherflowClient::connect(AetherflowClientOptions {
                pool: pool.to_owned(),
                session_directory_key: directory_key.to_owned(),
                ..AetherflowClientOptions::default()
            });

            let deadline = tokio::time::Instant::now() + Duration::from_secs(30);
            loop {
                if client.list_sessions().await.is_ok() {
                    break;
                }
                if serve.is_finished() {
                    bail!("test runner stopped during startup");
                }
                if tokio::time::Instant::now() >= deadline {
                    bail!("test runner did not become ready within 30 seconds");
                }
                tokio::time::sleep(Duration::from_millis(250)).await;
            }

            Ok((Self { shutdown, serve }, client))
        }

        async fn shutdown(self) -> Result<()> {
            self.shutdown.cancel();
            self.serve.await.context("join test runner")?
        }

        async fn crash(self) {
            self.serve.abort();
            let _ = self.serve.await;
        }
    }

    async fn prompt_to_completion(
        client: &AetherflowClient,
        session_id: SessionId,
        message: &str,
    ) -> Result<()> {
        let events = client.prompt_session(session_id, message).await?;
        stream_to_completion(events).await
    }

    async fn stream_to_completion(mut events: SessionEventStream) -> Result<()> {
        tokio::time::timeout(Duration::from_secs(10), async {
            while events.next().await?.is_some() {}
            Ok::<_, anyhow::Error>(())
        })
        .await??;
        Ok(())
    }

    #[cfg(unix)]
    async fn wait_for_persisted_prompt(
        directory: &Path,
        session_id: SessionId,
        prompt: &str,
    ) -> Result<()> {
        let record = directory.join(format!("{session_id}.jsonl"));
        tokio::time::timeout(Duration::from_secs(10), async {
            loop {
                if fs::read_to_string(&record).is_ok_and(|contents| contents.contains(prompt)) {
                    return;
                }
                tokio::time::sleep(Duration::from_millis(25)).await;
            }
        })
        .await
        .with_context(|| format!("prompt was not persisted to {}", record.display()))?;
        Ok(())
    }

    #[cfg(unix)]
    fn fake_pi(temp: &TempDir) -> Result<PathBuf> {
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
        Ok(executable)
    }
}
