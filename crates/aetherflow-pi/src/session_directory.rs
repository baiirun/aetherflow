use aetherflow_storage::{AgentId, Session, SessionAssociation, SessionId};
use anyhow::{Result, bail};
use async_trait::async_trait;
use rivetkit::{BindParam, ColumnValue, prelude::*};
use serde::{Deserialize, Serialize};
use std::{cmp::Reverse, future::Future, pin::Pin, sync::Arc};

pub const SESSION_DIRECTORY_ACTOR_NAME: &str = "session_directory";
pub const DEFAULT_SESSION_DIRECTORY_KEY: &str = "sessions-v2";

type BoxActionFuture<T> = Pin<Box<dyn Future<Output = Result<T>> + Send>>;

/// Durable session list data stored separately from the live Pi actor.
#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
pub struct SessionDescriptor {
    pub id: SessionId,
    pub agent_id: AgentId,
    pub association: SessionAssociation,
    pub title: Option<String>,
    pub archived: bool,
    pub updated_at_ms: u64,
}

impl SessionDescriptor {
    pub fn new(session: &Session, title: Option<String>, updated_at_ms: u64) -> Self {
        Self {
            id: session.id,
            agent_id: session.agent_id,
            association: session.association,
            title,
            archived: false,
            updated_at_ms,
        }
    }
}

#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct RegisterSession {
    pub session: SessionDescriptor,
}

impl Action for RegisterSession {
    type Output = SessionDescriptor;

    const NAME: &'static str = "register_session";
}

#[derive(Clone, Copy, Debug, Deserialize, Serialize)]
pub struct ListSessions;

impl Action for ListSessions {
    type Output = Vec<SessionDescriptor>;

    const NAME: &'static str = "list_sessions";
}

#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct RecordSessionActivity {
    pub session_id: SessionId,
    pub title: Option<String>,
    pub updated_at_ms: u64,
}

impl Action for RecordSessionActivity {
    type Output = SessionDescriptor;

    const NAME: &'static str = "record_session_activity";
}

#[derive(Clone, Copy, Debug, Deserialize, Serialize)]
pub struct SetSessionArchived {
    pub session_id: SessionId,
    pub archived: bool,
}

impl Action for SetSessionArchived {
    type Output = SessionDescriptor;

    const NAME: &'static str = "set_session_archived";
}

pub struct SessionDirectoryActor;

#[async_trait]
impl Actor for SessionDirectoryActor {
    type State = ();
    type Input = ();
    type Actions = (
        RegisterSession,
        ListSessions,
        RecordSessionActivity,
        SetSessionArchived,
    );
    type Events = ();
    type Queue = ();
    type ConnParams = ();
    type ConnState = ();
    type Action = rivetkit::action::Raw;

    const HAS_DATABASE: bool = true;

    async fn create_state(_ctx: &Ctx<Self>, _input: Self::Input) -> Result<Self::State> {
        Ok(())
    }

    async fn create(ctx: &Ctx<Self>) -> Result<Self> {
        ctx.sql()
            .execute(
                "CREATE TABLE IF NOT EXISTS sessions (\
                    sequence INTEGER PRIMARY KEY AUTOINCREMENT, \
                    id TEXT NOT NULL UNIQUE, \
                    descriptor TEXT NOT NULL\
                )",
                None,
            )
            .await?;
        Ok(Self)
    }

    async fn run(self: Arc<Self>, ctx: Ctx<Self>) -> Result<()> {
        ctx.abort_signal().cancelled().await;
        Ok(())
    }
}

impl Handles<RegisterSession> for SessionDirectoryActor {
    type Future = BoxActionFuture<SessionDescriptor>;

    fn handle(self: Arc<Self>, ctx: Ctx<Self>, action: RegisterSession) -> Self::Future {
        Box::pin(async move {
            let id = action.session.id.to_string();
            let existing = ctx
                .sql()
                .query(
                    "SELECT descriptor FROM sessions WHERE id = ?1",
                    Some(vec![BindParam::Text(id.clone())]),
                )
                .await?;
            if let Some(row) = existing.rows.first() {
                let Some(ColumnValue::Text(descriptor)) = row.first() else {
                    bail!("session directory stored an invalid descriptor for {id}");
                };
                let existing: SessionDescriptor = serde_json::from_str(descriptor)?;
                if existing != action.session {
                    bail!(
                        "session {} is already registered differently",
                        action.session.id
                    );
                }
                return Ok(existing);
            }

            ctx.sql()
                .execute(
                    "INSERT INTO sessions (id, descriptor) VALUES (?1, ?2)",
                    Some(vec![
                        BindParam::Text(id),
                        BindParam::Text(serde_json::to_string(&action.session)?),
                    ]),
                )
                .await?;
            Ok(action.session)
        })
    }
}

