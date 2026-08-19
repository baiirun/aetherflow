mod daemon;
mod preferences;
mod transcript;

use aetherflow_pi::{
    AetherflowClient, AetherflowClientOptions, AssistantMessageEvent, CreateSessionOptions,
    DEFAULT_SESSION_DIRECTORY_KEY, DEFAULT_SESSION_EVENT_PAGE_SIZE, DEFAULT_WORKSPACE_CATALOG_KEY,
    PiEvent, SESSION_DIRECTORY_ACTOR_NAME, SessionDescriptor, SessionEvent, SessionEventPayload,
    WORKSPACE_CATALOG_ACTOR_NAME,
};
use aetherflow_storage::{DirectoryId, SessionId, Workspace, WorkspaceId};
use daemon::{DaemonTarget, ManagedDaemon};
use gpui::{
    Animation, AnimationExt as _, App, Application, AssetSource, Bounds, Context, Div, Entity,
    ExternalPaths, HighlightStyle, Hsla, KeyBinding, ObjectFit, PathBuilder, PathPromptOptions,
    Pixels, ScrollHandle, SharedString, StyledImage as _, StyledText, Subscription, Timer,
    TitlebarOptions, Window, WindowBackgroundAppearance, WindowBounds, WindowOptions, canvas, div,
    ease_out_quint, img, point, prelude::*, px, rgb, rgba, size,
};
use gpui_component::input::{Input, InputEvent, InputState};
use gpui_component::resizable::{h_resizable, resizable_panel};
use gpui_component::text::{TextView, TextViewStyle};
use gpui_component::{Icon, IconName};
use preferences::DesktopPreferences;
use std::{
    borrow::Cow,
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
const SIDEBAR_MIN_WIDTH: f32 = 220.;
const SIDEBAR_MAX_WIDTH: f32 = 420.;
const CHAT_FONT_SIZE: f32 = 14.;
const SESSION_ROW_HEIGHT: f32 = 30.;
const SESSION_ROW_ACTION_HEIGHT: f32 = 22.;
const SESSION_ROW_ACTION_TOP: f32 = (SESSION_ROW_HEIGHT - SESSION_ROW_ACTION_HEIGHT) / 2.;
const WORKSPACE_FOLDER_ANIMATION_DURATION: Duration = Duration::from_millis(160);
const BOTTOM_FOLLOW_THRESHOLD: f32 = 32.;
const CONTENT_SHIFT_TIME_CONSTANT: Duration = Duration::from_millis(55);
const CONTENT_SHIFT_SETTLE_DISTANCE: f32 = 0.5;
const CONVERSATION_EVENT_PAGE_SIZE: u32 = DEFAULT_SESSION_EVENT_PAGE_SIZE;
const TOOL_GROUP_SHIMMER_DURATION: Duration = Duration::from_millis(1_600);
const TOOL_GROUP_SHIMMER_BAND_WIDTH: f32 = 0.24;
const AGENT_BLOB_SIZE: f32 = 32.;
const AGENT_BLOB_LOOP_DURATION: Duration = Duration::from_secs(600);
const AGENT_BLOB_TRANSITION_DURATION: Duration = Duration::from_millis(240);
const MAX_PENDING_IMAGES: usize = 4;
const MAX_IMAGE_FILE_BYTES: u64 = 25 * 1024 * 1024;
const MAX_PROCESSED_IMAGE_BYTES: usize = 8 * 1024 * 1024;
const MAX_TOTAL_IMAGE_BYTES: usize = 6 * 1024 * 1024;
const MAX_IMAGE_DIMENSION: u32 = 2_000;

struct DesktopAssets;

impl AssetSource for DesktopAssets {
    fn load(&self, path: &str) -> anyhow::Result<Option<Cow<'static, [u8]>>> {
        let bytes: Option<&'static [u8]> = match path {
            "icons/plus.svg" => Some(include_bytes!("../assets/icons/plus.svg")),
            "icons/close.svg" => Some(include_bytes!("../assets/icons/close.svg")),
            "icons/folder.svg" => Some(include_bytes!("../assets/icons/folder.svg")),
            "icons/folder-closed.svg" => Some(include_bytes!("../assets/icons/folder-closed.svg")),
            "icons/folder-open.svg" => Some(include_bytes!("../assets/icons/folder-open.svg")),
            _ => None,
        };
        Ok(bytes.map(Cow::Borrowed))
    }

    fn list(&self, _path: &str) -> anyhow::Result<Vec<SharedString>> {
        Ok(Vec::new())
    }
}

gpui::actions!(aetherflow, [ArchiveActiveSession, NewSession, Quit]);

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

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum AgentBlobState {
    Idle,
    Thinking,
    Working,
    Responding,
    Error,
}

#[derive(Clone, Copy, Debug)]
struct AgentBlobTransition {
    from: AgentBlobState,
    to: AgentBlobState,
    started_at: Instant,
}

#[derive(Clone, Copy, Debug)]
struct AgentTurnPresentation {
    session_id: SessionId,
    blob_transition: AgentBlobTransition,
    elapsed: Option<Duration>,
}

impl AgentBlobTransition {
    fn idle() -> Self {
        Self {
            from: AgentBlobState::Idle,
            to: AgentBlobState::Idle,
            started_at: Instant::now(),
        }
    }
}

enum DaemonStartup {
    Existing(DesktopData),
    Launched {
        daemon: ManagedDaemon,
        data: DesktopData,
    },
}

struct DesktopData {
    sessions: Vec<SessionDescriptor>,
    workspaces: Vec<Workspace>,
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
) -> Result<DesktopData, String> {
    daemon::recover_stalled_actor(
        target,
        SESSION_DIRECTORY_ACTOR_NAME,
        DEFAULT_SESSION_DIRECTORY_KEY,
    )
    .await
    .map_err(|error| format!("{error:#}"))?;
    daemon::recover_stalled_actor(
        target,
        WORKSPACE_CATALOG_ACTOR_NAME,
        DEFAULT_WORKSPACE_CATALOG_KEY,
    )
    .await
    .map_err(|error| format!("{error:#}"))?;
    let (sessions, workspaces) = tokio::try_join!(client.list_sessions(), client.list_workspaces())
        .map_err(|error| format!("{error:#}"))?;
    Ok(DesktopData {
        sessions,
        workspaces,
    })
}

struct DesktopShell {
    runtime: Arc<Runtime>,
    client: AetherflowClient,
    daemon_target: DaemonTarget,
    managed_daemon: Option<ManagedDaemon>,
    sessions: Vec<SessionDescriptor>,
    workspaces: Vec<Workspace>,
    selected_session_id: Option<SessionId>,
    selected_workspace_id: Option<WorkspaceId>,
    creating_directory_id: Option<DirectoryId>,
    creating_new_session: bool,
    conversations: HashMap<SessionId, Vec<ConversationItem>>,
    conversation_scrolls: HashMap<SessionId, ScrollHandle>,
    bottom_follow_animations: HashMap<SessionId, BottomFollowAnimation>,
    previous_stream_texts: HashMap<SessionId, String>,
    tool_group_expansion: HashMap<String, bool>,
    expanded_tool_calls: HashSet<String>,
    archived_sessions_collapsed: bool,
    collapsed_workspace_ids: HashSet<WorkspaceId>,
    workspace_disclosure_transition_versions: HashMap<WorkspaceId, u64>,
    loading_conversations: HashSet<SessionId>,
    conversation_errors: HashMap<SessionId, String>,
    new_session_messages: Vec<ConversationItem>,
    composer: Entity<InputState>,
    workspace_name: Entity<InputState>,
    workspace_modal_open: bool,
    workspace_draft_directories: Vec<PathBuf>,
    workspace_modal_error: Option<String>,
    is_selecting_workspace_directories: bool,
    pending_images: Vec<PendingImage>,
    next_pending_image_id: u64,
    active_turn_session_ids: HashSet<SessionId>,
    active_turn_started_at: HashMap<SessionId, Instant>,
    active_turn_tool_group_keys: HashMap<SessionId, String>,
    agent_blob_transitions: HashMap<SessionId, AgentBlobTransition>,
    creating_turn_started_at: Option<Instant>,
    completed_tool_group_durations: HashMap<String, Duration>,
    working_duration_tick_scheduled: bool,
    is_creating_session: bool,
    is_creating_workspace: bool,
    cancelling_turn_session_ids: HashSet<SessionId>,
    load_state: SessionLoadState,
    action_error: Option<String>,
    _subscriptions: Vec<Subscription>,
}

