use aetherflow_pi::{PiOptions, PiRpc, RpcCommand};
use anyhow::{Context, Result};
use clap::Parser;
use std::path::PathBuf;

#[derive(Parser)]
#[command(name = "aetherflowd")]
struct Args {
    #[arg(long, default_value = "pi")]
    pi: PathBuf,
}

#[tokio::main]
async fn main() -> Result<()> {
    let args = Args::parse();
    let mut pi = PiRpc::spawn(PiOptions {
        executable: args.pi,
        no_session: false,
    })?;

    pi.send(&RpcCommand::get_state("startup")).await?;
    while let Some(message) = pi.next_message().await? {
        if message.is_response_to("startup", "get_state") {
            println!(
                "aetherflowd supervising Pi session: {}",
                serde_json::to_string(&message)?
            );
            break;
        }
    }

    tokio::signal::ctrl_c()
        .await
        .context("failed to listen for shutdown signal")
}
