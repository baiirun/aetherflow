use serde::{Deserialize, Serialize};
use std::fmt;
use uuid::Uuid;

macro_rules! domain_id {
    ($name:ident) => {
        #[derive(Clone, Copy, Debug, Deserialize, Eq, Hash, PartialEq, Serialize)]
        #[serde(transparent)]
        pub struct $name(Uuid);

        impl $name {
            pub fn new() -> Self {
                Self(Uuid::new_v4())
            }
        }

        impl Default for $name {
            fn default() -> Self {
                Self::new()
            }
        }

        impl fmt::Display for $name {
            fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
                self.0.fmt(formatter)
            }
        }
    };
}

domain_id!(ChannelId);
domain_id!(AgentId);
domain_id!(SessionId);

#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
pub struct Channel {
    pub id: ChannelId,
    pub name: String,
}

impl Channel {
    pub fn new(name: impl Into<String>) -> Self {
        Self {
            id: ChannelId::new(),
            name: name.into(),
        }
    }
}

#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
pub struct Agent {
    pub id: AgentId,
    pub name: String,
}

impl Agent {
    pub fn new(name: impl Into<String>) -> Self {
        Self {
            id: AgentId::new(),
            name: name.into(),
        }
    }
}

#[derive(Clone, Copy, Debug, Default, Deserialize, Eq, PartialEq, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum SessionStatus {
    #[default]
    Idle,
    Running,
    Stopped,
}

/// A private agent session, optionally associated with a channel.
#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
pub struct Session {
    pub id: SessionId,
    pub agent_id: AgentId,
    pub channel_id: Option<ChannelId>,
    pub status: SessionStatus,
}

impl Session {
    pub fn new(agent_id: AgentId, channel_id: Option<ChannelId>) -> Self {
        Self {
            id: SessionId::new(),
            agent_id,
            channel_id,
            status: SessionStatus::default(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn session_keeps_agent_and_channel_identity_separate() {
        let channel = Channel::new("planning");
        let agent = Agent::new("scout");
        let session = Session::new(agent.id, Some(channel.id));

        assert_eq!(session.agent_id, agent.id);
        assert_eq!(session.channel_id, Some(channel.id));
        assert_eq!(session.status, SessionStatus::Idle);
    }
}
