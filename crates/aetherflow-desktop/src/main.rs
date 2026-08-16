use aetherflow_storage::{Agent, Channel, Session, SessionAssociation};
use gpui::{
    App, Application, Bounds, Context, Window, WindowBounds, WindowOptions, div, prelude::*, px,
    rgb, size,
};

struct DesktopShell {
    channel: Channel,
    agent: Agent,
    session: Session,
}

impl DesktopShell {
    fn new() -> Self {
        let channel = Channel::new("Aetherflow");
        let agent = Agent::new("local-agent");
        let session = Session::new(
            agent.id,
            SessionAssociation::Channel {
                channel_id: channel.id,
            },
        );
        Self {
            channel,
            agent,
            session,
        }
    }
}

impl Render for DesktopShell {
    fn render(&mut self, _window: &mut Window, _cx: &mut Context<Self>) -> impl IntoElement {
        div()
            .size_full()
            .flex()
            .flex_col()
            .gap_3()
            .p_6()
            .bg(rgb(0x101318))
            .text_color(rgb(0xe7eaf0))
            .child(div().text_2xl().child("Aetherflow"))
            .child(format!("Channel: {}", self.channel.name))
            .child(format!("Agent: {}", self.agent.name))
            .child(format!("Session: {}", self.session.id))
            .child("GPUI shell ready; daemon connection is the next seam.")
    }
}

fn main() {
    Application::new().run(|cx: &mut App| {
        let bounds = Bounds::centered(None, size(px(900.), px(640.)), cx);
        cx.open_window(
            WindowOptions {
                window_bounds: Some(WindowBounds::Windowed(bounds)),
                ..Default::default()
            },
            |_, cx| cx.new(|_| DesktopShell::new()),
        )
        .expect("failed to open the Aetherflow window");
        cx.activate(true);
    });
}
