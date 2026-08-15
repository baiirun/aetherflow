use crate::{JsonlReader, PiMessage, PiOptions, PiRpc, RpcCommand};
use aetherflow_storage::{Agent, AgentId, Channel, ChannelId, Session, SessionId, SessionStatus};
use serde::{Deserialize, Serialize};
use std::{
    collections::HashMap,
    sync::{Arc, RwLock, Weak},
};
use thiserror::Error;
use tokio::{
    io::{AsyncWriteExt, BufReader},
    process::{Child, ChildStdin, ChildStdout},
    sync::{broadcast, mpsc},
};

const EVENT_BUFFER_CAPACITY: usize = 256;
const COMMAND_BUFFER_CAPACITY: usize = 32;

#[derive(Debug, Error)]
pub enum RegistryError {
    #[error("agent {0} already exists")]
    DuplicateAgent(AgentId),
    #[error("channel {0} already exists")]
    DuplicateChannel(ChannelId),
    #[error("session {0} already exists")]
    DuplicateSession(SessionId),
    #[error("agent {0} does not exist")]
    UnknownAgent(AgentId),
    #[error("channel {0} does not exist")]
    UnknownChannel(ChannelId),
    #[error("session {0} does not exist")]
    UnknownSession(SessionId),
    #[error("session {0} is not accepting commands")]
    SessionUnavailable(SessionId),
    #[error("failed to start Pi for session {session_id}: {source}")]
    StartSession {
        session_id: SessionId,
        #[source]
        source: anyhow::Error,
    },
}

#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct SessionEvent {
    pub session_id: SessionId,
    pub agent_id: AgentId,
    pub channel_id: Option<ChannelId>,
    #[serde(flatten)]
    pub payload: SessionEventPayload,
}

#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(tag = "event", rename_all = "snake_case")]
pub enum SessionEventPayload {
    Pi { message: PiMessage },
    Stopped { error: Option<String> },
}

struct SessionEntry {
    session: Session,
    commands: mpsc::Sender<RpcCommand>,
}

#[derive(Default)]
struct RegistryState {
    agents: HashMap<AgentId, Agent>,
    channels: HashMap<ChannelId, Channel>,
    sessions: HashMap<SessionId, SessionEntry>,
}

/// Owns the durable identities known to the daemon and the runtime handle for
/// every active session. No agent, channel, or session is globally current.
#[derive(Clone)]
pub struct DaemonRegistry {
    state: Arc<RwLock<RegistryState>>,
    events: broadcast::Sender<SessionEvent>,
}

impl Default for DaemonRegistry {
    fn default() -> Self {
        Self::new()
    }
}

impl DaemonRegistry {
    pub fn new() -> Self {
        let (events, _) = broadcast::channel(EVENT_BUFFER_CAPACITY);
        Self {
            state: Arc::new(RwLock::new(RegistryState::default())),
            events,
        }
    }

    pub fn add_agent(&self, agent: Agent) -> Result<(), RegistryError> {
        let mut state = self.state.write().expect("daemon registry lock poisoned");
        if state.agents.contains_key(&agent.id) {
            return Err(RegistryError::DuplicateAgent(agent.id));
        }
        state.agents.insert(agent.id, agent);
        Ok(())
    }

    pub fn add_channel(&self, channel: Channel) -> Result<(), RegistryError> {
        let mut state = self.state.write().expect("daemon registry lock poisoned");
        if state.channels.contains_key(&channel.id) {
            return Err(RegistryError::DuplicateChannel(channel.id));
        }
        state.channels.insert(channel.id, channel);
        Ok(())
    }

    pub fn start_session(
        &self,
        mut session: Session,
        options: PiOptions,
    ) -> Result<(), RegistryError> {
        {
            let state = self.state.read().expect("daemon registry lock poisoned");
            self.validate_session(&state, &session)?;
        }

        let pi = PiRpc::spawn(options).map_err(|source| RegistryError::StartSession {
            session_id: session.id,
            source,
        })?;
        let (commands, command_rx) = mpsc::channel(COMMAND_BUFFER_CAPACITY);
        session.status = SessionStatus::Running;

        {
            let mut state = self.state.write().expect("daemon registry lock poisoned");
            self.validate_session(&state, &session)?;
            state.sessions.insert(
                session.id,
                SessionEntry {
                    session: session.clone(),
                    commands,
                },
            );
        }

        tokio::spawn(run_session(
            session,
            pi,
            command_rx,
            self.events.clone(),
            Arc::downgrade(&self.state),
        ));
        Ok(())
    }

