use serde::{Deserialize, Deserializer, Serialize, Serializer, de::Error as _};
use serde_json::{Map, Value};

/// One JSON object read from Pi's RPC stdout.
///
/// `event` exposes the canonical discriminated union while `raw` preserves the
/// exact object for logging, forwarding, and newer Pi event types.
#[derive(Clone, Debug, PartialEq)]
pub struct PiMessage {
    event: PiEvent,
    raw: Map<String, Value>,
}

impl PiMessage {
    pub fn event(&self) -> &PiEvent {
        &self.event
    }

    pub fn raw(&self) -> &Map<String, Value> {
        &self.raw
    }

    pub fn kind(&self) -> &str {
        self.raw["type"]
            .as_str()
            .expect("PiMessage construction validates `type`")
    }

    pub fn is_response_to(&self, id: &str, command: &str) -> bool {
        matches!(
            &self.event,
            PiEvent::Response(response)
                if response.id() == Some(id) && response.command() == command
        )
    }
}

impl<'de> Deserialize<'de> for PiMessage {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: Deserializer<'de>,
    {
        let raw = Map::<String, Value>::deserialize(deserializer)?;
        let kind = raw
            .get("type")
            .and_then(Value::as_str)
            .ok_or_else(|| D::Error::custom("Pi message is missing a string `type` field"))?;
        let event = PiEvent::decode(kind, Value::Object(raw.clone())).map_err(D::Error::custom)?;
        Ok(Self { event, raw })
    }
}

impl Serialize for PiMessage {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: Serializer,
    {
        self.raw.serialize(serializer)
    }
}

/// Canonical union emitted by Pi RPC stdout.
#[derive(Clone, Debug, PartialEq)]
pub enum PiEvent {
    Response(RpcResponse),
    AgentStart,
    AgentEnd(AgentEnd),
    AgentSettled,
    TurnStart,
    TurnEnd(TurnEnd),
    MessageStart(Message),
    MessageUpdate(MessageUpdate),
    MessageEnd(Message),
    ToolExecutionStart(ToolExecutionStart),
    ToolExecutionUpdate(ToolExecutionUpdate),
    ToolExecutionEnd(ToolExecutionEnd),
    QueueUpdate(QueueUpdate),
    CompactionStart(CompactionStart),
    CompactionEnd(CompactionEnd),
    EntryAppended(EntryAppended),
    SessionInfoChanged(SessionInfoChanged),
    ThinkingLevelChanged(ThinkingLevelChanged),
    AutoRetryStart(AutoRetryStart),
    AutoRetryEnd(AutoRetryEnd),
    SummarizationRetryScheduled(SummarizationRetryScheduled),
    SummarizationRetryAttemptStart(SummarizationRetrySource),
    SummarizationRetryFinished,
    BashExecutionUpdate(BashExecutionUpdate),
    ExtensionUiRequest(ExtensionUiRequest),
    ExtensionError(ExtensionError),
    Unknown { kind: String },
}

impl PiEvent {
    fn decode(kind: &str, raw: Value) -> serde_json::Result<Self> {
        fn payload<T: for<'de> Deserialize<'de>>(raw: Value) -> serde_json::Result<T> {
            serde_json::from_value(raw)
        }

        Ok(match kind {
            "response" => Self::Response(RpcResponse::decode(raw)?),
            "agent_start" => Self::AgentStart,
            "agent_end" => Self::AgentEnd(payload(raw)?),
            "agent_settled" => Self::AgentSettled,
            "turn_start" => Self::TurnStart,
            "turn_end" => Self::TurnEnd(payload(raw)?),
            "message_start" => Self::MessageStart(payload(raw)?),
            "message_update" => Self::MessageUpdate(payload(raw)?),
            "message_end" => Self::MessageEnd(payload(raw)?),
            "tool_execution_start" => Self::ToolExecutionStart(payload(raw)?),
            "tool_execution_update" => Self::ToolExecutionUpdate(payload(raw)?),
            "tool_execution_end" => Self::ToolExecutionEnd(payload(raw)?),
            "queue_update" => Self::QueueUpdate(payload(raw)?),
            "compaction_start" => Self::CompactionStart(payload(raw)?),
            "compaction_end" => Self::CompactionEnd(payload(raw)?),
            "entry_appended" => Self::EntryAppended(payload(raw)?),
            "session_info_changed" => Self::SessionInfoChanged(payload(raw)?),
            "thinking_level_changed" => Self::ThinkingLevelChanged(payload(raw)?),
            "auto_retry_start" => Self::AutoRetryStart(payload(raw)?),
            "auto_retry_end" => Self::AutoRetryEnd(payload(raw)?),
            "summarization_retry_scheduled" => Self::SummarizationRetryScheduled(payload(raw)?),
            "summarization_retry_attempt_start" => {
                Self::SummarizationRetryAttemptStart(SummarizationRetrySource::decode(raw)?)
            }
            "summarization_retry_finished" => Self::SummarizationRetryFinished,
            "bash_execution_update" => Self::BashExecutionUpdate(payload(raw)?),
            "extension_ui_request" => Self::ExtensionUiRequest(payload(raw)?),
            "extension_error" => Self::ExtensionError(payload(raw)?),
            kind => Self::Unknown {
                kind: kind.to_owned(),
            },
        })
    }
}

