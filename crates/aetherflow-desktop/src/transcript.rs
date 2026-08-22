use aetherflow_pi::{PiEvent, SessionEvent, SessionEventPayload};
use base64::Engine as _;
use gpui::{Image, ImageFormat};
use serde_json::{Value, json};
use std::sync::Arc;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ConversationRole {
    User,
    Assistant,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ConversationImage {
    pub mime_type: String,
    pub image: Arc<Image>,
}

impl ConversationImage {
    pub fn new(mime_type: impl Into<String>, data: Vec<u8>) -> Option<Self> {
        let mime_type = mime_type.into();
        let format = ImageFormat::from_mime_type(&mime_type)?;
        Some(Self {
            mime_type,
            image: Arc::new(Image::from_bytes(format, data)),
        })
    }

    pub fn data(&self) -> &[u8] {
        &self.image.bytes
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ConversationMessage {
    pub role: ConversationRole,
    pub text: String,
    pub images: Vec<ConversationImage>,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ToolStatus {
    Running,
    Succeeded,
    Failed,
}

#[derive(Clone, Debug, PartialEq)]
pub struct ToolCallView {
    pub id: String,
    pub name: String,
    pub args: Value,
    pub status: ToolStatus,
    pub output: Option<Value>,
}

#[derive(Clone, Debug, PartialEq)]
pub struct ToolGroup {
    pub calls: Vec<ToolCallView>,
}

#[derive(Clone, Debug, PartialEq)]
pub enum ConversationItem {
    Message(ConversationMessage),
    ToolGroup(ToolGroup),
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SubagentRunView {
    pub agent: String,
    pub task: String,
    pub status: ToolStatus,
    pub output: Option<String>,
}

impl ToolGroup {
    pub fn key(&self) -> &str {
        self.calls
            .first()
            .map(|call| call.id.as_str())
            .unwrap_or("empty-tool-group")
    }

    pub fn summary(&self) -> String {
        let running = self
            .calls
            .iter()
            .filter(|call| call.status == ToolStatus::Running)
            .count();
        let failed = self
            .calls
            .iter()
            .filter(|call| call.status == ToolStatus::Failed)
            .count();
        let noun = if self.calls.len() == 1 {
            "tool"
        } else {
            "tools"
        };
        let mut summary = if running > 0 {
            format!("Using {} {noun}", self.calls.len())
        } else {
            format!("Used {} {noun}", self.calls.len())
        };
        if failed > 0 {
            summary.push_str(&format!(" · {failed} failed"));
        }
        summary
    }
}

impl ToolCallView {
    pub fn label(&self) -> String {
        if self.name == "subagent" {
            return self.subagent_label();
        }

        match self.name.as_str() {
            "read" => value_string(&self.args, &["path", "file_path"])
                .map(|path| format!("Read · {path}"))
                .unwrap_or_else(|| "Read".to_owned()),
            "bash" | "exec" => value_string(&self.args, &["command", "cmd"])
                .map(|command| format!("Shell · {}", first_line(&command)))
                .unwrap_or_else(|| "Shell".to_owned()),
            "grep" | "search" => value_string(&self.args, &["pattern", "query"])
                .map(|pattern| format!("Searched · {pattern}"))
                .unwrap_or_else(|| "Searched".to_owned()),
            "write" => value_string(&self.args, &["path", "file_path"])
                .map(|path| format!("Wrote · {path}"))
                .unwrap_or_else(|| "Wrote file".to_owned()),
            "edit" => value_string(&self.args, &["path", "file_path"])
                .map(|path| format!("Edited · {path}"))
                .unwrap_or_else(|| "Edited file".to_owned()),
            _ => self.name.clone(),
        }
    }

    pub fn invocation_text(&self) -> String {
        serde_json::to_string_pretty(&self.args).unwrap_or_else(|_| self.args.to_string())
    }

    pub fn output_text(&self) -> Option<String> {
        let output = self.output.as_ref()?;
        tool_result_text(output).or_else(|| {
            Some(serde_json::to_string_pretty(output).unwrap_or_else(|_| output.to_string()))
        })
    }

    pub fn subagent_runs(&self) -> Vec<SubagentRunView> {
        if self.name != "subagent" {
            return Vec::new();
        }
        let mut runs = self
            .output
            .as_ref()
            .and_then(|output| output.get("details"))
            .and_then(|details| details.get("results"))
            .and_then(Value::as_array)
            .into_iter()
            .flatten()
            .map(|result| {
                let exit_code = result.get("exitCode").and_then(Value::as_i64);
                let status = match exit_code {
                    Some(-1) | None => ToolStatus::Running,
                    Some(0) => ToolStatus::Succeeded,
                    Some(_) => ToolStatus::Failed,
                };
                SubagentRunView {
                    agent: result
                        .get("agent")
                        .and_then(Value::as_str)
                        .unwrap_or("Subagent")
                        .to_owned(),
                    task: result
                        .get("task")
                        .and_then(Value::as_str)
                        .unwrap_or_default()
                        .to_owned(),
                    status,
                    output: final_assistant_text(result.get("messages")),
                }
            })
            .collect::<Vec<_>>();
        if self.status == ToolStatus::Running
            && !runs.iter().any(|run| run.status == ToolStatus::Running)
            && let Some(run) = runs.last_mut()
        {
            run.status = ToolStatus::Running;
        }
        runs
    }

    fn subagent_label(&self) -> String {
        if let Some(tasks) = self.args.get("tasks").and_then(Value::as_array) {
            return format!("{} subagents", tasks.len());
        }
        if let Some(chain) = self.args.get("chain").and_then(Value::as_array) {
            return format!("Subagent chain · {} steps", chain.len());
        }

        let agent = value_string(&self.args, &["agent"]).unwrap_or_else(|| "Subagent".to_owned());
        value_string(&self.args, &["task"])
            .filter(|task| !task.is_empty())
            .map(|task| format!("{agent} · {}", first_line(&task)))
            .unwrap_or(agent)
    }
}

pub fn conversation_from_events(events: &[SessionEvent]) -> Vec<ConversationItem> {
    let mut items = Vec::new();
    for event in events {
        apply_conversation_event(&mut items, event);
    }
    items
}

pub fn apply_conversation_event(items: &mut Vec<ConversationItem>, event: &SessionEvent) {
    let SessionEventPayload::Pi { message } = &event.payload else {
        return;
    };
    match message.event() {
        PiEvent::MessageUpdate(update) => {
            if let aetherflow_pi::AssistantMessageEvent::TextDelta { delta, .. } =
                &update.assistant_message_event
            {
                append_assistant_delta(items, delta);
            }
        }
        PiEvent::MessageEnd(completed) => {
            let Some(completed) = completed_conversation_message(&completed.message) else {
                return;
            };
            if let [
                ..,
                ConversationItem::Message(current),
                ConversationItem::Message(placeholder),
            ] = items.as_slice()
                && completed.role == ConversationRole::User
                && *current == completed
                && placeholder.role == ConversationRole::Assistant
                && placeholder.text.is_empty()
                && placeholder.images.is_empty()
            {
                return;
            }
            match items.last_mut() {
                Some(ConversationItem::Message(current))
                    if current.role == ConversationRole::Assistant
                        && completed.role == ConversationRole::Assistant =>
                {
                    *current = completed;
                }
                Some(ConversationItem::Message(current)) if *current == completed => {}
                _ => items.push(ConversationItem::Message(completed)),
            }
        }
        event => apply_tool_event(items, event),
    }
}

pub fn completed_user_message(event: &PiEvent) -> Option<ConversationMessage> {
    let PiEvent::MessageEnd(completed) = event else {
        return None;
    };
    let message = completed_conversation_message(&completed.message)?;
    (message.role == ConversationRole::User).then_some(message)
}

pub fn append_assistant_delta(items: &mut Vec<ConversationItem>, delta: &str) {
    if let Some(ConversationItem::Message(message)) = items.last_mut()
        && message.role == ConversationRole::Assistant
    {
        message.text.push_str(delta);
    } else {
        items.push(ConversationItem::Message(ConversationMessage {
            role: ConversationRole::Assistant,
            text: delta.to_owned(),
            images: Vec::new(),
        }));
    }
}

pub fn apply_tool_event(items: &mut Vec<ConversationItem>, event: &PiEvent) {
    match event {
        PiEvent::ToolExecutionStart(start) => {
            if let Some(call) = find_tool_mut(items, &start.tool_call_id) {
                call.name.clone_from(&start.tool_name);
                call.args.clone_from(&start.args);
                call.status = ToolStatus::Running;
                call.output = None;
                return;
            }
            remove_empty_trailing_assistant(items);
            push_tool(
                items,
                ToolCallView {
                    id: start.tool_call_id.clone(),
                    name: start.tool_name.clone(),
                    args: start.args.clone(),
                    status: ToolStatus::Running,
                    output: None,
                },
            );
        }
        PiEvent::ToolExecutionUpdate(update) => {
            if let Some(call) = find_tool_mut(items, &update.tool_call_id) {
                call.name.clone_from(&update.tool_name);
                call.args.clone_from(&update.args);
                call.status = ToolStatus::Running;
                call.output = Some(update.partial_result.clone());
                return;
            }
            remove_empty_trailing_assistant(items);
            push_tool(
                items,
                ToolCallView {
                    id: update.tool_call_id.clone(),
                    name: update.tool_name.clone(),
                    args: update.args.clone(),
                    status: ToolStatus::Running,
                    output: Some(update.partial_result.clone()),
                },
            );
        }
        PiEvent::ToolExecutionEnd(end) => {
            if let Some(call) = find_tool_mut(items, &end.tool_call_id) {
                call.name.clone_from(&end.tool_name);
                call.status = if end.is_error {
                    ToolStatus::Failed
                } else {
                    ToolStatus::Succeeded
                };
                call.output = Some(end.result.clone());
                return;
            }
            remove_empty_trailing_assistant(items);
            push_tool(
                items,
                ToolCallView {
                    id: end.tool_call_id.clone(),
                    name: end.tool_name.clone(),
                    args: json!({}),
                    status: if end.is_error {
                        ToolStatus::Failed
                    } else {
                        ToolStatus::Succeeded
                    },
                    output: Some(end.result.clone()),
                },
            );
        }
        _ => {}
    }
}

fn remove_empty_trailing_assistant(items: &mut Vec<ConversationItem>) {
    if matches!(
        items.last(),
        Some(ConversationItem::Message(ConversationMessage {
            role: ConversationRole::Assistant,
            text,
            images,
        })) if text.is_empty() && images.is_empty()
    ) {
        items.pop();
    }
}

fn push_tool(items: &mut Vec<ConversationItem>, call: ToolCallView) {
    if let Some(ConversationItem::ToolGroup(group)) = items.last_mut() {
        group.calls.push(call);
    } else {
        items.push(ConversationItem::ToolGroup(ToolGroup { calls: vec![call] }));
    }
}

fn find_tool_mut<'a>(
    items: &'a mut [ConversationItem],
    tool_call_id: &str,
) -> Option<&'a mut ToolCallView> {
    items.iter_mut().rev().find_map(|item| {
        let ConversationItem::ToolGroup(group) = item else {
            return None;
        };
        group.calls.iter_mut().find(|call| call.id == tool_call_id)
    })
}

fn completed_conversation_message(message: &Value) -> Option<ConversationMessage> {
    let role = match message.get("role")?.as_str()? {
        "user" => ConversationRole::User,
        "assistant" => ConversationRole::Assistant,
        _ => return None,
    };
    let content = message.get("content")?.as_array()?;
    let text = content
        .iter()
        .filter(|content| content.get("type").and_then(Value::as_str) == Some("text"))
        .filter_map(|content| content.get("text").and_then(Value::as_str))
        .collect::<String>();
    let images = content
        .iter()
        .filter(|content| content.get("type").and_then(Value::as_str) == Some("image"))
        .filter_map(|content| {
            let mime_type = content.get("mimeType")?.as_str()?.to_owned();
            let data = content.get("data")?.as_str()?;
            let data = base64::engine::general_purpose::STANDARD
                .decode(data)
                .ok()?;
            ConversationImage::new(mime_type, data)
        })
        .collect::<Vec<_>>();
    if text.is_empty() && images.is_empty() {
        return None;
    }

    Some(ConversationMessage { role, text, images })
}

fn value_string(value: &Value, keys: &[&str]) -> Option<String> {
    keys.iter()
        .find_map(|key| value.get(key).and_then(Value::as_str))
        .map(str::to_owned)
}

fn first_line(value: &str) -> String {
    value.lines().next().unwrap_or_default().trim().to_owned()
}

fn tool_result_text(result: &Value) -> Option<String> {
    let content = result.get("content")?.as_array()?;
    let text = content
        .iter()
        .filter(|part| part.get("type").and_then(Value::as_str) == Some("text"))
        .filter_map(|part| part.get("text").and_then(Value::as_str))
        .collect::<Vec<_>>()
        .join("\n");
    (!text.is_empty()).then_some(text)
}

fn final_assistant_text(messages: Option<&Value>) -> Option<String> {
    messages?
        .as_array()?
        .iter()
        .rev()
        .find(|message| message.get("role").and_then(Value::as_str) == Some("assistant"))
        .and_then(|message| message.get("content"))
        .and_then(Value::as_array)
        .map(|content| {
            content
                .iter()
                .filter(|part| part.get("type").and_then(Value::as_str) == Some("text"))
                .filter_map(|part| part.get("text").and_then(Value::as_str))
                .collect::<String>()
        })
        .filter(|text| !text.is_empty())
}

#[cfg(test)]
mod tests {
    use super::*;
    use aetherflow_pi::PiMessage;
    use aetherflow_storage::{AgentId, SessionAssociation, SessionId};
    use serde_json::json;

    fn session_event(sequence: u64, message: Value) -> SessionEvent {
        SessionEvent {
            sequence,
            session_id: SessionId::new(),
            agent_id: AgentId::new(),
            association: SessionAssociation::Standalone,
            payload: SessionEventPayload::Pi {
                message: Box::new(
                    serde_json::from_value::<PiMessage>(message).expect("valid Pi event"),
                ),
            },
        }
    }

    #[test]
    fn completed_user_event_does_not_duplicate_an_optimistic_prompt() {
        let user = ConversationMessage {
            role: ConversationRole::User,
            text: "Inspect it".to_owned(),
            images: Vec::new(),
        };
        let mut items = vec![
            ConversationItem::Message(user.clone()),
            ConversationItem::Message(ConversationMessage {
                role: ConversationRole::Assistant,
                text: String::new(),
                images: Vec::new(),
            }),
        ];
        let event = session_event(
            1,
            json!({
                "type": "message_end",
                "message": {
                    "role": "user",
                    "content": [{ "type": "text", "text": "Inspect it" }]
                }
            }),
        );

        apply_conversation_event(&mut items, &event);

        assert_eq!(items.len(), 2);
        assert_eq!(items[0], ConversationItem::Message(user));
    }

    #[test]
    fn rebuilds_messages_and_grouped_tool_calls_in_event_order() {
        let events = [
            session_event(
                1,
                json!({
                    "type": "message_end",
                    "message": { "role": "user", "content": [{ "type": "text", "text": "Inspect it" }] }
                }),
            ),
            session_event(
                2,
                json!({
                    "type": "tool_execution_start",
                    "toolCallId": "call-1",
                    "toolName": "read",
                    "args": { "path": "src/main.rs" }
                }),
            ),
            session_event(
                3,
                json!({
                    "type": "tool_execution_update",
                    "toolCallId": "call-1",
                    "toolName": "read",
                    "args": { "path": "src/main.rs" },
                    "partialResult": { "content": [{ "type": "text", "text": "partial" }] }
                }),
            ),
            session_event(
                4,
                json!({
                    "type": "tool_execution_start",
                    "toolCallId": "call-2",
                    "toolName": "grep",
                    "args": { "pattern": "main" }
                }),
            ),
            session_event(
                5,
                json!({
                    "type": "tool_execution_end",
                    "toolCallId": "call-1",
                    "toolName": "read",
                    "result": { "content": [{ "type": "text", "text": "complete" }] },
                    "isError": false
                }),
            ),
            session_event(
                6,
                json!({
                    "type": "tool_execution_end",
                    "toolCallId": "call-2",
                    "toolName": "grep",
                    "result": { "content": [{ "type": "text", "text": "src/main.rs:1" }] },
                    "isError": true
                }),
            ),
            session_event(
                7,
                json!({
                    "type": "message_end",
                    "message": { "role": "assistant", "content": [{ "type": "text", "text": "Done" }] }
                }),
            ),
        ];

        let items = conversation_from_events(&events);

        assert_eq!(items.len(), 3);
        let ConversationItem::ToolGroup(group) = &items[1] else {
            panic!("tool group expected");
        };
        assert_eq!(group.calls.len(), 2);
        assert_eq!(group.calls[0].status, ToolStatus::Succeeded);
        assert_eq!(group.calls[0].output_text().as_deref(), Some("complete"));
        assert_eq!(group.calls[1].status, ToolStatus::Failed);
    }

    #[test]
    fn rebuilds_image_attachments_from_completed_user_messages() {
        let events = [session_event(
            1,
            json!({
                "type": "message_end",
                "message": {
                    "role": "user",
                    "content": [
                        { "type": "text", "text": "What is this?" },
                        { "type": "image", "mimeType": "image/png", "data": "AQID" }
                    ]
                }
            }),
        )];

        let items = conversation_from_events(&events);
        let ConversationItem::Message(message) = &items[0] else {
            panic!("message expected");
        };
        assert_eq!(message.text, "What is this?");
        assert_eq!(
            message.images,
            vec![ConversationImage::new("image/png", vec![1, 2, 3]).expect("supported image")]
        );
    }

    #[test]
    fn partial_tool_results_replace_instead_of_append() {
        let mut items = vec![ConversationItem::Message(ConversationMessage {
            role: ConversationRole::Assistant,
            text: String::new(),
            images: Vec::new(),
        })];
        let start = serde_json::from_value::<PiMessage>(json!({
            "type": "tool_execution_start",
            "toolCallId": "call-1",
            "toolName": "bash",
            "args": { "command": "cargo test" }
        }))
        .unwrap();
        let first = serde_json::from_value::<PiMessage>(json!({
            "type": "tool_execution_update",
            "toolCallId": "call-1",
            "toolName": "bash",
            "args": { "command": "cargo test" },
            "partialResult": { "content": [{ "type": "text", "text": "one" }] }
        }))
        .unwrap();
        let second = serde_json::from_value::<PiMessage>(json!({
            "type": "tool_execution_update",
            "toolCallId": "call-1",
            "toolName": "bash",
            "args": { "command": "cargo test" },
            "partialResult": { "content": [{ "type": "text", "text": "one two" }] }
        }))
        .unwrap();

        apply_tool_event(&mut items, start.event());
        apply_tool_event(&mut items, first.event());
        apply_tool_event(&mut items, second.event());

        let ConversationItem::ToolGroup(group) = &items[0] else {
            panic!("tool group expected");
        };
        assert_eq!(group.calls[0].output_text().as_deref(), Some("one two"));
    }

    #[test]
    fn subagent_result_extracts_nested_agent_progress() {
        let call = ToolCallView {
            id: "call-1".to_owned(),
            name: "subagent".to_owned(),
            args: json!({ "agent": "scout", "task": "Inspect the protocol" }),
            status: ToolStatus::Succeeded,
            output: Some(json!({
                "details": {
                    "mode": "single",
                    "results": [{
                        "agent": "scout",
                        "task": "Inspect the protocol",
                        "exitCode": 0,
                        "messages": [{
                            "role": "assistant",
                            "content": [{ "type": "text", "text": "Found it." }]
                        }]
                    }]
                }
            })),
        };

        assert_eq!(call.label(), "scout · Inspect the protocol");
        assert_eq!(
            call.subagent_runs(),
            vec![SubagentRunView {
                agent: "scout".to_owned(),
                task: "Inspect the protocol".to_owned(),
                status: ToolStatus::Succeeded,
                output: Some("Found it.".to_owned()),
            }]
        );
    }
}
