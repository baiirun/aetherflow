use aetherflow_storage::SessionId;
use anyhow::{Context, Result, bail};
use serde::{Deserialize, Serialize};
use std::{path::PathBuf, process::Stdio};
use tokio::{
    io::{AsyncBufRead, AsyncBufReadExt, AsyncWriteExt, BufReader},
    process::{Child, ChildStdin, Command},
};

mod attachment_store;
mod client;
pub mod daemon;
pub mod protocol;
mod session_actor;
mod session_directory;

pub use attachment_store::{LocalAttachmentStore, MAX_ATTACHMENT_BYTES};
pub use client::{
    AetherflowClient, AetherflowClientOptions, CreateSessionOptions, DEFAULT_ATTACHMENT_ADDRESS,
    DEFAULT_ATTACHMENT_ENDPOINT, DEFAULT_ENDPOINT, DEFAULT_NAMESPACE, DEFAULT_POOL, DEFAULT_TOKEN,
    SessionEventStream, SessionEventSubscription,
};

pub use protocol::{
    AssistantMessageEvent, CompactionReason, ExtensionError, ExtensionUiRequest, NotifyType,
    PiEvent, PiMessage, QueueMode, RpcResponse, SummarizationRetrySource, ThinkingLevel, ToolCall,
    WidgetPlacement,
};
pub use session_actor::{
    DEFAULT_SESSION_EVENT_PAGE_SIZE, GetSessionState, MAX_SESSION_EVENT_PAGE_SIZE,
    ReadSessionEvents, SESSION_ACTOR_NAME, SendSessionCommand, SessionActor, SessionActorConfig,
    SessionActorState, SessionCommand, SessionEvent, SessionEventPayload, rivet_registry,
};
pub use session_directory::{
    DEFAULT_SESSION_DIRECTORY_KEY, SESSION_DIRECTORY_ACTOR_NAME, SessionDescriptor,
    SessionDirectoryActor,
};

#[derive(Clone, Copy, Debug, Deserialize, Eq, PartialEq, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum ImageContentType {
    Image,
}

#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ImageContent {
    #[serde(rename = "type")]
    pub content_type: ImageContentType,
    pub data: String,
    pub mime_type: String,
}

impl ImageContent {
    pub fn new(data: impl Into<String>, mime_type: impl Into<String>) -> Self {
        Self {
            content_type: ImageContentType::Image,
            data: data.into(),
            mime_type: mime_type.into(),
        }
    }
}

#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum RpcCommand {
    Prompt {
        #[serde(skip_serializing_if = "Option::is_none")]
        id: Option<String>,
        message: String,
        #[serde(skip_serializing_if = "Option::is_none")]
        images: Option<Vec<ImageContent>>,
        #[serde(rename = "streamingBehavior", skip_serializing_if = "Option::is_none")]
        streaming_behavior: Option<StreamingBehavior>,
    },
    Steer {
        #[serde(skip_serializing_if = "Option::is_none")]
        id: Option<String>,
        message: String,
        #[serde(skip_serializing_if = "Option::is_none")]
        images: Option<Vec<ImageContent>>,
    },
    FollowUp {
        #[serde(skip_serializing_if = "Option::is_none")]
        id: Option<String>,
        message: String,
        #[serde(skip_serializing_if = "Option::is_none")]
        images: Option<Vec<ImageContent>>,
    },
    Abort {
        #[serde(skip_serializing_if = "Option::is_none")]
        id: Option<String>,
    },
    GetState {
        #[serde(skip_serializing_if = "Option::is_none")]
        id: Option<String>,
    },
    SetSteeringMode {
        #[serde(skip_serializing_if = "Option::is_none")]
        id: Option<String>,
        mode: QueueMode,
    },
    SetFollowUpMode {
        #[serde(skip_serializing_if = "Option::is_none")]
        id: Option<String>,
        mode: QueueMode,
    },
}

impl RpcCommand {
    pub fn prompt(id: impl Into<String>, message: impl Into<String>) -> Self {
        Self::Prompt {
            id: Some(id.into()),
            message: message.into(),
            images: None,
            streaming_behavior: None,
        }
    }

    pub fn prompt_with_images(
        id: impl Into<String>,
        message: impl Into<String>,
        images: Vec<ImageContent>,
    ) -> Self {
        Self::Prompt {
            id: Some(id.into()),
            message: message.into(),
            images: (!images.is_empty()).then_some(images),
            streaming_behavior: None,
        }
    }

    pub fn abort(id: impl Into<String>) -> Self {
        Self::Abort {
            id: Some(id.into()),
        }
    }

