use aetherflow_storage::{Directory, Workspace, WorkspaceId};
use anyhow::{Result, bail};
use async_trait::async_trait;
use rivetkit::{BindParam, ColumnValue, prelude::*};
use serde::{Deserialize, Serialize};
use std::{future::Future, pin::Pin, sync::Arc};

pub const WORKSPACE_CATALOG_ACTOR_NAME: &str = "workspace_catalog";
pub const DEFAULT_WORKSPACE_CATALOG_KEY: &str = "workspaces-v2";

type BoxActionFuture<T> = Pin<Box<dyn Future<Output = Result<T>> + Send>>;

#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct RegisterWorkspace {
    pub workspace: Workspace,
}

impl Action for RegisterWorkspace {
    type Output = Workspace;

    const NAME: &'static str = "register_workspace";
}

#[derive(Clone, Copy, Debug, Deserialize, Serialize)]
pub struct ListWorkspaces;

impl Action for ListWorkspaces {
    type Output = Vec<Workspace>;

    const NAME: &'static str = "list_workspaces";
}

#[derive(Clone, Copy, Debug, Deserialize, Serialize)]
pub struct GetWorkspace {
    pub workspace_id: WorkspaceId,
}

impl Action for GetWorkspace {
    type Output = Workspace;

    const NAME: &'static str = "get_workspace";
}

#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct AddWorkspaceDirectory {
    pub workspace_id: WorkspaceId,
    pub directory: Directory,
}

impl Action for AddWorkspaceDirectory {
    type Output = Workspace;

    const NAME: &'static str = "add_workspace_directory";
}

pub struct WorkspaceCatalogActor;

#[async_trait]
impl Actor for WorkspaceCatalogActor {
    type State = ();
    type Input = ();
    type Actions = (
        RegisterWorkspace,
        ListWorkspaces,
        GetWorkspace,
        AddWorkspaceDirectory,
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
                "CREATE TABLE IF NOT EXISTS workspaces (\
                    sequence INTEGER PRIMARY KEY AUTOINCREMENT, \
                    id TEXT NOT NULL UNIQUE, \
                    workspace TEXT NOT NULL\
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

impl Handles<RegisterWorkspace> for WorkspaceCatalogActor {
    type Future = BoxActionFuture<Workspace>;

    fn handle(self: Arc<Self>, ctx: Ctx<Self>, action: RegisterWorkspace) -> Self::Future {
        Box::pin(async move {
            action.workspace.validate()?;
            let id = action.workspace.id.to_string();
            let existing = ctx
                .sql()
                .query(
                    "SELECT workspace FROM workspaces WHERE id = ?1",
                    Some(vec![BindParam::Text(id.clone())]),
                )
                .await?;
            if let Some(row) = existing.rows.first() {
                let Some(ColumnValue::Text(workspace)) = row.first() else {
                    bail!("workspace catalog stored invalid data for {id}");
                };
                let existing: Workspace = serde_json::from_str(workspace)?;
                if existing != action.workspace {
                    bail!(
                        "workspace {} is already registered differently",
                        action.workspace.id
                    );
                }
                return Ok(existing);
            }

            ctx.sql()
                .execute(
                    "INSERT INTO workspaces (id, workspace) VALUES (?1, ?2)",
                    Some(vec![
                        BindParam::Text(id),
                        BindParam::Text(serde_json::to_string(&action.workspace)?),
                    ]),
                )
                .await?;
            Ok(action.workspace)
        })
    }
}

impl Handles<ListWorkspaces> for WorkspaceCatalogActor {
    type Future = BoxActionFuture<Vec<Workspace>>;

    fn handle(self: Arc<Self>, ctx: Ctx<Self>, _action: ListWorkspaces) -> Self::Future {
        Box::pin(async move {
            let result = ctx
                .sql()
                .query("SELECT workspace FROM workspaces ORDER BY sequence", None)
                .await?;
            result
                .rows
                .into_iter()
                .map(|row| decode_workspace(row.into_iter().next()))
                .collect()
        })
    }
}

impl Handles<GetWorkspace> for WorkspaceCatalogActor {
    type Future = BoxActionFuture<Workspace>;

    fn handle(self: Arc<Self>, ctx: Ctx<Self>, action: GetWorkspace) -> Self::Future {
        Box::pin(async move { read_workspace(&ctx, action.workspace_id).await })
    }
}

impl Handles<AddWorkspaceDirectory> for WorkspaceCatalogActor {
    type Future = BoxActionFuture<Workspace>;

    fn handle(self: Arc<Self>, ctx: Ctx<Self>, action: AddWorkspaceDirectory) -> Self::Future {
        Box::pin(async move {
            let mut workspace = read_workspace(&ctx, action.workspace_id).await?;
            if workspace
                .directories
                .iter()
                .any(|directory| directory.path == action.directory.path)
            {
                return Ok(workspace);
            }
            workspace.directories.push(action.directory);
            workspace.validate()?;
            write_workspace(&ctx, &workspace).await?;
            Ok(workspace)
        })
    }
}

fn decode_workspace(value: Option<ColumnValue>) -> Result<Workspace> {
    let Some(ColumnValue::Text(workspace)) = value else {
        bail!("workspace catalog stored invalid data");
    };
    let workspace: Workspace = serde_json::from_str(&workspace)?;
    workspace.validate()?;
    Ok(workspace)
}

async fn read_workspace(
    ctx: &Ctx<WorkspaceCatalogActor>,
    workspace_id: WorkspaceId,
) -> Result<Workspace> {
    let result = ctx
        .sql()
        .query(
            "SELECT workspace FROM workspaces WHERE id = ?1",
            Some(vec![BindParam::Text(workspace_id.to_string())]),
        )
        .await?;
    let Some(row) = result.rows.into_iter().next() else {
        bail!("workspace {workspace_id} is not registered");
    };
    decode_workspace(row.into_iter().next())
}

async fn write_workspace(ctx: &Ctx<WorkspaceCatalogActor>, workspace: &Workspace) -> Result<()> {
    ctx.sql()
        .execute(
            "UPDATE workspaces SET workspace = ?1 WHERE id = ?2",
            Some(vec![
                BindParam::Text(serde_json::to_string(workspace)?),
                BindParam::Text(workspace.id.to_string()),
            ]),
        )
        .await?;
    Ok(())
}
