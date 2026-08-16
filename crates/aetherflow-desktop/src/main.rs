mod transcript;

use aetherflow_pi::{
    AetherflowClient, AetherflowClientOptions, AssistantMessageEvent, CreateSessionOptions,
    MAX_SESSION_EVENT_PAGE_SIZE, PiEvent, SessionDescriptor, SessionEvent, SessionEventPayload,
};
use aetherflow_storage::SessionId;
use gpui::{
    Animation, AnimationExt as _, App, Application, Bounds, Context, Div, Entity, Pixels,
    ScrollHandle, SharedString, Subscription, TitlebarOptions, Window, WindowBackgroundAppearance,
    WindowBounds, WindowOptions, div, ease_out_quint, prelude::*, px, rgb, rgba, size,
};
use gpui_component::input::{Input, InputEvent, InputState};
use gpui_component::text::{TextView, TextViewStyle};
use std::{
    cmp::Reverse,
    collections::{HashMap, HashSet},
    sync::Arc,
    time::{Duration, SystemTime, UNIX_EPOCH},
};
use tokio::runtime::Runtime;
use transcript::{
    ConversationItem, ConversationMessage, ConversationRole, ToolCallView, ToolGroup, ToolStatus,
    append_assistant_delta, apply_tool_event, conversation_from_events,
};

const SIDEBAR_WIDTH: f32 = 280.;
const BOTTOM_FOLLOW_THRESHOLD: f32 = 32.;

enum SessionLoadState {
    Loading,
    Loaded,
    Failed(String),
}

