use aetherflow_pi::{
    AetherflowClient, AetherflowClientOptions, AssistantMessageEvent, CreateSessionOptions,
    MAX_SESSION_EVENT_PAGE_SIZE, PiEvent, SessionDescriptor, SessionEvent, SessionEventPayload,
};
use aetherflow_storage::SessionId;
use gpui::{
    App, Application, Bounds, Context, Div, Entity, Pixels, ScrollHandle, Subscription,
    TitlebarOptions, Window, WindowBackgroundAppearance, WindowBounds, WindowOptions, div,
    prelude::*, px, rgb, rgba, size,
};
use gpui_component::input::{Input, InputEvent, InputState};
use gpui_component::text::{TextView, TextViewStyle};
use std::{
    cmp::Reverse,
    collections::{HashMap, HashSet},
    sync::Arc,
    time::{SystemTime, UNIX_EPOCH},
};
use tokio::runtime::Runtime;

const SIDEBAR_WIDTH: f32 = 280.;
const BOTTOM_FOLLOW_THRESHOLD: f32 = 32.;

enum SessionLoadState {
    Loading,
    Loaded,
    Failed(String),
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum ConversationRole {
    User,
    Assistant,
}

#[derive(Clone, Debug, Eq, PartialEq)]
struct ConversationMessage {
    role: ConversationRole,
    text: String,
}

enum PromptUpdate {
    Created(SessionId),
    TextDelta {
        session_id: SessionId,
        delta: String,
    },
    Finished,
    Failed(String),
}

struct DesktopShell {
    runtime: Arc<Runtime>,
    client: AetherflowClient,
    sessions: Vec<SessionDescriptor>,
    selected_session_id: Option<SessionId>,
    creating_new_session: bool,
    conversations: HashMap<SessionId, Vec<ConversationMessage>>,
    conversation_scrolls: HashMap<SessionId, ScrollHandle>,
    loading_conversations: HashSet<SessionId>,
    conversation_errors: HashMap<SessionId, String>,
    new_session_messages: Vec<ConversationMessage>,
    composer: Entity<InputState>,
    is_sending: bool,
    load_state: SessionLoadState,
    action_error: Option<String>,
    _subscriptions: Vec<Subscription>,
}

impl DesktopShell {
    fn new(window: &mut Window, cx: &mut Context<Self>) -> Self {
        let composer = cx.new(|cx| InputState::new(window, cx).placeholder("Message Aetherflow"));
        let input_subscription = cx.subscribe_in(
            &composer,
            window,
            |shell, _, event: &InputEvent, window, cx| {
                if matches!(event, InputEvent::PressEnter { .. }) {
                    shell.submit_prompt(window, cx);
                }
            },
        );
        let mut shell = Self {
            runtime: Arc::new(Runtime::new().expect("failed to start desktop async runtime")),
            client: AetherflowClient::connect(AetherflowClientOptions::default()),
            sessions: Vec::new(),
            selected_session_id: None,
            creating_new_session: false,
            conversations: HashMap::new(),
            conversation_scrolls: HashMap::new(),
            loading_conversations: HashSet::new(),
            conversation_errors: HashMap::new(),
            new_session_messages: Vec::new(),
            composer,
            is_sending: false,
            load_state: SessionLoadState::Loading,
            action_error: None,
            _subscriptions: vec![input_subscription],
        };
        shell.load_sessions(cx);
        shell
    }

    fn load_sessions(&mut self, cx: &mut Context<Self>) {
        self.load_state = SessionLoadState::Loading;
        let client = self.client.clone();
        let request = self.runtime.spawn(async move {
            client
                .list_sessions()
                .await
                .map_err(|error| format!("{error:#}"))
        });

        cx.spawn(async move |shell, cx| {
            let result = request
                .await
                .map_err(|error| format!("session request task failed: {error}"))
                .and_then(|result| result);
            let _ = shell.update(cx, |shell, cx| {
                match result {
                    Ok(sessions) => {
                        shell.replace_sessions(sessions);
                        if let Some(session_id) = shell.selected_session_id {
                            shell.load_conversation(session_id, cx);
                        }
                    }
                    Err(error) => shell.load_state = SessionLoadState::Failed(error),
                }
                cx.notify();
            });
        })
        .detach();
    }

