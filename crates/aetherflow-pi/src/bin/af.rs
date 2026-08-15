use aetherflow_pi::{
    GetSessionState, PiEvent, PiMessage, PiOptions, PiRpc, RpcCommand, SESSION_ACTOR_NAME,
    SendSessionCommand, SessionActor, SessionActorConfig, SessionEvent, SessionEventPayload,
};
use aetherflow_storage::{Agent, AgentId, ChannelId, Session, SessionAssociation, SessionId};
use anyhow::{Context, Result, bail};
use clap::{Args as ClapArgs, Parser, Subcommand};
use rivetkit::{
    TypedClientExt,
    client::{Client, ClientConfig, GetOrCreateOptions},
};
use std::path::{Path, PathBuf};
use tokio::sync::mpsc;

const DEFAULT_ENDPOINT: &str = "http://127.0.0.1:6420";
const DEFAULT_TOKEN: &str = "dev";
const DEFAULT_NAMESPACE: &str = "default";
const DEFAULT_POOL: &str = "rivetkit-rust";

#[derive(Parser)]
#[command(name = "af")]
struct Args {
    /// Rivet Engine endpoint used by daemon-backed session commands.
    #[arg(long, default_value = DEFAULT_ENDPOINT)]
    endpoint: String,
    #[arg(long, default_value = DEFAULT_TOKEN)]
    token: String,
    #[arg(long, default_value = DEFAULT_NAMESPACE)]
    namespace: String,
    #[arg(long, default_value = DEFAULT_POOL)]
    pool: String,
    #[command(subcommand)]
    command: Command,
}

#[derive(Subcommand)]
enum Command {
    /// Directly probe an ephemeral, headless Pi process without the daemon.
    Pi {
        #[arg(long, default_value = "pi")]
        executable: PathBuf,
        #[command(subcommand)]
        command: PiCommand,
    },
    /// Create and interact with durable daemon-backed Pi sessions.
    Session {
        #[command(subcommand)]
        command: SessionCommand,
    },
}

#[derive(Subcommand)]
enum PiCommand {
    State,
    Prompt { message: String },
}

#[derive(Subcommand)]
enum SessionCommand {
    /// Create a durable session actor and its persistent Pi session.
    Create(CreateSessionArgs),
    /// Read the durable state for a session actor.
    State { session_id: SessionId },
    /// Prompt a session and print its unrendered Pi event stream.
    Prompt {
        session_id: SessionId,
        message: String,
    },
}

#[derive(ClapArgs)]
struct CreateSessionArgs {
    #[arg(long)]
    agent_id: Option<AgentId>,
    #[arg(long)]
    channel_id: Option<ChannelId>,
    #[arg(long, default_value = ".")]
    cwd: PathBuf,
    #[arg(long)]
    session_dir: Option<PathBuf>,
    #[arg(long, default_value = "pi")]
    pi: PathBuf,
}

#[tokio::main]
async fn main() -> Result<()> {
    let args = Args::parse();

    match args.command {
        Command::Pi {
            executable,
            command,
        } => run_direct_pi(executable, command).await,
        Command::Session { command } => {
            let client = Client::new(
                ClientConfig::new(args.endpoint)
                    .token(args.token)
                    .namespace(args.namespace)
                    .pool_name(args.pool),
            );
            run_session_command(&client, command).await
        }
    }
}

async fn run_direct_pi(executable: PathBuf, command: PiCommand) -> Result<()> {
    let mut pi = PiRpc::spawn(PiOptions {
        executable,
        ..PiOptions::default()
    })?;

    match command {
        PiCommand::State => {
            pi.send(&RpcCommand::get_state("state")).await?;
            print_until(&mut pi, |message| {
                message.is_response_to("state", "get_state")
            })
            .await
        }
        PiCommand::Prompt { message } => {
            pi.send(&RpcCommand::prompt("prompt", message)).await?;
            print_until(&mut pi, |message| {
                matches!(message.event(), PiEvent::AgentEnd(_))
            })
            .await
        }
    }
}

async fn run_session_command(client: &Client, command: SessionCommand) -> Result<()> {
    match command {
        SessionCommand::Create(args) => create_session(client, args).await,
        SessionCommand::State { session_id } => {
            let state = session_handle(client, session_id)
                .send(GetSessionState)
                .await
                .with_context(|| format!("read session {session_id}"))?;
            println!("{}", serde_json::to_string_pretty(&state)?);
            Ok(())
        }
        SessionCommand::Prompt {
            session_id,
            message,
        } => prompt_session(client, session_id, message).await,
    }
}

async fn create_session(client: &Client, args: CreateSessionArgs) -> Result<()> {
    let cwd = absolute_existing_directory(&args.cwd)?;
    let session_dir = absolute_path(args.session_dir.unwrap_or(default_session_dir()?))?;
    let executable = resolve_executable(args.pi)?;
    let agent_id = args.agent_id.unwrap_or_else(|| Agent::new("local").id);
    let association = args
        .channel_id
        .map_or(SessionAssociation::Standalone, |channel_id| {
            SessionAssociation::Channel { channel_id }
        });
    let session = Session::new(agent_id, association);
    let mut pi = PiOptions::persistent(cwd, session_dir, session.id);
    pi.executable = executable;
    let config = SessionActorConfig {
        session: session.clone(),
        pi,
    };
    let handle = client.get_or_create_typed::<SessionActor>(
        SESSION_ACTOR_NAME,
        [session.id.to_string()],
        GetOrCreateOptions {
            create_with_input: Some(serde_json::to_value(config)?),
            ..GetOrCreateOptions::default()
        },
    )?;

    handle
        .send(GetSessionState)
        .await
        .context("create session actor")?;
    println!("{}", session.id);
    Ok(())
}

async fn prompt_session(client: &Client, session_id: SessionId, message: String) -> Result<()> {
    let connection = session_handle(client, session_id).connect();
    let (events, mut event_rx) = mpsc::unbounded_channel();
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

    while let Some(event) = event_rx.recv().await {
        println!("{}", serde_json::to_string(&event)?);
        let done = match &event.payload {
            SessionEventPayload::Pi { message } => {
                matches!(message.event(), PiEvent::AgentEnd(_))
            }
            SessionEventPayload::Stopped { .. } => true,
        };
        if done {
            subscription.unsubscribe().await;
            connection.disconnect().await;
            return Ok(());
        }
    }

    bail!("session {session_id} event stream closed before the prompt completed")
}

fn session_handle(
    client: &Client,
    session_id: SessionId,
) -> rivetkit::TypedActorHandle<SessionActor> {
    client
        .get_typed_default::<SessionActor>(SESSION_ACTOR_NAME, [session_id.to_string()])
        .expect("session actor key is valid")
}

fn default_session_dir() -> Result<PathBuf> {
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

async fn print_until(pi: &mut PiRpc, done: impl Fn(&PiMessage) -> bool) -> Result<()> {
    while let Some(message) = pi.next_message().await? {
        println!("{}", serde_json::to_string(&message)?);
        if done(&message) {
            return Ok(());
        }
    }
    Ok(())
}