#[derive(Clone, Debug, PartialEq)]
pub enum RpcResponse {
    Prompt {
        id: Option<String>,
    },
    Steer {
        id: Option<String>,
    },
    FollowUp {
        id: Option<String>,
    },
    Abort {
        id: Option<String>,
    },
    NewSession {
        id: Option<String>,
        cancelled: bool,
    },
    GetState {
        id: Option<String>,
        state: RpcSessionState,
    },
    SetModel {
        id: Option<String>,
        model: Value,
    },
    CycleModel {
        id: Option<String>,
        result: Option<ModelCycle>,
    },
    GetAvailableModels {
        id: Option<String>,
        models: Vec<Value>,
    },
    SetThinkingLevel {
        id: Option<String>,
    },
    CycleThinkingLevel {
        id: Option<String>,
        level: Option<ThinkingLevel>,
    },
    GetAvailableThinkingLevels {
        id: Option<String>,
        levels: Vec<ThinkingLevel>,
    },
    SetSteeringMode {
        id: Option<String>,
    },
    SetFollowUpMode {
        id: Option<String>,
    },
    Compact {
        id: Option<String>,
        result: Value,
    },
    SetAutoCompaction {
        id: Option<String>,
    },
    SetAutoRetry {
        id: Option<String>,
    },
    AbortRetry {
        id: Option<String>,
    },
    Bash {
        id: Option<String>,
        result: Value,
    },
    AbortBash {
        id: Option<String>,
    },
    GetSessionStats {
        id: Option<String>,
        stats: SessionStats,
    },
    ExportHtml {
        id: Option<String>,
        path: String,
    },
    SwitchSession {
        id: Option<String>,
        cancelled: bool,
    },
    Fork {
        id: Option<String>,
        text: String,
        cancelled: bool,
    },
    Clone {
        id: Option<String>,
        cancelled: bool,
    },
    GetForkMessages {
        id: Option<String>,
        messages: Vec<ForkMessage>,
    },
    GetEntries {
        id: Option<String>,
        entries: Vec<Value>,
        leaf_id: Option<String>,
    },
    GetTree {
        id: Option<String>,
        tree: Vec<Value>,
        leaf_id: Option<String>,
    },
    GetLastAssistantText {
        id: Option<String>,
        text: Option<String>,
    },
    SetSessionName {
        id: Option<String>,
    },
    GetMessages {
        id: Option<String>,
        messages: Vec<Value>,
    },
    GetCommands {
        id: Option<String>,
        commands: Vec<RpcSlashCommand>,
    },
    Error {
        id: Option<String>,
        command: String,
        error: String,
    },
    UnknownSuccess {
        id: Option<String>,
        command: String,
        data: Option<Value>,
    },
}