    fn replace_sessions(&mut self, sessions: Vec<SessionDescriptor>) {
        self.selected_session_id = selection_after_refresh(self.selected_session_id, &sessions);
        if sessions.is_empty() {
            self.creating_new_session = true;
        }
        self.sessions = sessions;
        self.load_state = SessionLoadState::Loaded;
    }

    fn selected_session(&self) -> Option<&SessionDescriptor> {
        let selected = self.selected_session_id?;
        self.sessions.iter().find(|session| session.id == selected)
    }

    fn select_session(&mut self, session_id: SessionId, cx: &mut Context<Self>) {
        self.creating_new_session = false;
        self.selected_session_id = Some(session_id);
        self.action_error = None;
        self.load_conversation(session_id, cx);
    }

    fn load_conversation(&mut self, session_id: SessionId, cx: &mut Context<Self>) {
        self.conversation_scrolls.entry(session_id).or_default();
        if self.conversations.contains_key(&session_id)
            || !self.loading_conversations.insert(session_id)
        {
            return;
        }
        self.conversation_errors.remove(&session_id);

        let client = self.client.clone();
        let request = self.runtime.spawn(async move {
            let mut after_sequence = 0;
            let mut events = Vec::new();
            loop {
                let page = client
                    .session_events(session_id, after_sequence, MAX_SESSION_EVENT_PAGE_SIZE)
                    .await
                    .map_err(|error| format!("Could not load session messages: {error:#}"))?;
                let page_len = page.len();
                if let Some(event) = page.last() {
                    after_sequence = event.sequence;
                }
                events.extend(page);
                if page_len < MAX_SESSION_EVENT_PAGE_SIZE as usize {
                    break;
                }
            }
            Ok::<_, String>(conversation_from_events(&events))
        });

        cx.spawn(async move |shell, cx| {
            let result = request
                .await
                .map_err(|error| format!("session message task failed: {error}"))
                .and_then(|result| result);
            let _ = shell.update(cx, |shell, cx| {
                shell.loading_conversations.remove(&session_id);
                match result {
                    Ok(messages) => {
                        let follow = shell.conversation_is_at_bottom(session_id);
                        shell.conversations.insert(session_id, messages);
                        shell.follow_conversation_if(session_id, follow);
                    }
                    Err(error) => {
                        shell.conversation_errors.insert(session_id, error);
                    }
                }
                cx.notify();
            });
        })
        .detach();
    }

    fn conversation_is_at_bottom(&self, session_id: SessionId) -> bool {
        self.conversation_scrolls
            .get(&session_id)
            .is_none_or(scroll_handle_is_at_bottom)
    }

    fn follow_conversation_if(&self, session_id: SessionId, follow: bool) {
        if follow {
            self.conversation_scrolls
                .get(&session_id)
                .expect("selected conversations have a scroll handle")
                .scroll_to_bottom();
        }
    }