    pub fn get_state(id: impl Into<String>) -> Self {
        Self::GetState {
            id: Some(id.into()),
        }
    }

    pub fn set_steering_mode(id: impl Into<String>, mode: QueueMode) -> Self {
        Self::SetSteeringMode {
            id: Some(id.into()),
            mode,
        }
    }

    pub fn set_follow_up_mode(id: impl Into<String>, mode: QueueMode) -> Self {
        Self::SetFollowUpMode {
            id: Some(id.into()),
            mode,
        }
    }
}

#[derive(Clone, Copy, Debug, Deserialize, Serialize)]
pub enum StreamingBehavior {
    #[serde(rename = "steer")]
    Steer,
    #[serde(rename = "followUp")]
    FollowUp,
}

#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct PiOptions {
    pub executable: PathBuf,
    pub cwd: PathBuf,
    pub storage: PiSessionStorage,
}

#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(tag = "mode", rename_all = "snake_case")]
pub enum PiSessionStorage {
    Ephemeral,
    Persistent {
        directory: PathBuf,
        session_id: SessionId,
    },
}

impl Default for PiOptions {
    fn default() -> Self {
        Self {
            executable: PathBuf::from("pi"),
            cwd: std::env::current_dir().unwrap_or_else(|_| PathBuf::from(".")),
            storage: PiSessionStorage::Ephemeral,
        }
    }
}

impl PiOptions {
    pub fn persistent(
        cwd: impl Into<PathBuf>,
        directory: impl Into<PathBuf>,
        session_id: SessionId,
    ) -> Self {
        Self {
            executable: PathBuf::from("pi"),
            cwd: cwd.into(),
            storage: PiSessionStorage::Persistent {
                directory: directory.into(),
                session_id,
            },
        }
    }

    pub fn ephemeral(cwd: impl Into<PathBuf>) -> Self {
        Self {
            cwd: cwd.into(),
            ..Self::default()
        }
    }
}

/// Owns one headless Pi process and its strict LF-delimited JSON stream.
pub struct PiRpc {
    _child: Child,
    stdin: ChildStdin,
    stdout: JsonlReader<BufReader<tokio::process::ChildStdout>>,
}

impl PiRpc {
    pub fn spawn(options: PiOptions) -> Result<Self> {
        let mut command = Command::new(&options.executable);
        command
            .arg("--mode")
            .arg("rpc")
            .current_dir(&options.cwd)
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::inherit())
            .kill_on_drop(true);

        match &options.storage {
            PiSessionStorage::Ephemeral => {
                command.arg("--no-session");
            }
            PiSessionStorage::Persistent {
                directory,
                session_id,
            } => {
                command
                    .arg("--session-dir")
                    .arg(directory)
                    .arg("--session-id")
                    .arg(session_id.to_string());
            }
        }

        let mut child = command
            .spawn()
            .with_context(|| format!("failed to start {}", options.executable.display()))?;
        let stdin = child.stdin.take().context("Pi did not expose stdin")?;
        let stdout = child.stdout.take().context("Pi did not expose stdout")?;

        Ok(Self {
            _child: child,
            stdin,
            stdout: JsonlReader::new(BufReader::new(stdout)),
        })
    }

    pub async fn send(&mut self, command: &RpcCommand) -> Result<()> {
        let mut record = serde_json::to_vec(command)?;
        record.push(b'\n');
        self.stdin.write_all(&record).await?;
        self.stdin.flush().await?;
        Ok(())
    }

    pub async fn next_message(&mut self) -> Result<Option<PiMessage>> {
        self.stdout.next().await
    }
}

pub struct JsonlReader<R> {
    reader: R,
    buffer: Vec<u8>,
}

impl<R: AsyncBufRead + Unpin> JsonlReader<R> {
    pub fn new(reader: R) -> Self {
        Self {
            reader,
            buffer: Vec::new(),
        }
    }