impl DesktopShell {
    fn new(window: &mut Window, cx: &mut Context<Self>) -> Self {
        let (preferences, preferences_error) = match DesktopPreferences::load() {
            Ok(preferences) => (preferences, None),
            Err(error) => (
                DesktopPreferences::default(),
                Some(format!("Could not load desktop preferences: {error:#}")),
            ),
        };
        let composer = cx.new(|cx| InputState::new(window, cx).placeholder("Message Aetherflow"));
        let workspace_name = cx.new(|cx| InputState::new(window, cx).placeholder("Workspace name"));
        let input_subscription = cx.subscribe_in(
            &composer,
            window,
            |shell, _, event: &InputEvent, window, cx| {
                if matches!(event, InputEvent::PressEnter { .. }) {
                    shell.submit_prompt(window, cx);
                }
            },
        );
        let workspace_name_subscription = cx.subscribe_in(
            &workspace_name,
            window,
            |shell, _, event: &InputEvent, _, cx| match event {
                InputEvent::Change => cx.notify(),
                InputEvent::PressEnter { .. } => shell.create_workspace(cx),
                InputEvent::Focus | InputEvent::Blur => {}
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
            workspaces: Vec::new(),
            selected_session_id: None,
            selected_workspace_id: None,
            creating_directory_id: None,
            creating_new_session: false,
            conversations: HashMap::new(),
            conversation_scrolls: HashMap::new(),
            bottom_follow_animations: HashMap::new(),
            previous_stream_texts: HashMap::new(),
            tool_group_expansion: HashMap::new(),
            expanded_tool_calls: HashSet::new(),
            archived_sessions_collapsed: preferences.archived_sessions_collapsed,
            collapsed_workspace_ids: preferences.collapsed_workspace_ids.into_iter().collect(),
            workspace_disclosure_transition_versions: HashMap::new(),
            loading_conversations: HashSet::new(),
            conversation_errors: HashMap::new(),
            new_session_messages: Vec::new(),
            composer,
            workspace_name,
            workspace_modal_open: false,
            workspace_draft_directories: Vec::new(),
            workspace_modal_error: None,
            is_selecting_workspace_directories: false,
            pending_images: Vec::new(),
            next_pending_image_id: 0,
            active_turn_session_ids: HashSet::new(),
            active_turn_started_at: HashMap::new(),
            active_turn_tool_group_keys: HashMap::new(),
            agent_blob_transitions: HashMap::new(),
            creating_turn_started_at: None,
            completed_tool_group_durations: HashMap::new(),
            working_duration_tick_scheduled: false,
            is_creating_session: false,
            is_creating_workspace: false,
            cancelling_turn_session_ids: HashSet::new(),
            load_state: SessionLoadState::Loading,
            action_error: preferences_error,
            _subscriptions: vec![input_subscription, workspace_name_subscription],
        };
        shell.connect_daemon(cx);
        shell
    }

    fn toggle_archived_sessions(&mut self, cx: &mut Context<Self>) {
        self.archived_sessions_collapsed = !self.archived_sessions_collapsed;
        self.save_preferences();
        cx.notify();
    }

    fn toggle_workspace(&mut self, workspace_id: WorkspaceId, cx: &mut Context<Self>) {
        if !self.collapsed_workspace_ids.remove(&workspace_id) {
            self.collapsed_workspace_ids.insert(workspace_id);
        }
        let transition_version = self
            .workspace_disclosure_transition_versions
            .entry(workspace_id)
            .or_default();
        *transition_version = transition_version.saturating_add(1);
        self.save_preferences();
        cx.notify();
    }

    fn save_preferences(&mut self) {
        let mut collapsed_workspace_ids = self
            .collapsed_workspace_ids
            .iter()
            .copied()
            .collect::<Vec<_>>();
        collapsed_workspace_ids.sort_by_key(ToString::to_string);
        let preferences = DesktopPreferences {
            archived_sessions_collapsed: self.archived_sessions_collapsed,
            collapsed_workspace_ids,
        };
        if let Err(error) = preferences.save() {
            self.action_error = Some(format!("Could not save desktop preferences: {error:#}"));
        }
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
            let replaced_incompatible_daemon = daemon::replace_incompatible_daemon(&daemon_target)
                .await
                .map_err(|error| format!("{error:#}"))?;
            if !replaced_incompatible_daemon
                && runner_snapshot.is_ready()
                && daemon::attachment_is_ready(&daemon_target)
                    .await
                    .map_err(|error| format!("{error:#}"))?
            {
                let data = load_sessions_after_daemon_ready(&client, &daemon_target).await?;
                return Ok(DaemonStartup::Existing(data));
            }

            if managed_daemon_running && !replaced_incompatible_daemon {
                daemon::wait_for_runner(&daemon_target)
                    .await
                    .map_err(|error| format!("{error:#}"))?;
                let data = load_sessions_after_daemon_ready(&client, &daemon_target).await?;
                return Ok(DaemonStartup::Existing(data));
            }

            let mut daemon = tokio::task::spawn_blocking(daemon::launch)
                .await
                .map_err(|error| format!("daemon launch task failed: {error}"))?
                .map_err(|error| format!("{error:#}"))?;
            daemon::wait_for_launched_runner(&daemon_target, &mut daemon, runner_snapshot.keys())
                .await
                .map_err(|error| format!("{error:#}"))?;
            let data = load_sessions_after_daemon_ready(&client, &daemon_target).await?;
            Ok(DaemonStartup::Launched { daemon, data })
        });