    fn submit_prompt(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.is_sending
            || self
                .selected_session_id
                .is_some_and(|session_id| self.loading_conversations.contains(&session_id))
        {
            return;
        }

        let prompt = self.composer.read(cx).value().trim().to_owned();
        if prompt.is_empty() {
            return;
        }

        let create_new = self.creating_new_session;
        let session_id = if create_new {
            None
        } else {
            self.selected_session_id
        };
        let messages = vec![
            ConversationMessage {
                role: ConversationRole::User,
                text: prompt.clone(),
            },
            ConversationMessage {
                role: ConversationRole::Assistant,
                text: String::new(),
            },
        ];
        if let Some(session_id) = session_id {
            let follow = self.conversation_is_at_bottom(session_id);
            self.conversations
                .entry(session_id)
                .or_default()
                .extend(messages);
            self.follow_conversation_if(session_id, follow);
        } else if create_new {
            self.new_session_messages.extend(messages);
        } else {
            return;
        }

        self.composer
            .update(cx, |input, cx| input.set_value("", window, cx));
        self.is_sending = true;
        self.action_error = None;

        let client = self.client.clone();
        let (updates, mut update_rx) = tokio::sync::mpsc::unbounded_channel();
        self.runtime.spawn(async move {
            let session_id = match session_id {
                Some(session_id) => session_id,
                None => match client.create_session(CreateSessionOptions::default()).await {
                    Ok(session_id) => {
                        let _ = updates.send(PromptUpdate::Created(session_id));
                        session_id
                    }
                    Err(error) => {
                        let _ = updates.send(PromptUpdate::Failed(format!(
                            "Could not create session: {error:#}"
                        )));
                        return;
                    }
                },
            };

            let mut stream = match client.prompt_session(session_id, prompt).await {
                Ok(stream) => stream,
                Err(error) => {
                    let _ = updates.send(PromptUpdate::Failed(format!(
                        "Could not prompt session: {error:#}"
                    )));
                    return;
                }
            };

            loop {
                match stream.next().await {
                    Ok(Some(event)) => {
                        if let Some(error) = stopped_error(&event) {
                            let _ = updates.send(PromptUpdate::Failed(error));
                            return;
                        }
                        if let Some(delta) = assistant_text_delta(&event) {
                            let _ = updates.send(PromptUpdate::TextDelta {
                                session_id,
                                delta: delta.to_owned(),
                            });
                        }
                    }
                    Ok(None) => {
                        let _ = updates.send(PromptUpdate::Finished);
                        return;
                    }
                    Err(error) => {
                        let _ = updates.send(PromptUpdate::Failed(format!(
                            "Session stream failed: {error:#}"
                        )));
                        return;
                    }
                }
            }
        });

        cx.spawn(async move |shell, cx| {
            while let Some(update) = update_rx.recv().await {
                let finished = matches!(update, PromptUpdate::Finished | PromptUpdate::Failed(_));
                let _ = shell.update(cx, |shell, cx| {
                    shell.apply_prompt_update(update, cx);
                    cx.notify();
                });
                if finished {
                    break;
                }
            }
        })
        .detach();
        cx.notify();
    }

    fn apply_prompt_update(&mut self, update: PromptUpdate, cx: &mut Context<Self>) {
        match update {
            PromptUpdate::Created(session_id) => {
                self.selected_session_id = Some(session_id);
                self.creating_new_session = false;
                let messages = std::mem::take(&mut self.new_session_messages);
                self.conversations.insert(session_id, messages);
                self.conversation_scrolls
                    .entry(session_id)
                    .or_default()
                    .scroll_to_bottom();
                self.load_sessions(cx);
            }
            PromptUpdate::TextDelta { session_id, delta } => {
                let follow = self.conversation_is_at_bottom(session_id);
                append_assistant_delta(self.conversations.entry(session_id).or_default(), &delta);
                self.follow_conversation_if(session_id, follow);
            }
            PromptUpdate::Finished => {
                self.is_sending = false;
                self.load_sessions(cx);
            }
            PromptUpdate::Failed(error) => {
                self.is_sending = false;
                self.action_error = Some(error);
            }
        }
    }

    fn set_session_archived(
        &mut self,
        session_id: SessionId,
        archived: bool,
        cx: &mut Context<Self>,
    ) {
        self.action_error = None;
        let client = self.client.clone();
        let request = self.runtime.spawn(async move {
            client
                .set_session_archived(session_id, archived)
                .await
                .map_err(|error| format!("{error:#}"))
        });

        cx.spawn(async move |shell, cx| {
            let result = request
                .await
                .map_err(|error| format!("session request task failed: {error}"))
                .and_then(|result| result);
            let _ = shell.update(cx, |shell, cx| {
                match result {
                    Ok(updated) => {
                        if let Some(session) = shell
                            .sessions
                            .iter_mut()
                            .find(|session| session.id == updated.id)
                        {
                            *session = updated;
                        }
                        shell
                            .sessions
                            .sort_by_key(|session| Reverse(session.updated_at_ms));
                    }
                    Err(error) => shell.action_error = Some(error),
                }
                cx.notify();
            });
        })
        .detach();
    }