impl RpcResponse {
    fn decode(raw: Value) -> serde_json::Result<Self> {
        let wire: RpcResponseWire = serde_json::from_value(raw)?;
        if !wire.success {
            return match (wire.data, wire.error) {
                (None, Some(error)) => Ok(Self::Error {
                    id: wire.id,
                    command: wire.command,
                    error,
                }),
                (Some(_), _) => Err(protocol_error("failed response contains `data`")),
                (None, None) => Err(protocol_error("failed response is missing `error`")),
            };
        }
        if wire.error.is_some() {
            return Err(protocol_error("successful response contains `error`"));
        }

        let id = wire.id;
        let data = wire.data;
        Ok(match wire.command.as_str() {
            "prompt" => Self::Prompt {
                id: no_data(id, data)?,
            },
            "steer" => Self::Steer {
                id: no_data(id, data)?,
            },
            "follow_up" => Self::FollowUp {
                id: no_data(id, data)?,
            },
            "abort" => Self::Abort {
                id: no_data(id, data)?,
            },
            "new_session" => {
                let data: CancelledData = required_data(data, "new_session")?;
                Self::NewSession {
                    id,
                    cancelled: data.cancelled,
                }
            }
            "get_state" => Self::GetState {
                id,
                state: required_data(data, "get_state")?,
            },
            "set_model" => Self::SetModel {
                id,
                model: required_value(data, "set_model")?,
            },
            "cycle_model" => Self::CycleModel {
                id,
                result: optional_data(data)?,
            },
            "get_available_models" => {
                let data: ModelsData = required_data(data, "get_available_models")?;
                Self::GetAvailableModels {
                    id,
                    models: data.models,
                }
            }
            "set_thinking_level" => Self::SetThinkingLevel {
                id: no_data(id, data)?,
            },
            "cycle_thinking_level" => {
                let data: Option<ThinkingLevelData> = optional_data(data)?;
                Self::CycleThinkingLevel {
                    id,
                    level: data.map(|data| data.level),
                }
            }
            "get_available_thinking_levels" => {
                let data: ThinkingLevelsData =
                    required_data(data, "get_available_thinking_levels")?;
                Self::GetAvailableThinkingLevels {
                    id,
                    levels: data.levels,
                }
            }
            "set_steering_mode" => Self::SetSteeringMode {
                id: no_data(id, data)?,
            },
            "set_follow_up_mode" => Self::SetFollowUpMode {
                id: no_data(id, data)?,
            },
            "compact" => Self::Compact {
                id,
                result: required_value(data, "compact")?,
            },
            "set_auto_compaction" => Self::SetAutoCompaction {
                id: no_data(id, data)?,
            },
            "set_auto_retry" => Self::SetAutoRetry {
                id: no_data(id, data)?,
            },
            "abort_retry" => Self::AbortRetry {
                id: no_data(id, data)?,
            },
            "bash" => Self::Bash {
                id,
                result: required_value(data, "bash")?,
            },
            "abort_bash" => Self::AbortBash {
                id: no_data(id, data)?,
            },
            "get_session_stats" => Self::GetSessionStats {
                id,
                stats: required_data(data, "get_session_stats")?,
            },
            "export_html" => {
                let data: PathData = required_data(data, "export_html")?;
                Self::ExportHtml {
                    id,
                    path: data.path,
                }
            }
            "switch_session" => {
                let data: CancelledData = required_data(data, "switch_session")?;
                Self::SwitchSession {
                    id,
                    cancelled: data.cancelled,
                }
            }
            "fork" => {
                let data: ForkData = required_data(data, "fork")?;
                Self::Fork {
                    id,
                    text: data.text,
                    cancelled: data.cancelled,
                }
            }
            "clone" => {
                let data: CancelledData = required_data(data, "clone")?;
                Self::Clone {
                    id,
                    cancelled: data.cancelled,
                }
            }
            "get_fork_messages" => {
                let data: ForkMessagesData = required_data(data, "get_fork_messages")?;
                Self::GetForkMessages {
                    id,
                    messages: data.messages,
                }
            }
            "get_entries" => {
                let data: EntriesData = required_data(data, "get_entries")?;
                Self::GetEntries {
                    id,
                    entries: data.entries,
                    leaf_id: data.leaf_id,
                }
            }
            "get_tree" => {
                let data: TreeData = required_data(data, "get_tree")?;
                Self::GetTree {
                    id,
                    tree: data.tree,
                    leaf_id: data.leaf_id,
                }
            }
            "get_last_assistant_text" => {
                let data: TextData = required_data(data, "get_last_assistant_text")?;
                Self::GetLastAssistantText {
                    id,
                    text: data.text,
                }
            }
            "set_session_name" => Self::SetSessionName {
                id: no_data(id, data)?,
            },
            "get_messages" => {
                let data: MessagesData = required_data(data, "get_messages")?;
                Self::GetMessages {
                    id,
                    messages: data.messages,
                }
            }
            "get_commands" => {
                let data: CommandsData = required_data(data, "get_commands")?;
                Self::GetCommands {
                    id,
                    commands: data.commands,
                }
            }
            command => Self::UnknownSuccess {
                id,
                command: command.to_owned(),
                data,
            },
        })
    }

