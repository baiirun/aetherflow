//! Local HTTP transport for content-addressed session attachments.

use super::DaemonHealth;
use crate::{LocalAttachmentStore, MAX_ATTACHMENT_BYTES};
use aetherflow_storage::{AttachmentId, AttachmentRef};
use anyhow::{Context, Result};
use axum::{
    Json, Router,
    body::Bytes,
    extract::{DefaultBodyLimit, Path, State},
    http::{HeaderMap, StatusCode, header},
    response::{IntoResponse, Response},
    routing::{get, post},
};
use std::{net::SocketAddr, str::FromStr};
use tokio_util::sync::CancellationToken;
use tracing::info;

pub async fn bind(address: SocketAddr) -> Result<tokio::net::TcpListener> {
    tokio::net::TcpListener::bind(address)
        .await
        .with_context(|| format!("bind attachment server to {address}"))
}

pub async fn serve(
    store: LocalAttachmentStore,
    listener: tokio::net::TcpListener,
    shutdown: CancellationToken,
) -> Result<()> {
    let router = Router::new()
        .route("/health", get(health))
        .route("/attachments", post(upload))
        .route("/attachments/{id}", get(download))
        .layer(DefaultBodyLimit::max(MAX_ATTACHMENT_BYTES))
        .with_state(store.clone());
    info!(
        target: "aetherflowd",
        event = "attachments.listening",
        address = %listener.local_addr()?,
        storage_path = %store.root().display(),
        max_bytes = MAX_ATTACHMENT_BYTES,
        "attachment transport is ready"
    );
    axum::serve(listener, router)
        .with_graceful_shutdown(shutdown.cancelled_owned())
        .await
        .context("serve attachment transport")
}

async fn health() -> Json<DaemonHealth> {
    Json(DaemonHealth::current())
}

async fn upload(
    State(store): State<LocalAttachmentStore>,
    headers: HeaderMap,
    body: Bytes,
) -> Result<Json<AttachmentRef>, AttachmentHttpError> {
    let media_type = headers
        .get(header::CONTENT_TYPE)
        .and_then(|value| value.to_str().ok())
        .ok_or_else(|| AttachmentHttpError::bad_request("Content-Type is required"))?
        .to_owned();
    let attachment = tokio::task::spawn_blocking(move || store.put(&media_type, &body))
        .await
        .map_err(|error| AttachmentHttpError::internal(format!("attachment task failed: {error}")))?
        .map_err(|error| AttachmentHttpError::bad_request(format!("{error:#}")))?;
    Ok(Json(attachment))
}

async fn download(
    State(store): State<LocalAttachmentStore>,
    Path(id): Path<String>,
) -> Result<Response, AttachmentHttpError> {
    let id = AttachmentId::from_str(&id)
        .map_err(|error| AttachmentHttpError::bad_request(error.to_string()))?;
    let (attachment, bytes) = tokio::task::spawn_blocking(move || store.get(&id))
        .await
        .map_err(|error| AttachmentHttpError::internal(format!("attachment task failed: {error}")))?
        .map_err(|error| AttachmentHttpError::not_found(format!("{error:#}")))?;
    Ok(([(header::CONTENT_TYPE, attachment.media_type)], bytes).into_response())
}

struct AttachmentHttpError {
    status: StatusCode,
    message: String,
}

impl AttachmentHttpError {
    fn bad_request(message: impl Into<String>) -> Self {
        Self {
            status: StatusCode::BAD_REQUEST,
            message: message.into(),
        }
    }

    fn not_found(message: impl Into<String>) -> Self {
        Self {
            status: StatusCode::NOT_FOUND,
            message: message.into(),
        }
    }

    fn internal(message: impl Into<String>) -> Self {
        Self {
            status: StatusCode::INTERNAL_SERVER_ERROR,
            message: message.into(),
        }
    }
}

impl IntoResponse for AttachmentHttpError {
    fn into_response(self) -> Response {
        (self.status, self.message).into_response()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn uploads_and_downloads_a_large_attachment_over_http() -> Result<()> {
        let temp = tempfile::tempdir()?;
        let store = LocalAttachmentStore::new(temp.path());
        let listener = bind("127.0.0.1:0".parse()?).await?;
        let address = listener.local_addr()?;
        let shutdown = CancellationToken::new();
        let server = tokio::spawn(serve(store, listener, shutdown.clone()));
        let bytes = vec![11; 96 * 1024];
        let client = reqwest::Client::new();

        let response = client
            .get(format!("http://{address}/health"))
            .send()
            .await?;
        assert_eq!(response.status(), reqwest::StatusCode::OK);
        assert_eq!(
            response.json::<DaemonHealth>().await?,
            DaemonHealth::current()
        );

        let response = client
            .post(format!("http://{address}/attachments"))
            .header(reqwest::header::CONTENT_TYPE, "image/png")
            .body(bytes.clone())
            .send()
            .await?;
        assert_eq!(response.status(), reqwest::StatusCode::OK);
        let attachment: AttachmentRef = response.json().await?;

        let response = client
            .get(format!("http://{address}/attachments/{}", attachment.id))
            .send()
            .await?;
        assert_eq!(response.status(), reqwest::StatusCode::OK);
        assert_eq!(
            response.headers()[reqwest::header::CONTENT_TYPE],
            "image/png"
        );
        assert_eq!(response.bytes().await?.as_ref(), bytes);

        shutdown.cancel();
        server.await??;
        Ok(())
    }
}
