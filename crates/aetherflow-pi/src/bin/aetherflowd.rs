use aetherflow_pi::DaemonRegistry;
use anyhow::{Context, Result};

#[tokio::main]
async fn main() -> Result<()> {
    let registry = DaemonRegistry::new();
    println!(
        "aetherflowd ready: {} agents, {} channels, {} sessions",
        registry.agents().len(),
        registry.channels().len(),
        registry.sessions().len()
    );

    tokio::signal::ctrl_c()
        .await
        .context("failed to listen for shutdown signal")
}