    pub fn id(&self) -> Option<&str> {
        match self {
            Self::Prompt { id }
            | Self::Steer { id }
            | Self::FollowUp { id }
            | Self::Abort { id }
            | Self::NewSession { id, .. }
            | Self::GetState { id, .. }
            | Self::SetModel { id, .. }
            | Self::CycleModel { id, .. }
            | Self::GetAvailableModels { id, .. }
            | Self::SetThinkingLevel { id }
            | Self::CycleThinkingLevel { id, .. }
            | Self::GetAvailableThinkingLevels { id, .. }
            | Self::SetSteeringMode { id }
            | Self::SetFollowUpMode { id }
            | Self::Compact { id, .. }
            | Self::SetAutoCompaction { id }
            | Self::SetAutoRetry { id }
            | Self::AbortRetry { id }
            | Self::Bash { id, .. }
            | Self::AbortBash { id }
            | Self::GetSessionStats { id, .. }
            | Self::ExportHtml { id, .. }
            | Self::SwitchSession { id, .. }
            | Self::Fork { id, .. }
            | Self::Clone { id, .. }
            | Self::GetForkMessages { id, .. }
            | Self::GetEntries { id, .. }
            | Self::GetTree { id, .. }
            | Self::GetLastAssistantText { id, .. }
            | Self::SetSessionName { id }
            | Self::GetMessages { id, .. }
            | Self::GetCommands { id, .. }
            | Self::Error { id, .. }
            | Self::UnknownSuccess { id, .. } => id.as_deref(),
        }
    }

    pub fn command(&self) -> &str {
        match self {
            Self::Prompt { .. } => "prompt",
            Self::Steer { .. } => "steer",
            Self::FollowUp { .. } => "follow_up",
            Self::Abort { .. } => "abort",
            Self::NewSession { .. } => "new_session",
            Self::GetState { .. } => "get_state",
            Self::SetModel { .. } => "set_model",
            Self::CycleModel { .. } => "cycle_model",
            Self::GetAvailableModels { .. } => "get_available_models",
            Self::SetThinkingLevel { .. } => "set_thinking_level",
            Self::CycleThinkingLevel { .. } => "cycle_thinking_level",
            Self::GetAvailableThinkingLevels { .. } => "get_available_thinking_levels",
            Self::SetSteeringMode { .. } => "set_steering_mode",
            Self::SetFollowUpMode { .. } => "set_follow_up_mode",
            Self::Compact { .. } => "compact",
            Self::SetAutoCompaction { .. } => "set_auto_compaction",
            Self::SetAutoRetry { .. } => "set_auto_retry",
            Self::AbortRetry { .. } => "abort_retry",
            Self::Bash { .. } => "bash",
            Self::AbortBash { .. } => "abort_bash",
            Self::GetSessionStats { .. } => "get_session_stats",
            Self::ExportHtml { .. } => "export_html",
            Self::SwitchSession { .. } => "switch_session",
            Self::Fork { .. } => "fork",
            Self::Clone { .. } => "clone",
            Self::GetForkMessages { .. } => "get_fork_messages",
            Self::GetEntries { .. } => "get_entries",
            Self::GetTree { .. } => "get_tree",
            Self::GetLastAssistantText { .. } => "get_last_assistant_text",
            Self::SetSessionName { .. } => "set_session_name",
            Self::GetMessages { .. } => "get_messages",
            Self::GetCommands { .. } => "get_commands",
            Self::Error { command, .. } | Self::UnknownSuccess { command, .. } => command,
        }
    }
}

