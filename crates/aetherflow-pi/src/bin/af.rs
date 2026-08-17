use aetherflow_pi::{
    AetherflowClient, AetherflowClientOptions, CreateSessionOptions, DEFAULT_ATTACHMENT_ENDPOINT,
    DEFAULT_ENDPOINT, DEFAULT_NAMESPACE, DEFAULT_POOL, DEFAULT_SESSION_DIRECTORY_KEY,
    DEFAULT_SESSION_EVENT_PAGE_SIZE, DEFAULT_TOKEN, MAX_SESSION_EVENT_PAGE_SIZE, PiEvent,
    PiMessage, PiOptions, PiRpc, RpcCommand, SessionEventStream,
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
    /// Aetherflow daemon endpoint used for attachment upload and download.
    #[arg(long, default_value = DEFAULT_ATTACHMENT_ENDPOINT)]
    attachment_endpoint: String,
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
    /// Hide a session from the active desktop list.
    Archive { session_id: SessionId },
    /// Return a session to the active desktop list.
    Unarchive { session_id: SessionId },
    /// Prompt a session and print its unrendered Pi event stream.
    Prompt {
        session_id: SessionId,
        message: String,
    },
    /// Read the durable, sequenced event log for a session.
    Events {
        session_id: SessionId,
        /// Return events whose sequence is greater than this cursor.
        #[arg(long, default_value_t = 0)]
        after: u64,
        /// Maximum snapshot size, or page size while following.
        #[arg(
            long,
            default_value_t = DEFAULT_SESSION_EVENT_PAGE_SIZE,
            value_parser = clap::value_parser!(u32).range(1..=MAX_SESSION_EVENT_PAGE_SIZE as i64)
        )]
        limit: u32,
        /// Continue printing events after replay catches up.
        #[arg(long)]
        follow: bool,
    },
}

#[derive(ClapArgs)]
struct CreateSessionArgs {
    /// Prompt the session immediately after creating it.
    prompt: Option<String>,
    /// Attach to and print the initial prompt's Pi event stream.
    #[arg(long, requires = "prompt")]
    attach: bool,
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
                attachment_endpoint: args.attachment_endpoint,
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
        SessionCommand::Archive { session_id } => {
            client.set_session_archived(session_id, true).await?;
            println!("{session_id}");
            Ok(())
        }
        SessionCommand::Unarchive { session_id } => {
            client.set_session_archived(session_id, false).await?;
            println!("{session_id}");
            Ok(())
        }
        SessionCommand::Prompt {
            session_id,
            message,
        } => prompt_session(client, session_id, message).await,
        SessionCommand::Events {
            session_id,
            after,
            limit,
            follow,
        } => session_events(client, session_id, after, limit, follow).await,
    }
}

async fn session_events(
    client: &AetherflowClient,
    session_id: SessionId,
    after: u64,
    limit: u32,
    follow: bool,
) -> Result<()> {
    if !follow {
        for event in client.session_events(session_id, after, limit).await? {
            println!("{}", serde_json::to_string(&event)?);
        }
        return Ok(());
    }

    let mut events = client
        .follow_session_events(session_id, after, limit)
        .await?;
    loop {
        tokio::select! {
            event = events.next() => {
                let Some(event) = event? else {
                    break;
                };
                println!("{}", serde_json::to_string(&event)?);
            }
            signal = tokio::signal::ctrl_c() => {
                signal?;
                break;
            }
        }
    }
    events.close().await;
    Ok(())
}

