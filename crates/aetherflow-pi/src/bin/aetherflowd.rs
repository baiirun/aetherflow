use aetherflow_pi::rivet_registry;
use anyhow::Result;

#[tokio::main]
async fn main() -> Result<()> {
    rivet_registry().start().await
}
