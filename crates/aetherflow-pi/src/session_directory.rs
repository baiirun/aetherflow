use aetherflow_storage::{AgentId, Session, SessionAssociation, SessionId};
use anyhow::{Result, bail};
use async_trait::async_trait;
use rivetkit::{BindParam, ColumnValue, prelude::*};
use serde::{Deserialize, Serialize};
use std::{future::Future, pin::Pin, sync::Arc};

pub const SESSION_DIRECTORY_ACTOR_NAME: &str = "session_directory";
pub const DEFAULT_SESSION_DIRECTORY_KEY: &str = "sessions";

type BoxActionFuture<T> = Pin<Box<dyn Future<Output = Result<T>> + Send>>;

/// Durable session identity stored separately from the live Pi actor.
#[derive(Clone, Copy, Debug, Deserialize, Eq, PartialEq, Serialize)]
pub struct SessionDescriptor {
    pub id: SessionId,
    pub agent_id: AgentId,
    pub association: SessionAssociation,
}

impl From<&Session> for SessionDescriptor {
    fn from(session: &Session) -> Self {
        Self {
            id: session.id,
            agent_id: session.agent_id,
            association: session.association,
        }
    }
}

#[derive(Clone, Copy, Debug, Deserialize, Serialize)]
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

pub struct SessionDirectoryActor;

#[async_trait]
impl Actor for SessionDirectoryActor {
    type State = ();
    type Input = ();
    type Actions = (RegisterSession, ListSessions);
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
            result
                .rows
                .into_iter()
                .map(|row| match row.into_iter().next() {
                    Some(ColumnValue::Text(descriptor)) => {
                        serde_json::from_str(&descriptor).map_err(Into::into)
                    }
                    _ => Err(anyhow::anyhow!(
                        "session directory stored an invalid descriptor"
                    )),
                })
                .collect()
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use aetherflow_storage::{Agent, SessionAssociation};

    #[test]
    fn descriptor_excludes_transient_session_status() {
        let agent = Agent::new("test");
        let session = Session::new(agent.id, SessionAssociation::Standalone);

        assert_eq!(
            SessionDescriptor::from(&session),
            SessionDescriptor {
                id: session.id,
                agent_id: session.agent_id,
                association: SessionAssociation::Standalone,
            }
        );
    }
}