enum PromptUpdate {
    Created(SessionId),
    TextDelta {
        session_id: SessionId,
        delta: String,
    },
    ToolEvent {
        session_id: SessionId,
        event: Box<PiEvent>,
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
    conversations: HashMap<SessionId, Vec<ConversationItem>>,
    conversation_scrolls: HashMap<SessionId, ScrollHandle>,
    previous_stream_texts: HashMap<SessionId, String>,
    tool_group_expansion: HashMap<String, bool>,
    expanded_tool_calls: HashSet<String>,
    loading_conversations: HashSet<SessionId>,
    conversation_errors: HashMap<SessionId, String>,
    new_session_messages: Vec<ConversationItem>,
    composer: Entity<InputState>,
    is_sending: bool,
    active_turn_session_id: Option<SessionId>,
    is_cancelling: bool,
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
            previous_stream_texts: HashMap::new(),
            tool_group_expansion: HashMap::new(),
            expanded_tool_calls: HashSet::new(),
            loading_conversations: HashSet::new(),
            conversation_errors: HashMap::new(),
            new_session_messages: Vec::new(),
            composer,
            is_sending: false,
            active_turn_session_id: None,
            is_cancelling: false,
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
        let messages = [
            ConversationItem::Message(ConversationMessage {
                role: ConversationRole::User,
                text: prompt.clone(),
            }),
            ConversationItem::Message(ConversationMessage {
                role: ConversationRole::Assistant,
                text: String::new(),
            }),
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
        self.active_turn_session_id = session_id;
        self.is_cancelling = false;
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
                        if is_tool_event(&event) {
                            let SessionEventPayload::Pi { message } = event.payload else {
                                unreachable!("tool events are Pi events")
                            };
                            let _ = updates.send(PromptUpdate::ToolEvent {
                                session_id,
                                event: Box::new(message.event().clone()),
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
                self.active_turn_session_id = Some(session_id);
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
                let conversation = self.conversations.entry(session_id).or_default();
                if let Some(previous) = trailing_assistant_text(conversation) {
                    self.previous_stream_texts
                        .insert(session_id, previous.to_owned());
                } else {
                    self.previous_stream_texts.remove(&session_id);
                }
                append_assistant_delta(conversation, &delta);
                self.follow_conversation_if(session_id, follow);
            }
            PromptUpdate::ToolEvent { session_id, event } => {
                let follow = self.conversation_is_at_bottom(session_id);
                apply_tool_event(self.conversations.entry(session_id).or_default(), &event);
                self.follow_conversation_if(session_id, follow);
            }
            PromptUpdate::Finished => {
                if let Some(session_id) = self.active_turn_session_id {
                    self.previous_stream_texts.remove(&session_id);
                }
                self.is_sending = false;
                self.active_turn_session_id = None;
                self.is_cancelling = false;
                self.load_sessions(cx);
            }
            PromptUpdate::Failed(error) => {
                if let Some(session_id) = self.active_turn_session_id {
                    self.previous_stream_texts.remove(&session_id);
                }
                self.is_sending = false;
                self.active_turn_session_id = None;
                self.is_cancelling = false;
                self.action_error = Some(error);
            }
        }
    }

    fn cancel_turn(&mut self, cx: &mut Context<Self>) {
        let Some(session_id) = self.active_turn_session_id else {
            return;
        };
        if self.is_cancelling {
            return;
        }

        self.is_cancelling = true;
        self.action_error = None;
        let client = self.client.clone();
        let request = self.runtime.spawn(async move {
            client
                .cancel_turn(session_id)
                .await
                .map_err(|error| format!("Could not cancel turn: {error:#}"))
        });

        cx.spawn(async move |shell, cx| {
            let result = request
                .await
                .map_err(|error| format!("cancel request task failed: {error}"))
                .and_then(|result| result);
            let _ = shell.update(cx, |shell, cx| {
                if let Err(error) = result {
                    shell.is_cancelling = false;
                    shell.action_error = Some(error);
                }
                cx.notify();
            });
        })
        .detach();
        cx.notify();
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

        let messages = messages.expect("non-empty conversations were checked above");
        let session_has_active_turn = self.active_turn_session_id == Some(session_id);
        for (index, item) in messages.iter().enumerate() {
            conversation = match item {
                ConversationItem::Message(message) => {
                    let is_empty_assistant =
                        message.role == ConversationRole::Assistant && message.text.is_empty();
                    if !should_render_conversation_item(
                        item,
                        index,
                        messages.len(),
                        self.is_sending,
                        session_has_active_turn,
                    ) {
                        continue;
                    }

                    match message.role {
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
                                let is_live_tail = session_has_active_turn
                                    && index == messages.len().saturating_sub(1);
                                render_assistant_markdown(
                                    session_id,
                                    index,
                                    message,
                                    is_live_tail,
                                    self.previous_stream_texts
                                        .get(&session_id)
                                        .filter(|_| is_live_tail)
                                        .map(String::as_str),
                                    window,
                                    cx,
                                )
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
                    }
                }
                ConversationItem::ToolGroup(group) => {
                    conversation.child(self.render_tool_group(group, window, cx))
                }
            };
        }

        conversation
    }

    fn render_tool_group(
        &self,
        group: &ToolGroup,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> gpui::AnyElement {
        let group_key = group.key().to_owned();
        let open = self
            .tool_group_expansion
            .get(&group_key)
            .copied()
            .unwrap_or_else(|| group.should_open_by_default());
        let icon = if group
            .calls
            .iter()
            .any(|call| call.status == ToolStatus::Running)
        {
            "◌"
        } else if group
            .calls
            .iter()
            .any(|call| call.status == ToolStatus::Failed)
        {
            "!"
        } else {
            "✓"
        };
        let toggle_group_key = group_key.clone();
        let mut container = div().w_full().max_w(px(720.)).text_sm().child(
            div()
                .id(SharedString::from(format!("tool-group-{group_key}")))
                .h(px(30.))
                .flex()
                .items_center()
                .gap_2()
                .cursor_pointer()
                .text_color(rgb(0x85898f))
                .hover(|style| style.text_color(rgb(0xb5b8bd)))
                .on_click(cx.listener(move |shell, _, _, cx| {
                    shell
                        .tool_group_expansion
                        .insert(toggle_group_key.clone(), !open);
                    cx.notify();
                }))
                .child(
                    div()
                        .size(px(18.))
                        .flex()
                        .items_center()
                        .justify_center()
                        .rounded_md()
                        .bg(rgb(0x25282d))
                        .text_xs()
                        .child(if open { "▾" } else { "▸" }),
                )
                .child(icon)
                .child(group.summary()),
        );

        if open {
            container = container.child(
                div()
                    .ml(px(8.))
                    .pl_4()
                    .border_l_1()
                    .border_color(rgb(0x2c3036))
                    .flex()
                    .flex_col()
                    .gap_1()
                    .children(
                        group
                            .calls
                            .iter()
                            .map(|call| self.render_tool_call(call, window, cx)),
                    ),
            );
        }

        container
            .relative()
            .with_animation(
                SharedString::from(format!("tool-group-reveal-{group_key}")),
                transcript_reveal_animation(),
                |group, delta| group.top(px(4.) - delta * px(4.)).opacity(delta),
            )
            .into_any_element()
    }

    fn render_tool_call(
        &self,
        call: &ToolCallView,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> gpui::AnyElement {
        let expanded = self.expanded_tool_calls.contains(&call.id);
        let call_id = call.id.clone();
        let status_color = match call.status {
            ToolStatus::Running => rgb(0x8f96d9),
            ToolStatus::Succeeded => rgb(0x7f858b),
            ToolStatus::Failed => rgb(0xe28b8b),
        };
        let status_icon = match call.status {
            ToolStatus::Running => "◌",
            ToolStatus::Succeeded => "✓",
            ToolStatus::Failed => "!",
        };
        let mut tool = div().w_full().child(
            div()
                .id(SharedString::from(format!("tool-call-{}", call.id)))
                .min_h(px(30.))
                .py_1()
                .flex()
                .items_center()
                .gap_2()
                .cursor_pointer()
                .text_color(rgb(0xaeb1b5))
                .hover(|style| style.text_color(rgb(0xd6d7d9)))
                .on_click(cx.listener(move |shell, _, _, cx| {
                    if !shell.expanded_tool_calls.remove(&call_id) {
                        shell.expanded_tool_calls.insert(call_id.clone());
                    }
                    cx.notify();
                }))
                .child(div().text_color(status_color).child(status_icon))
                .child(
                    div()
                        .flex_1()
                        .min_w_0()
                        .truncate()
                        .child(truncate(&call.label(), 110)),
                )
                .child(
                    div()
                        .flex_none()
                        .text_xs()
                        .text_color(rgb(0x696d73))
                        .child(if expanded { "Hide" } else { "Details" }),
                ),
        );

        if expanded {
            tool = tool.child(self.render_tool_details(call, window, cx));
        }
        tool.relative()
            .with_animation(
                SharedString::from(format!("tool-call-reveal-{}", call.id)),
                transcript_reveal_animation(),
                |tool, delta| tool.top(px(2.) - delta * px(2.)).opacity(delta),
            )
            .into_any_element()
    }

    fn render_tool_details(
        &self,
        call: &ToolCallView,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> gpui::AnyElement {
        let mut details = div()
            .ml_6()
            .mb_2()
            .p_3()
            .rounded_lg()
            .bg(rgb(0x171a1f))
            .border_1()
            .border_color(rgb(0x292d33))
            .flex()
            .flex_col()
            .gap_3()
            .child(tool_detail_block("Invocation", call.invocation_text()));

        let subagents = call.subagent_runs();
        if !subagents.is_empty() {
            for (index, run) in subagents.into_iter().enumerate() {
                let icon = match run.status {
                    ToolStatus::Running => "◌",
                    ToolStatus::Succeeded => "✓",
                    ToolStatus::Failed => "!",
                };
                details = details.child(
                    div()
                        .flex()
                        .flex_col()
                        .gap_2()
                        .child(
                            div()
                                .flex()
                                .items_center()
                                .gap_2()
                                .text_color(rgb(0xbec1c6))
                                .child(icon)
                                .child(run.agent),
                        )
                        .when(!run.task.is_empty(), |item| {
                            item.child(div().text_xs().text_color(rgb(0x85898f)).child(run.task))
                        })
                        .when_some(run.output, |item, output| {
                            item.child(
                                TextView::markdown(
                                    SharedString::from(format!(
                                        "subagent-output-{}-{index}",
                                        call.id
                                    )),
                                    output,
                                    window,
                                    cx,
                                )
                                .style(conversation_markdown_style(cx))
                                .selectable(true)
                                .w_full(),
                            )
                        }),
                );
            }
        } else if let Some(output) = call.output_text() {
            details = details.child(tool_detail_block("Output", output));
        }

        details
            .relative()
            .with_animation(
                SharedString::from(format!("tool-details-reveal-{}", call.id)),
                transcript_reveal_animation(),
                |details, delta| details.top(px(3.) - delta * px(3.)).opacity(delta),
            )
            .into_any_element()
    }

    fn render_composer(&self, cx: &mut Context<Self>) -> Div {
        let disabled = self.is_sending
            || self
                .selected_session_id
                .is_some_and(|session_id| self.loading_conversations.contains(&session_id));
        let action = if self.is_sending {
            let cancel_enabled = self.active_turn_session_id.is_some() && !self.is_cancelling;
            div()
                .id("stop-turn")
                .size(px(34.))
                .flex()
                .items_center()
                .justify_center()
                .rounded_lg()
                .text_xs()
                .when(cancel_enabled, |style| {
                    style
                        .bg(rgb(0xe7eaf0))
                        .text_color(rgb(0x17191d))
                        .cursor_pointer()
                        .hover(|style| style.bg(rgb(0xffffff)))
                })
                .when(!cancel_enabled, |style| {
                    style.bg(rgb(0x303238)).text_color(rgb(0x777a80))
                })
                .child("■")
                .on_click(cx.listener(|shell, _, _, cx| {
                    shell.cancel_turn(cx);
                }))
        } else {
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
                }))
        };
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
                    .child(action),
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

fn should_render_conversation_item(
    item: &ConversationItem,
    index: usize,
    item_count: usize,
    is_sending: bool,
    session_has_active_turn: bool,
) -> bool {
    let is_empty_assistant = matches!(
        item,
        ConversationItem::Message(ConversationMessage {
            role: ConversationRole::Assistant,
            text,
        }) if text.is_empty()
    );
    !is_empty_assistant
        || (is_sending && session_has_active_turn && index.checked_add(1) == Some(item_count))
}

fn is_tool_event(event: &SessionEvent) -> bool {
    let SessionEventPayload::Pi { message } = &event.payload else {
        return false;
    };
    matches!(
        message.event(),
        PiEvent::ToolExecutionStart(_)
            | PiEvent::ToolExecutionUpdate(_)
            | PiEvent::ToolExecutionEnd(_)
    )
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

fn tool_detail_block(label: &'static str, text: String) -> Div {
    let text = truncate_lines(&text, 24);
    div()
        .flex()
        .flex_col()
        .gap_1()
        .child(div().text_xs().text_color(rgb(0x696d73)).child(label))
        .child(
            div()
                .w_full()
                .p_2()
                .rounded_md()
                .bg(rgb(0x111419))
                .font_family("SF Mono")
                .text_xs()
                .text_color(rgb(0xaeb1b5))
                .child(text),
        )
}

fn truncate_lines(value: &str, max_lines: usize) -> String {
    let mut lines = value.lines();
    let visible = lines
        .by_ref()
        .take(max_lines)
        .collect::<Vec<_>>()
        .join("\n");
    let hidden = lines.count();
    if hidden == 0 {
        visible
    } else {
        format!("{visible}\n… {hidden} more lines")
    }
}

fn conversation_markdown_style(cx: &App) -> TextViewStyle {
    let theme = gpui_component::Theme::global(cx);
    let mut style = TextViewStyle::default().paragraph_gap(gpui::rems(0.75));
    style.highlight_theme = theme.highlight_theme.clone();
    style.is_dark = theme.is_dark();
    style
}

fn transcript_reveal_animation() -> Animation {
    Animation::new(Duration::from_millis(180)).with_easing(ease_out_quint())
}

fn stream_delta_animation() -> Animation {
    Animation::new(Duration::from_millis(120)).with_easing(ease_out_quint())
}

fn render_assistant_markdown(
    session_id: SessionId,
    index: usize,
    message: &ConversationMessage,
    is_live_tail: bool,
    previous_text: Option<&str>,
    window: &mut Window,
    cx: &mut App,
) -> gpui::AnyElement {
    let current = TextView::markdown(
        assistant_markdown_id(session_id, index, message.text.len(), is_live_tail),
        message.text.clone(),
        window,
        cx,
    )
    .style(conversation_markdown_style(cx))
    .selectable(true)
    .w_full();

    if !is_live_tail {
        return current.into_any_element();
    }

    let Some(previous_text) = previous_text.filter(|text| !text.is_empty()) else {
        return current
            .relative()
            .with_animation(
                SharedString::from(format!("assistant-reveal-{session_id}-{index}")),
                transcript_reveal_animation(),
                |text, delta| text.top(px(3.) - delta * px(3.)).opacity(delta),
            )
            .into_any_element();
    };

    let update_key = format!("{session_id}-{index}-{}", message.text.len());
    let previous = TextView::markdown(
        SharedString::from(format!(
            "assistant-markdown-{session_id}-{index}-previous-{}",
            previous_text.len()
        )),
        previous_text.to_owned(),
        window,
        cx,
    )
    .style(conversation_markdown_style(cx))
    .w_full()
    .absolute()
    .top_0()
    .left_0();
    let current = current.relative().with_animation(
        SharedString::from(format!("assistant-stream-new-{update_key}")),
        stream_delta_animation(),
        |text, delta| text.opacity(delta),
    );

    div()
        .relative()
        .w_full()
        .child(previous)
        .child(current)
        .into_any_element()
}

fn assistant_markdown_id(
    session_id: SessionId,
    index: usize,
    text_bytes: usize,
    is_live_tail: bool,
) -> SharedString {
    if is_live_tail {
        format!("assistant-markdown-{session_id}-{index}-live-{text_bytes}").into()
    } else {
        format!("assistant-markdown-{session_id}-{index}").into()
    }
}

fn trailing_assistant_text(conversation: &[ConversationItem]) -> Option<&str> {
    let ConversationItem::Message(message) = conversation.last()? else {
        return None;
    };
    (message.role == ConversationRole::Assistant && !message.text.is_empty())
        .then_some(message.text.as_str())
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
        let mut messages = vec![ConversationItem::Message(ConversationMessage {
            role: ConversationRole::Assistant,
            text: "Hello".to_owned(),
        })];

        append_assistant_delta(&mut messages, ", world");

        let ConversationItem::Message(message) = &messages[0] else {
            panic!("message expected")
        };
        assert_eq!(message.text, "Hello, world");
    }

    #[test]
    fn assistant_delta_creates_a_response_when_needed() {
        let mut messages = Vec::new();

        append_assistant_delta(&mut messages, "Hello");

        assert_eq!(
            messages,
            vec![ConversationItem::Message(ConversationMessage {
                role: ConversationRole::Assistant,
                text: "Hello".to_owned(),
            })]
        );
    }

    #[test]
    fn assistant_delta_does_not_merge_across_a_user_message() {
        let mut messages = vec![
            ConversationItem::Message(ConversationMessage {
                role: ConversationRole::Assistant,
                text: "First response.".to_owned(),
            }),
            ConversationItem::Message(ConversationMessage {
                role: ConversationRole::User,
                text: "Continue.".to_owned(),
            }),
        ];

        append_assistant_delta(&mut messages, "Second response.");

        assert_eq!(messages.len(), 3);
        let ConversationItem::Message(first) = &messages[0] else {
            panic!("message expected")
        };
        let ConversationItem::Message(last) = &messages[2] else {
            panic!("message expected")
        };
        assert_eq!(first.text, "First response.");
        assert_eq!(last.role, ConversationRole::Assistant);
        assert_eq!(last.text, "Second response.");
    }

    #[test]
    fn only_the_active_turn_shows_a_working_placeholder_after_cancellation() {
        let messages = [
            ConversationItem::Message(ConversationMessage {
                role: ConversationRole::User,
                text: "First turn".to_owned(),
            }),
            ConversationItem::Message(ConversationMessage {
                role: ConversationRole::Assistant,
                text: String::new(),
            }),
            ConversationItem::Message(ConversationMessage {
                role: ConversationRole::User,
                text: "Try again".to_owned(),
            }),
            ConversationItem::Message(ConversationMessage {
                role: ConversationRole::Assistant,
                text: String::new(),
            }),
        ];

        let visible_working_placeholders = messages
            .iter()
            .enumerate()
            .filter(|(index, item)| {
                should_render_conversation_item(item, *index, messages.len(), true, true)
                    && matches!(
                        item,
                        ConversationItem::Message(ConversationMessage {
                            role: ConversationRole::Assistant,
                            text,
                        }) if text.is_empty()
                    )
            })
            .count();

        assert_eq!(visible_working_placeholders, 1);
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
                ConversationItem::Message(ConversationMessage {
                    role: ConversationRole::User,
                    text: "Where are we?".to_owned(),
                }),
                ConversationItem::Message(ConversationMessage {
                    role: ConversationRole::Assistant,
                    text: "In the archive.".to_owned(),
                }),
            ]
        );
    }

    #[test]
    fn live_markdown_updates_bypass_the_debounced_view_state() {
        let session_id = SessionId::new();

        assert_ne!(
            assistant_markdown_id(session_id, 2, 1, true),
            assistant_markdown_id(session_id, 2, 80, true),
        );
        assert_eq!(
            assistant_markdown_id(session_id, 2, 1, false),
            assistant_markdown_id(session_id, 2, 80, false),
        );
    }
}