    fn render_session_row(
        &self,
        session: SessionDescriptor,
        index: usize,
        cx: &mut Context<Self>,
    ) -> impl IntoElement {
        let selected = self.selected_session_id == Some(session.id);
        let group = format!("session-row-{}", session.id);
        let title = session_title(&session);
        let activity = relative_time(session.updated_at_ms, current_time_ms());
        let archived = session.archived;

        div()
            .group(group.clone())
            .id(("session", index))
            .relative()
            .mb_1()
            .px_3()
            .py_2()
            .rounded_lg()
            .cursor_pointer()
            .when(selected, |style| style.bg(rgb(0x2b2d2f)))
            .hover(|style| style.bg(rgb(0x252729)))
            .on_click(cx.listener(move |shell, _, _, cx| {
                shell.select_session(session.id, cx);
                cx.notify();
            }))
            .child(
                div()
                    .w_full()
                    .flex()
                    .items_center()
                    .gap_2()
                    .child(
                        div()
                            .flex_1()
                            .min_w_0()
                            .truncate()
                            .text_sm()
                            .text_color(rgb(0xd6d7d9))
                            .child(title),
                    )
                    .child(
                        div()
                            .flex_none()
                            .text_xs()
                            .text_color(rgb(0x74777a))
                            .group_hover(group.clone(), |style| style.opacity(0.))
                            .child(activity),
                    ),
            )
            .child(
                div()
                    .id(("archive-session", index))
                    .absolute()
                    .right_2()
                    .top(px(5.))
                    .px_2()
                    .py_1()
                    .rounded_md()
                    .bg(rgb(0x343638))
                    .opacity(0.)
                    .group_hover(group, |style| style.opacity(1.))
                    .hover(|style| style.bg(rgb(0x414447)))
                    .text_xs()
                    .text_color(rgb(0xaeb1b5))
                    .child(if archived { "Restore" } else { "Archive" })
                    .on_click(cx.listener(move |shell, _, _, cx| {
                        cx.stop_propagation();
                        shell.set_session_archived(session.id, !archived, cx);
                    })),
            )
    }

    fn render_sidebar(&self, cx: &mut Context<Self>) -> Div {
        let mut list = div()
            .id("session-list")
            .flex_1()
            .min_h_0()
            .overflow_y_scroll()
            .px_2()
            .pt(px(72.))
            .pb_2();

        match &self.load_state {
            SessionLoadState::Loading => {
                list = list.child(
                    div()
                        .p_3()
                        .text_sm()
                        .text_color(rgb(0x8b93a1))
                        .child("Loading sessions…"),
                );
            }
            SessionLoadState::Failed(error) => {
                let error = error.clone();
                list = list.child(
                    div()
                        .p_3()
                        .flex()
                        .flex_col()
                        .gap_3()
                        .text_sm()
                        .child(div().text_color(rgb(0xf29b9b)).child("Daemon unavailable"))
                        .child(div().text_color(rgb(0x8b93a1)).child(truncate(&error, 120)))
                        .child(
                            div()
                                .id("retry-sessions")
                                .px_3()
                                .py_2()
                                .rounded_md()
                                .bg(rgb(0x2a303b))
                                .hover(|style| style.bg(rgb(0x353d4a)))
                                .cursor_pointer()
                                .text_color(rgb(0xe7eaf0))
                                .child("Retry")
                                .on_click(cx.listener(|shell, _, _, cx| {
                                    shell.load_sessions(cx);
                                    cx.notify();
                                })),
                        ),
                );
            }
            SessionLoadState::Loaded if self.sessions.is_empty() => {
                list = list.child(
                    div()
                        .p_3()
                        .text_sm()
                        .text_color(rgb(0x8b93a1))
                        .child("No sessions yet."),
                );
            }
            SessionLoadState::Loaded => {
                for (index, session) in self.sessions.iter().enumerate() {
                    if !session.archived {
                        list = list.child(self.render_session_row(session.clone(), index, cx));
                    }
                }

                if self.sessions.iter().any(|session| session.archived) {
                    list = list.child(
                        div()
                            .mt_5()
                            .px_3()
                            .pb_2()
                            .text_xs()
                            .text_color(rgb(0x66696d))
                            .child("Archived"),
                    );
                    for (index, session) in self.sessions.iter().enumerate() {
                        if session.archived {
                            list = list.child(self.render_session_row(session.clone(), index, cx));
                        }
                    }
                }

                if let Some(error) = &self.action_error {
                    list = list.child(
                        div()
                            .mt_3()
                            .px_3()
                            .text_xs()
                            .text_color(rgb(0xf29b9b))
                            .child(truncate(error, 100)),
                    );
                }
            }
        }

        div()
            .w(px(SIDEBAR_WIDTH))
            .h_full()
            .flex()
            .flex_col()
            .bg(rgba(0x1d1f20d9))
            .child(list)
    }

