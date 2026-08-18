use serde::{Deserialize, Deserializer, Serialize, Serializer, de::Error as _};
use std::{fmt, path::PathBuf, str::FromStr};
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
domain_id!(DirectoryId);
domain_id!(SessionId);
domain_id!(WorkspaceId);

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

#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
pub struct Directory {
    pub id: DirectoryId,
    pub path: PathBuf,
}

impl Directory {
    pub fn new(path: impl Into<PathBuf>) -> Self {
        Self {
            id: DirectoryId::new(),
            path: path.into(),
        }
    }
}

#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
pub struct Workspace {
    pub id: WorkspaceId,
    pub name: String,
    pub directories: Vec<Directory>,
    pub primary_directory_id: DirectoryId,
}

impl Workspace {
    pub fn new(
        name: impl Into<String>,
        paths: impl IntoIterator<Item = PathBuf>,
    ) -> Result<Self, WorkspaceInvariantError> {
        let name = name.into().trim().to_owned();
        let directories = paths.into_iter().map(Directory::new).collect::<Vec<_>>();
        let Some(primary_directory_id) = directories.first().map(|directory| directory.id) else {
            return Err(WorkspaceInvariantError::Empty);
        };
        let workspace = Self {
            id: WorkspaceId::new(),
            name,
            directories,
            primary_directory_id,
        };
        workspace.validate()?;
        Ok(workspace)
    }

    pub fn primary_directory(&self) -> Result<&Directory, WorkspaceInvariantError> {
        self.directory(self.primary_directory_id)
    }

    pub fn directory(
        &self,
        directory_id: DirectoryId,
    ) -> Result<&Directory, WorkspaceInvariantError> {
        self.directories
            .iter()
            .find(|directory| directory.id == directory_id)
            .ok_or(WorkspaceInvariantError::UnknownDirectory(directory_id))
    }

    pub fn validate(&self) -> Result<(), WorkspaceInvariantError> {
        if self.name.is_empty() || self.name.trim() != self.name {
            return Err(WorkspaceInvariantError::InvalidName);
        }
        if self.directories.is_empty() {
            return Err(WorkspaceInvariantError::Empty);
        }
        self.primary_directory()?;
        for (index, directory) in self.directories.iter().enumerate() {
            if !directory.path.is_absolute() {
                return Err(WorkspaceInvariantError::RelativeDirectory(directory.id));
            }
            if self.directories[..index]
                .iter()
                .any(|existing| existing.id == directory.id || existing.path == directory.path)
            {
                return Err(WorkspaceInvariantError::DuplicateDirectory);
            }
        }
        Ok(())
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum WorkspaceInvariantError {
    InvalidName,
    Empty,
    UnknownDirectory(DirectoryId),
    RelativeDirectory(DirectoryId),
    DuplicateDirectory,
}

impl fmt::Display for WorkspaceInvariantError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::InvalidName => {
                formatter.write_str("workspace name must contain non-whitespace characters")
            }
            Self::Empty => formatter.write_str("workspace must contain at least one directory"),
            Self::UnknownDirectory(directory_id) => write!(
                formatter,
                "directory {directory_id} does not belong to the workspace"
            ),
            Self::RelativeDirectory(directory_id) => {
                write!(formatter, "directory {directory_id} path must be absolute")
            }
            Self::DuplicateDirectory => formatter.write_str("workspace directories must be unique"),
        }
    }
}

impl std::error::Error for WorkspaceInvariantError {}

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

#[derive(Clone, Copy, Debug, Deserialize, Eq, PartialEq, Serialize)]
pub struct SessionWorkspace {
    pub workspace_id: WorkspaceId,
    pub working_directory_id: DirectoryId,
}

/// A private agent session that is either standalone or associated with a channel.
#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
pub struct Session {
    pub id: SessionId,
    pub agent_id: AgentId,
    pub association: SessionAssociation,
    pub workspace: SessionWorkspace,
    pub status: SessionStatus,
}

impl Session {
    pub fn new(
        agent_id: AgentId,
        association: SessionAssociation,
        workspace_id: WorkspaceId,
        working_directory_id: DirectoryId,
    ) -> Self {
        Self {
            id: SessionId::new(),
            agent_id,
            association,
            workspace: SessionWorkspace {
                workspace_id,
                working_directory_id,
            },
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
            WorkspaceId::new(),
            DirectoryId::new(),
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
        let session = Session::new(
            agent.id,
            SessionAssociation::Standalone,
            WorkspaceId::new(),
            DirectoryId::new(),
        );

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
    fn workspace_requires_a_directory_and_uses_the_first_as_primary() {
        assert_eq!(
            Workspace::new("Test", Vec::new()),
            Err(WorkspaceInvariantError::Empty)
        );

        let workspace =
            Workspace::new("Test", [PathBuf::from("/work/a"), PathBuf::from("/work/b")])
                .expect("non-empty workspace");

        assert_eq!(workspace.name, "Test");
        assert_eq!(workspace.directories.len(), 2);
        assert_eq!(
            workspace.primary_directory().unwrap().path,
            PathBuf::from("/work/a")
        );
        assert_eq!(workspace.validate(), Ok(()));
    }

    #[test]
    fn workspace_rejects_relative_directory_paths() {
        assert!(matches!(
            Workspace::new("Test", [PathBuf::from("relative")]),
            Err(WorkspaceInvariantError::RelativeDirectory(_))
        ));
    }

    #[test]
    fn workspace_requires_a_name() {
        assert_eq!(
            Workspace::new("  ", [PathBuf::from("/work")]),
            Err(WorkspaceInvariantError::InvalidName)
        );
    }

    #[test]
    fn session_workspace_is_independent_from_channel_association() {
        let workspace = Workspace::new("Test", [PathBuf::from("/work")]).unwrap();
        let directory = workspace.primary_directory().unwrap();
        let channel_id = ChannelId::new();
        let session = Session::new(
            AgentId::new(),
            SessionAssociation::Channel { channel_id },
            workspace.id,
            directory.id,
        );

        assert_eq!(
            session.workspace,
            SessionWorkspace {
                workspace_id: workspace.id,
                working_directory_id: directory.id,
            }
        );
        assert_eq!(
            session.association,
            SessionAssociation::Channel { channel_id }
        );
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
