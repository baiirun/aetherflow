use aetherflow_pi::{AetherflowClient, AetherflowClientOptions, SessionDescriptor};
use aetherflow_storage::SessionId;
use gpui::{
    App, Application, Bounds, Context, Div, TitlebarOptions, Window, WindowBackgroundAppearance,
    WindowBounds, WindowOptions, div, prelude::*, px, rgb, rgba, size,
};
use std::{
    cmp::Reverse,
    sync::Arc,
    time::{SystemTime, UNIX_EPOCH},
};
use tokio::runtime::Runtime;

const SIDEBAR_WIDTH: f32 = 280.;

enum SessionLoadState {
    Loading,
    Loaded,
    Failed(String),
}

struct DesktopShell {
    runtime: Arc<Runtime>,
    client: AetherflowClient,
    sessions: Vec<SessionDescriptor>,
    selected_session_id: Option<SessionId>,
    load_state: SessionLoadState,
    action_error: Option<String>,
}

impl DesktopShell {
    fn new(cx: &mut Context<Self>) -> Self {
        let mut shell = Self {
            runtime: Arc::new(Runtime::new().expect("failed to start desktop async runtime")),
            client: AetherflowClient::connect(AetherflowClientOptions::default()),
            sessions: Vec::new(),
            selected_session_id: None,
            load_state: SessionLoadState::Loading,
            action_error: None,
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
                    Ok(sessions) => shell.replace_sessions(sessions),
                    Err(error) => shell.load_state = SessionLoadState::Failed(error),
                }
                cx.notify();
            });
        })
        .detach();
    }

    fn replace_sessions(&mut self, sessions: Vec<SessionDescriptor>) {
        self.selected_session_id = selection_after_refresh(self.selected_session_id, &sessions);
        self.sessions = sessions;
        self.load_state = SessionLoadState::Loaded;
        self.action_error = None;
    }

    fn selected_session(&self) -> Option<&SessionDescriptor> {
        let selected = self.selected_session_id?;
        self.sessions.iter().find(|session| session.id == selected)
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
                shell.selected_session_id = Some(session.id);
                cx.notify();
            }))
            .child(
                div()
                    .pr_12()
                    .truncate()
                    .text_sm()
                    .text_color(rgb(0xd6d7d9))
                    .child(title),
            )
            .child(
                div()
                    .absolute()
                    .right_3()
                    .top(px(9.))
                    .text_xs()
                    .text_color(rgb(0x74777a))
                    .group_hover(group.clone(), |style| style.opacity(0.))
                    .child(activity),
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
                    .opacity(0.)
                    .group_hover(group, |style| style.opacity(1.))
                    .hover(|style| style.bg(rgb(0x343638)))
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
            .p_2();

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
            .child(
                div()
                    .h(px(88.))
                    .pt(px(26.))
                    .px_5()
                    .flex()
                    .items_center()
                    .justify_between()
                    .child(div().text_sm().text_color(rgb(0x74777a)).child("Sessions"))
                    .child(
                        div()
                            .id("refresh-sessions")
                            .px_2()
                            .py_1()
                            .rounded_md()
                            .cursor_pointer()
                            .text_sm()
                            .text_color(rgb(0x74777a))
                            .hover(|style| style.bg(rgb(0x252729)).text_color(rgb(0xd6d7d9)))
                            .child("↻")
                            .on_click(cx.listener(|shell, _, _, cx| {
                                shell.load_sessions(cx);
                                cx.notify();
                            })),
                    ),
            )
            .child(list)
    }

    fn render_main_panel(&self) -> Div {
        let panel = div()
            .flex_1()
            .min_w_0()
            .h_full()
            .flex()
            .flex_col()
            .bg(rgba(0x101318d9));

        let Some(session) = self.selected_session() else {
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

        panel
            .child(
                div()
                    .h(px(64.))
                    .px_6()
                    .flex()
                    .items_center()
                    .border_b_1()
                    .border_color(rgb(0x2a303b))
                    .child(
                        div()
                            .font_weight(gpui::FontWeight::SEMIBOLD)
                            .child(session_title(session)),
                    ),
            )
            .child(
                div()
                    .flex_1()
                    .flex()
                    .items_center()
                    .justify_center()
                    .text_color(rgb(0x74777a))
                    .child("Transcript coming next."),
            )
    }
}

impl Render for DesktopShell {
    fn render(&mut self, _window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        div()
            .size_full()
            .flex()
            .bg(rgba(0x00000000))
            .font_family("Inter Variable")
            .text_color(rgb(0xe7eaf0))
            .child(self.render_sidebar(cx))
            .child(self.render_main_panel())
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

fn main() {
    Application::new().run(|cx: &mut App| {
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
            |_, cx| cx.new(DesktopShell::new),
        )
        .expect("failed to open the Aetherflow window");
        cx.activate(true);
    });
}

#[cfg(test)]
mod tests {
    use super::*;
    use aetherflow_storage::{AgentId, SessionAssociation};

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
}
