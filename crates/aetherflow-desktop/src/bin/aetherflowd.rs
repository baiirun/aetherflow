use std::process::ExitCode;

#[tokio::main]
async fn main() -> ExitCode {
    aetherflow_pi::daemon::run().await
}