        cx.spawn(async move |shell, cx| {
            let result = request
                .await
                .map_err(|error| format!("daemon startup task failed: {error}"))
                .and_then(|result| result);
            let _ = shell.update(cx, |shell, cx| match result {
                Ok(startup) => match startup {
                    DaemonStartup::Existing(data) => {
                        shell.replace_data(data);
                        if let Some(session_id) = shell.selected_session_id {
                            shell.load_conversation(session_id, cx);
                        }
                        cx.notify();
                    }
                    DaemonStartup::Launched { daemon, data } => {
                        shell.managed_daemon = Some(daemon);
                        shell.replace_data(data);
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
            let (sessions, workspaces) =
                tokio::try_join!(client.list_sessions(), client.list_workspaces())
                    .map_err(|error| format!("{error:#}"))?;
            Ok::<_, String>(DesktopData {
                sessions,
                workspaces,
            })
        });

        cx.spawn(async move |shell, cx| {
            let result = request
                .await
                .map_err(|error| format!("session request task failed: {error}"))
                .and_then(|result| result);
            let _ = shell.update(cx, |shell, cx| {
                match result {
                    Ok(data) => {
                        shell.replace_data(data);
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

    fn replace_data(&mut self, data: DesktopData) {
        self.workspaces = data.workspaces;
        self.replace_sessions(data.sessions);
        if self.selected_workspace_id.is_none() {
            self.selected_workspace_id = self
                .selected_session()
                .map(session_workspace_id)
                .or_else(|| self.workspaces.first().map(|workspace| workspace.id));
        }
    }

    fn selected_session(&self) -> Option<&SessionDescriptor> {
        let selected = self.selected_session_id?;
        self.sessions.iter().find(|session| session.id == selected)
    }

    fn select_session(&mut self, session_id: SessionId, cx: &mut Context<Self>) {
        self.creating_new_session = false;
        self.selected_session_id = Some(session_id);
        if let Some((workspace_id, directory_id)) = self
            .sessions
            .iter()
            .find(|session| session.id == session_id)
            .map(session_workspace_ids)
        {
            self.selected_workspace_id = Some(workspace_id);
            self.creating_directory_id = Some(directory_id);
        }
        self.action_error = None;
        self.load_conversation(session_id, cx);
    }

    fn start_new_session(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.creating_new_session && self.is_creating_session {
            return;
        }
        if let Some((workspace_id, directory_id)) =
            self.selected_session().map(session_workspace_ids)
        {
            self.selected_workspace_id = Some(workspace_id);
            self.creating_directory_id = Some(directory_id);
        }
        let Some(workspace_id) = self
            .selected_workspace_id
            .or_else(|| self.workspaces.first().map(|workspace| workspace.id))
        else {
            self.open_workspace_modal(window, cx);
            return;
        };
        self.selected_workspace_id = Some(workspace_id);
        if self.creating_directory_id.is_none()
            || self
                .workspace_directory(workspace_id, self.creating_directory_id)
                .is_none()
        {
            self.creating_directory_id = self
                .workspaces
                .iter()
                .find(|workspace| workspace.id == workspace_id)
                .map(|workspace| workspace.primary_directory_id);
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

    fn workspace_directory(
        &self,
        workspace_id: WorkspaceId,
        directory_id: Option<DirectoryId>,
    ) -> Option<&aetherflow_storage::Directory> {
        let directory_id = directory_id?;
        self.workspaces
            .iter()
            .find(|workspace| workspace.id == workspace_id)?
            .directories
            .iter()
            .find(|directory| directory.id == directory_id)
    }

    fn open_workspace_modal(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.workspace_modal_open {
            return;
        }
        self.workspace_modal_open = true;
        self.workspace_draft_directories.clear();
        self.workspace_modal_error = None;
        self.action_error = None;
        self.workspace_name.update(cx, |input, cx| {
            input.set_value("", window, cx);
            input.focus(window, cx);
        });
        cx.notify();
    }

    fn close_workspace_modal(&mut self, cx: &mut Context<Self>) {
        if self.is_creating_workspace {
            return;
        }
        self.workspace_modal_open = false;
        self.workspace_draft_directories.clear();
        self.workspace_modal_error = None;
        self.is_selecting_workspace_directories = false;
        cx.notify();
    }

    fn select_workspace_directories(&mut self, cx: &mut Context<Self>) {
        if self.is_selecting_workspace_directories || self.is_creating_workspace {
            return;
        }
        self.is_selecting_workspace_directories = true;
        self.workspace_modal_error = None;
        let paths = cx.prompt_for_paths(PathPromptOptions {
            files: false,
            directories: true,
            multiple: true,
            prompt: Some("Open".into()),
        });
        cx.spawn(async move |shell, cx| {
            let selection = paths.await;
            let Some(shell) = shell.upgrade() else {
                return;
            };
            shell
                .update(cx, |shell, cx| match selection {
                    Ok(Ok(Some(paths))) => {
                        shell.is_selecting_workspace_directories = false;
                        for path in paths {
                            if !shell.workspace_draft_directories.contains(&path) {
                                shell.workspace_draft_directories.push(path);
                            }
                        }
                        cx.notify();
                    }
                    Ok(Ok(None)) => {
                        shell.is_selecting_workspace_directories = false;
                        cx.notify();
                    }
                    Ok(Err(error)) => {
                        shell.is_selecting_workspace_directories = false;
                        shell.workspace_modal_error =
                            Some(format!("Could not open directory picker: {error:#}"));
                        cx.notify();
                    }
                    Err(error) => {
                        shell.is_selecting_workspace_directories = false;
                        shell.workspace_modal_error =
                            Some(format!("Directory picker closed unexpectedly: {error}"));
                        cx.notify();
                    }
                })
                .ok();
        })
        .detach();
    }

    fn remove_workspace_directory_draft(&mut self, index: usize, cx: &mut Context<Self>) {
        if index < self.workspace_draft_directories.len() && !self.is_creating_workspace {
            self.workspace_draft_directories.remove(index);
            cx.notify();
        }
    }

    fn create_workspace(&mut self, cx: &mut Context<Self>) {
        if self.is_creating_workspace {
            return;
        }
        let name = self.workspace_name.read(cx).value().trim().to_owned();
        if name.is_empty() {
            self.workspace_modal_error = Some("Enter a Workspace name.".to_owned());
            cx.notify();
            return;
        }
        if self.workspace_draft_directories.is_empty() {
            self.workspace_modal_error = Some("Add at least one directory.".to_owned());
            cx.notify();
            return;
        }
        self.is_creating_workspace = true;
        self.workspace_modal_error = None;
        let paths = self.workspace_draft_directories.clone();
        let client = self.client.clone();
        let request = self
            .runtime
            .spawn(async move { client.create_workspace(name, paths).await });
        cx.spawn(async move |shell, cx| {
            let result = request
                .await
                .map_err(|error| format!("workspace task failed: {error}"))
                .and_then(|result| result.map_err(|error| format!("{error:#}")));
            let _ = shell.update(cx, |shell, cx| {
                shell.is_creating_workspace = false;
                match result {
                    Ok(workspace) => {
                        shell.selected_workspace_id = Some(workspace.id);
                        shell.creating_directory_id = Some(workspace.primary_directory_id);
                        shell.workspaces.push(workspace);
                        shell.workspace_modal_open = false;
                        shell.workspace_draft_directories.clear();
                        shell.workspace_modal_error = None;
                        shell.creating_new_session = true;
                        shell.selected_session_id = None;
                        shell.new_session_messages.clear();
                        shell.pending_images.clear();
                        shell.action_error = None;
                    }
                    Err(error) => shell.workspace_modal_error = Some(error),
                }
                cx.notify();
            });
        })
        .detach();
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
        let content_offset_y = self
            .bottom_follow_animations
            .get(&session_id)
            .map_or(px(0.), |animation| animation.content_offset_y);
        self.conversation_scrolls
            .get(&session_id)
            .is_none_or(|scroll_handle| {
                is_near_animated_bottom(
                    scroll_handle.offset().y,
                    scroll_handle.max_offset().height,
                    content_offset_y,
                )
            })
    }

    fn follow_conversation_if(&mut self, session_id: SessionId, follow: bool) {
        if follow {
            let content_offset_y = self
                .bottom_follow_animations
                .get(&session_id)
                .map_or(px(0.), |animation| animation.content_offset_y);
            let previous_max_offset = unshifted_max_offset(
                self.conversation_scrolls
                    .get(&session_id)
                    .expect("selected conversations have a scroll handle")
                    .max_offset()
                    .height,
                content_offset_y,
            );
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
            let new_max_offset = unshifted_max_offset(
                scroll_handle.max_offset().height,
                animation.content_offset_y,
            );
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

        if !is_near_animated_bottom(
            current_offset.y,
            scroll_handle.max_offset().height,
            animation.content_offset_y,
        ) {
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
        let new_session_workspace =
            create_new.then(|| self.selected_workspace_id.zip(self.creating_directory_id));
        let new_session_workspace = match new_session_workspace {
            Some(Some(selection)) => Some(selection),
            Some(None) => {
                self.action_error = Some("Open a directory before creating a session.".to_owned());
                cx.notify();
                return;
            }
            None => None,
        };
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
            self.set_agent_blob_state(session_id, AgentBlobState::Thinking);
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
                None => {
                    let Some((workspace_id, directory_id)) = new_session_workspace else {
                        unreachable!("new sessions require a workspace")
                    };
                    let mut options = CreateSessionOptions::new(workspace_id);
                    options.directory_id = Some(directory_id);
                    match client.create_session(options).await {
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
                    }
                }
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
                self.set_agent_blob_state(session_id, AgentBlobState::Thinking);
                let messages = std::mem::take(&mut self.new_session_messages);
                self.conversations.insert(session_id, messages);
                self.conversation_scrolls
                    .entry(session_id)
                    .or_default()
                    .scroll_to_bottom();
                self.load_sessions(cx);
            }
            PromptUpdate::TextDelta { session_id, delta } => {
                self.set_agent_blob_state(session_id, AgentBlobState::Responding);
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
                self.set_agent_blob_state(session_id, AgentBlobState::Working);
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
                self.set_agent_blob_state(session_id, AgentBlobState::Idle);
                self.finish_turn_timing(session_id);
                self.cancelling_turn_session_ids.remove(&session_id);
                self.load_sessions(cx);
            }
            PromptUpdate::Failed { session_id, error } => {
                if let Some(session_id) = session_id {
                    self.previous_stream_texts.remove(&session_id);
                    self.active_turn_session_ids.remove(&session_id);
                    self.set_agent_blob_state(session_id, AgentBlobState::Error);
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

    fn set_agent_blob_state(&mut self, session_id: SessionId, state: AgentBlobState) {
        let transition = self
            .agent_blob_transitions
            .entry(session_id)
            .or_insert_with(AgentBlobTransition::idle);
        if transition.to == state {
            return;
        }
        *transition = AgentBlobTransition {
            from: transition.to,
            to: state,
            started_at: Instant::now(),
        };
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

    fn archive_active_session(&mut self, cx: &mut Context<Self>) {
        let Some(session_id) = archive_target_session_id(self.selected_session_id, &self.sessions)
        else {
            return;
        };
        self.set_session_archived(session_id, true, cx);
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
        nested: bool,
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
            .when(nested, |row| row.pl(px(40.)).pr_3())
            .when(!nested, |row| row.px_3())
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
            SessionLoadState::Loaded => {
                list = list.child(
                    div()
                        .group("workspaces-heading")
                        .h(px(28.))
                        .px_3()
                        .flex()
                        .items_center()
                        .justify_between()
                        .text_xs()
                        .text_color(rgb(0x66696d))
                        .child("Workspaces")
                        .child(
                            div()
                                .id("create-workspace")
                                .size(px(24.))
                                .flex()
                                .items_center()
                                .justify_center()
                                .rounded_md()
                                .cursor_pointer()
                                .opacity(0.)
                                .group_hover("workspaces-heading", |action| action.opacity(1.))
                                .hover(|style| style.bg(rgb(0x2b2d2f)).text_color(rgb(0xd6d7d9)))
                                .child(Icon::new(IconName::Plus).size_4().text_color(rgb(0x8b8e92)))
                                .on_click(cx.listener(|shell, _, window, cx| {
                                    shell.open_workspace_modal(window, cx);
                                })),
                        ),
                );

                for (workspace_index, workspace) in self.workspaces.iter().enumerate() {
                    let workspace_id = workspace.id;
                    let primary_directory_id = workspace.primary_directory_id;
                    let workspace_collapsed = self.collapsed_workspace_ids.contains(&workspace_id);
                    let transition_version = self
                        .workspace_disclosure_transition_versions
                        .get(&workspace_id)
                        .copied();
                    let group = format!("workspace-row-{workspace_id}");
                    list = list.child(
                        div()
                            .group(group.clone())
                            .id(("workspace", workspace_index))
                            .relative()
                            .mt_2()
                            .h(px(32.))
                            .px_3()
                            .flex()
                            .items_center()
                            .gap_2()
                            .cursor_pointer()
                            .child(render_workspace_folder_icon(
                                workspace_id,
                                workspace_collapsed,
                                transition_version,
                            ))
                            .child(
                                div()
                                    .flex_1()
                                    .min_w_0()
                                    .truncate()
                                    .text_sm()
                                    .text_color(rgb(0xd6d7d9))
                                    .child(workspace.name.clone()),
                            )
                            .child(
                                div()
                                    .id(("new-workspace-session", workspace_index))
                                    .size(px(24.))
                                    .flex()
                                    .items_center()
                                    .justify_center()
                                    .rounded_md()
                                    .opacity(0.)
                                    .group_hover(group, |action| action.opacity(1.))
                                    .hover(|style| style.bg(rgb(0x3a3c3f)))
                                    .child(
                                        Icon::new(IconName::Plus)
                                            .size_4()
                                            .text_color(rgb(0xb8bbc0)),
                                    )
                                    .on_click(cx.listener(move |shell, _, window, cx| {
                                        cx.stop_propagation();
                                        shell.selected_session_id = None;
                                        shell.selected_workspace_id = Some(workspace_id);
                                        shell.creating_directory_id = Some(primary_directory_id);
                                        shell.start_new_session(window, cx);
                                    })),
                            )
                            .on_click(cx.listener(move |shell, _, _, cx| {
                                shell.toggle_workspace(workspace_id, cx);
                            })),
                    );
                    if !workspace_collapsed {
                        for (index, session) in self.sessions.iter().enumerate() {
                            if !session.archived && session_workspace_id(session) == workspace_id {
                                list = list.child(self.render_session_row(
                                    session.clone(),
                                    index,
                                    true,
                                    cx,
                                ));
                            }
                        }
                    }
                }

                if self.workspaces.is_empty() && self.sessions.is_empty() {
                    list = list.child(
                        div()
                            .px_3()
                            .pt_3()
                            .text_sm()
                            .text_color(rgb(0x8b93a1))
                            .child("Create a Workspace to start."),
                    );
                }

                if self.sessions.iter().any(|session| session.archived) {
                    let archived_sessions_collapsed = self.archived_sessions_collapsed;
                    list = list.child(
                        div()
                            .id("archived-sessions-toggle")
                            .mt_5()
                            .px_3()
                            .pb_2()
                            .text_xs()
                            .text_color(rgb(0x66696d))
                            .cursor_pointer()
                            .hover(|style| style.text_color(rgb(0x929599)))
                            .child("Archived")
                            .on_click(cx.listener(|shell, _, _, cx| {
                                shell.toggle_archived_sessions(cx);
                            })),
                    );
                    if !archived_sessions_collapsed {
                        for (index, session) in self.sessions.iter().enumerate() {
                            if session.archived {
                                list = list.child(self.render_session_row(
                                    session.clone(),
                                    index,
                                    false,
                                    cx,
                                ));
                            }
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
            .w_full()
            .h_full()
            .flex()
            .flex_col()
            .bg(rgba(0x101318d9))
            .border_r_1()
            .border_color(rgb(0x2a303b))
            .child(list)
    }

    fn render_workspace_modal(&self, cx: &mut Context<Self>) -> Div {
        let can_create = !self.workspace_name.read(cx).value().trim().is_empty()
            && !self.workspace_draft_directories.is_empty()
            && !self.is_creating_workspace;
        let mut directories = div().flex().flex_col().gap_2();
        for (index, path) in self.workspace_draft_directories.iter().enumerate() {
            let path_text = path.display().to_string();
            directories = directories.child(
                div()
                    .id(("workspace-directory-draft", index))
                    .h(px(38.))
                    .px_3()
                    .flex()
                    .items_center()
                    .gap_2()
                    .rounded_lg()
                    .bg(rgb(0x202225))
                    .border_1()
                    .border_color(rgb(0x303338))
                    .child(
                        Icon::new(IconName::Folder)
                            .size_4()
                            .text_color(rgb(0xaeb1b5)),
                    )
                    .child(
                        div()
                            .flex_1()
                            .min_w_0()
                            .truncate()
                            .text_sm()
                            .text_color(rgb(0xd6d7d9))
                            .child(path_text),
                    )
                    .child(
                        div()
                            .id(("remove-workspace-directory", index))
                            .size(px(24.))
                            .flex()
                            .items_center()
                            .justify_center()
                            .rounded_md()
                            .cursor_pointer()
                            .hover(|style| style.bg(rgb(0x34373b)))
                            .child(
                                Icon::new(IconName::Close)
                                    .size_4()
                                    .text_color(rgb(0x8b8e92)),
                            )
                            .on_click(cx.listener(move |shell, _, _, cx| {
                                shell.remove_workspace_directory_draft(index, cx);
                            })),
                    ),
            );
        }

        div()
            .absolute()
            .top_0()
            .right_0()
            .bottom_0()
            .left_0()
            .flex()
            .items_center()
            .justify_center()
            .bg(rgba(0x00000099))
            .child(
                div()
                    .w(px(640.))
                    .max_h(px(680.))
                    .mx_6()
                    .p_6()
                    .flex()
                    .flex_col()
                    .gap_4()
                    .rounded_2xl()
                    .border_1()
                    .border_color(rgb(0x2a2d31))
                    .bg(rgb(0x17191c))
                    .shadow_xl()
                    .child(
                        div()
                            .flex()
                            .items_center()
                            .justify_between()
                            .child(
                                div()
                                    .text_xl()
                                    .font_weight(gpui::FontWeight::SEMIBOLD)
                                    .child("Create workspace"),
                            )
                            .child(
                                div()
                                    .id("close-workspace-modal")
                                    .size(px(28.))
                                    .flex()
                                    .items_center()
                                    .justify_center()
                                    .rounded_md()
                                    .cursor_pointer()
                                    .hover(|style| style.bg(rgb(0x292c30)))
                                    .child(
                                        Icon::new(IconName::Close)
                                            .size_4()
                                            .text_color(rgb(0xaeb1b5)),
                                    )
                                    .on_click(cx.listener(|shell, _, _, cx| {
                                        shell.close_workspace_modal(cx);
                                    })),
                            ),
                    )
                    .child(
                        div()
                            .h(px(44.))
                            .px_3()
                            .flex()
                            .items_center()
                            .gap_2()
                            .rounded_lg()
                            .border_1()
                            .border_color(rgb(0x626aa5))
                            .bg(rgb(0x1b1d20))
                            .child(
                                Icon::new(IconName::Folder)
                                    .size_4()
                                    .text_color(rgb(0xb8bbc0)),
                            )
                            .child(
                                Input::new(&self.workspace_name)
                                    .appearance(false)
                                    .bordered(false)
                                    .focus_bordered(false)
                                    .flex_1()
                                    .disabled(self.is_creating_workspace),
                            ),
                    )
                    .child(
                        div()
                            .text_sm()
                            .font_weight(gpui::FontWeight::SEMIBOLD)
                            .child("Directories"),
                    )
                    .child(directories)
                    .child(
                        div()
                            .id("add-workspace-directories")
                            .min_h(px(112.))
                            .px_4()
                            .flex()
                            .flex_col()
                            .items_center()
                            .justify_center()
                            .gap_2()
                            .rounded_lg()
                            .border_1()
                            .border_color(rgb(0x303338))
                            .bg(rgb(0x1b1d20))
                            .cursor_pointer()
                            .hover(|style| style.bg(rgb(0x202327)).border_color(rgb(0x3b3f45)))
                            .child(
                                Icon::new(IconName::FolderOpen)
                                    .size_5()
                                    .text_color(rgb(0x8b8e92)),
                            )
                            .child(div().text_sm().text_color(rgb(0xd6d7d9)).child(
                                if self.is_selecting_workspace_directories {
                                    "Opening directory picker…"
                                } else {
                                    "Add directories Aetherflow can read and edit"
                                },
                            ))
                            .on_click(cx.listener(|shell, _, _, cx| {
                                shell.select_workspace_directories(cx);
                            })),
                    )
                    .when_some(self.workspace_modal_error.clone(), |modal, error| {
                        modal.child(
                            div()
                                .text_sm()
                                .text_color(rgb(0xf29b9b))
                                .child(truncate(&error, 180)),
                        )
                    })
                    .child(
                        div()
                            .mt_2()
                            .flex()
                            .items_center()
                            .justify_end()
                            .gap_3()
                            .child(
                                div()
                                    .id("cancel-workspace-modal")
                                    .h(px(38.))
                                    .px_4()
                                    .flex()
                                    .items_center()
                                    .rounded_lg()
                                    .cursor_pointer()
                                    .text_sm()
                                    .text_color(rgb(0xaeb1b5))
                                    .hover(|style| style.bg(rgb(0x24272b)))
                                    .child("Cancel")
                                    .on_click(cx.listener(|shell, _, _, cx| {
                                        shell.close_workspace_modal(cx);
                                    })),
                            )
                            .child(
                                div()
                                    .id("submit-workspace-modal")
                                    .h(px(38.))
                                    .px_4()
                                    .flex()
                                    .items_center()
                                    .rounded_lg()
                                    .text_sm()
                                    .font_weight(gpui::FontWeight::SEMIBOLD)
                                    .when(can_create, |button| {
                                        button
                                            .cursor_pointer()
                                            .bg(rgb(0xe7eaf0))
                                            .text_color(rgb(0x181a1d))
                                            .hover(|style| style.bg(rgb(0xffffff)))
                                    })
                                    .when(!can_create, |button| {
                                        button.bg(rgb(0x2a2d31)).text_color(rgb(0x696d72))
                                    })
                                    .child(if self.is_creating_workspace {
                                        "Creating…"
                                    } else {
                                        "Create workspace"
                                    })
                                    .on_click(cx.listener(|shell, _, _, cx| {
                                        shell.create_workspace(cx);
                                    })),
                            ),
                    ),
            )
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
        let agent_blob_transition = self
            .agent_blob_transitions
            .get(&session_id)
            .copied()
            .unwrap_or_else(AgentBlobTransition::idle);
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
                                    .child(render_agent_blob(session_id, agent_blob_transition))
                                    .child(render_working_text(
                                        format!("assistant-{session_id}-{index}"),
                                        active_turn_elapsed.unwrap_or_default(),
                                    ))
                                    .into_any_element()
                            } else {
                                let is_transcript_tail = index == messages.len().saturating_sub(1);
                                let is_live_tail = session_has_active_turn && is_transcript_tail;
                                let message_content = div()
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
                                    });
                                div()
                                    .flex()
                                    .items_start()
                                    .gap_3()
                                    .child(div().size(px(AGENT_BLOB_SIZE)).flex_none().when(
                                        is_transcript_tail,
                                        |slot| {
                                            slot.child(render_agent_blob(
                                                session_id,
                                                agent_blob_transition,
                                            ))
                                        },
                                    ))
                                    .child(message_content.flex_1().min_w_0().max_w(px(720.)))
                                    .into_any_element()
                            };
                            transcript.child(
                                div()
                                    .id(("assistant-message", index))
                                    .w_full()
                                    .max_w(px(764.))
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
                        AgentTurnPresentation {
                            session_id,
                            blob_transition: agent_blob_transition,
                            elapsed: active_turn_elapsed,
                        },
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
        agent_turn: AgentTurnPresentation,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> gpui::AnyElement {
        let group_key = group.key().to_owned();
        let open = tool_group_is_open(self.tool_group_expansion.get(&group_key).copied());
        let toggle_group_key = group_key.clone();
        let mut container =
            div().w_full().max_w(px(764.)).text_sm().child(
                div()
                    .id(SharedString::from(format!("tool-group-{group_key}")))
                    .h(px(36.))
                    .flex()
                    .items_center()
                    .gap_3()
                    .cursor_pointer()
                    .text_color(rgb(0x85898f))
                    .hover(|style| style.text_color(rgb(0xb5b8bd)))
                    .on_click(cx.listener(move |shell, _, _, cx| {
                        shell
                            .tool_group_expansion
                            .insert(toggle_group_key.clone(), !open);
                        cx.notify();
                    }))
                    .child(div().size(px(AGENT_BLOB_SIZE)).flex_none().when(
                        show_activity,
                        |slot| {
                            slot.child(render_agent_blob(
                                agent_turn.session_id,
                                agent_turn.blob_transition,
                            ))
                        },
                    ))
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
                        agent_turn.elapsed,
                        self.completed_tool_group_durations.get(&group_key).copied(),
                    )),
            );

        if open {
            container = container.child(
                div()
                    .ml(px(44.))
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
                        .child(status_icon),
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
            .relative()
            .flex()
            .bg(rgba(0x00000000))
            .font_family("Inter Variable")
            .text_color(rgb(0xe7eaf0))
            .child(
                h_resizable("desktop-panels")
                    .child(
                        resizable_panel()
                            .size(px(SIDEBAR_WIDTH))
                            .size_range(px(SIDEBAR_MIN_WIDTH)..px(SIDEBAR_MAX_WIDTH))
                            .child(self.render_sidebar(cx)),
                    )
                    .child(resizable_panel().child(self.render_main_panel(window, cx))),
            )
            .when(self.workspace_modal_open, |root| {
                root.child(self.render_workspace_modal(cx))
            })
    }
}

fn render_workspace_folder_icon(
    workspace_id: WorkspaceId,
    collapsed: bool,
    transition_version: Option<u64>,
) -> gpui::AnyElement {
    let target_openness = if collapsed { 0. } else { 1. };
    let Some(transition_version) = transition_version else {
        return render_workspace_folder_drawing(target_openness);
    };

    div()
        .size_4()
        .flex_none()
        .relative()
        .with_animation(
            SharedString::from(format!(
                "workspace-folder-{workspace_id}-{transition_version}"
            )),
            Animation::new(WORKSPACE_FOLDER_ANIMATION_DURATION).with_easing(ease_out_quint()),
            move |folder, progress| {
                let openness = if collapsed { 1. - progress } else { progress };
                folder.child(render_workspace_folder_drawing(openness))
            },
        )
        .into_any_element()
}

#[derive(Clone, Copy, Debug, PartialEq)]
struct WorkspaceFolderGeometry {
    back_left_end: (f32, f32),
    back_right_end: (f32, f32),
    top_left: (f32, f32),
    top_right: (f32, f32),
    bottom_right: (f32, f32),
    bottom_left: (f32, f32),
}

fn workspace_folder_geometry(openness: f32) -> WorkspaceFolderGeometry {
    let openness = openness.clamp(0., 1.);
    WorkspaceFolderGeometry {
        back_left_end: interpolate_point((3., 17.5), (3., 7.), openness),
        back_right_end: interpolate_point((21., 17.5), (21., 10.), openness),
        // The flap starts at the top of the closed folder and travels down and
        // outward. Starting it at the bottom makes the flap appear to grow up.
        top_left: interpolate_point((3., 8.5), (4.5, 10.), openness),
        top_right: interpolate_point((21., 8.5), (21.5, 10.), openness),
        bottom_right: interpolate_point((21., 17.5), (18.5, 19.), openness),
        bottom_left: interpolate_point((3., 17.5), (3.5, 19.), openness),
    }
}

fn interpolate_point(from: (f32, f32), to: (f32, f32), progress: f32) -> (f32, f32) {
    (
        from.0 + (to.0 - from.0) * progress,
        from.1 + (to.1 - from.1) * progress,
    )
}

fn render_workspace_folder_drawing(openness: f32) -> gpui::AnyElement {
    let geometry = workspace_folder_geometry(openness);
    canvas(
        move |bounds, _, _| {
            let scale = f32::from(bounds.size.width).min(f32::from(bounds.size.height)) / 24.;
            let stroke_width = px(2. * scale);
            let position = |(x, y): (f32, f32)| {
                point(
                    bounds.origin.x + px(x * scale),
                    bounds.origin.y + px(y * scale),
                )
            };

            let mut back = PathBuilder::stroke(stroke_width);
            back.move_to(position(geometry.back_left_end));
            back.line_to(position((3., 6.5)));
            back.curve_to(position((5.5, 4.)), position((3., 4.)));
            back.line_to(position((10., 4.)));
            back.line_to(position((12., 6.)));
            back.line_to(position((18.5, 6.)));
            back.curve_to(position((21., 8.5)), position((21., 6.)));
            back.line_to(position(geometry.back_right_end));

            let mut front = PathBuilder::stroke(stroke_width);
            front.move_to(position(geometry.top_left));
            front.line_to(position(geometry.bottom_left));
            front.line_to(position(geometry.bottom_right));
            front.line_to(position(geometry.top_right));

            let mut opening = PathBuilder::stroke(stroke_width);
            opening.move_to(position(geometry.top_left));
            opening.line_to(position(geometry.top_right));

            (back.build(), front.build(), opening.build())
        },
        move |_, (back, front, opening), window, _| {
            let color = Hsla::from(rgb(0xb8bbc0));
            if let Ok(back) = back {
                window.paint_path(back, color);
            }
            if let Ok(front) = front {
                window.paint_path(front, color);
            }
            if let Ok(opening) = opening {
                window.paint_path(opening, color.opacity(openness));
            }
        },
    )
    .size_4()
    .into_any_element()
}

fn short_id(id: SessionId) -> String {
    short_uuid(&id.to_string())
}

fn session_workspace_ids(session: &SessionDescriptor) -> (WorkspaceId, DirectoryId) {
    (
        session.workspace.workspace_id,
        session.workspace.working_directory_id,
    )
}

fn session_workspace_id(session: &SessionDescriptor) -> WorkspaceId {
    session.workspace.workspace_id
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

fn archive_target_session_id(
    selected_session_id: Option<SessionId>,
    sessions: &[SessionDescriptor],
) -> Option<SessionId> {
    let selected_session_id = selected_session_id?;
    sessions
        .iter()
        .find(|session| session.id == selected_session_id && !session.archived)
        .map(|session| session.id)
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

fn render_agent_blob(session_id: SessionId, transition: AgentBlobTransition) -> gpui::AnyElement {
    div()
        .size(px(AGENT_BLOB_SIZE))
        .flex_none()
        .relative()
        .overflow_hidden()
        .with_animation(
            SharedString::from(format!("agent-blob-{session_id}")),
            Animation::new(AGENT_BLOB_LOOP_DURATION).repeat(),
            move |blob, progress| {
                let time = progress * AGENT_BLOB_LOOP_DURATION.as_secs_f32();
                let transition_progress = agent_blob_transition_progress(
                    transition.started_at.elapsed(),
                    AGENT_BLOB_TRANSITION_DURATION,
                );
                let pose = interpolate_agent_blob_pose(
                    agent_blob_pose(transition.from, time),
                    agent_blob_pose(transition.to, time),
                    transition_progress,
                );
                blob.child(render_agent_blob_frame(pose))
            },
        )
        .into_any_element()
}

#[derive(Clone, Copy, Debug, PartialEq)]
struct AgentBlobPose {
    outline: [(f32, f32); 10],
    left_eye: (f32, f32),
    right_eye: (f32, f32),
    left_eye_size: (f32, f32),
    right_eye_size: (f32, f32),
    mouth: (f32, f32, f32, f32),
}

fn agent_blob_pose(state: AgentBlobState, time: f32) -> AgentBlobPose {
    let slow = (time * 2.1).sin();
    let quick = (time * 5.4).sin();
    match state {
        AgentBlobState::Idle => {
            let breath = (time * 1.25).sin();
            let eye_openness = idle_blink_openness(time);
            let gaze = idle_gaze_offset(time);
            let eye_size = (2.5 + (1. - eye_openness) * 0.5, 3.4 * eye_openness);
            AgentBlobPose {
                outline: [
                    (12., 1.2 + breath * 0.55),
                    (15.1 + breath * 0.15, 6.5 + breath * 0.2),
                    (21.8 - breath * 0.35, 5.6),
                    (18.1 - breath * 0.2, 11.5),
                    (22.2 - breath * 0.4, 18.2),
                    (14.5, 16.8 - breath * 0.3),
                    (9.2, 22.2 - breath * 0.55),
                    (7.7 + breath * 0.2, 15.4),
                    (1.8 + breath * 0.35, 11.5),
                    (7.6 + breath * 0.2, 8.6),
                ],
                left_eye: (9.1 + gaze.0, 10.4 + gaze.1),
                right_eye: (15.1 + gaze.0, 10.4 + gaze.1),
                left_eye_size: eye_size,
                right_eye_size: eye_size,
                mouth: (10.4, 14.3 + breath * 0.12, 3.3, 1.2),
            }
        }
        AgentBlobState::Thinking => {
            let pondering = (time * 0.95).sin();
            let gaze = (0.45 + pondering * 0.4, -0.45 + pondering * 0.15);
            AgentBlobPose {
                outline: [
                    (13.5 + slow * 1.2, 0.8),
                    (15.8, 6.8),
                    (22.6, 6.),
                    (18.4, 11.3),
                    (21.1, 18.8),
                    (14., 17.2),
                    (8.4, 22.7),
                    (7.5, 15.3),
                    (2., 12.7),
                    (8.1 - slow * 0.8, 8.2),
                ],
                left_eye: (9.8 + gaze.0, 9.3 + gaze.1),
                right_eye: (15.6 + gaze.0, 8.5 + gaze.1),
                left_eye_size: (2.3, 2.55 + (pondering + 1.) * 0.35),
                right_eye_size: (2.3, 3.25 - (pondering + 1.) * 0.35),
                mouth: (11.3 + pondering * 0.25, 14.1, 3., 1.2),
            }
        }
        AgentBlobState::Working => {
            let scan = (time * 3.1).sin();
            AgentBlobPose {
                outline: [
                    (12. + quick * 0.8, 2.2),
                    (15.6, 7.),
                    (23., 7. + quick * 1.3),
                    (18.7, 11.8),
                    (22. + quick * 1.1, 18.),
                    (14.2, 17.),
                    (8., 22. - quick * 0.9),
                    (7., 15.),
                    (1.4 - quick * 1.1, 12.),
                    (7.4, 8.),
                ],
                left_eye: (8.9 + scan * 0.65, 10.7),
                right_eye: (15.2 + scan * 0.65, 10.7),
                left_eye_size: (3., 1.45 + scan.max(0.) * 0.35),
                right_eye_size: (3., 1.45 + (-scan).max(0.) * 0.35),
                mouth: (10.1, 14.2 + quick * 0.12, 4., 1.2),
            }
        }
        AgentBlobState::Responding => {
            let cadence = (quick + 1.) * 0.5;
            let gaze = (time * 1.7).sin() * 0.25;
            AgentBlobPose {
                outline: [
                    (12., 1.4 - slow * 0.8),
                    (15.2, 6.6),
                    (22.3, 5.8 + slow * 0.8),
                    (18.3, 11.4),
                    (22.4, 18.6),
                    (14.4, 16.8),
                    (9., 22.8 + slow * 0.75),
                    (7.5, 15.4),
                    (1.4, 11.4 - slow * 0.8),
                    (7.7, 8.4),
                ],
                left_eye: (9. + gaze, 10.1),
                right_eye: (15.1 + gaze, 10.1),
                left_eye_size: (2.5, 3.2 + cadence * 0.45),
                right_eye_size: (2.5, 3.65 - cadence * 0.35),
                mouth: (10.7, 13.1, 2.7, 2.8 + cadence * 1.3),
            }
        }
        AgentBlobState::Error => AgentBlobPose {
            outline: [
                (12., 3.2),
                (15.2, 7.4),
                (21.4, 7.8),
                (18., 12.3),
                (20.7, 20.4),
                (14., 18.),
                (8.4, 23.),
                (7.1, 16.4),
                (2.1, 14.2),
                (7.5, 9.4),
            ],
            left_eye: (8.9, 11.),
            right_eye: (15.2, 11.),
            left_eye_size: (3.2, 1.25),
            right_eye_size: (2.6, 1.55),
            mouth: (11., 14.2, 2.2, 3.),
        },
    }
}

fn idle_blink_openness(time: f32) -> f32 {
    let phase = time.rem_euclid(5.6);
    if phase < 5.32 {
        return 1.;
    }

    let progress = (phase - 5.32) / 0.28;
    1. - (progress * std::f32::consts::PI).sin() * 0.82
}

fn idle_gaze_offset(time: f32) -> (f32, f32) {
    let phase = time.rem_euclid(9.);
    match phase {
        phase if phase < 2.4 => (0., 0.),
        phase if phase < 2.65 => {
            interpolate_point((0., 0.), (-0.75, 0.15), smooth_step((phase - 2.4) / 0.25))
        }
        phase if phase < 5. => (-0.75, 0.15),
        phase if phase < 5.25 => interpolate_point(
            (-0.75, 0.15),
            (0.65, -0.45),
            smooth_step((phase - 5.) / 0.25),
        ),
        phase if phase < 7.8 => (0.65, -0.45),
        phase if phase < 8.1 => {
            interpolate_point((0.65, -0.45), (0., 0.), smooth_step((phase - 7.8) / 0.3))
        }
        _ => (0., 0.),
    }
}

fn interpolate_agent_blob_pose(
    from: AgentBlobPose,
    to: AgentBlobPose,
    progress: f32,
) -> AgentBlobPose {
    AgentBlobPose {
        outline: std::array::from_fn(|index| {
            interpolate_point(from.outline[index], to.outline[index], progress)
        }),
        left_eye: interpolate_point(from.left_eye, to.left_eye, progress),
        right_eye: interpolate_point(from.right_eye, to.right_eye, progress),
        left_eye_size: interpolate_point(from.left_eye_size, to.left_eye_size, progress),
        right_eye_size: interpolate_point(from.right_eye_size, to.right_eye_size, progress),
        mouth: (
            interpolate_value(from.mouth.0, to.mouth.0, progress),
            interpolate_value(from.mouth.1, to.mouth.1, progress),
            interpolate_value(from.mouth.2, to.mouth.2, progress),
            interpolate_value(from.mouth.3, to.mouth.3, progress),
        ),
    }
}

fn agent_blob_transition_progress(elapsed: Duration, duration: Duration) -> f32 {
    let progress = (elapsed.as_secs_f32() / duration.as_secs_f32()).clamp(0., 1.);
    smooth_step(progress)
}

fn smooth_step(progress: f32) -> f32 {
    let progress = progress.clamp(0., 1.);
    progress * progress * (3. - 2. * progress)
}

fn interpolate_value(from: f32, to: f32, progress: f32) -> f32 {
    from + (to - from) * progress
}

fn render_agent_blob_frame(pose: AgentBlobPose) -> gpui::AnyElement {
    let scale = AGENT_BLOB_SIZE / 24.;
    let eye = |position: (f32, f32), size: (f32, f32)| {
        div()
            .absolute()
            .left(px((position.0 - size.0 / 2.) * scale))
            .top(px((position.1 - size.1 / 2.) * scale))
            .w(px(size.0 * scale))
            .h(px(size.1 * scale))
            .rounded_full()
            .bg(rgb(0xf1ecdf))
    };

    div()
        .size(px(AGENT_BLOB_SIZE))
        .relative()
        .child(
            canvas(
                move |bounds, _, _| {
                    let position = |(x, y): (f32, f32)| {
                        point(
                            bounds.origin.x + px(x * scale),
                            bounds.origin.y + px(y * scale),
                        )
                    };
                    let build_path = |mut path: PathBuilder| {
                        path.move_to(position(midpoint(pose.outline[7], pose.outline[0])));
                        for index in 0..pose.outline.len() {
                            let current = pose.outline[index];
                            let next = pose.outline[(index + 1) % pose.outline.len()];
                            path.curve_to(position(midpoint(current, next)), position(current));
                        }
                        path.close();
                        path.build()
                    };
                    (
                        build_path(PathBuilder::fill()),
                        build_path(PathBuilder::stroke(px(1.1 * scale))),
                    )
                },
                |_, (fill, outline), window, _| {
                    if let Ok(fill) = fill {
                        window.paint_path(fill, Hsla::from(rgb(0x686dc2)));
                    }
                    if let Ok(outline) = outline {
                        window.paint_path(outline, Hsla::from(rgb(0x3e437d)));
                    }
                },
            )
            .size(px(AGENT_BLOB_SIZE)),
        )
        .child(eye(pose.left_eye, pose.left_eye_size))
        .child(eye(pose.right_eye, pose.right_eye_size))
        .child(
            div()
                .absolute()
                .left(px(pose.mouth.0 * scale))
                .top(px(pose.mouth.1 * scale))
                .w(px(pose.mouth.2 * scale))
                .h(px(pose.mouth.3 * scale))
                .rounded_full()
                .bg(rgb(0xf1ecdf)),
        )
        .into_any_element()
}

fn midpoint(left: (f32, f32), right: (f32, f32)) -> (f32, f32) {
    ((left.0 + right.0) / 2., (left.1 + right.1) / 2.)
}

fn tool_group_shows_activity(
    session_has_active_turn: bool,
    item_index: usize,
    item_count: usize,
    has_running_tool: bool,
) -> bool {
    has_running_tool || (session_has_active_turn && item_index == item_count.saturating_sub(1))
}

fn tool_group_is_open(explicit_expansion: Option<bool>) -> bool {
    explicit_expansion.unwrap_or(false)
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
    let current_source = if is_live_tail {
        streaming_markdown_source(&message.text)
    } else {
        Cow::Borrowed(message.text.as_str())
    };
    let current = TextView::markdown(
        assistant_markdown_id(session_id, index, message.text.len(), is_live_tail),
        current_source.into_owned(),
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
        streaming_markdown_source(previous_text).into_owned(),
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

fn streaming_markdown_source(markdown: &str) -> Cow<'_, str> {
    let Some((syntax_start, label_start, label_end)) = incomplete_inline_link(markdown) else {
        return Cow::Borrowed(markdown);
    };

    let mut stabilized = String::with_capacity(syntax_start + label_end - label_start);
    stabilized.push_str(&markdown[..syntax_start]);
    stabilized.push_str(&markdown[label_start..label_end]);
    Cow::Owned(stabilized)
}

fn incomplete_inline_link(markdown: &str) -> Option<(usize, usize, usize)> {
    let mut label_starts = Vec::new();
    let mut escaped = false;
    for (index, character) in markdown.char_indices() {
        if escaped {
            escaped = false;
            continue;
        }
        if character == '\\' {
            escaped = true;
            continue;
        }
        if character == '[' {
            label_starts.push(index);
            continue;
        }
        if character != ']' {
            continue;
        }

        let label_start = label_starts.pop()?;
        let destination_start = index + character.len_utf8();
        if !markdown[destination_start..].starts_with('(') {
            if destination_start == markdown.len() {
                return Some((
                    link_syntax_start(markdown, label_start),
                    label_start + 1,
                    index,
                ));
            }
            continue;
        }
        let destination_start = destination_start + 1;
        if markdown_destination_is_closed(&markdown[destination_start..]) {
            continue;
        }

        return Some((
            link_syntax_start(markdown, label_start),
            label_start + 1,
            index,
        ));
    }
    label_starts.first().copied().map(|label_start| {
        (
            link_syntax_start(markdown, label_start),
            label_start + 1,
            markdown.len(),
        )
    })
}

fn link_syntax_start(markdown: &str, label_start: usize) -> usize {
    label_start
        .checked_sub(1)
        .filter(|start| markdown.as_bytes()[*start] == b'!')
        .unwrap_or(label_start)
}

fn markdown_destination_is_closed(destination: &str) -> bool {
    let mut depth = 1_u32;
    let mut escaped = false;
    for character in destination.chars() {
        if escaped {
            escaped = false;
            continue;
        }
        if character == '\\' {
            escaped = true;
            continue;
        }
        match character {
            '(' => depth += 1,
            ')' => {
                depth -= 1;
                if depth == 0 {
                    return true;
                }
            }
            _ => {}
        }
    }
    false
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

fn is_near_animated_bottom(offset_y: Pixels, max_offset: Pixels, content_offset_y: Pixels) -> bool {
    is_near_bottom(offset_y, unshifted_max_offset(max_offset, content_offset_y))
}

fn unshifted_max_offset(max_offset: Pixels, content_offset_y: Pixels) -> Pixels {
    max_offset - content_offset_y
}

fn is_near_bottom(offset_y: Pixels, max_offset: Pixels) -> bool {
    max_offset + offset_y <= px(BOTTOM_FOLLOW_THRESHOLD)
}

fn desktop_key_bindings() -> [KeyBinding; 3] {
    [
        KeyBinding::new("cmd-shift-a", ArchiveActiveSession, None),
        KeyBinding::new("cmd-n", NewSession, None),
        KeyBinding::new("cmd-q", Quit, None),
    ]
}

fn main() {
    Application::new()
        .with_assets(DesktopAssets)
        .run(|cx: &mut App| {
            gpui_component::init(cx);
            cx.on_action(|_: &Quit, cx| cx.quit());
            cx.bind_keys(desktop_key_bindings());
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
                    window_background: WindowBackgroundAppearance::Blurred,
                    ..Default::default()
                },
                |window, cx| {
                    let shell = cx.new(|cx| DesktopShell::new(window, cx));
                    let archive_session_shell = shell.downgrade();
                    cx.on_action(move |_: &ArchiveActiveSession, cx| {
                        let _ = archive_session_shell.update(cx, |shell, cx| {
                            shell.archive_active_session(cx);
                        });
                    });
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
    use aetherflow_storage::{AgentId, SessionAssociation, SessionWorkspace};
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
    fn agent_blob_states_share_topology_and_interpolate() {
        let idle = agent_blob_pose(AgentBlobState::Idle, 0.);
        let working = agent_blob_pose(AgentBlobState::Working, 0.);
        let midpoint = interpolate_agent_blob_pose(idle, working, 0.5);

        assert_eq!(idle.outline.len(), working.outline.len());
        assert_eq!(
            midpoint.outline[0],
            (
                (idle.outline[0].0 + working.outline[0].0) / 2.,
                (idle.outline[0].1 + working.outline[0].1) / 2.,
            )
        );
        assert_eq!(
            midpoint.left_eye,
            (
                (idle.left_eye.0 + working.left_eye.0) / 2.,
                (idle.left_eye.1 + working.left_eye.1) / 2.,
            )
        );
    }

    #[test]
    fn idle_agent_blob_breathes_and_blinks_between_long_open_eye_intervals() {
        let resting = agent_blob_pose(AgentBlobState::Idle, 0.);
        let breathing = agent_blob_pose(AgentBlobState::Idle, 1.25);
        let blinking = agent_blob_pose(AgentBlobState::Idle, 5.46);

        assert_ne!(resting.outline, breathing.outline);
        assert_eq!(idle_blink_openness(0.), 1.);
        assert_eq!(idle_blink_openness(5.), 1.);
        assert!(idle_blink_openness(5.46) < 0.2);
        assert!(blinking.left_eye_size.1 < resting.left_eye_size.1);
    }

    #[test]
    fn agent_blob_expressions_change_within_a_state() {
        let idle_centered = agent_blob_pose(AgentBlobState::Idle, 1.);
        let idle_looking_left = agent_blob_pose(AgentBlobState::Idle, 3.);
        let thinking_a = agent_blob_pose(AgentBlobState::Thinking, 0.);
        let thinking_b = agent_blob_pose(AgentBlobState::Thinking, 3.);
        let working_a = agent_blob_pose(AgentBlobState::Working, 0.);
        let working_b = agent_blob_pose(AgentBlobState::Working, 0.5);

        assert_ne!(idle_centered.left_eye, idle_looking_left.left_eye);
        assert_ne!(thinking_a.left_eye_size, thinking_b.left_eye_size);
        assert_ne!(thinking_a.right_eye_size, thinking_b.right_eye_size);
        assert_ne!(working_a.left_eye, working_b.left_eye);
    }

    #[test]
    fn agent_blob_transition_progress_is_smooth_and_bounded() {
        assert_eq!(
            agent_blob_transition_progress(Duration::ZERO, AGENT_BLOB_TRANSITION_DURATION),
            0.
        );
        assert_eq!(
            agent_blob_transition_progress(
                AGENT_BLOB_TRANSITION_DURATION,
                AGENT_BLOB_TRANSITION_DURATION,
            ),
            1.
        );
        assert_eq!(
            agent_blob_transition_progress(
                AGENT_BLOB_TRANSITION_DURATION * 2,
                AGENT_BLOB_TRANSITION_DURATION,
            ),
            1.
        );
    }

    #[test]
    fn active_turn_keeps_activity_on_its_last_tool_group_between_calls() {
        assert!(tool_group_shows_activity(true, 2, 3, false));
        assert!(!tool_group_shows_activity(true, 1, 3, false));
        assert!(!tool_group_shows_activity(false, 2, 3, false));
        assert!(tool_group_shows_activity(false, 1, 3, true));
    }

    #[test]
    fn tool_groups_expand_only_from_explicit_user_state() {
        assert!(!tool_group_is_open(None));
        assert!(tool_group_is_open(Some(true)));
        assert!(!tool_group_is_open(Some(false)));
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

    #[gpui::test]
    fn cmd_shift_a_dispatches_archive_active_session(cx: &mut gpui::TestAppContext) {
        let dispatch_count = std::rc::Rc::new(std::cell::Cell::new(0));
        let observed_dispatch_count = dispatch_count.clone();
        cx.update(|cx| {
            cx.bind_keys(desktop_key_bindings());
            cx.on_action(move |_: &ArchiveActiveSession, _| {
                observed_dispatch_count.set(observed_dispatch_count.get() + 1);
            });
        });

        let cx = cx.add_empty_window();
        cx.simulate_keystrokes("cmd-shift-a");

        assert_eq!(dispatch_count.get(), 1);
    }

    #[test]
    fn refresh_preserves_a_valid_selection_and_falls_back_to_the_first_session() {
        let first = SessionDescriptor {
            id: SessionId::new(),
            agent_id: AgentId::new(),
            association: SessionAssociation::Standalone,
            workspace: SessionWorkspace {
                workspace_id: WorkspaceId::new(),
                working_directory_id: DirectoryId::new(),
            },
            title: Some("First".to_owned()),
            archived: false,
            updated_at_ms: 2,
        };
        let second = SessionDescriptor {
            id: SessionId::new(),
            agent_id: AgentId::new(),
            association: SessionAssociation::Standalone,
            workspace: SessionWorkspace {
                workspace_id: WorkspaceId::new(),
                working_directory_id: DirectoryId::new(),
            },
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
    fn only_selected_unarchived_sessions_are_archive_targets() {
        let session = SessionDescriptor {
            id: SessionId::new(),
            agent_id: AgentId::new(),
            association: SessionAssociation::Standalone,
            workspace: SessionWorkspace {
                workspace_id: WorkspaceId::new(),
                working_directory_id: DirectoryId::new(),
            },
            title: Some("Selected".to_owned()),
            archived: false,
            updated_at_ms: 1,
        };
        let session_id = session.id;

        assert_eq!(
            archive_target_session_id(Some(session_id), std::slice::from_ref(&session)),
            Some(session_id)
        );
        assert_eq!(
            archive_target_session_id(None, std::slice::from_ref(&session)),
            None
        );
        assert_eq!(
            archive_target_session_id(Some(SessionId::new()), std::slice::from_ref(&session)),
            None
        );

        let archived_session = SessionDescriptor {
            archived: true,
            ..session
        };
        assert_eq!(
            archive_target_session_id(Some(session_id), &[archived_session]),
            None
        );
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
    fn bottom_follow_does_not_treat_its_visual_offset_as_user_scrolling() {
        assert!(is_near_animated_bottom(px(-140.), px(180.), px(40.)));
        assert!(!is_near_animated_bottom(px(-100.), px(180.), px(40.)));
    }

    #[test]
    fn repeated_growth_does_not_count_the_existing_visual_offset_twice() {
        let previous_max_offset = unshifted_max_offset(px(180.), px(40.));
        let new_max_offset = unshifted_max_offset(px(200.), px(40.));
        let (scroll_y, content_offset_y) =
            pinned_bottom_offsets(previous_max_offset, new_max_offset, px(40.));

        assert_eq!(scroll_y, px(-160.));
        assert_eq!(content_offset_y, px(60.));
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
    fn incomplete_streamed_links_do_not_render_markdown_syntax_or_destinations() {
        let complete_link = "See [the docs](file:///tmp/complete.md)";
        for prefix_end in complete_link
            .char_indices()
            .map(|(index, character)| index + character.len_utf8())
            .filter(|prefix_end| *prefix_end < complete_link.len())
        {
            let rendered = streaming_markdown_source(&complete_link[..prefix_end]);
            assert!(!rendered.contains('['), "rendered {rendered:?}");
            assert!(!rendered.contains(']'), "rendered {rendered:?}");
            assert!(!rendered.contains("file:///"), "rendered {rendered:?}");
        }
        assert_eq!(
            streaming_markdown_source("See [the docs](file:///tmp/incomplete"),
            "See the docs"
        );
        assert_eq!(
            streaming_markdown_source("See [the docs](file:///tmp/a_(draft).md"),
            "See the docs"
        );
        assert_eq!(
            streaming_markdown_source("See [the docs](file:///tmp/complete.md)"),
            "See [the docs](file:///tmp/complete.md)"
        );
    }

    #[test]
    fn conversation_history_uses_transport_safe_event_pages() {
        assert_eq!(
            CONVERSATION_EVENT_PAGE_SIZE,
            DEFAULT_SESSION_EVENT_PAGE_SIZE
        );
    }

    #[test]
    fn workspace_folder_geometry_interpolates_between_closed_and_open_shapes() {
        assert_eq!(
            workspace_folder_geometry(0.),
            WorkspaceFolderGeometry {
                back_left_end: (3., 17.5),
                back_right_end: (21., 17.5),
                top_left: (3., 8.5),
                top_right: (21., 8.5),
                bottom_right: (21., 17.5),
                bottom_left: (3., 17.5),
            }
        );
        assert_eq!(
            workspace_folder_geometry(0.5),
            WorkspaceFolderGeometry {
                back_left_end: (3., 12.25),
                back_right_end: (21., 13.75),
                top_left: (3.75, 9.25),
                top_right: (21.25, 9.25),
                bottom_right: (19.75, 18.25),
                bottom_left: (3.25, 18.25),
            }
        );
        assert_eq!(
            workspace_folder_geometry(1.),
            WorkspaceFolderGeometry {
                back_left_end: (3., 7.),
                back_right_end: (21., 10.),
                top_left: (4.5, 10.),
                top_right: (21.5, 10.),
                bottom_right: (18.5, 19.),
                bottom_left: (3.5, 19.),
            }
        );
    }
}