    pub async fn send(
        &self,
        session_id: SessionId,
        command: RpcCommand,
    ) -> Result<(), RegistryError> {
        let commands = {
            let state = self.state.read().expect("daemon registry lock poisoned");
            state
                .sessions
                .get(&session_id)
                .ok_or(RegistryError::UnknownSession(session_id))?
                .commands
                .clone()
        };

        commands
            .send(command)
            .await
            .map_err(|_| RegistryError::SessionUnavailable(session_id))
    }

    pub fn subscribe(&self) -> broadcast::Receiver<SessionEvent> {
        self.events.subscribe()
    }

    pub fn agents(&self) -> Vec<Agent> {
        self.state
            .read()
            .expect("daemon registry lock poisoned")
            .agents
            .values()
            .cloned()
            .collect()
    }

    pub fn channels(&self) -> Vec<Channel> {
        self.state
            .read()
            .expect("daemon registry lock poisoned")
            .channels
            .values()
            .cloned()
            .collect()
    }

    pub fn sessions(&self) -> Vec<Session> {
        self.state
            .read()
            .expect("daemon registry lock poisoned")
            .sessions
            .values()
            .map(|entry| entry.session.clone())
            .collect()
    }

    fn validate_session(
        &self,
        state: &RegistryState,
        session: &Session,
    ) -> Result<(), RegistryError> {
        if state.sessions.contains_key(&session.id) {
            return Err(RegistryError::DuplicateSession(session.id));
        }
        if !state.agents.contains_key(&session.agent_id) {
            return Err(RegistryError::UnknownAgent(session.agent_id));
        }
        if let Some(channel_id) = session.channel_id
            && !state.channels.contains_key(&channel_id)
        {
            return Err(RegistryError::UnknownChannel(channel_id));
        }
        Ok(())
    }
}

async fn run_session(
    session: Session,
    pi: PiRpc,
    mut commands: mpsc::Receiver<RpcCommand>,
    events: broadcast::Sender<SessionEvent>,
    state: Weak<RwLock<RegistryState>>,
) {
    let PiRpc {
        _child: child,
        stdin,
        stdout,
    } = pi;
    let error = run_session_loop(child, stdin, stdout, &mut commands, &session, &events)
        .await
        .err()
        .map(|error| error.to_string());

    if let Some(state) = state.upgrade()
        && let Some(entry) = state
            .write()
            .expect("daemon registry lock poisoned")
            .sessions
            .get_mut(&session.id)
    {
        entry.session.status = SessionStatus::Stopped;
    }

    let _ = events.send(SessionEvent {
        session_id: session.id,
        agent_id: session.agent_id,
        channel_id: session.channel_id,
        payload: SessionEventPayload::Stopped { error },
    });
}

async fn run_session_loop(
    _child: Child,
    mut stdin: ChildStdin,
    mut stdout: JsonlReader<BufReader<ChildStdout>>,
    commands: &mut mpsc::Receiver<RpcCommand>,
    session: &Session,
    events: &broadcast::Sender<SessionEvent>,
) -> anyhow::Result<()> {
    loop {
        tokio::select! {
            command = commands.recv() => {
                let Some(command) = command else { return Ok(()) };
                let mut record = serde_json::to_vec(&command)?;
                record.push(b'\n');
                stdin.write_all(&record).await?;
                stdin.flush().await?;
            }
            message = stdout.next() => {
                let Some(message) = message? else { return Ok(()) };
                let _ = events.send(SessionEvent {
                    session_id: session.id,
                    agent_id: session.agent_id,
                    channel_id: session.channel_id,
                    payload: SessionEventPayload::Pi { message },
                });
            }
        }
    }
}