async fn create_session(client: &AetherflowClient, args: CreateSessionArgs) -> Result<()> {
    let association = args
        .channel_id
        .map_or(SessionAssociation::Standalone, |channel_id| {
            SessionAssociation::Channel { channel_id }
        });
    let attached_prompt = if args.attach {
        args.prompt.clone()
    } else {
        None
    };
    let session_id = client
        .create_session(CreateSessionOptions {
            agent_id: args.agent_id,
            association,
            cwd: args.cwd,
            pi_session_directory: args.session_dir,
            pi_executable: args.pi,
            initial_prompt: if args.attach { None } else { args.prompt },
        })
        .await?;
    println!("{session_id}");
    if let Some(prompt) = attached_prompt {
        let events = client.prompt_session(session_id, prompt).await?;
        print_session_events(events).await?;
    }
    Ok(())
}

async fn prompt_session(
    client: &AetherflowClient,
    session_id: SessionId,
    message: String,
) -> Result<()> {
    let events = client.prompt_session(session_id, message).await?;
    print_session_events(events).await
}

async fn print_session_events(mut events: SessionEventStream) -> Result<()> {
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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn session_create_accepts_an_initial_prompt() {
        let args = Args::try_parse_from(["af", "session", "create", "start here"]).unwrap();

        let Command::Session {
            command: SessionCommand::Create(create),
        } = args.command
        else {
            panic!("expected session create command");
        };

        assert_eq!(create.prompt.as_deref(), Some("start here"));
        assert!(!create.attach);
    }

    #[test]
    fn session_create_keeps_the_initial_prompt_optional() {
        let args = Args::try_parse_from(["af", "session", "create"]).unwrap();

        let Command::Session {
            command: SessionCommand::Create(create),
        } = args.command
        else {
            panic!("expected session create command");
        };

        assert_eq!(create.prompt, None);
        assert!(!create.attach);
    }

    #[test]
    fn session_create_attach_requires_a_prompt() {
        let error = Args::try_parse_from(["af", "session", "create", "--attach"])
            .err()
            .expect("attach without a prompt should fail");

        assert_eq!(
            error.kind(),
            clap::error::ErrorKind::MissingRequiredArgument
        );
    }

    #[test]
    fn session_create_can_attach_to_the_initial_prompt() {
        let args =
            Args::try_parse_from(["af", "session", "create", "start here", "--attach"]).unwrap();

        let Command::Session {
            command: SessionCommand::Create(create),
        } = args.command
        else {
            panic!("expected session create command");
        };

        assert_eq!(create.prompt.as_deref(), Some("start here"));
        assert!(create.attach);
    }

    #[test]
    fn session_events_accepts_cursor_limit_and_follow() {
        let args = Args::try_parse_from([
            "af",
            "session",
            "events",
            "00000000-0000-0000-0000-000000000001",
            "--after",
            "42",
            "--limit",
            "25",
            "--follow",
        ])
        .unwrap();

        let Command::Session {
            command:
                SessionCommand::Events {
                    after,
                    limit,
                    follow,
                    ..
                },
        } = args.command
        else {
            panic!("expected session events command");
        };

        assert_eq!(after, 42);
        assert_eq!(limit, 25);
        assert!(follow);
    }

    #[test]
    fn session_events_rejects_an_unbounded_page() {
        let error = Args::try_parse_from([
            "af",
            "session",
            "events",
            "00000000-0000-0000-0000-000000000001",
            "--limit",
            "1001",
        ])
        .err()
        .expect("an oversized event page should fail");

        assert_eq!(error.kind(), clap::error::ErrorKind::ValueValidation);
    }

    #[test]
    fn session_archive_and_unarchive_accept_a_session_id() {
        let id = "00000000-0000-0000-0000-000000000001";

        let archive = Args::try_parse_from(["af", "session", "archive", id]).unwrap();
        let Command::Session {
            command: SessionCommand::Archive { session_id },
        } = archive.command
        else {
            panic!("expected session archive command");
        };
        assert_eq!(session_id.to_string(), id);

        let unarchive = Args::try_parse_from(["af", "session", "unarchive", id]).unwrap();
        assert!(matches!(
            unarchive.command,
            Command::Session {
                command: SessionCommand::Unarchive { .. }
            }
        ));
    }
}