    fn render_main_panel(&self, window: &mut Window, cx: &mut Context<Self>) -> Div {
        let panel = div()
            .flex_1()
            .min_w_0()
            .h_full()
            .flex()
            .flex_col()
            .bg(rgba(0x101318d9));

        if self.creating_new_session {
            return panel
                .child(
                    div()
                        .flex_1()
                        .min_h_0()
                        .flex()
                        .flex_col()
                        .items_center()
                        .justify_center()
                        .text_color(rgb(0x74777a))
                        .child("Send a message to start a new session."),
                )
                .child(self.render_composer(cx));
        }

        let Some(session_id) = self.selected_session_id else {
            return panel.child(
                div()
                    .flex_1()
                    .flex()
                    .flex_col()
                    .items_center()
                    .justify_center()
                    .gap_2()
                    .text_color(rgb(0x8b93a1))
                    .child(
                        div()
                            .text_lg()
                            .text_color(rgb(0xe7eaf0))
                            .child("Aetherflow"),
                    )
                    .child("Select a session to open it."),
            );
        };

        let title = self
            .selected_session()
            .map(session_title)
            .unwrap_or_else(|| format!("Session {}", short_id(session_id)));

        panel
            .child(
                div()
                    .h(px(64.))
                    .px_6()
                    .flex()
                    .items_center()
                    .border_b_1()
                    .border_color(rgb(0x2a303b))
                    .child(div().font_weight(gpui::FontWeight::SEMIBOLD).child(title)),
            )
            .child(self.render_conversation(session_id, window, cx))
            .child(self.render_composer(cx))
    }