#[derive(Deserialize)]
struct RpcResponseWire {
    id: Option<String>,
    command: String,
    success: bool,
    data: Option<Value>,
    error: Option<String>,
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct RpcSessionState {
    pub model: Option<Value>,
    pub thinking_level: ThinkingLevel,
    pub is_streaming: bool,
    pub is_compacting: bool,
    pub steering_mode: QueueMode,
    pub follow_up_mode: QueueMode,
    pub session_file: Option<String>,
    pub session_id: String,
    pub session_name: Option<String>,
    pub auto_compaction_enabled: bool,
    pub message_count: u64,
    pub pending_message_count: u64,
}

#[derive(Clone, Copy, Debug, Deserialize, Serialize, PartialEq, Eq)]
pub enum QueueMode {
    #[serde(rename = "all")]
    All,
    #[serde(rename = "one-at-a-time")]
    OneAtATime,
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct ModelCycle {
    pub model: Value,
    pub thinking_level: ThinkingLevel,
    pub is_scoped: bool,
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct SessionStats {
    pub session_file: Option<String>,
    pub session_id: String,
    pub user_messages: u64,
    pub assistant_messages: u64,
    pub tool_calls: u64,
    pub tool_results: u64,
    pub total_messages: u64,
    pub tokens: TokenUsage,
    pub cost: f64,
    pub context_usage: Option<Value>,
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct TokenUsage {
    pub input: u64,
    pub output: u64,
    pub cache_read: u64,
    pub cache_write: u64,
    pub total: u64,
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct ForkMessage {
    pub entry_id: String,
    pub text: String,
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct RpcSlashCommand {
    pub name: String,
    pub description: Option<String>,
    pub source: RpcSlashCommandSource,
    pub source_info: Value,
}

#[derive(Clone, Copy, Debug, Deserialize, Serialize, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
pub enum RpcSlashCommandSource {
    Extension,
    Prompt,
    Skill,
}

#[derive(Deserialize)]
struct CancelledData {
    cancelled: bool,
}

#[derive(Deserialize)]
struct ModelsData {
    models: Vec<Value>,
}

#[derive(Deserialize)]
struct ThinkingLevelData {
    level: ThinkingLevel,
}

#[derive(Deserialize)]
struct ThinkingLevelsData {
    levels: Vec<ThinkingLevel>,
}

#[derive(Deserialize)]
struct PathData {
    path: String,
}

#[derive(Deserialize)]
struct ForkData {
    text: String,
    cancelled: bool,
}

#[derive(Deserialize)]
struct ForkMessagesData {
    messages: Vec<ForkMessage>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct EntriesData {
    entries: Vec<Value>,
    leaf_id: Option<String>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct TreeData {
    tree: Vec<Value>,
    leaf_id: Option<String>,
}

#[derive(Deserialize)]
struct TextData {
    text: Option<String>,
}

#[derive(Deserialize)]
struct MessagesData {
    messages: Vec<Value>,
}

#[derive(Deserialize)]
struct CommandsData {
    commands: Vec<RpcSlashCommand>,
}

fn no_data(id: Option<String>, data: Option<Value>) -> serde_json::Result<Option<String>> {
    if data.is_some() {
        return Err(protocol_error("response unexpectedly contains `data`"));
    }
    Ok(id)
}

fn required_value(data: Option<Value>, command: &str) -> serde_json::Result<Value> {
    data.ok_or_else(|| protocol_error(&format!("{command} response is missing `data`")))
}

fn required_data<T: for<'de> Deserialize<'de>>(
    data: Option<Value>,
    command: &str,
) -> serde_json::Result<T> {
    serde_json::from_value(required_value(data, command)?)
}

fn optional_data<T: for<'de> Deserialize<'de>>(
    data: Option<Value>,
) -> serde_json::Result<Option<T>> {
    data.map(serde_json::from_value).transpose()
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct AgentEnd {
    pub messages: Vec<Value>,
    pub will_retry: bool,
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct TurnEnd {
    pub message: Value,
    pub tool_results: Vec<Value>,
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
pub struct Message {
    pub message: Value,
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct MessageUpdate {
    pub assistant_message_event: AssistantMessageEvent,
}

/// Pi's provider-independent streaming delta union after cumulative `partial`
/// snapshots are removed for JSON/RPC output.
#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum AssistantMessageEvent {
    Start,
    TextStart {
        #[serde(rename = "contentIndex")]
        content_index: usize,
    },
    TextDelta {
        #[serde(rename = "contentIndex")]
        content_index: usize,
        delta: String,
    },
    TextEnd {
        #[serde(rename = "contentIndex")]
        content_index: usize,
        content: String,
    },
    ThinkingStart {
        #[serde(rename = "contentIndex")]
        content_index: usize,
    },
    ThinkingDelta {
        #[serde(rename = "contentIndex")]
        content_index: usize,
        delta: String,
    },
    ThinkingEnd {
        #[serde(rename = "contentIndex")]
        content_index: usize,
        content: String,
    },
    ToolcallStart {
        #[serde(rename = "contentIndex")]
        content_index: usize,
    },
    ToolcallDelta {
        #[serde(rename = "contentIndex")]
        content_index: usize,
        delta: String,
    },
    ToolcallEnd {
        #[serde(rename = "contentIndex")]
        content_index: usize,
        #[serde(rename = "toolCall")]
        tool_call: ToolCall,
    },
    Done {
        reason: SuccessfulStopReason,
        message: Value,
    },
    Error {
        reason: ErrorStopReason,
        error: Value,
    },
    #[serde(other)]
    Unknown,
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct ToolCall {
    pub id: String,
    pub name: String,
    pub arguments: Map<String, Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub thought_signature: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub namespace: Option<String>,
}

#[derive(Clone, Copy, Debug, Deserialize, Serialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub enum SuccessfulStopReason {
    Stop,
    Length,
    ToolUse,
    Deferred,
}

#[derive(Clone, Copy, Debug, Deserialize, Serialize, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
pub enum ErrorStopReason {
    Aborted,
    Error,
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct ToolExecutionStart {
    pub tool_call_id: String,
    pub tool_name: String,
    pub args: Value,
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct ToolExecutionUpdate {
    pub tool_call_id: String,
    pub tool_name: String,
    pub args: Value,
    pub partial_result: Value,
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct ToolExecutionEnd {
    pub tool_call_id: String,
    pub tool_name: String,
    pub result: Value,
    pub is_error: bool,
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct QueueUpdate {
    pub steering: Vec<String>,
    pub follow_up: Vec<String>,
}

#[derive(Clone, Copy, Debug, Deserialize, Serialize, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
pub enum CompactionReason {
    Manual,
    Threshold,
    Overflow,
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
pub struct CompactionStart {
    pub reason: CompactionReason,
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct CompactionEnd {
    pub reason: CompactionReason,
    pub result: Option<Value>,
    pub aborted: bool,
    pub will_retry: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error_message: Option<String>,
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
pub struct EntryAppended {
    pub entry: Value,
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
pub struct SessionInfoChanged {
    pub name: Option<String>,
}

#[derive(Clone, Copy, Debug, Deserialize, Serialize, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
pub enum ThinkingLevel {
    Off,
    Minimal,
    Low,
    Medium,
    High,
    Xhigh,
    Max,
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
pub struct ThinkingLevelChanged {
    pub level: ThinkingLevel,
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct AutoRetryStart {
    pub attempt: u32,
    pub max_attempts: u32,
    pub delay_ms: u64,
    pub error_message: String,
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct AutoRetryEnd {
    pub success: bool,
    pub attempt: u32,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub final_error: Option<String>,
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct SummarizationRetryScheduled {
    pub attempt: u32,
    pub max_attempts: u32,
    pub delay_ms: u64,
    pub error_message: String,
}

#[derive(Clone, Debug, PartialEq)]
pub enum SummarizationRetrySource {
    BranchSummary,
    Compaction { reason: CompactionReason },
}

impl SummarizationRetrySource {
    fn decode(raw: Value) -> serde_json::Result<Self> {
        let wire: SummarizationRetrySourceWire = serde_json::from_value(raw)?;
        match (wire.source.as_str(), wire.reason) {
            ("branchSummary", None) => Ok(Self::BranchSummary),
            ("compaction", Some(reason)) => Ok(Self::Compaction { reason }),
            ("branchSummary", Some(_)) => {
                Err(protocol_error("branchSummary retry contains `reason`"))
            }
            ("compaction", None) => Err(protocol_error("compaction retry is missing `reason`")),
            _ => Err(protocol_error("unknown summarization retry `source`")),
        }
    }
}

#[derive(Deserialize)]
struct SummarizationRetrySourceWire {
    source: String,
    reason: Option<CompactionReason>,
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
pub struct BashExecutionUpdate {
    pub id: Option<String>,
    pub delta: String,
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(tag = "method")]
pub enum ExtensionUiRequest {
    #[serde(rename = "select")]
    Select {
        id: String,
        title: String,
        options: Vec<String>,
        timeout: Option<u64>,
    },
    #[serde(rename = "confirm")]
    Confirm {
        id: String,
        title: String,
        message: String,
        timeout: Option<u64>,
    },
    #[serde(rename = "input")]
    Input {
        id: String,
        title: String,
        placeholder: Option<String>,
        timeout: Option<u64>,
    },
    #[serde(rename = "editor")]
    Editor {
        id: String,
        title: String,
        prefill: Option<String>,
    },
    #[serde(rename = "notify")]
    Notify {
        id: String,
        message: String,
        #[serde(rename = "notifyType")]
        notify_type: Option<NotifyType>,
    },
    #[serde(rename = "setStatus")]
    SetStatus {
        id: String,
        #[serde(rename = "statusKey")]
        status_key: String,
        #[serde(rename = "statusText")]
        status_text: Option<String>,
    },
    #[serde(rename = "setWidget")]
    SetWidget {
        id: String,
        #[serde(rename = "widgetKey")]
        widget_key: String,
        #[serde(rename = "widgetLines")]
        widget_lines: Option<Vec<String>>,
        #[serde(rename = "widgetPlacement")]
        widget_placement: Option<WidgetPlacement>,
    },
    #[serde(rename = "setTitle")]
    SetTitle { id: String, title: String },
    #[serde(rename = "set_editor_text")]
    SetEditorText { id: String, text: String },
    #[serde(other)]
    Unknown,
}

#[derive(Clone, Copy, Debug, Deserialize, Serialize, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
pub enum NotifyType {
    Info,
    Warning,
    Error,
}

#[derive(Clone, Copy, Debug, Deserialize, Serialize, PartialEq, Eq)]
pub enum WidgetPlacement {
    #[serde(rename = "aboveEditor")]
    AboveEditor,
    #[serde(rename = "belowEditor")]
    BelowEditor,
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct ExtensionError {
    pub extension_path: String,
    pub event: String,
    pub error: String,
}

fn protocol_error(message: &str) -> serde_json::Error {
    <serde_json::Error as serde::de::Error>::custom(message)
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn parse(value: Value) -> PiMessage {
        serde_json::from_value(value).unwrap()
    }

    #[test]
    fn response_success_and_failure_are_distinct_shapes() {
        let success = parse(json!({
            "id": "state",
            "type": "response",
            "command": "get_state",
            "success": true,
            "data": {
                "thinkingLevel": "off",
                "isStreaming": false,
                "isCompacting": false,
                "steeringMode": "all",
                "followUpMode": "all",
                "sessionId": "one",
                "autoCompactionEnabled": true,
                "messageCount": 0,
                "pendingMessageCount": 0
            }
        }));
        let failure = parse(json!({
            "id": "state",
            "type": "response",
            "command": "get_state",
            "success": false,
            "error": "no model"
        }));

        assert!(matches!(
            success.event(),
            PiEvent::Response(RpcResponse::GetState { .. })
        ));
        assert!(matches!(
            failure.event(),
            PiEvent::Response(RpcResponse::Error { .. })
        ));
        assert!(success.is_response_to("state", "get_state"));
    }

    #[test]
    fn response_rejects_impossible_success_error_combination() {
        let result = serde_json::from_value::<PiMessage>(json!({
            "type": "response",
            "command": "get_state",
            "success": true,
            "error": "contradiction"
        }));

        assert!(result.unwrap_err().to_string().contains("contains `error`"));
    }

    #[test]
    fn every_canonical_success_response_has_a_distinct_variant() {
        let state = json!({
            "thinkingLevel": "off",
            "isStreaming": false,
            "isCompacting": false,
            "steeringMode": "all",
            "followUpMode": "one-at-a-time",
            "sessionId": "session",
            "autoCompactionEnabled": true,
            "messageCount": 0,
            "pendingMessageCount": 0
        });
        let stats = json!({
            "sessionId": "session",
            "userMessages": 0,
            "assistantMessages": 0,
            "toolCalls": 0,
            "toolResults": 0,
            "totalMessages": 0,
            "tokens": {"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0, "total": 0},
            "cost": 0.0
        });
        let cases = [
            ("prompt", None),
            ("steer", None),
            ("follow_up", None),
            ("abort", None),
            ("new_session", Some(json!({"cancelled": false}))),
            ("get_state", Some(state)),
            ("set_model", Some(json!({}))),
            (
                "cycle_model",
                Some(json!({"model": {}, "thinkingLevel": "high", "isScoped": true})),
            ),
            ("get_available_models", Some(json!({"models": []}))),
            ("set_thinking_level", None),
            ("cycle_thinking_level", Some(json!({"level": "medium"}))),
            (
                "get_available_thinking_levels",
                Some(json!({"levels": ["off", "max"]})),
            ),
            ("set_steering_mode", None),
            ("set_follow_up_mode", None),
            ("compact", Some(json!({}))),
            ("set_auto_compaction", None),
            ("set_auto_retry", None),
            ("abort_retry", None),
            ("bash", Some(json!({}))),
            ("abort_bash", None),
            ("get_session_stats", Some(stats)),
            ("export_html", Some(json!({"path": "/tmp/session.html"}))),
            ("switch_session", Some(json!({"cancelled": false}))),
            ("fork", Some(json!({"text": "branch", "cancelled": false}))),
            ("clone", Some(json!({"cancelled": false}))),
            ("get_fork_messages", Some(json!({"messages": []}))),
            ("get_entries", Some(json!({"entries": [], "leafId": null}))),
            ("get_tree", Some(json!({"tree": [], "leafId": null}))),
            ("get_last_assistant_text", Some(json!({"text": null}))),
            ("set_session_name", None),
            ("get_messages", Some(json!({"messages": []}))),
            ("get_commands", Some(json!({"commands": []}))),
        ];

        for (command, data) in cases {
            let mut value = json!({
                "id": "request",
                "type": "response",
                "command": command,
                "success": true
            });
            if let Some(data) = data {
                value["data"] = data;
            }
            let message = parse(value);
            let PiEvent::Response(response) = message.event() else {
                panic!("{command} did not decode as a response");
            };
            assert_eq!(response.command(), command);
            assert_eq!(response.id(), Some("request"));
            assert!(!matches!(response, RpcResponse::UnknownSuccess { .. }));
        }
    }

    #[test]
    fn message_update_uses_the_json_delta_shape_without_partial_snapshot() {
        let message = parse(json!({
            "type": "message_update",
            "assistantMessageEvent": {
                "type": "text_delta",
                "contentIndex": 0,
                "delta": "hello"
            }
        }));

        assert!(matches!(
            message.event(),
            PiEvent::MessageUpdate(MessageUpdate {
                assistant_message_event: AssistantMessageEvent::TextDelta {
                    content_index: 0,
                    delta
                }
            }) if delta == "hello"
        ));
    }

    #[test]
    fn extension_ui_method_is_a_nested_union() {
        let message = parse(json!({
            "type": "extension_ui_request",
            "id": "ui-1",
            "method": "setWidget",
            "widgetKey": "status",
            "widgetLines": ["working"],
            "widgetPlacement": "belowEditor"
        }));

        assert!(matches!(
            message.event(),
            PiEvent::ExtensionUiRequest(ExtensionUiRequest::SetWidget {
                widget_placement: Some(WidgetPlacement::BelowEditor),
                ..
            })
        ));
    }

    #[test]
    fn unknown_event_round_trips_without_losing_fields() {
        let value = json!({"type": "future_event", "nested": {"answer": 42}});
        let message = parse(value.clone());

        assert!(matches!(
            message.event(),
            PiEvent::Unknown { kind } if kind == "future_event"
        ));
        assert_eq!(serde_json::to_value(message).unwrap(), value);
    }

    #[test]
    fn representative_session_events_decode_to_distinct_variants() {
        let cases = [
            (json!({"type": "agent_start"}), "agent_start"),
            (
                json!({"type": "agent_end", "messages": [], "willRetry": false}),
                "agent_end",
            ),
            (json!({"type": "agent_settled"}), "agent_settled"),
            (
                json!({"type": "queue_update", "steering": [], "followUp": []}),
                "queue_update",
            ),
            (
                json!({"type": "compaction_start", "reason": "threshold"}),
                "compaction_start",
            ),
            (
                json!({"type": "bash_execution_update", "id": "b-1", "delta": "ok"}),
                "bash_execution_update",
            ),
        ];

        for (value, expected) in cases {
            let message = parse(value);
            let actual = match message.event() {
                PiEvent::AgentStart => "agent_start",
                PiEvent::AgentEnd(_) => "agent_end",
                PiEvent::AgentSettled => "agent_settled",
                PiEvent::QueueUpdate(_) => "queue_update",
                PiEvent::CompactionStart(_) => "compaction_start",
                PiEvent::BashExecutionUpdate(_) => "bash_execution_update",
                event => panic!("unexpected event: {event:?}"),
            };
            assert_eq!(actual, expected);
        }
    }
}