    /// Splits only on LF, preserving U+2028 and U+2029 inside JSON strings.
    pub async fn next(&mut self) -> Result<Option<PiMessage>> {
        self.buffer.clear();
        let read = self.reader.read_until(b'\n', &mut self.buffer).await?;
        if read == 0 {
            return Ok(None);
        }
        if self.buffer.last() != Some(&b'\n') {
            bail!("Pi closed stdout with an unterminated JSONL record");
        }
        self.buffer.pop();
        if self.buffer.last() == Some(&b'\r') {
            self.buffer.pop();
        }
        Ok(Some(serde_json::from_slice(&self.buffer)?))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tokio::io::BufReader;

    #[test]
    fn command_matches_pi_prompt_shape() {
        let command = RpcCommand::prompt("request-1", "hello");
        let value = serde_json::to_value(command).unwrap();

        assert_eq!(
            value,
            serde_json::json!({"id": "request-1", "type": "prompt", "message": "hello"})
        );
    }

    #[test]
    fn image_prompt_matches_pi_rpc_shape() {
        let command = RpcCommand::prompt_with_images(
            "request-image",
            "What is this?",
            vec![ImageContent::new("AQID", "image/png")],
        );
        let value = serde_json::to_value(command).unwrap();

        assert_eq!(
            value,
            serde_json::json!({
                "id": "request-image",
                "type": "prompt",
                "message": "What is this?",
                "images": [{
                    "type": "image",
                    "data": "AQID",
                    "mimeType": "image/png"
                }]
            })
        );
    }

    #[test]
    fn command_matches_pi_abort_shape() {
        let command = RpcCommand::abort("request-2");
        let value = serde_json::to_value(command).unwrap();

        assert_eq!(
            value,
            serde_json::json!({"id": "request-2", "type": "abort"})
        );
    }

    #[test]
    fn queue_mode_commands_match_pi_shapes() {
        let cases = [
            (
                RpcCommand::set_steering_mode("steering", QueueMode::All),
                serde_json::json!({
                    "id": "steering",
                    "type": "set_steering_mode",
                    "mode": "all"
                }),
            ),
            (
                RpcCommand::set_follow_up_mode("follow-up", QueueMode::OneAtATime),
                serde_json::json!({
                    "id": "follow-up",
                    "type": "set_follow_up_mode",
                    "mode": "one-at-a-time"
                }),
            ),
        ];

        for (command, expected) in cases {
            assert_eq!(serde_json::to_value(command).unwrap(), expected);
        }
    }

    #[tokio::test]
    async fn reader_preserves_unicode_line_separators() {
        let input = b"{\"type\":\"event\",\"text\":\"before\xE2\x80\xA8after\"}\n";
        let mut reader = JsonlReader::new(BufReader::new(&input[..]));

        let message = reader.next().await.unwrap().unwrap();

        assert!(matches!(message.event(), PiEvent::Unknown { kind } if kind == "event"));
        assert_eq!(message.raw()["text"], "before\u{2028}after");
    }

    #[tokio::test]
    async fn reader_accepts_crlf_records() {
        let input = b"{\"type\":\"agent_start\"}\r\n";
        let mut reader = JsonlReader::new(BufReader::new(&input[..]));

        let message = reader.next().await.unwrap().unwrap();

        assert!(matches!(message.event(), PiEvent::AgentStart));
    }

    #[tokio::test]
    async fn reader_rejects_unterminated_records() {
        let input = b"{\"type\":\"response\"}";
        let mut reader = JsonlReader::new(BufReader::new(&input[..]));

        let error = reader.next().await.unwrap_err();

        assert!(error.to_string().contains("unterminated JSONL record"));
    }

    #[cfg(unix)]
    #[tokio::test]
    async fn persistent_process_uses_explicit_cwd_directory_and_session_id() {
        use std::{fs, os::unix::fs::PermissionsExt};
        use tokio::time::{Duration, timeout};

        let directory = tempfile::tempdir().unwrap();
        let cwd = directory.path().join("worktree");
        let session_directory = directory.path().join("sessions");
        let executable = directory.path().join("fake-pi");
        fs::create_dir(&cwd).unwrap();
        fs::write(
            &executable,
            "#!/bin/sh\npwd > \"$0.pwd\"\nprintf '%s\\n' \"$@\" > \"$0.args\"\nwhile IFS= read -r _; do :; done\n",
        )
        .unwrap();
        let mut permissions = fs::metadata(&executable).unwrap().permissions();
        permissions.set_mode(0o755);
        fs::set_permissions(&executable, permissions).unwrap();

        let session_id = SessionId::new();
        let mut options = PiOptions::persistent(&cwd, &session_directory, session_id);
        options.executable = executable.clone();
        let process = PiRpc::spawn(options).unwrap();

        timeout(Duration::from_secs(2), async {
            while !executable.with_extension("args").exists() {
                tokio::time::sleep(Duration::from_millis(10)).await;
            }
        })
        .await
        .expect("fake Pi did not record its arguments");
        drop(process);

        assert_eq!(
            fs::read_to_string(executable.with_extension("pwd"))
                .unwrap()
                .trim(),
            cwd.canonicalize().unwrap().to_string_lossy()
        );
        assert_eq!(
            fs::read_to_string(executable.with_extension("args")).unwrap(),
            format!(
                "--mode\nrpc\n--session-dir\n{}\n--session-id\n{}\n",
                session_directory.display(),
                session_id
            )
        );
    }
}
