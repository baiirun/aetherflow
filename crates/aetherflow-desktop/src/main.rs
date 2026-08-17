mod daemon;
mod transcript;

use aetherflow_pi::{
    AetherflowClient, AetherflowClientOptions, AssistantMessageEvent, CreateSessionOptions,
    DEFAULT_SESSION_DIRECTORY_KEY, DEFAULT_SESSION_EVENT_PAGE_SIZE, PiEvent,
    SESSION_DIRECTORY_ACTOR_NAME, SessionDescriptor, SessionEvent, SessionEventPayload,
};
use aetherflow_storage::SessionId;
use daemon::{DaemonTarget, ManagedDaemon};
use gpui::{
    Animation, AnimationExt as _, App, Application, Bounds, Context, Div, Entity, ExternalPaths,
    HighlightStyle, Hsla, KeyBinding, ObjectFit, Pixels, ScrollHandle, SharedString,
    StyledImage as _, StyledText, Subscription, Timer, TitlebarOptions, Window,
    WindowBackgroundAppearance, WindowBounds, WindowOptions, div, ease_out_quint, img, point,
    prelude::*, px, rgb, rgba, size,
};
use gpui_component::input::{Input, InputEvent, InputState};
use gpui_component::text::{TextView, TextViewStyle};
use std::{
    cmp::Reverse,
    collections::{HashMap, HashSet},
    io::Cursor,
    ops::Range,
    path::{Path, PathBuf},
    sync::Arc,
    time::{Duration, Instant, SystemTime, UNIX_EPOCH},
};
use tokio::runtime::Runtime;
use transcript::{
    ConversationImage, ConversationItem, ConversationMessage, ConversationRole, ToolCallView,
    ToolGroup, ToolStatus, append_assistant_delta, apply_tool_event, conversation_from_events,
};

const SIDEBAR_WIDTH: f32 = 280.;
const CHAT_FONT_SIZE: f32 = 14.;
const SESSION_ROW_HEIGHT: f32 = 30.;
const SESSION_ROW_ACTION_HEIGHT: f32 = 22.;
const SESSION_ROW_ACTION_TOP: f32 = (SESSION_ROW_HEIGHT - SESSION_ROW_ACTION_HEIGHT) / 2.;
const BOTTOM_FOLLOW_THRESHOLD: f32 = 32.;
const CONTENT_SHIFT_TIME_CONSTANT: Duration = Duration::from_millis(55);
const CONTENT_SHIFT_SETTLE_DISTANCE: f32 = 0.5;
const CONVERSATION_EVENT_PAGE_SIZE: u32 = DEFAULT_SESSION_EVENT_PAGE_SIZE;
const TOOL_ACTIVITY_ORB_SIZE: f64 = 20.;
const TOOL_ACTIVITY_ORB_DURATION: Duration = Duration::from_secs(600);
const TOOL_ACTIVITY_ORB_SPEED: f64 = 3.9;
const TOOL_GROUP_SHIMMER_DURATION: Duration = Duration::from_millis(1_600);
const TOOL_GROUP_SHIMMER_BAND_WIDTH: f32 = 0.24;
const MAX_PENDING_IMAGES: usize = 4;
const MAX_IMAGE_FILE_BYTES: u64 = 25 * 1024 * 1024;
const MAX_PROCESSED_IMAGE_BYTES: usize = 8 * 1024 * 1024;
const MAX_TOTAL_IMAGE_BYTES: usize = 6 * 1024 * 1024;
const MAX_IMAGE_DIMENSION: u32 = 2_000;

gpui::actions!(aetherflow, [NewSession, Quit]);

#[derive(Default)]
struct BottomFollowAnimation {
    measured_max_offset: Option<Pixels>,
    content_offset_y: Pixels,
    last_frame_at: Option<Instant>,
    frame_scheduled: bool,
}

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
    Finished(SessionId),
    Failed {
        session_id: Option<SessionId>,
        error: String,
    },
}

enum DaemonStartup {
    Existing(Vec<SessionDescriptor>),
    Launched {
        daemon: ManagedDaemon,
        sessions: Vec<SessionDescriptor>,
    },
}

#[derive(Clone)]
struct PendingImage {
    id: u64,
    path: PathBuf,
    image: ConversationImage,
}

async fn load_sessions_after_daemon_ready(
    client: &AetherflowClient,
    target: &DaemonTarget,
) -> Result<Vec<SessionDescriptor>, String> {
    daemon::recover_stalled_actor(
        target,
        SESSION_DIRECTORY_ACTOR_NAME,
        DEFAULT_SESSION_DIRECTORY_KEY,
    )
    .await
    .map_err(|error| format!("{error:#}"))?;
    client
        .list_sessions()
        .await
        .map_err(|error| format!("{error:#}"))
}

