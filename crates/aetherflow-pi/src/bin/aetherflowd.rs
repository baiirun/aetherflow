use aetherflow_pi::{DEFAULT_ATTACHMENT_ADDRESS, LocalAttachmentStore, rivet_registry};
use anyhow::{Context, Result, bail};
use rivetkit::ServeConfig;
use std::{net::SocketAddr, process::ExitCode, time::Instant};
use tokio_util::sync::CancellationToken;
use tracing::{error, info};
use tracing_subscriber::EnvFilter;
use vendored_engine::EngineConfiguration;

#[path = "aetherflowd/attachment_http.rs"]
mod attachment_http;
#[path = "aetherflowd/vendored_engine.rs"]
mod vendored_engine;

#[tokio::main]
async fn main() -> ExitCode {
    init_logging();
    let started_at = Instant::now();

    match run().await {
        Ok(outcome) => {
            info!(
                target: "aetherflowd",
                event = "daemon.stopped",
                outcome,
                duration_ms = started_at.elapsed().as_millis(),
                "Aetherflow daemon stopped"
            );
            ExitCode::SUCCESS
        }
        Err(error) => {
            error!(
                target: "aetherflowd",
                event = "daemon.stopped",
                outcome = "error",
                duration_ms = started_at.elapsed().as_millis(),
                error = ?error,
                "Aetherflow daemon failed"
            );
            ExitCode::FAILURE
        }
    }
}

async fn run() -> Result<&'static str> {
    let mut config = ServeConfig::from_env();
    info!(
        target: "aetherflowd",
        event = "daemon.starting",
        version = env!("CARGO_PKG_VERSION"),
        process_id = std::process::id(),
        endpoint = %config.endpoint,
        namespace = %config.namespace,
        pool = %config.pool_name,
        engine_spawn = ?config.engine_spawn,
        "starting Aetherflow daemon"
    );
    let attachment_address = std::env::var("AETHERFLOW_ATTACHMENT_ADDRESS")
        .unwrap_or_else(|_| DEFAULT_ATTACHMENT_ADDRESS.to_owned())
        .parse::<SocketAddr>()
        .context("parse AETHERFLOW_ATTACHMENT_ADDRESS")?;
    let attachment_store = LocalAttachmentStore::from_env()?;
    let attachment_listener = attachment_http::bind(attachment_address).await?;

    let engine = vendored_engine::configure(&mut config)
        .context("configure the Rivet Engine used by aetherflowd")?;
    log_engine_configuration(&engine, config.engine_auto_download);

    info!(
        target: "aetherflowd",
        event = "runner.starting",
        actor_types = "Session,SessionDirectory",
        endpoint = %config.endpoint,
        namespace = %config.namespace,
        pool = %config.pool_name,
        "starting Rivet actor runner"
    );
    let mut runner =
        tokio::spawn(rivet_registry().serve_with_config(config, CancellationToken::new()));
    let attachment_shutdown = CancellationToken::new();
    let mut attachments = tokio::spawn(attachment_http::serve(
        attachment_store,
        attachment_listener,
        attachment_shutdown.clone(),
    ));

    tokio::select! {
        result = &mut runner => {
            attachment_shutdown.cancel();
            attachments.await.context("attachment server task failed")??;
            result.context("Rivet actor runner task failed")?
                .context("Rivet actor runner exited with an error")?;
            Ok("runner_completed")
        },
        result = &mut attachments => {
            runner.abort();
            let _ = runner.await;
            result.context("attachment server task failed")??;
            bail!("attachment server exited unexpectedly")
        },
        signal = shutdown_signal() => {
            info!(
                target: "aetherflowd",
                event = "daemon.shutdown_requested",
                signal,
                "shutdown requested"
            );
            // Rivet's graceful registry cancellation destroys its actors. Dropping the
            // runner transport instead lets the Engine reassign those actors after this
            // process restarts, preserving their state and keys.
            runner.abort();
            let _ = runner.await;
            attachment_shutdown.cancel();
            attachments.await.context("attachment server task failed")??;
            Ok("signal")
        }
    }
}

fn log_engine_configuration(engine: &EngineConfiguration, auto_download: bool) {
    match engine {
        EngineConfiguration::Bundled {
            version,
            binary_path,
            asset_action,
        } => info!(
            target: "aetherflowd",
            event = "engine.configured",
            source = "bundled",
            version,
            binary_path = %binary_path.display(),
            asset_action = %asset_action,
            auto_download,
            "Rivet Engine configured"
        ),
        EngineConfiguration::Configured { binary_path } => info!(
            target: "aetherflowd",
            event = "engine.configured",
            source = "configured",
            binary_path = %binary_path.display(),
            auto_download,
            "Rivet Engine configured"
        ),
        EngineConfiguration::External => info!(
            target: "aetherflowd",
            event = "engine.configured",
            source = "external",
            auto_download,
            "Rivet Engine configured"
        ),
    }
}

fn init_logging() {
    let filter = EnvFilter::try_from_default_env()
        .unwrap_or_else(|_| EnvFilter::new("aetherflowd=info,rivetkit=info"));
    tracing_subscriber::fmt()
        .with_env_filter(filter)
        .with_target(false)
        .compact()
        .init();
}

async fn shutdown_signal() -> &'static str {
    #[cfg(unix)]
    {
        use tokio::signal::unix::{SignalKind, signal};

        let Ok(mut terminate) = signal(SignalKind::terminate()) else {
            let _ = tokio::signal::ctrl_c().await;
            return "interrupt";
        };
        tokio::select! {
            _ = tokio::signal::ctrl_c() => "interrupt",
            _ = terminate.recv() => "terminate",
        }
    }

    #[cfg(not(unix))]
    {
        let _ = tokio::signal::ctrl_c().await;
        "interrupt"
    }
}
