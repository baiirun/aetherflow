use aetherflow_pi::{AetherflowClient, AetherflowClientOptions, SessionDescriptor};
use aetherflow_storage::SessionId;
use gpui::{
    App, Application, Bounds, Context, Div, TitlebarOptions, Window, WindowBounds, WindowOptions,
    div, prelude::*, px, rgb, size,
};
use std::sync::Arc;
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
}

impl DesktopShell {
    fn new(cx: &mut Context<Self>) -> Self {
        let mut shell = Self {
            runtime: Arc::new(Runtime::new().expect("failed to start desktop async runtime")),
            client: AetherflowClient::connect(AetherflowClientOptions::default()),
            sessions: Vec::new(),
            selected_session_id: None,
            load_state: SessionLoadState::Loading,
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
    }

    fn selected_session(&self) -> Option<SessionDescriptor> {
        let selected = self.selected_session_id?;
        self.sessions
            .iter()
            .copied()
            .find(|session| session.id == selected)
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
                list = list.children(self.sessions.iter().copied().enumerate().map(
                    |(index, session)| {
                        let selected = self.selected_session_id == Some(session.id);
                        div()
                            .id(("session", index))
                            .mb_1()
                            .px_4()
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
                                    .text_base()
                                    .text_color(rgb(0xd6d7d9))
                                    .child(format!("Session {}", short_id(session.id))),
                            )
                    },
                ));
            }
        }

        div()
            .w(px(SIDEBAR_WIDTH))
            .h_full()
            .flex()
            .flex_col()
            .bg(rgb(0x1d1f20))
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
            .bg(rgb(0x101318));

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
                            .child(format!("Session {}", short_id(session.id))),
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
            .bg(rgb(0x101318))
            .text_color(rgb(0xe7eaf0))
            .child(self.render_sidebar(cx))
            .child(self.render_main_panel())
    }
}

fn short_id(id: SessionId) -> String {
    short_uuid(&id.to_string())
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
        };
        let second = SessionDescriptor {
            id: SessionId::new(),
            agent_id: AgentId::new(),
            association: SessionAssociation::Standalone,
        };
        let sessions = [first, second];

        assert_eq!(
            selection_after_refresh(Some(second.id), &sessions),
            Some(second.id)
        );
        assert_eq!(
            selection_after_refresh(Some(SessionId::new()), &sessions),
            Some(first.id)
        );
        assert_eq!(selection_after_refresh(Some(first.id), &[]), None);
    }
}
