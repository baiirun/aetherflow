use serde::{Deserialize, Deserializer, Serialize, Serializer, de::Error as _};
use std::{fmt, str::FromStr};
use uuid::Uuid;

macro_rules! domain_id {
    ($name:ident) => {
        #[derive(Clone, Copy, Debug, Eq, Hash, PartialEq)]
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

        impl FromStr for $name {
            type Err = uuid::Error;

            fn from_str(value: &str) -> Result<Self, Self::Err> {
                Uuid::parse_str(value).map(Self)
            }
        }

        impl Serialize for $name {
            fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
            where
                S: Serializer,
            {
                serializer.serialize_str(&self.to_string())
            }
        }

        impl<'de> Deserialize<'de> for $name {
            fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
            where
                D: Deserializer<'de>,
            {
                let value = String::deserialize(deserializer)?;
                value.parse().map_err(D::Error::custom)
            }
        }
    };
}

domain_id!(ChannelId);
domain_id!(AgentId);
domain_id!(SessionId);

/// Content-addressed identity for a stored attachment.
#[derive(Clone, Debug, Eq, Hash, PartialEq)]
pub struct AttachmentId(String);

impl AttachmentId {
    pub fn from_sha256(digest: [u8; 32]) -> Self {
        Self(hex::encode(digest))
    }
}

impl fmt::Display for AttachmentId {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(&self.0)
    }
}

impl FromStr for AttachmentId {
    type Err = ParseAttachmentIdError;

    fn from_str(value: &str) -> Result<Self, Self::Err> {
        if value.len() != 64 || !value.bytes().all(|byte| byte.is_ascii_hexdigit()) {
            return Err(ParseAttachmentIdError);
        }
        Ok(Self(value.to_ascii_lowercase()))
    }
}

impl Serialize for AttachmentId {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: Serializer,
    {
        serializer.serialize_str(&self.0)
    }
}

impl<'de> Deserialize<'de> for AttachmentId {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: Deserializer<'de>,
    {
        String::deserialize(deserializer)?
            .parse()
            .map_err(D::Error::custom)
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ParseAttachmentIdError;

impl fmt::Display for ParseAttachmentIdError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("attachment ID must be a 64-character SHA-256 digest")
    }
}

impl std::error::Error for ParseAttachmentIdError {}

#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
pub struct AttachmentRef {
    pub id: AttachmentId,
    pub media_type: String,
    pub byte_len: u64,
}

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

#[derive(Clone, Copy, Debug, Default, Deserialize, Eq, PartialEq, Serialize)]
#[serde(tag = "kind", rename_all = "snake_case")]
pub enum SessionAssociation {
    #[default]
    Standalone,
    Channel {
        channel_id: ChannelId,
    },
}

/// A private agent session that is either standalone or associated with a channel.
#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
pub struct Session {
    pub id: SessionId,
    pub agent_id: AgentId,
    pub association: SessionAssociation,
    pub status: SessionStatus,
}

impl Session {
    pub fn new(agent_id: AgentId, association: SessionAssociation) -> Self {
        Self {
            id: SessionId::new(),
            agent_id,
            association,
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
        let session = Session::new(
            agent.id,
            SessionAssociation::Channel {
                channel_id: channel.id,
            },
        );

        assert_eq!(session.agent_id, agent.id);
        assert_eq!(
            session.association,
            SessionAssociation::Channel {
                channel_id: channel.id
            }
        );
        assert_eq!(session.status, SessionStatus::Idle);
    }

    #[test]
    fn standalone_session_is_an_explicit_domain_state() {
        let agent = Agent::new("scout");
        let session = Session::new(agent.id, SessionAssociation::Standalone);

        assert_eq!(session.association, SessionAssociation::Standalone);
    }

    #[test]
    fn session_association_has_an_explicit_wire_discriminator() {
        let channel_id = ChannelId::new();

        assert_eq!(
            serde_json::to_value(SessionAssociation::Standalone).unwrap(),
            serde_json::json!({ "kind": "standalone" })
        );
        assert_eq!(
            serde_json::to_value(SessionAssociation::Channel { channel_id }).unwrap(),
            serde_json::json!({
                "kind": "channel",
                "channel_id": channel_id,
            })
        );
    }

    #[test]
    fn domain_ids_round_trip_through_their_string_form() {
        let session_id = SessionId::new();

        assert_eq!(session_id.to_string().parse(), Ok(session_id));
    }

    #[test]
    fn attachment_ids_reject_values_that_could_escape_the_store() {
        assert!("../attachment".parse::<AttachmentId>().is_err());
        assert!("g".repeat(64).parse::<AttachmentId>().is_err());

        let id = AttachmentId::from_sha256([0xab; 32]);
        assert_eq!(id.to_string(), "ab".repeat(32));
        assert_eq!(id.to_string().parse(), Ok(id));
    }
}