impl Handles<ListSessions> for SessionDirectoryActor {
    type Future = BoxActionFuture<Vec<SessionDescriptor>>;

    fn handle(self: Arc<Self>, ctx: Ctx<Self>, _action: ListSessions) -> Self::Future {
        Box::pin(async move {
            let result = ctx
                .sql()
                .query("SELECT descriptor FROM sessions ORDER BY sequence", None)
                .await?;
            let mut sessions = result
                .rows
                .into_iter()
                .map(|row| match row.into_iter().next() {
                    Some(ColumnValue::Text(descriptor)) => {
                        serde_json::from_str::<SessionDescriptor>(&descriptor).map_err(Into::into)
                    }
                    _ => Err(anyhow::anyhow!(
                        "session directory stored an invalid descriptor"
                    )),
                })
                .collect::<Result<Vec<_>>>()?;
            sessions.sort_by_key(|session| Reverse(session.updated_at_ms));
            Ok(sessions)
        })
    }
}

impl Handles<RecordSessionActivity> for SessionDirectoryActor {
    type Future = BoxActionFuture<SessionDescriptor>;

    fn handle(self: Arc<Self>, ctx: Ctx<Self>, action: RecordSessionActivity) -> Self::Future {
        Box::pin(async move {
            let mut session = read_descriptor(&ctx, action.session_id).await?;
            if session.title.is_none() {
                session.title = action.title;
            }
            session.updated_at_ms = action.updated_at_ms;
            write_descriptor(&ctx, &session).await?;
            Ok(session)
        })
    }
}

impl Handles<SetSessionArchived> for SessionDirectoryActor {
    type Future = BoxActionFuture<SessionDescriptor>;

    fn handle(self: Arc<Self>, ctx: Ctx<Self>, action: SetSessionArchived) -> Self::Future {
        Box::pin(async move {
            let mut session = read_descriptor(&ctx, action.session_id).await?;
            session.archived = action.archived;
            write_descriptor(&ctx, &session).await?;
            Ok(session)
        })
    }
}

async fn read_descriptor(
    ctx: &Ctx<SessionDirectoryActor>,
    session_id: SessionId,
) -> Result<SessionDescriptor> {
    let result = ctx
        .sql()
        .query(
            "SELECT descriptor FROM sessions WHERE id = ?1",
            Some(vec![BindParam::Text(session_id.to_string())]),
        )
        .await?;
    let Some(row) = result.rows.first() else {
        bail!("session {session_id} is not registered");
    };
    let Some(ColumnValue::Text(descriptor)) = row.first() else {
        bail!("session directory stored an invalid descriptor for {session_id}");
    };
    Ok(serde_json::from_str(descriptor)?)
}

async fn write_descriptor(
    ctx: &Ctx<SessionDirectoryActor>,
    session: &SessionDescriptor,
) -> Result<()> {
    ctx.sql()
        .execute(
            "UPDATE sessions SET descriptor = ?1 WHERE id = ?2",
            Some(vec![
                BindParam::Text(serde_json::to_string(session)?),
                BindParam::Text(session.id.to_string()),
            ]),
        )
        .await?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use aetherflow_storage::{Agent, SessionAssociation};

    #[test]
    fn descriptor_contains_only_sidebar_state_beyond_session_identity() {
        let agent = Agent::new("test");
        let session = Session::new(agent.id, SessionAssociation::Standalone);

        assert_eq!(
            SessionDescriptor::new(&session, Some("Test session".to_owned()), 42),
            SessionDescriptor {
                id: session.id,
                agent_id: session.agent_id,
                association: SessionAssociation::Standalone,
                title: Some("Test session".to_owned()),
                archived: false,
                updated_at_ms: 42,
            }
        );
    }
}
