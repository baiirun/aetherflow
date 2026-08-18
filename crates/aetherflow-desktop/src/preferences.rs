use aetherflow_storage::WorkspaceId;
use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};
use std::{
    fs,
    path::{Path, PathBuf},
};

#[derive(Clone, Debug, Default, Deserialize, Eq, PartialEq, Serialize)]
#[serde(default, rename_all = "camelCase")]
pub(crate) struct DesktopPreferences {
    pub(crate) archived_sessions_collapsed: bool,
    pub(crate) collapsed_workspace_ids: Vec<WorkspaceId>,
}

impl DesktopPreferences {
    pub(crate) fn load() -> Result<Self> {
        Self::load_from(&default_preferences_path()?)
    }

    pub(crate) fn save(&self) -> Result<()> {
        self.save_to(&default_preferences_path()?)
    }

    fn load_from(path: &Path) -> Result<Self> {
        match fs::read(path) {
            Ok(bytes) => serde_json::from_slice(&bytes)
                .with_context(|| format!("decode desktop preferences at {}", path.display())),
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(Self::default()),
            Err(error) => Err(error)
                .with_context(|| format!("read desktop preferences at {}", path.display())),
        }
    }

    fn save_to(&self, path: &Path) -> Result<()> {
        let parent = path
            .parent()
            .context("desktop preferences path has no parent directory")?;
        fs::create_dir_all(parent).with_context(|| {
            format!("create desktop preferences directory {}", parent.display())
        })?;
        let bytes = serde_json::to_vec_pretty(self).context("encode desktop preferences")?;
        fs::write(path, bytes)
            .with_context(|| format!("write desktop preferences at {}", path.display()))
    }
}

fn default_preferences_path() -> Result<PathBuf> {
    if let Some(path) = std::env::var_os("AETHERFLOW_DATA_DIR") {
        return Ok(PathBuf::from(path).join("desktop-preferences.json"));
    }
    let user_home = std::env::var_os("HOME")
        .context("HOME is not set; set AETHERFLOW_DATA_DIR to choose preference storage")?;
    Ok(PathBuf::from(user_home).join(".aetherflow/desktop-preferences.json"))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn missing_preferences_default_to_an_expanded_archive_section() -> Result<()> {
        let directory = tempfile::tempdir()?;

        let preferences = DesktopPreferences::load_from(&directory.path().join("missing.json"))?;

        assert!(!preferences.archived_sessions_collapsed);
        assert!(preferences.collapsed_workspace_ids.is_empty());
        Ok(())
    }

    #[test]
    fn presentation_state_survives_a_save_and_load() -> Result<()> {
        let directory = tempfile::tempdir()?;
        let path = directory.path().join("desktop-preferences.json");
        let preferences = DesktopPreferences {
            archived_sessions_collapsed: true,
            collapsed_workspace_ids: vec![WorkspaceId::new(), WorkspaceId::new()],
        };

        preferences.save_to(&path)?;

        assert_eq!(DesktopPreferences::load_from(&path)?, preferences);
        Ok(())
    }

    #[test]
    fn older_preferences_default_to_no_collapsed_workspaces() -> Result<()> {
        let directory = tempfile::tempdir()?;
        let path = directory.path().join("desktop-preferences.json");
        fs::write(&path, r#"{"archivedSessionsCollapsed":true}"#)?;

        let preferences = DesktopPreferences::load_from(&path)?;

        assert!(preferences.archived_sessions_collapsed);
        assert!(preferences.collapsed_workspace_ids.is_empty());
        Ok(())
    }
}
