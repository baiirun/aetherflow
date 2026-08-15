use aetherflow_pi::rivet_registry;
use anyhow::Result;
use rivetkit::ServeConfig;
use tokio_util::sync::CancellationToken;

#[path = "aetherflowd/vendored_engine.rs"]
mod vendored_engine;

#[tokio::main]
async fn main() -> Result<()> {
    let mut config = ServeConfig::from_env();
    vendored_engine::configure(&mut config)?;
    let mut runner =
        tokio::spawn(rivet_registry().serve_with_config(config, CancellationToken::new()));

    tokio::select! {
        result = &mut runner => result??,
        _ = shutdown_signal() => {
            // Rivet's graceful registry cancellation destroys its actors. Dropping the
            // runner transport instead lets the Engine reassign those actors after this
            // process restarts, preserving their state and keys.
            runner.abort();
            let _ = runner.await;
        }
    }
    Ok(())
}

async fn shutdown_signal() {
    #[cfg(unix)]
    {
        use tokio::signal::unix::{SignalKind, signal};

        let Ok(mut terminate) = signal(SignalKind::terminate()) else {
            let _ = tokio::signal::ctrl_c().await;
            return;
        };
        tokio::select! {
            _ = tokio::signal::ctrl_c() => {}
            _ = terminate.recv() => {}
        }
    }

    #[cfg(not(unix))]
    {
        let _ = tokio::signal::ctrl_c().await;
    }
}
