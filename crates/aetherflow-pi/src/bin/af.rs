use aetherflow_pi::{PiEvent, PiMessage, PiOptions, PiRpc, RpcCommand};
use anyhow::Result;
use clap::{Parser, Subcommand};
use std::path::PathBuf;

#[derive(Parser)]
#[command(name = "af")]
struct Args {
    #[arg(long, default_value = "pi")]
    pi: PathBuf,
    #[command(subcommand)]
    command: Command,
}

#[derive(Subcommand)]
enum Command {
    /// Read the state of an ephemeral, headless Pi session.
    State,
    /// Send one prompt and print Pi's unrendered JSON event stream.
    Prompt { message: String },
}

#[tokio::main]
async fn main() -> Result<()> {
    let args = Args::parse();
    let mut pi = PiRpc::spawn(PiOptions {
        executable: args.pi,
        no_session: true,
    })?;

    match args.command {
        Command::State => {
            pi.send(&RpcCommand::get_state("state")).await?;
            print_until(&mut pi, |message| {
                message.is_response_to("state", "get_state")
            })
            .await
        }
        Command::Prompt { message } => {
            pi.send(&RpcCommand::prompt("prompt", message)).await?;
            print_until(&mut pi, |message| {
                matches!(message.event(), PiEvent::AgentEnd(_))
            })
            .await
        }
    }
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
