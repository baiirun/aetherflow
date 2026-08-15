use anyhow::{Context, Result, bail};
use serde::{Deserialize, Serialize};
use serde_json::{Map, Value};
use std::{path::PathBuf, process::Stdio};
use tokio::{
    io::{AsyncBufRead, AsyncBufReadExt, AsyncWriteExt, BufReader},
    process::{Child, ChildStdin, Command},
};

#[derive(Clone, Debug, Serialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum RpcCommand {
    Prompt {
        #[serde(skip_serializing_if = "Option::is_none")]
        id: Option<String>,
        message: String,
        #[serde(rename = "streamingBehavior", skip_serializing_if = "Option::is_none")]
        streaming_behavior: Option<StreamingBehavior>,
    },
    Steer {
        #[serde(skip_serializing_if = "Option::is_none")]
        id: Option<String>,
        message: String,
    },
    FollowUp {
        #[serde(skip_serializing_if = "Option::is_none")]
        id: Option<String>,
        message: String,
    },
    Abort {
        #[serde(skip_serializing_if = "Option::is_none")]
        id: Option<String>,
    },
    GetState {
        #[serde(skip_serializing_if = "Option::is_none")]
        id: Option<String>,
    },
}

impl RpcCommand {
    pub fn prompt(id: impl Into<String>, message: impl Into<String>) -> Self {
        Self::Prompt {
            id: Some(id.into()),
            message: message.into(),
            streaming_behavior: None,
        }
    }

    pub fn get_state(id: impl Into<String>) -> Self {
        Self::GetState {
            id: Some(id.into()),
        }
    }
}

#[derive(Clone, Copy, Debug, Serialize)]
pub enum StreamingBehavior {
    #[serde(rename = "steer")]
    Steer,
    #[serde(rename = "followUp")]
    FollowUp,
}

/// A forward-compatible Pi message. `kind` is the JSON `type`; all protocol
/// fields remain available without requiring this crate to mirror every Pi event.
#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct RpcMessage {
    #[serde(rename = "type")]
    pub kind: String,
    #[serde(flatten)]
    pub fields: Map<String, Value>,
}

impl RpcMessage {
    pub fn is_response_to(&self, id: &str, command: &str) -> bool {
        self.kind == "response"
            && self.fields.get("id").and_then(Value::as_str) == Some(id)
            && self.fields.get("command").and_then(Value::as_str) == Some(command)
    }
}

#[derive(Clone, Debug)]
pub struct PiOptions {
    pub executable: PathBuf,
    pub no_session: bool,
}

impl Default for PiOptions {
    fn default() -> Self {
        Self {
            executable: PathBuf::from("pi"),
            no_session: false,
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
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::inherit())
            .kill_on_drop(true);

        if options.no_session {
            command.arg("--no-session");
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

    pub async fn next_message(&mut self) -> Result<Option<RpcMessage>> {
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
    pub async fn next(&mut self) -> Result<Option<RpcMessage>> {
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

    #[tokio::test]
    async fn reader_preserves_unicode_line_separators() {
        let input = b"{\"type\":\"event\",\"text\":\"before\xE2\x80\xA8after\"}\n";
        let mut reader = JsonlReader::new(BufReader::new(&input[..]));

        let message = reader.next().await.unwrap().unwrap();

        assert_eq!(message.kind, "event");
        assert_eq!(message.fields["text"], "before\u{2028}after");
    }

    #[tokio::test]
    async fn reader_accepts_crlf_records() {
        let input = b"{\"type\":\"response\"}\r\n";
        let mut reader = JsonlReader::new(BufReader::new(&input[..]));

        let message = reader.next().await.unwrap().unwrap();

        assert_eq!(message.kind, "response");
    }

    #[tokio::test]
    async fn reader_rejects_unterminated_records() {
        let input = b"{\"type\":\"response\"}";
        let mut reader = JsonlReader::new(BufReader::new(&input[..]));

        let error = reader.next().await.unwrap_err();

        assert!(error.to_string().contains("unterminated JSONL record"));
    }
}