    fn render_conversation(
        &self,
        session_id: SessionId,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> impl IntoElement {
        let scroll_handle = self
            .conversation_scrolls
            .get(&session_id)
            .expect("selected conversations have a scroll handle");
        if scroll_handle_is_at_bottom(scroll_handle) {
            scroll_handle.scroll_to_bottom();
        }
        let mut conversation = div()
            .id("conversation")
            .flex_1()
            .min_h_0()
            .overflow_y_scroll()
            .track_scroll(scroll_handle)
            .px_8()
            .py_6()
            .flex()
            .flex_col()
            .gap_5();

        if self.loading_conversations.contains(&session_id) {
            return conversation
                .items_center()
                .justify_center()
                .text_color(rgb(0x74777a))
                .child("Loading messages…");
        }

        if let Some(error) = self.conversation_errors.get(&session_id) {
            return conversation
                .items_center()
                .justify_center()
                .gap_2()
                .text_color(rgb(0xf29b9b))
                .child("Could not load messages")
                .child(
                    div()
                        .max_w(px(520.))
                        .text_xs()
                        .text_color(rgb(0x8b93a1))
                        .child(truncate(error, 180)),
                );
        }

        let messages = self.conversations.get(&session_id);
        if messages.is_none_or(Vec::is_empty) {
            return conversation
                .items_center()
                .justify_center()
                .text_color(rgb(0x74777a))
                .child("Send a message to continue this session.");
        }

        for (index, message) in messages.into_iter().flatten().enumerate() {
            let is_empty_assistant =
                message.role == ConversationRole::Assistant && message.text.is_empty();
            if is_empty_assistant && !self.is_sending {
                continue;
            }

            conversation = match message.role {
                ConversationRole::User => conversation.child(
                    div()
                        .id(("user-message", index))
                        .w_full()
                        .flex()
                        .justify_end()
                        .child(
                            div()
                                .max_w(px(640.))
                                .px_4()
                                .py_3()
                                .rounded_xl()
                                .bg(rgb(0x25282d))
                                .child(
                                    TextView::markdown(
                                        ("user-markdown", index),
                                        message.text.clone(),
                                        window,
                                        cx,
                                    )
                                    .style(conversation_markdown_style(cx))
                                    .selectable(true)
                                    .w_full(),
                                ),
                        ),
                ),
                ConversationRole::Assistant => {
                    let content = if is_empty_assistant {
                        div().child("Working…").into_any_element()
                    } else {
                        TextView::markdown(
                            ("assistant-markdown", index),
                            message.text.clone(),
                            window,
                            cx,
                        )
                        .style(conversation_markdown_style(cx))
                        .selectable(true)
                        .w_full()
                        .into_any_element()
                    };
                    conversation.child(
                        div()
                            .id(("assistant-message", index))
                            .w_full()
                            .max_w(px(720.))
                            .text_color(rgb(0xd6d7d9))
                            .child(content),
                    )
                }
            };
        }

        conversation
    }

    fn render_composer(&self, cx: &mut Context<Self>) -> Div {
        let disabled = self.is_sending
            || self
                .selected_session_id
                .is_some_and(|session_id| self.loading_conversations.contains(&session_id));
        div()
            .w_full()
            .px_8()
            .pb_6()
            .child(
                div()
                    .w_full()
                    .max_w(px(760.))
                    .mx_auto()
                    .h(px(52.))
                    .pl_3()
                    .pr_2()
                    .flex()
                    .items_center()
                    .gap_2()
                    .rounded_xl()
                    .border_1()
                    .border_color(rgb(0x30343a))
                    .bg(rgba(0x1d2025f2))
                    .child(
                        Input::new(&self.composer)
                            .appearance(false)
                            .bordered(false)
                            .focus_bordered(false)
                            .disabled(disabled)
                            .flex_1(),
                    )
                    .child(
                        div()
                            .id("send-prompt")
                            .size(px(34.))
                            .flex()
                            .items_center()
                            .justify_center()
                            .rounded_lg()
                            .text_base()
                            .when(disabled, |style| {
                                style.bg(rgb(0x303238)).text_color(rgb(0x777a80))
                            })
                            .when(!disabled, |style| {
                                style
                                    .bg(rgb(0xe7eaf0))
                                    .text_color(rgb(0x17191d))
                                    .cursor_pointer()
                                    .hover(|style| style.bg(rgb(0xffffff)))
                            })
                            .child("↑")
                            .on_click(cx.listener(|shell, _, window, cx| {
                                shell.submit_prompt(window, cx);
                            })),
                    ),
            )
            .when_some(self.action_error.clone(), |composer, error| {
                composer.child(
                    div()
                        .w_full()
                        .max_w(px(760.))
                        .mx_auto()
                        .pt_2()
                        .text_xs()
                        .text_color(rgb(0xf29b9b))
                        .child(truncate(&error, 180)),
                )
            })
    }
}

impl Render for DesktopShell {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        div()
            .size_full()
            .flex()
            .bg(rgba(0x00000000))
            .font_family("Inter Variable")
            .text_color(rgb(0xe7eaf0))
            .child(self.render_sidebar(cx))
            .child(self.render_main_panel(window, cx))
    }
}

fn short_id(id: SessionId) -> String {
    short_uuid(&id.to_string())
}

fn session_title(session: &SessionDescriptor) -> String {
    session
        .title
        .clone()
        .unwrap_or_else(|| format!("Session {}", short_id(session.id)))
}

fn current_time_ms() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .ok()
        .and_then(|duration| u64::try_from(duration.as_millis()).ok())
        .unwrap_or(0)
}

fn relative_time(timestamp_ms: u64, now_ms: u64) -> String {
    let seconds = now_ms.saturating_sub(timestamp_ms) / 1_000;
    match seconds {
        0..=59 => "now".to_owned(),
        60..=3_599 => format!("{}m", seconds / 60),
        3_600..=86_399 => format!("{}h", seconds / 3_600),
        86_400..=604_799 => format!("{}d", seconds / 86_400),
        _ => format!("{}w", seconds / 604_800),
    }
}

fn short_uuid(id: &str) -> String {
    id.chars().take(8).collect()
}

fn truncate(value: &str, max_chars: usize) -> String {
    let mut chars = value.chars();
    let truncated: String = chars.by_ref().take(max_chars).collect();
    if chars.next().is_some() {
        format!("{truncated}…")
    } else {
        truncated
    }
}

fn selection_after_refresh(
    current: Option<SessionId>,
    sessions: &[SessionDescriptor],
) -> Option<SessionId> {
    current
        .filter(|selected| sessions.iter().any(|session| session.id == *selected))
        .or_else(|| sessions.first().map(|session| session.id))
}