#[cfg(all(test, unix))]
mod tests {
    use super::*;
    use std::{collections::HashMap, fs, os::unix::fs::PermissionsExt, path::Path};
    use tempfile::TempDir;
    use tokio::time::{Duration, timeout};

    #[test]
    fn session_requires_registered_agent_and_channel() {
        let registry = DaemonRegistry::new();
        let agent = Agent::new("unregistered");
        let channel = Channel::new("unregistered");
        let session = Session::new(agent.id, Some(channel.id));

        let error = registry
            .start_session(session.clone(), PiOptions::default())
            .unwrap_err();

        assert!(matches!(error, RegistryError::UnknownAgent(id) if id == agent.id));

        registry.add_agent(agent).unwrap();
        let error = registry
            .start_session(session, PiOptions::default())
            .unwrap_err();

        assert!(matches!(error, RegistryError::UnknownChannel(id) if id == channel.id));
    }

    #[tokio::test]
    async fn events_from_two_processes_keep_their_session_identity() {
        let fake_pi = FakePi::new();
        let registry = DaemonRegistry::new();
        let first_channel = Channel::new("first-channel");
        let second_channel = Channel::new("second-channel");
        let first_agent = Agent::new("first");
        let second_agent = Agent::new("second");
        let first_session = Session::new(first_agent.id, Some(first_channel.id));
        let second_session = Session::new(second_agent.id, Some(second_channel.id));

        registry.add_channel(first_channel.clone()).unwrap();
        registry.add_channel(second_channel.clone()).unwrap();
        registry.add_agent(first_agent.clone()).unwrap();
        registry.add_agent(second_agent.clone()).unwrap();
        let mut events = registry.subscribe();
        registry
            .start_session(first_session.clone(), fake_pi.options())
            .unwrap();
        registry
            .start_session(second_session.clone(), fake_pi.options())
            .unwrap();

        let (first_send, second_send) = tokio::join!(
            registry.send(first_session.id, RpcCommand::get_state("first")),
            registry.send(second_session.id, RpcCommand::get_state("second")),
        );
        first_send.unwrap();
        second_send.unwrap();

        let mut received = HashMap::new();
        while received.len() < 2 {
            let event = timeout(Duration::from_secs(2), events.recv())
                .await
                .expect("timed out waiting for fake Pi")
                .unwrap();
            if matches!(event.payload, SessionEventPayload::Pi { .. }) {
                received.insert(event.session_id, (event.agent_id, event.channel_id));
            }
        }

        assert_eq!(
            received[&first_session.id],
            (first_agent.id, Some(first_channel.id))
        );
        assert_eq!(
            received[&second_session.id],
            (second_agent.id, Some(second_channel.id))
        );
        assert_eq!(registry.agents().len(), 2);
        assert_eq!(registry.channels().len(), 2);
        assert_eq!(registry.sessions().len(), 2);
    }

    struct FakePi {
        _directory: TempDir,
        executable: std::path::PathBuf,
    }

    impl FakePi {
        fn new() -> Self {
            let directory = tempfile::tempdir().unwrap();
            let executable = directory.path().join("pi");
            write_fake_pi(&executable);
            Self {
                _directory: directory,
                executable,
            }
        }

        fn options(&self) -> PiOptions {
            PiOptions {
                executable: self.executable.clone(),
                no_session: true,
            }
        }
    }

    fn write_fake_pi(path: &Path) {
        fs::write(
            path,
            "#!/bin/sh\nwhile IFS= read -r record; do\n  printf '{\"type\":\"response\",\"command\":\"get_state\",\"success\":true,\"data\":{\"thinkingLevel\":\"off\",\"isStreaming\":false,\"isCompacting\":false,\"steeringMode\":\"all\",\"followUpMode\":\"all\",\"sessionId\":\"fake\",\"autoCompactionEnabled\":true,\"messageCount\":0,\"pendingMessageCount\":0}}\\n'\ndone\n",
        )
        .unwrap();
        let mut permissions = fs::metadata(path).unwrap().permissions();
        permissions.set_mode(0o755);
        fs::set_permissions(path, permissions).unwrap();
    }
}
