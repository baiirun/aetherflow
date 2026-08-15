use aetherflow_pi::{
    AetherflowClient, AetherflowClientOptions, CreateSessionOptions, DEFAULT_ENDPOINT,
    DEFAULT_NAMESPACE, DEFAULT_POOL, DEFAULT_SESSION_DIRECTORY_KEY, DEFAULT_TOKEN, PiEvent,
    PiMessage, PiOptions, PiRpc, RpcCommand,
};
use aetherflow_storage::{AgentId, ChannelId, SessionAssociation, SessionId};
use anyhow::Result;
use clap::{Args as ClapArgs, Parser, Subcommand};
use std::path::PathBuf;

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
    /// Isolates session discovery within a Rivet namespace.
    #[arg(long, default_value = DEFAULT_SESSION_DIRECTORY_KEY, hide = true)]
    session_directory_key: String,
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
    /// List every durable session known to this Aetherflow namespace.
    List,
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
            let client = AetherflowClient::connect(AetherflowClientOptions {
                endpoint: args.endpoint,
                token: args.token,
                namespace: args.namespace,
                pool: args.pool,
                session_directory_key: args.session_directory_key,
            });
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

async fn run_session_command(client: &AetherflowClient, command: SessionCommand) -> Result<()> {
    match command {
        SessionCommand::Create(args) => create_session(client, args).await,
        SessionCommand::List => {
            println!(
                "{}",
                serde_json::to_string_pretty(&client.list_sessions().await?)?
            );
            Ok(())
        }
        SessionCommand::State { session_id } => {
            let state = client.session_state(session_id).await?;
            println!("{}", serde_json::to_string_pretty(&state)?);
            Ok(())
        }
        SessionCommand::Prompt {
            session_id,
            message,
        } => prompt_session(client, session_id, message).await,
    }
}

async fn create_session(client: &AetherflowClient, args: CreateSessionArgs) -> Result<()> {
    let association = args
        .channel_id
        .map_or(SessionAssociation::Standalone, |channel_id| {
            SessionAssociation::Channel { channel_id }
        });
    let session = client
        .create_session(CreateSessionOptions {
            agent_id: args.agent_id,
            association,
            cwd: args.cwd,
            pi_session_directory: args.session_dir,
            pi_executable: args.pi,
        })
        .await?;
    println!("{}", session.id);
    Ok(())
}

async fn prompt_session(
    client: &AetherflowClient,
    session_id: SessionId,
    message: String,
) -> Result<()> {
    let mut events = client.prompt_session(session_id, message).await?;
    while let Some(event) = events.next().await? {
        println!("{}", serde_json::to_string(&event)?);
    }
    Ok(())
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