fn assistant_text_delta(event: &SessionEvent) -> Option<&str> {
    let SessionEventPayload::Pi { message } = &event.payload else {
        return None;
    };
    let PiEvent::MessageUpdate(update) = message.event() else {
        return None;
    };
    let AssistantMessageEvent::TextDelta { delta, .. } = &update.assistant_message_event else {
        return None;
    };
    Some(delta)
}

fn conversation_from_events(events: &[SessionEvent]) -> Vec<ConversationMessage> {
    events
        .iter()
        .filter_map(completed_conversation_message)
        .collect()
}

fn completed_conversation_message(event: &SessionEvent) -> Option<ConversationMessage> {
    let SessionEventPayload::Pi { message } = &event.payload else {
        return None;
    };
    let PiEvent::MessageEnd(completed) = message.event() else {
        return None;
    };

    let role = match completed.message.get("role")?.as_str()? {
        "user" => ConversationRole::User,
        "assistant" => ConversationRole::Assistant,
        _ => return None,
    };
    let text = completed
        .message
        .get("content")?
        .as_array()?
        .iter()
        .filter(|content| content.get("type").and_then(|kind| kind.as_str()) == Some("text"))
        .filter_map(|content| content.get("text").and_then(|text| text.as_str()))
        .collect::<String>();
    if text.is_empty() {
        return None;
    }

    Some(ConversationMessage { role, text })
}

fn stopped_error(event: &SessionEvent) -> Option<String> {
    let SessionEventPayload::Stopped { error } = &event.payload else {
        return None;
    };
    Some(
        error
            .clone()
            .unwrap_or_else(|| "The session stopped before completing the response.".to_owned()),
    )
}

fn append_assistant_delta(messages: &mut Vec<ConversationMessage>, delta: &str) {
    if let Some(message) = messages
        .last_mut()
        .filter(|message| message.role == ConversationRole::Assistant)
    {
        message.text.push_str(delta);
    } else {
        messages.push(ConversationMessage {
            role: ConversationRole::Assistant,
            text: delta.to_owned(),
        });
    }
}

fn conversation_markdown_style(cx: &App) -> TextViewStyle {
    let theme = gpui_component::Theme::global(cx);
    let mut style = TextViewStyle::default().paragraph_gap(gpui::rems(0.75));
    style.highlight_theme = theme.highlight_theme.clone();
    style.is_dark = theme.is_dark();
    style
}

fn scroll_handle_is_at_bottom(scroll_handle: &ScrollHandle) -> bool {
    is_near_bottom(scroll_handle.offset().y, scroll_handle.max_offset().height)
}

fn is_near_bottom(offset_y: Pixels, max_offset: Pixels) -> bool {
    max_offset + offset_y <= px(BOTTOM_FOLLOW_THRESHOLD)
}

fn main() {
    Application::new().run(|cx: &mut App| {
        gpui_component::init(cx);
        let theme = gpui_component::Theme::global_mut(cx);
        theme.background = gpui::transparent_black();
        theme.font_family = "Inter Variable".into();
        let bounds = Bounds::centered(None, size(px(1040.), px(700.)), cx);
        cx.open_window(
            WindowOptions {
                window_bounds: Some(WindowBounds::Windowed(bounds)),
                titlebar: Some(TitlebarOptions {
                    appears_transparent: true,
                    ..Default::default()
                }),
                window_background: WindowBackgroundAppearance::Transparent,
                ..Default::default()
            },
            |window, cx| {
                let shell = cx.new(|cx| DesktopShell::new(window, cx));
                cx.new(|cx| gpui_component::Root::new(shell, window, cx))
            },
        )
        .expect("failed to open the Aetherflow window");
        cx.activate(true);
    });
}

#[cfg(test)]
mod tests {
    use super::*;
    use aetherflow_pi::PiMessage;
    use aetherflow_storage::{AgentId, SessionAssociation};
    use serde_json::json;