struct DesktopShell {
    runtime: Arc<Runtime>,
    client: AetherflowClient,
    daemon_target: DaemonTarget,
    managed_daemon: Option<ManagedDaemon>,
    sessions: Vec<SessionDescriptor>,
    selected_session_id: Option<SessionId>,
    creating_new_session: bool,
    conversations: HashMap<SessionId, Vec<ConversationItem>>,
    conversation_scrolls: HashMap<SessionId, ScrollHandle>,
    bottom_follow_animations: HashMap<SessionId, BottomFollowAnimation>,
    previous_stream_texts: HashMap<SessionId, String>,
    tool_group_expansion: HashMap<String, bool>,
    expanded_tool_calls: HashSet<String>,
    loading_conversations: HashSet<SessionId>,
    conversation_errors: HashMap<SessionId, String>,
    new_session_messages: Vec<ConversationItem>,
    composer: Entity<InputState>,
    pending_images: Vec<PendingImage>,
    next_pending_image_id: u64,
    active_turn_session_ids: HashSet<SessionId>,
    active_turn_started_at: HashMap<SessionId, Instant>,
    active_turn_tool_group_keys: HashMap<SessionId, String>,
    creating_turn_started_at: Option<Instant>,
    completed_tool_group_durations: HashMap<String, Duration>,
    working_duration_tick_scheduled: bool,
    is_creating_session: bool,
    cancelling_turn_session_ids: HashSet<SessionId>,
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
        let client_options = desktop_client_options();
        let daemon_target = DaemonTarget {
            endpoint: client_options.endpoint.clone(),
            attachment_endpoint: client_options.attachment_endpoint.clone(),
            token: client_options.token.clone(),
            namespace: client_options.namespace.clone(),
            pool: client_options.pool.clone(),
        };
        let mut shell = Self {
            runtime: Arc::new(Runtime::new().expect("failed to start desktop async runtime")),
            client: AetherflowClient::connect(client_options),
            daemon_target,
            managed_daemon: None,
            sessions: Vec::new(),
            selected_session_id: None,
            creating_new_session: false,
            conversations: HashMap::new(),
            conversation_scrolls: HashMap::new(),
            bottom_follow_animations: HashMap::new(),
            previous_stream_texts: HashMap::new(),
            tool_group_expansion: HashMap::new(),
            expanded_tool_calls: HashSet::new(),
            loading_conversations: HashSet::new(),
            conversation_errors: HashMap::new(),
            new_session_messages: Vec::new(),
            composer,
            pending_images: Vec::new(),
            next_pending_image_id: 0,
            active_turn_session_ids: HashSet::new(),
            active_turn_started_at: HashMap::new(),
            active_turn_tool_group_keys: HashMap::new(),
            creating_turn_started_at: None,
            completed_tool_group_durations: HashMap::new(),
            working_duration_tick_scheduled: false,
            is_creating_session: false,
            cancelling_turn_session_ids: HashSet::new(),
            load_state: SessionLoadState::Loading,
            action_error: None,
            _subscriptions: vec![input_subscription],
        };
        shell.connect_daemon(cx);
        shell
    }

    fn connect_daemon(&mut self, cx: &mut Context<Self>) {
        self.load_state = SessionLoadState::Loading;
        let managed_daemon_running = if let Some(daemon) = &mut self.managed_daemon {
            match daemon.is_running() {
                Ok(true) => true,
                Ok(false) => {
                    self.managed_daemon = None;
                    false
                }
                Err(error) => {
                    self.load_state = SessionLoadState::Failed(format!("{error:#}"));
                    cx.notify();
                    return;
                }
            }
        } else {
            false
        };

        let client = self.client.clone();
        let daemon_target = self.daemon_target.clone();
        let request = self.runtime.spawn(async move {
            let runner_snapshot = daemon::runner_snapshot(&daemon_target)
                .await
                .map_err(|error| format!("{error:#}"))?;
            if runner_snapshot.is_ready()
                && daemon::attachment_is_ready(&daemon_target)
                    .await
                    .map_err(|error| format!("{error:#}"))?
            {
                let sessions = load_sessions_after_daemon_ready(&client, &daemon_target).await?;
                return Ok(DaemonStartup::Existing(sessions));
            }

            if managed_daemon_running {
                daemon::wait_for_runner(&daemon_target)
                    .await
                    .map_err(|error| format!("{error:#}"))?;
                let sessions = load_sessions_after_daemon_ready(&client, &daemon_target).await?;
                return Ok(DaemonStartup::Existing(sessions));
            }

            let mut daemon = tokio::task::spawn_blocking(daemon::launch)
                .await
                .map_err(|error| format!("daemon launch task failed: {error}"))?
                .map_err(|error| format!("{error:#}"))?;
            daemon::wait_for_launched_runner(&daemon_target, &mut daemon, runner_snapshot.keys())
                .await
                .map_err(|error| format!("{error:#}"))?;
            let sessions = load_sessions_after_daemon_ready(&client, &daemon_target).await?;
            Ok(DaemonStartup::Launched { daemon, sessions })
        });

        cx.spawn(async move |shell, cx| {
            let result = request
                .await
                .map_err(|error| format!("daemon startup task failed: {error}"))
                .and_then(|result| result);
            let _ = shell.update(cx, |shell, cx| match result {
                Ok(startup) => match startup {
                    DaemonStartup::Existing(sessions) => {
                        shell.replace_sessions(sessions);
                        if let Some(session_id) = shell.selected_session_id {
                            shell.load_conversation(session_id, cx);
                        }
                        cx.notify();
                    }
                    DaemonStartup::Launched { daemon, sessions } => {
                        shell.managed_daemon = Some(daemon);
                        shell.replace_sessions(sessions);
                        if let Some(session_id) = shell.selected_session_id {
                            shell.load_conversation(session_id, cx);
                        }
                        cx.notify();
                    }
                },
                Err(error) => {
                    shell.load_state = SessionLoadState::Failed(error);
                    cx.notify();
                }
            });
        })
        .detach();
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

    fn start_new_session(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.creating_new_session && self.is_creating_session {
            return;
        }
        self.creating_new_session = true;
        self.selected_session_id = None;
        self.new_session_messages.clear();
        self.pending_images.clear();
        self.action_error = None;
        self.composer.update(cx, |input, cx| {
            input.set_value("", window, cx);
            input.focus(window, cx);
        });
        cx.notify();
    }

    fn attach_dropped_images(&mut self, paths: &[PathBuf], cx: &mut Context<Self>) {
        let mut errors = Vec::new();
        for path in paths {
            if self.pending_images.len() >= MAX_PENDING_IMAGES {
                errors.push(format!("You can attach up to {MAX_PENDING_IMAGES} images."));
                break;
            }
            if self.pending_images.iter().any(|image| image.path == *path) {
                continue;
            }
            match load_conversation_image(path) {
                Ok(image) => {
                    let attached_bytes = self
                        .pending_images
                        .iter()
                        .map(|pending| pending.image.data().len())
                        .sum::<usize>();
                    if attached_bytes.saturating_add(image.data().len()) > MAX_TOTAL_IMAGE_BYTES {
                        errors.push("Attachments must total no more than 6 MB.".to_owned());
                        break;
                    }
                    let id = self.next_pending_image_id;
                    self.next_pending_image_id = self.next_pending_image_id.wrapping_add(1);
                    self.pending_images.push(PendingImage {
                        id,
                        path: path.clone(),
                        image,
                    });
                }
                Err(error) => errors.push(error),
            }
        }
        self.action_error = (!errors.is_empty()).then(|| errors.join(" "));
        cx.notify();
    }

    fn remove_pending_image(&mut self, image_id: u64, cx: &mut Context<Self>) {
        self.pending_images.retain(|image| image.id != image_id);
        self.action_error = None;
        cx.notify();
    }

    fn composer_is_sending(&self) -> bool {
        composer_is_sending(
            self.creating_new_session,
            self.is_creating_session,
            self.selected_session_id,
            &self.active_turn_session_ids,
        )
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
                    .session_events(session_id, after_sequence, CONVERSATION_EVENT_PAGE_SIZE)
                    .await
                    .map_err(|error| format!("Could not load session messages: {error:#}"))?;
                let page_len = page.len();
                if let Some(event) = page.last() {
                    after_sequence = event.sequence;
                }
                events.extend(page);
                if page_len < CONVERSATION_EVENT_PAGE_SIZE as usize {
                    break;
                }
            }
            client
                .hydrate_events_attachments(&mut events)
                .await
                .map_err(|error| format!("Could not load session attachments: {error:#}"))?;
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
                        shell.conversations.insert(session_id, messages);
                        shell
                            .conversation_scrolls
                            .get(&session_id)
                            .expect("loaded conversations have a scroll handle")
                            .scroll_to_bottom();
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

    fn follow_conversation_if(&mut self, session_id: SessionId, follow: bool) {
        if follow {
            let previous_max_offset = self
                .conversation_scrolls
                .get(&session_id)
                .expect("selected conversations have a scroll handle")
                .max_offset()
                .height;
            self.bottom_follow_animations
                .entry(session_id)
                .or_default()
                .measured_max_offset
                .get_or_insert(previous_max_offset);
        }
    }

    fn schedule_bottom_follow(
        &mut self,
        session_id: SessionId,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let Some(animation) = self.bottom_follow_animations.get_mut(&session_id) else {
            return;
        };
        if animation.frame_scheduled {
            return;
        }
        animation.frame_scheduled = true;
        cx.on_next_frame(window, move |shell, _, cx| {
            shell.advance_bottom_follow(session_id, cx);
        });
    }

    fn advance_bottom_follow(&mut self, session_id: SessionId, cx: &mut Context<Self>) {
        let Some(scroll_handle) = self.conversation_scrolls.get(&session_id).cloned() else {
            self.bottom_follow_animations.remove(&session_id);
            return;
        };
        let current_offset = scroll_handle.offset();
        let now = Instant::now();
        let Some(animation) = self.bottom_follow_animations.get_mut(&session_id) else {
            return;
        };
        animation.frame_scheduled = false;

        if let Some(previous_max_offset) = animation.measured_max_offset.take() {
            if !is_near_bottom(current_offset.y, previous_max_offset) {
                self.bottom_follow_animations.remove(&session_id);
                cx.notify();
                return;
            }
            let new_max_offset = scroll_handle.max_offset().height;
            let (scroll_y, content_offset_y) = pinned_bottom_offsets(
                previous_max_offset,
                new_max_offset,
                animation.content_offset_y,
            );
            scroll_handle.set_offset(point(current_offset.x, scroll_y));
            animation.content_offset_y = content_offset_y;
            animation.last_frame_at = Some(now);
            cx.notify();
            return;
        }

        if !scroll_handle_is_at_bottom(&scroll_handle) {
            self.bottom_follow_animations.remove(&session_id);
            cx.notify();
            return;
        }

        let frame_duration =
            animation
                .last_frame_at
                .map_or(Duration::from_millis(16), |last_frame| {
                    now.saturating_duration_since(last_frame)
                        .min(Duration::from_millis(50))
                });
        let next_offset_y = next_content_shift_offset(animation.content_offset_y, frame_duration);

        if next_offset_y == px(0.) {
            self.bottom_follow_animations.remove(&session_id);
            cx.notify();
            return;
        }

        animation.content_offset_y = next_offset_y;
        animation.last_frame_at = Some(now);
        cx.notify();
    }

    fn submit_prompt(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.composer_is_sending()
            || self
                .selected_session_id
                .is_some_and(|session_id| self.loading_conversations.contains(&session_id))
        {
            return;
        }

        let prompt = self.composer.read(cx).value().trim().to_owned();
        let conversation_images = self
            .pending_images
            .iter()
            .map(|pending| pending.image.clone())
            .collect::<Vec<_>>();
        if prompt.is_empty() && conversation_images.is_empty() {
            return;
        }
        let attachment_uploads = conversation_images
            .iter()
            .map(|image| (image.mime_type.clone(), image.data().to_vec()))
            .collect::<Vec<_>>();

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
                images: conversation_images,
            }),
            ConversationItem::Message(ConversationMessage {
                role: ConversationRole::Assistant,
                text: String::new(),
                images: Vec::new(),
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
        self.pending_images.clear();
        if let Some(session_id) = session_id {
            self.active_turn_session_ids.insert(session_id);
            self.active_turn_started_at
                .insert(session_id, Instant::now());
            self.cancelling_turn_session_ids.remove(&session_id);
        } else {
            self.is_creating_session = true;
            self.creating_turn_started_at = Some(Instant::now());
        }
        self.schedule_working_duration_tick(cx);
        self.action_error = None;

        let client = self.client.clone();
        let (updates, mut update_rx) = tokio::sync::mpsc::unbounded_channel();
        self.runtime.spawn(async move {
            let mut attachments = Vec::with_capacity(attachment_uploads.len());
            for (media_type, bytes) in attachment_uploads {
                match client.upload_attachment(&media_type, bytes).await {
                    Ok(attachment) => attachments.push(attachment),
                    Err(error) => {
                        let _ = updates.send(PromptUpdate::Failed {
                            session_id,
                            error: format!("Could not upload attachment: {error:#}"),
                        });
                        return;
                    }
                }
            }
            let session_id = match session_id {
                Some(session_id) => session_id,
                None => match client.create_session(CreateSessionOptions::default()).await {
                    Ok(session_id) => {
                        let _ = updates.send(PromptUpdate::Created(session_id));
                        session_id
                    }
                    Err(error) => {
                        let _ = updates.send(PromptUpdate::Failed {
                            session_id: None,
                            error: format!("Could not create session: {error:#}"),
                        });
                        return;
                    }
                },
            };

            let mut stream = match client
                .prompt_session_with_attachments(session_id, prompt, attachments)
                .await
            {
                Ok(stream) => stream,
                Err(error) => {
                    let _ = updates.send(PromptUpdate::Failed {
                        session_id: Some(session_id),
                        error: format!("Could not prompt session: {error:#}"),
                    });
                    return;
                }
            };

            loop {
                match stream.next().await {
                    Ok(Some(event)) => {
                        if let Some(error) = stopped_error(&event) {
                            let _ = updates.send(PromptUpdate::Failed {
                                session_id: Some(session_id),
                                error,
                            });
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
                        let _ = updates.send(PromptUpdate::Finished(session_id));
                        return;
                    }
                    Err(error) => {
                        let _ = updates.send(PromptUpdate::Failed {
                            session_id: Some(session_id),
                            error: format!("Session stream failed: {error:#}"),
                        });
                        return;
                    }
                }
            }
        });

        cx.spawn(async move |shell, cx| {
            while let Some(update) = update_rx.recv().await {
                let finished = matches!(
                    update,
                    PromptUpdate::Finished(_) | PromptUpdate::Failed { .. }
                );
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
                self.is_creating_session = false;
                self.active_turn_session_ids.insert(session_id);
                self.active_turn_started_at.insert(
                    session_id,
                    self.creating_turn_started_at
                        .take()
                        .unwrap_or_else(Instant::now),
                );
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
                let conversation = self.conversations.entry(session_id).or_default();
                apply_tool_event(conversation, &event);
                if let Some(group_key) = conversation.iter().rev().find_map(|item| match item {
                    ConversationItem::ToolGroup(group) => Some(group.key().to_owned()),
                    ConversationItem::Message(_) => None,
                }) {
                    self.active_turn_tool_group_keys
                        .insert(session_id, group_key);
                }
                self.follow_conversation_if(session_id, follow);
            }
            PromptUpdate::Finished(session_id) => {
                self.previous_stream_texts.remove(&session_id);
                self.active_turn_session_ids.remove(&session_id);
                self.finish_turn_timing(session_id);
                self.cancelling_turn_session_ids.remove(&session_id);
                self.load_sessions(cx);
            }
            PromptUpdate::Failed { session_id, error } => {
                if let Some(session_id) = session_id {
                    self.previous_stream_texts.remove(&session_id);
                    self.active_turn_session_ids.remove(&session_id);
                    self.finish_turn_timing(session_id);
                    self.cancelling_turn_session_ids.remove(&session_id);
                } else {
                    self.is_creating_session = false;
                    self.creating_turn_started_at = None;
                }
                self.action_error = Some(error);
            }
        }
    }

    fn finish_turn_timing(&mut self, session_id: SessionId) {
        let Some(started_at) = self.active_turn_started_at.remove(&session_id) else {
            return;
        };
        let Some(group_key) = self.active_turn_tool_group_keys.remove(&session_id) else {
            return;
        };
        self.completed_tool_group_durations
            .insert(group_key, started_at.elapsed());
    }

    fn schedule_working_duration_tick(&mut self, cx: &mut Context<Self>) {
        if self.working_duration_tick_scheduled {
            return;
        }
        self.working_duration_tick_scheduled = true;
        cx.spawn(async move |shell, cx| {
            Timer::after(Duration::from_secs(1)).await;
            let Some(shell) = shell.upgrade() else {
                return;
            };
            shell
                .update(cx, |shell, cx| {
                    shell.working_duration_tick_scheduled = false;
                    if shell.is_creating_session || !shell.active_turn_session_ids.is_empty() {
                        cx.notify();
                        shell.schedule_working_duration_tick(cx);
                    }
                })
                .ok();
        })
        .detach();
    }

    fn cancel_turn(&mut self, cx: &mut Context<Self>) {
        let Some(session_id) = self
            .selected_session_id
            .filter(|session_id| self.active_turn_session_ids.contains(session_id))
        else {
            return;
        };
        if !self.cancelling_turn_session_ids.insert(session_id) {
            return;
        }

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
                    shell.cancelling_turn_session_ids.remove(&session_id);
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
            .mb_0p5()
            .h(px(SESSION_ROW_HEIGHT))
            .px_3()
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
                    .h_full()
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
                    .top(px(SESSION_ROW_ACTION_TOP))
                    .h(px(SESSION_ROW_ACTION_HEIGHT))
                    .px_2()
                    .flex()
                    .items_center()
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
                                    shell.connect_daemon(cx);
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

    fn render_main_panel(&mut self, window: &mut Window, cx: &mut Context<Self>) -> Div {
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
        &mut self,
        session_id: SessionId,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> impl IntoElement {
        self.schedule_bottom_follow(session_id, window, cx);
        let content_offset_y = self
            .bottom_follow_animations
            .get(&session_id)
            .map_or(px(0.), |animation| animation.content_offset_y);
        let scroll_handle = self
            .conversation_scrolls
            .get(&session_id)
            .expect("selected conversations have a scroll handle")
            .clone();
        let conversation = div()
            .id("conversation")
            .flex_1()
            .min_h_0()
            .overflow_y_scroll()
            .track_scroll(&scroll_handle)
            .px_8()
            .py_6()
            .flex()
            .flex_col()
            .text_size(px(CHAT_FONT_SIZE));

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
        let session_has_active_turn = self.active_turn_session_ids.contains(&session_id);
        let active_turn_elapsed = self
            .active_turn_started_at
            .get(&session_id)
            .map(Instant::elapsed);
        let mut transcript = div()
            .relative()
            .top(content_offset_y)
            .w_full()
            .flex()
            .flex_col()
            .gap_5();
        for (index, item) in messages.iter().enumerate() {
            transcript = match item {
                ConversationItem::Message(message) => {
                    let is_empty_assistant = message.role == ConversationRole::Assistant
                        && message.text.is_empty()
                        && message.images.is_empty();
                    if !should_render_conversation_item(
                        item,
                        index,
                        messages.len(),
                        session_has_active_turn,
                    ) {
                        continue;
                    }

                    match message.role {
                        ConversationRole::User => transcript.child(
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
                                        .flex()
                                        .flex_col()
                                        .gap_2()
                                        .rounded_xl()
                                        .bg(rgb(0x25282d))
                                        .when(!message.images.is_empty(), |bubble| {
                                            bubble.child(render_conversation_images(
                                                &message.images,
                                                format!("user-{session_id}-{index}"),
                                            ))
                                        })
                                        .when(!message.text.is_empty(), |bubble| {
                                            bubble.child(
                                                TextView::markdown(
                                                    ("user-markdown", index),
                                                    message.text.clone(),
                                                    window,
                                                    cx,
                                                )
                                                .style(conversation_markdown_style(cx))
                                                .selectable(true)
                                                .w_full(),
                                            )
                                        }),
                                ),
                        ),
                        ConversationRole::Assistant => {
                            let content = if is_empty_assistant {
                                div()
                                    .flex()
                                    .items_center()
                                    .gap_2()
                                    .child(render_tool_activity_orb(format!(
                                        "assistant-{session_id}-{index}"
                                    )))
                                    .child(render_working_text(
                                        format!("assistant-{session_id}-{index}"),
                                        active_turn_elapsed.unwrap_or_default(),
                                    ))
                                    .into_any_element()
                            } else {
                                let is_live_tail = session_has_active_turn
                                    && index == messages.len().saturating_sub(1);
                                div()
                                    .flex()
                                    .flex_col()
                                    .gap_2()
                                    .when(!message.images.is_empty(), |content| {
                                        content.child(render_conversation_images(
                                            &message.images,
                                            format!("assistant-{session_id}-{index}"),
                                        ))
                                    })
                                    .when(!message.text.is_empty(), |content| {
                                        content.child(render_assistant_markdown(
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
                                        ))
                                    })
                                    .into_any_element()
                            };
                            transcript.child(
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
                    let has_running_tool = group
                        .calls
                        .iter()
                        .any(|call| call.status == ToolStatus::Running);
                    let show_activity = tool_group_shows_activity(
                        session_has_active_turn,
                        index,
                        messages.len(),
                        has_running_tool,
                    );
                    transcript.child(self.render_tool_group(
                        group,
                        show_activity,
                        active_turn_elapsed,
                        window,
                        cx,
                    ))
                }
            };
        }

        conversation.child(transcript)
    }

    fn render_tool_group(
        &self,
        group: &ToolGroup,
        show_activity: bool,
        active_turn_elapsed: Option<Duration>,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> gpui::AnyElement {
        let group_key = group.key().to_owned();
        let open = self
            .tool_group_expansion
            .get(&group_key)
            .copied()
            .unwrap_or_else(|| group.should_open_by_default());
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
                .when(show_activity, |header| {
                    header.child(render_tool_activity_orb(format!("group-{group_key}")))
                })
                .child(render_tool_group_summary(
                    group.summary(),
                    &group_key,
                    show_activity,
                    group.calls.len(),
                    group
                        .calls
                        .iter()
                        .filter(|call| call.status == ToolStatus::Failed)
                        .count(),
                    active_turn_elapsed,
                    self.completed_tool_group_durations.get(&group_key).copied(),
                )),
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
                .child(
                    div()
                        .flex_none()
                        .flex()
                        .items_center()
                        .gap_1()
                        .text_color(status_color)
                        .child(status_icon)
                        .when(call.status == ToolStatus::Running, |indicator| {
                            indicator.child(render_tool_activity_orb(format!("call-{}", call.id)))
                        }),
                )
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

    fn render_pending_images(&self, cx: &mut Context<Self>) -> Div {
        div()
            .w_full()
            .flex()
            .flex_wrap()
            .gap_2()
            .children(self.pending_images.iter().map(|pending| {
                let image_id = pending.id;
                div()
                    .id(("pending-image", image_id as usize))
                    .relative()
                    .size(px(48.))
                    .overflow_hidden()
                    .rounded_lg()
                    .border_1()
                    .border_color(rgb(0x3a3e45))
                    .bg(rgb(0x17191d))
                    .child(render_image(&pending.image))
                    .child(
                        div()
                            .id(("remove-pending-image", image_id as usize))
                            .absolute()
                            .top(px(3.))
                            .right(px(3.))
                            .size(px(18.))
                            .flex()
                            .items_center()
                            .justify_center()
                            .rounded_full()
                            .bg(rgba(0x111318dd))
                            .text_xs()
                            .text_color(rgb(0xd6d7d9))
                            .cursor_pointer()
                            .hover(|style| style.bg(rgb(0x2d3036)))
                            .child("×")
                            .on_click(cx.listener(move |shell, _, _, cx| {
                                shell.remove_pending_image(image_id, cx);
                            })),
                    )
            }))
    }

    fn render_composer(&self, cx: &mut Context<Self>) -> Div {
        let is_sending = self.composer_is_sending();
        let disabled = is_sending
            || self
                .selected_session_id
                .is_some_and(|session_id| self.loading_conversations.contains(&session_id));
        let action = if is_sending {
            let cancel_enabled = self.selected_session_id.is_some_and(|session_id| {
                self.active_turn_session_ids.contains(&session_id)
                    && !self.cancelling_turn_session_ids.contains(&session_id)
            });
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
                    .min_h(px(52.))
                    .p_2()
                    .flex()
                    .flex_col()
                    .gap_2()
                    .rounded_xl()
                    .border_1()
                    .border_color(rgb(0x30343a))
                    .bg(rgba(0x1d2025f2))
                    .drag_over::<ExternalPaths>(|style, _, _, _| {
                        style.border_color(rgb(0x8f96ff)).bg(rgba(0x252a3bf2))
                    })
                    .on_drop(cx.listener(|shell, paths: &ExternalPaths, _, cx| {
                        shell.attach_dropped_images(paths.paths(), cx);
                    }))
                    .when(!self.pending_images.is_empty(), |composer| {
                        composer.child(self.render_pending_images(cx))
                    })
                    .child(
                        div()
                            .w_full()
                            .h(px(36.))
                            .flex()
                            .items_center()
                            .gap_2()
                            .pl_1()
                            .child(
                                Input::new(&self.composer)
                                    .text_size(px(CHAT_FONT_SIZE))
                                    .appearance(false)
                                    .bordered(false)
                                    .focus_bordered(false)
                                    .disabled(disabled)
                                    .flex_1(),
                            )
                            .child(action),
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

fn desktop_client_options() -> AetherflowClientOptions {
    let mut options = AetherflowClientOptions::default();
    if let Ok(endpoint) = std::env::var("RIVET_ENDPOINT") {
        options.endpoint = endpoint;
    }
    if let Ok(endpoint) = std::env::var("AETHERFLOW_ATTACHMENT_ENDPOINT") {
        options.attachment_endpoint = endpoint;
    }
    if let Ok(token) = std::env::var("RIVET_TOKEN") {
        options.token = token;
    }
    if let Ok(namespace) = std::env::var("RIVET_NAMESPACE") {
        options.namespace = namespace;
    }
    if let Ok(pool) = std::env::var("RIVET_POOL_NAME") {
        options.pool = pool;
    }
    options
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

fn composer_is_sending(
    creating_new_session: bool,
    is_creating_session: bool,
    selected_session_id: Option<SessionId>,
    active_turn_session_ids: &HashSet<SessionId>,
) -> bool {
    if creating_new_session {
        is_creating_session
    } else {
        selected_session_id.is_some_and(|session_id| active_turn_session_ids.contains(&session_id))
    }
}

fn should_render_conversation_item(
    item: &ConversationItem,
    index: usize,
    item_count: usize,
    session_has_active_turn: bool,
) -> bool {
    let is_empty_assistant = matches!(
        item,
        ConversationItem::Message(ConversationMessage {
            role: ConversationRole::Assistant,
            text,
            images,
        }) if text.is_empty() && images.is_empty()
    );
    !is_empty_assistant || (session_has_active_turn && index.checked_add(1) == Some(item_count))
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

fn load_conversation_image(path: &Path) -> Result<ConversationImage, String> {
    let metadata = std::fs::metadata(path)
        .map_err(|error| format!("Could not read {}: {error}", path.display()))?;
    if !metadata.is_file() {
        return Err(format!("{} is not an image file.", path.display()));
    }
    if metadata.len() > MAX_IMAGE_FILE_BYTES {
        return Err(format!(
            "{} is larger than 25 MB.",
            path.file_name()
                .unwrap_or(path.as_os_str())
                .to_string_lossy()
        ));
    }

    let bytes = std::fs::read(path)
        .map_err(|error| format!("Could not read {}: {error}", path.display()))?;
    let format = image::guess_format(&bytes)
        .map_err(|_| format!("{} is not a supported image.", path.display()))?;
    let mime_type = match format {
        image::ImageFormat::Png => "image/png",
        image::ImageFormat::Jpeg => "image/jpeg",
        image::ImageFormat::Gif => "image/gif",
        image::ImageFormat::WebP => "image/webp",
        _ => return Err(format!("{} is not a supported image.", path.display())),
    };
    let decoded = image::load_from_memory_with_format(&bytes, format)
        .map_err(|error| format!("Could not decode {}: {error}", path.display()))?;
    let (data, mime_type) =
        if decoded.width() > MAX_IMAGE_DIMENSION || decoded.height() > MAX_IMAGE_DIMENSION {
            let resized = decoded.thumbnail(MAX_IMAGE_DIMENSION, MAX_IMAGE_DIMENSION);
            let output_format = if format == image::ImageFormat::Jpeg {
                image::ImageFormat::Jpeg
            } else {
                image::ImageFormat::Png
            };
            let mut encoded = Cursor::new(Vec::new());
            resized
                .write_to(&mut encoded, output_format)
                .map_err(|error| format!("Could not resize {}: {error}", path.display()))?;
            (
                encoded.into_inner(),
                if output_format == image::ImageFormat::Jpeg {
                    "image/jpeg"
                } else {
                    "image/png"
                },
            )
        } else {
            (bytes, mime_type)
        };
    if data.len() > MAX_PROCESSED_IMAGE_BYTES {
        return Err(format!(
            "{} is still larger than 8 MB after processing.",
            path.file_name()
                .unwrap_or(path.as_os_str())
                .to_string_lossy()
        ));
    }

    ConversationImage::new(mime_type, data)
        .ok_or_else(|| format!("{} is not a supported image.", path.display()))
}

fn render_image(image: &ConversationImage) -> gpui::AnyElement {
    img(image.image.clone())
        .w_full()
        .h_full()
        .object_fit(ObjectFit::Cover)
        .into_any_element()
}

fn render_conversation_images(images: &[ConversationImage], key: String) -> gpui::AnyElement {
    div()
        .id(SharedString::from(format!("conversation-images-{key}")))
        .flex()
        .flex_wrap()
        .gap_2()
        .children(images.iter().enumerate().map(|(index, image)| {
            div()
                .id((SharedString::from(key.clone()), index))
                .w(px(180.))
                .h(px(128.))
                .overflow_hidden()
                .rounded_lg()
                .bg(rgb(0x17191d))
                .child(render_image(image))
        }))
        .into_any_element()
}

fn conversation_markdown_style(cx: &App) -> TextViewStyle {
    let theme = gpui_component::Theme::global(cx);
    let mut style = TextViewStyle::default().paragraph_gap(gpui::rems(0.75));
    style.heading_base_font_size = px(CHAT_FONT_SIZE);
    style.highlight_theme = theme.highlight_theme.clone();
    style.is_dark = theme.is_dark();
    style
}

fn render_tool_activity_orb(key: impl Into<SharedString>) -> gpui::AnyElement {
    let key = key.into();
    div()
        .size(px(TOOL_ACTIVITY_ORB_SIZE as f32))
        .flex_none()
        .relative()
        .overflow_hidden()
        .with_animation(
            key,
            Animation::new(TOOL_ACTIVITY_ORB_DURATION).repeat(),
            |orb, progress| {
                let time = f64::from(progress)
                    * TOOL_ACTIVITY_ORB_DURATION.as_secs_f64()
                    * TOOL_ACTIVITY_ORB_SPEED;
                orb.children(tool_activity_frame(time).into_iter().map(|dot| {
                    let diameter = dot.radius * 2.;
                    let gray = ((1. - dot.white.clamp(0., 1.)) * 255.).round() as u32;
                    let color = (gray << 16) | (gray << 8) | gray;
                    div()
                        .absolute()
                        .left(px((dot.x - dot.radius) as f32))
                        .top(px((dot.y - dot.radius) as f32))
                        .size(px(diameter as f32))
                        .rounded_full()
                        .bg(rgb(color))
                        .opacity(dot.alpha as f32)
                }))
            },
        )
        .into_any_element()
}

/// Port of thinking-orbs' MIT-licensed 20px `working`/`orbits` frame.
/// The source geometry is deterministic 3D math; GPUI only paints its final,
/// depth-sorted dots. See vendor/thinking-orbs/LICENSE.
#[derive(Clone, Copy, Debug)]
struct ToolActivityDot {
    x: f64,
    y: f64,
    z: f64,
    radius: f64,
    white: f64,
    alpha: f64,
}

fn tool_activity_frame(time: f64) -> Vec<ToolActivityDot> {
    const ORBIT_COUNT: usize = 3;
    const GHOST_COUNT: usize = 10;
    const PARTICLES_PER_ORBIT: usize = 3;
    const GHOST_RADIUS: f64 = 2.16;
    const GHOST_ALPHA: f64 = 0.5;
    const PARTICLE_RADIUS: f64 = 2.88;
    const PARTICLE_DEPTH_RADIUS: f64 = 3.84;
    const MIN_RADIUS: f64 = 0.3;

    let center = TOOL_ACTIVITY_ORB_SIZE / 2.;
    let outer_radius = center * 0.82;
    let radius_scale = (TOOL_ACTIVITY_ORB_SIZE / 300.).powf(0.6);
    let yaw = time * 0.12;
    let tilt = 0.3;
    let mut dots = Vec::with_capacity(ORBIT_COUNT * (GHOST_COUNT + PARTICLES_PER_ORBIT));

    for orbit in 0..ORBIT_COUNT {
        let orbit = orbit as f64;
        let h1 = thinking_orb_hash(orbit, 1.7);
        let h2 = thinking_orb_hash(orbit, 5.2);
        let h3 = thinking_orb_hash(orbit, 8.9);
        let orbit_radius = outer_radius * (0.45 + 0.52 * h1);
        let theta = h1 * std::f64::consts::TAU;
        let phi = (2. * h2 - 1.).acos();
        let normal_x = phi.sin() * theta.cos();
        let normal_y = phi.cos();
        let normal_z = phi.sin() * theta.sin();
        let mut basis_u_x = -normal_y;
        let mut basis_u_y = normal_x;
        let basis_u_z = 0.;
        let basis_u_length = (basis_u_x * basis_u_x + basis_u_y * basis_u_y)
            .sqrt()
            .max(1e-6);
        basis_u_x /= basis_u_length;
        basis_u_y /= basis_u_length;
        let basis_v_x = normal_y * basis_u_z - normal_z * basis_u_y;
        let basis_v_y = normal_z * basis_u_x - normal_x * basis_u_z;
        let basis_v_z = normal_x * basis_u_y - normal_y * basis_u_x;
        let speed = (0.25 + 0.55 * h3) * if h3 > 0.5 { 1. } else { -1. };

        for index in 0..GHOST_COUNT {
            let angle = index as f64 / GHOST_COUNT as f64 * std::f64::consts::TAU;
            let (x, y, z) = project_tool_activity_point(
                (basis_u_x * angle.cos() + basis_v_x * angle.sin()) * orbit_radius,
                (basis_u_y * angle.cos() + basis_v_y * angle.sin()) * orbit_radius,
                (basis_u_z * angle.cos() + basis_v_z * angle.sin()) * orbit_radius,
                yaw,
                tilt,
                center,
            );
            let depth = (z / orbit_radius + 1.) / 2.;
            dots.push(ToolActivityDot {
                x,
                y,
                z,
                radius: (GHOST_RADIUS * radius_scale).max(MIN_RADIUS),
                white: 0.72,
                alpha: GHOST_ALPHA * (0.4 + 0.6 * depth),
            });
        }

        for index in 0..PARTICLES_PER_ORBIT {
            let angle = time * speed
                + index as f64 / PARTICLES_PER_ORBIT as f64 * std::f64::consts::TAU
                + h2 * 6.;
            let (x, y, z) = project_tool_activity_point(
                (basis_u_x * angle.cos() + basis_v_x * angle.sin()) * orbit_radius,
                (basis_u_y * angle.cos() + basis_v_y * angle.sin()) * orbit_radius,
                (basis_u_z * angle.cos() + basis_v_z * angle.sin()) * orbit_radius,
                yaw,
                tilt,
                center,
            );
            let depth = (z / orbit_radius + 1.) / 2.;
            dots.push(ToolActivityDot {
                x,
                y,
                z,
                radius: ((PARTICLE_RADIUS + PARTICLE_DEPTH_RADIUS * depth) * radius_scale)
                    .max(MIN_RADIUS),
                white: 0.3 - 0.22 * depth,
                alpha: 1.,
            });
        }
    }

    dots.sort_by(|left, right| left.z.total_cmp(&right.z));
    dots
}

fn thinking_orb_hash(a: f64, b: f64) -> f64 {
    let hash = (a * 12.9898 + b * 78.233).sin() * 43_758.545_3;
    hash - hash.floor()
}

fn project_tool_activity_point(
    x: f64,
    y: f64,
    z: f64,
    yaw: f64,
    tilt: f64,
    center: f64,
) -> (f64, f64, f64) {
    let x_rotated = x * yaw.cos() + z * yaw.sin();
    let z_rotated = -x * yaw.sin() + z * yaw.cos();
    let y_rotated = y * tilt.cos() - z_rotated * tilt.sin();
    let depth = y * tilt.sin() + z_rotated * tilt.cos();
    (center + x_rotated, center - y_rotated, depth)
}

fn tool_group_shows_activity(
    session_has_active_turn: bool,
    item_index: usize,
    item_count: usize,
    has_running_tool: bool,
) -> bool {
    has_running_tool || (session_has_active_turn && item_index == item_count.saturating_sub(1))
}

fn render_tool_group_summary(
    summary: String,
    group_key: &str,
    show_activity: bool,
    tool_count: usize,
    failed_count: usize,
    active_elapsed: Option<Duration>,
    completed_elapsed: Option<Duration>,
) -> gpui::AnyElement {
    if show_activity {
        return render_working_tool_summary(
            format!("tool-group-{group_key}"),
            active_elapsed.unwrap_or_default(),
            tool_count,
        );
    }

    if let Some(elapsed) = completed_elapsed {
        return StyledText::new(worked_tool_summary(elapsed, tool_count, failed_count))
            .into_any_element();
    }

    StyledText::new(summary).into_any_element()
}

fn render_working_text(animation_key: String, elapsed: Duration) -> gpui::AnyElement {
    render_working_label(
        animation_key,
        format!("Working for {}", format_duration(elapsed)),
    )
}

fn render_working_tool_summary(
    animation_key: String,
    elapsed: Duration,
    tool_count: usize,
) -> gpui::AnyElement {
    render_working_label(animation_key, working_tool_summary(elapsed, tool_count))
}

fn render_working_label(animation_key: String, label: String) -> gpui::AnyElement {
    let animated_label = label.clone();
    StyledText::new(label)
        .with_animation(
            SharedString::from(format!("working-shimmer-{animation_key}")),
            Animation::new(TOOL_GROUP_SHIMMER_DURATION).repeat(),
            move |text, progress| {
                text.with_highlights(shimmer_highlights(&animated_label, progress))
            },
        )
        .into_any_element()
}

fn working_tool_summary(elapsed: Duration, tool_count: usize) -> String {
    let noun = if tool_count == 1 { "tool" } else { "tools" };
    format!(
        "Working for {} · {tool_count} {noun}",
        format_duration(elapsed)
    )
}

fn worked_tool_summary(elapsed: Duration, tool_count: usize, failed_count: usize) -> String {
    let noun = if tool_count == 1 { "tool" } else { "tools" };
    let mut summary = format!(
        "Worked for {} · {tool_count} {noun}",
        format_duration(elapsed)
    );
    if failed_count > 0 {
        summary.push_str(&format!(" · {failed_count} failed"));
    }
    summary
}

fn format_duration(duration: Duration) -> String {
    let total_seconds = duration.as_secs();
    let hours = total_seconds / 3_600;
    let minutes = total_seconds % 3_600 / 60;
    let seconds = total_seconds % 60;
    if hours > 0 {
        format!("{hours}h{minutes}m{seconds}s")
    } else if minutes > 0 {
        format!("{minutes}m{seconds}s")
    } else {
        format!("{seconds}s")
    }
}

fn shimmer_highlights(text: &str, progress: f32) -> Vec<(Range<usize>, HighlightStyle)> {
    let character_count = text.chars().count();
    text.char_indices()
        .enumerate()
        .filter_map(|(character_index, (byte_index, character))| {
            let intensity = shimmer_intensity(character_index, character_count, progress);
            (intensity > 0.).then(|| {
                (
                    byte_index..byte_index + character.len_utf8(),
                    HighlightStyle {
                        color: Some(interpolate_hsla(
                            rgb(0x85898f).into(),
                            rgb(0xe3e5e8).into(),
                            intensity,
                        )),
                        ..Default::default()
                    },
                )
            })
        })
        .collect()
}

fn shimmer_intensity(character_index: usize, character_count: usize, progress: f32) -> f32 {
    if character_count == 0 {
        return 0.;
    }

    let character_position = if character_count == 1 {
        0.5
    } else {
        character_index as f32 / (character_count - 1) as f32
    };
    let band_center = progress.clamp(0., 1.) * (1. + 2. * TOOL_GROUP_SHIMMER_BAND_WIDTH)
        - TOOL_GROUP_SHIMMER_BAND_WIDTH;
    let distance = (character_position - band_center).abs();
    let intensity = (1. - distance / TOOL_GROUP_SHIMMER_BAND_WIDTH).clamp(0., 1.);
    intensity * intensity
}

fn interpolate_hsla(from: Hsla, to: Hsla, amount: f32) -> Hsla {
    let amount = amount.clamp(0., 1.);
    Hsla {
        h: from.h + (to.h - from.h) * amount,
        s: from.s + (to.s - from.s) * amount,
        l: from.l + (to.l - from.l) * amount,
        a: from.a + (to.a - from.a) * amount,
    }
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

fn pinned_bottom_offsets(
    previous_max_offset: Pixels,
    new_max_offset: Pixels,
    current_content_offset: Pixels,
) -> (Pixels, Pixels) {
    (
        -new_max_offset,
        current_content_offset + new_max_offset - previous_max_offset,
    )
}

fn next_content_shift_offset(current: Pixels, elapsed: Duration) -> Pixels {
    if current.abs() <= px(CONTENT_SHIFT_SETTLE_DISTANCE) {
        return px(0.);
    }

    let elapsed_seconds = elapsed.max(Duration::from_millis(1)).as_secs_f32();
    let time_constant_seconds = CONTENT_SHIFT_TIME_CONSTANT.as_secs_f32();
    let progress = 1. - (-elapsed_seconds / time_constant_seconds).exp();
    current * (1. - progress)
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
        cx.on_action(|_: &Quit, cx| cx.quit());
        cx.bind_keys([
            KeyBinding::new("cmd-n", NewSession, None),
            KeyBinding::new("cmd-q", Quit, None),
        ]);
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
                let new_session_shell = shell.downgrade();
                cx.on_action(move |_: &NewSession, cx| {
                    let Some(window_handle) = cx.active_window() else {
                        return;
                    };
                    let _ = window_handle.update(cx, |_, window, cx| {
                        let _ = new_session_shell.update(cx, |shell, cx| {
                            shell.start_new_session(window, cx);
                        });
                    });
                });
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
    fn shimmer_band_moves_across_the_tool_group_heading() {
        assert_eq!(shimmer_intensity(2, 5, 0.5), 1.);
        assert_eq!(shimmer_intensity(0, 5, 0.5), 0.);
        assert_eq!(shimmer_intensity(4, 5, 0.5), 0.);
    }

    #[test]
    fn shimmer_highlights_use_unicode_byte_boundaries() {
        let highlights = shimmer_highlights("a·b", 0.5);

        assert!(highlights.iter().any(|(range, _)| range == &(1..3)));
        assert!(
            highlights
                .iter()
                .all(|(range, _)| "a·b".is_char_boundary(range.start)
                    && "a·b".is_char_boundary(range.end))
        );
    }

    #[test]
    fn dropped_images_are_validated_from_their_bytes() {
        let directory = tempfile::tempdir().expect("temporary directory");
        let image_path = directory.path().join("screenshot.png");
        image::DynamicImage::new_rgba8(2, 2)
            .save_with_format(&image_path, image::ImageFormat::Png)
            .expect("write test image");

        let image = load_conversation_image(&image_path).expect("valid dropped image");

        assert_eq!(image.mime_type, "image/png");
        assert!(!image.data().is_empty());
    }

    #[test]
    fn tool_activity_frame_matches_thinking_orbs_working_20_golden_data() {
        let frame = tool_activity_frame(0.6);

        assert_eq!(frame.len(), 39);
        assert!(frame.windows(2).all(|dots| dots[0].z <= dots[1].z));
        let sums = frame.iter().fold([0.; 6], |mut sums, dot| {
            for (sum, value) in sums
                .iter_mut()
                .zip([dot.x, dot.y, dot.z, dot.radius, dot.white, dot.alpha])
            {
                *sum += value;
            }
            sums
        });
        for (value, expected) in sums
            .into_iter()
            .zip([390., 390.000001, 0.000001, 21.27006, 23.31, 19.5])
        {
            assert!((value - expected).abs() < 0.0001, "{value} != {expected}");
        }
        for (actual, expected) in [
            (
                frame.first().expect("frame should contain a first dot"),
                [11.235898, 9.940794, -7.451958, 0.425401, 0.72, 0.202026],
            ),
            (
                frame.last().expect("frame should contain a last dot"),
                [9.230714, 10.206142, 7.51188, 1.321364, 0.080613, 1.],
            ),
        ] {
            for (value, expected) in [
                actual.x,
                actual.y,
                actual.z,
                actual.radius,
                actual.white,
                actual.alpha,
            ]
            .into_iter()
            .zip(expected)
            {
                assert!((value - expected).abs() < 0.0001, "{value} != {expected}");
            }
        }
    }

    #[test]
    fn active_turn_keeps_activity_on_its_last_tool_group_between_calls() {
        assert!(tool_group_shows_activity(true, 2, 3, false));
        assert!(!tool_group_shows_activity(true, 1, 3, false));
        assert!(!tool_group_shows_activity(false, 2, 3, false));
        assert!(tool_group_shows_activity(false, 1, 3, true));
    }

    #[test]
    fn working_tool_summary_reports_the_live_tool_count() {
        assert_eq!(
            working_tool_summary(Duration::from_secs(8), 1),
            "Working for 8s · 1 tool"
        );
        assert_eq!(
            working_tool_summary(Duration::from_secs(113), 3),
            "Working for 1m53s · 3 tools"
        );
    }

    #[test]
    fn worked_tool_summary_preserves_duration_and_failures() {
        assert_eq!(
            worked_tool_summary(Duration::from_secs(113), 3, 0),
            "Worked for 1m53s · 3 tools"
        );
        assert_eq!(
            worked_tool_summary(Duration::from_secs(3_723), 2, 1),
            "Worked for 1h2m3s · 2 tools · 1 failed"
        );
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
            images: Vec::new(),
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
                images: Vec::new(),
            })]
        );
    }

    #[test]
    fn assistant_delta_does_not_merge_across_a_user_message() {
        let mut messages = vec![
            ConversationItem::Message(ConversationMessage {
                role: ConversationRole::Assistant,
                text: "First response.".to_owned(),
                images: Vec::new(),
            }),
            ConversationItem::Message(ConversationMessage {
                role: ConversationRole::User,
                text: "Continue.".to_owned(),
                images: Vec::new(),
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
                images: Vec::new(),
            }),
            ConversationItem::Message(ConversationMessage {
                role: ConversationRole::Assistant,
                text: String::new(),
                images: Vec::new(),
            }),
            ConversationItem::Message(ConversationMessage {
                role: ConversationRole::User,
                text: "Try again".to_owned(),
                images: Vec::new(),
            }),
            ConversationItem::Message(ConversationMessage {
                role: ConversationRole::Assistant,
                text: String::new(),
                images: Vec::new(),
            }),
        ];

        let visible_working_placeholders = messages
            .iter()
            .enumerate()
            .filter(|(index, item)| {
                should_render_conversation_item(item, *index, messages.len(), true)
                    && matches!(
                        item,
                        ConversationItem::Message(ConversationMessage {
                            role: ConversationRole::Assistant,
                            text,
                            ..
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
    fn content_shift_moves_toward_zero_without_snapping() {
        let next = next_content_shift_offset(px(100.), Duration::from_millis(16));

        assert!(next < px(100.));
        assert!(next > px(0.));
        assert_eq!(
            next_content_shift_offset(px(0.25), Duration::from_millis(16)),
            px(0.)
        );
    }

    #[test]
    fn bottom_follow_keeps_the_scroll_position_pinned() {
        let (scroll_y, content_offset_y) = pinned_bottom_offsets(px(100.), px(140.), px(0.));

        assert_eq!(scroll_y, px(-140.));
        assert_eq!(content_offset_y, px(40.));
        assert_eq!(scroll_y + content_offset_y, px(-100.));
    }

    #[test]
    fn another_sessions_active_turn_does_not_disable_a_new_session_composer() {
        let active_session_id = SessionId::new();
        let inactive_session_id = SessionId::new();
        let active_turn_session_ids = HashSet::from([active_session_id]);
        let disabled = composer_is_sending(true, false, None, &active_turn_session_ids);

        assert!(!disabled);
        assert!(!composer_is_sending(
            false,
            false,
            Some(inactive_session_id),
            &active_turn_session_ids,
        ));
        assert!(composer_is_sending(
            false,
            false,
            Some(active_session_id),
            &active_turn_session_ids,
        ));
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
                    images: Vec::new(),
                }),
                ConversationItem::Message(ConversationMessage {
                    role: ConversationRole::Assistant,
                    text: "In the archive.".to_owned(),
                    images: Vec::new(),
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

    #[test]
    fn conversation_history_uses_transport_safe_event_pages() {
        assert_eq!(
            CONVERSATION_EVENT_PAGE_SIZE,
            DEFAULT_SESSION_EVENT_PAGE_SIZE
        );
    }
}