    fn session_event(message: serde_json::Value) -> SessionEvent {
        SessionEvent {
            sequence: 1,
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
    fn truncation_is_unicode_safe() {
        assert_eq!(truncate("hello", 5), "hello");
        assert_eq!(truncate("héllo world", 5), "héllo…");
    }

    #[test]
    fn short_uuid_uses_the_first_eight_characters() {
        assert_eq!(
            short_uuid("12345678-1234-1234-1234-123456789abc"),
            "12345678"
        );
    }

    #[test]
    fn refresh_preserves_a_valid_selection_and_falls_back_to_the_first_session() {
        let first = SessionDescriptor {
            id: SessionId::new(),
            agent_id: AgentId::new(),
            association: SessionAssociation::Standalone,
            title: Some("First".to_owned()),
            archived: false,
            updated_at_ms: 2,
        };
        let second = SessionDescriptor {
            id: SessionId::new(),
            agent_id: AgentId::new(),
            association: SessionAssociation::Standalone,
            title: Some("Second".to_owned()),
            archived: false,
            updated_at_ms: 1,
        };
        let first_id = first.id;
        let second_id = second.id;
        let sessions = [first, second];

        assert_eq!(
            selection_after_refresh(Some(second_id), &sessions),
            Some(second_id)
        );
        assert_eq!(
            selection_after_refresh(Some(SessionId::new()), &sessions),
            Some(first_id)
        );
        assert_eq!(selection_after_refresh(Some(first_id), &[]), None);
    }

    #[test]
    fn relative_activity_time_stays_compact() {
        let now = 10_000_000;
        assert_eq!(relative_time(now, now), "now");
        assert_eq!(relative_time(now - 120_000, now), "2m");
        assert_eq!(relative_time(now - 7_200_000, now), "2h");
    }

    #[test]
    fn assistant_deltas_append_to_the_current_response() {
        let mut messages = vec![ConversationMessage {
            role: ConversationRole::Assistant,
            text: "Hello".to_owned(),
        }];

        append_assistant_delta(&mut messages, ", world");

        assert_eq!(messages[0].text, "Hello, world");
    }

    #[test]
    fn assistant_delta_creates_a_response_when_needed() {
        let mut messages = Vec::new();

        append_assistant_delta(&mut messages, "Hello");

        assert_eq!(
            messages,
            vec![ConversationMessage {
                role: ConversationRole::Assistant,
                text: "Hello".to_owned(),
            }]
        );
    }

    #[test]
    fn assistant_delta_does_not_merge_across_a_user_message() {
        let mut messages = vec![
            ConversationMessage {
                role: ConversationRole::Assistant,
                text: "First response.".to_owned(),
            },
            ConversationMessage {
                role: ConversationRole::User,
                text: "Continue.".to_owned(),
            },
        ];

        append_assistant_delta(&mut messages, "Second response.");

        assert_eq!(messages.len(), 3);
        assert_eq!(messages[0].text, "First response.");
        assert_eq!(messages[2].role, ConversationRole::Assistant);
        assert_eq!(messages[2].text, "Second response.");
    }

    #[test]
    fn bottom_following_stops_after_scrolling_away() {
        assert!(is_near_bottom(px(-200.), px(200.)));
        assert!(is_near_bottom(px(-175.), px(200.)));
        assert!(!is_near_bottom(px(-150.), px(200.)));
        assert!(is_near_bottom(px(0.), px(0.)));
    }

    #[test]
    fn completed_pi_messages_rebuild_a_conversation() {
        let events = [
            session_event(json!({
                "type": "message_end",
                "message": {
                    "role": "user",
                    "content": [{ "type": "text", "text": "Where are we?" }]
                }
            })),
            session_event(json!({
                "type": "message_update",
                "assistantMessageEvent": {
                    "type": "text_delta",
                    "contentIndex": 0,
                    "delta": "In the archive."
                }
            })),
            session_event(json!({
                "type": "message_end",
                "message": {
                    "role": "assistant",
                    "content": [
                        { "type": "thinking", "thinking": "Recall context" },
                        { "type": "text", "text": "In the " },
                        { "type": "text", "text": "archive." }
                    ]
                }
            })),
        ];

        assert_eq!(
            conversation_from_events(&events),
            vec![
                ConversationMessage {
                    role: ConversationRole::User,
                    text: "Where are we?".to_owned(),
                },
                ConversationMessage {
                    role: ConversationRole::Assistant,
                    text: "In the archive.".to_owned(),
                },
            ]
        );
    }
}
