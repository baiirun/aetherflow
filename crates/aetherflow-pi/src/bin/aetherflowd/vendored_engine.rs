use anyhow::{Context, Result, bail};
use rivetkit::{EngineSpawnMode, ServeConfig};
use std::{
    fs::{self, OpenOptions},
    io::Write,
    path::{Path, PathBuf},
};
use url::Url;

const ENGINE_VERSION: &str = "2.3.10";

#[cfg(all(target_os = "macos", target_arch = "aarch64"))]
const ENGINE_TARGET: &str = "aarch64-apple-darwin";

#[cfg(all(target_os = "macos", target_arch = "aarch64"))]
const ENGINE_BYTES: &[u8] = include_bytes!(concat!(
    env!("CARGO_MANIFEST_DIR"),
    "/../../vendor/rivet-engine/2.3.10/aarch64-apple-darwin/rivet-engine"
));

pub fn configure(config: &mut ServeConfig) -> Result<()> {
    if config.engine_binary_path.is_some() || !manages_local_engine(config)? {
        return Ok(());
    }

    config.engine_binary_path = Some(install()?);
    config.engine_auto_download = false;
    Ok(())
}

fn manages_local_engine(config: &ServeConfig) -> Result<bool> {
    match config.engine_spawn {
        EngineSpawnMode::Always => Ok(true),
        EngineSpawnMode::Never => Ok(false),
        EngineSpawnMode::Auto => {
            let endpoint = Url::parse(&config.endpoint)
                .with_context(|| format!("parse Rivet endpoint {}", config.endpoint))?;
            let Some(host) = endpoint.host_str() else {
                bail!("Rivet endpoint {} has no host", config.endpoint);
            };
            let ip_host = host
                .strip_prefix('[')
                .and_then(|value| value.strip_suffix(']'))
                .unwrap_or(host);
            Ok(host.eq_ignore_ascii_case("localhost")
                || host.ends_with(".localhost")
                || ip_host
                    .parse::<std::net::IpAddr>()
                    .is_ok_and(|address| address.is_loopback() || address.is_unspecified()))
        }
    }
}

#[cfg(all(target_os = "macos", target_arch = "aarch64"))]
fn install() -> Result<PathBuf> {
    let destination = runtime_root()?.join(format!(
        "rivet-engine/{ENGINE_VERSION}/{ENGINE_TARGET}/rivet-engine"
    ));
    install_asset(&destination, ENGINE_BYTES)
}

#[cfg(not(all(target_os = "macos", target_arch = "aarch64")))]
fn install() -> Result<PathBuf> {
    bail!(
        "Aetherflow does not vendor Rivet Engine {ENGINE_VERSION} for {}-{}",
        std::env::consts::ARCH,
        std::env::consts::OS
    )
}

fn runtime_root() -> Result<PathBuf> {
    if let Some(path) = std::env::var_os("AETHERFLOW_DATA_DIR") {
        return Ok(PathBuf::from(path).join("runtime"));
    }
    let home = std::env::var_os("HOME").context(
        "HOME is not set; set AETHERFLOW_DATA_DIR to choose Aetherflow's runtime directory",
    )?;
    Ok(PathBuf::from(home).join(".aetherflow/runtime"))
}

fn install_asset(destination: &Path, bytes: &[u8]) -> Result<PathBuf> {
    if destination
        .metadata()
        .is_ok_and(|metadata| metadata.is_file() && metadata.len() == bytes.len() as u64)
    {
        make_executable(destination)?;
        return Ok(destination.to_owned());
    }

    let parent = destination
        .parent()
        .context("vendored Rivet Engine destination has no parent")?;
    fs::create_dir_all(parent)
        .with_context(|| format!("create Rivet Engine directory {}", parent.display()))?;
    let temporary = destination.with_extension(format!("tmp-{}", std::process::id()));
    let mut file = OpenOptions::new()
        .create(true)
        .truncate(true)
        .write(true)
        .open(&temporary)
        .with_context(|| format!("create temporary Rivet Engine {}", temporary.display()))?;
    file.write_all(bytes)
        .with_context(|| format!("write temporary Rivet Engine {}", temporary.display()))?;
    file.sync_all()
        .with_context(|| format!("sync temporary Rivet Engine {}", temporary.display()))?;
    make_executable(&temporary)?;
    fs::rename(&temporary, destination).with_context(|| {
        format!(
            "install vendored Rivet Engine from {} to {}",
            temporary.display(),
            destination.display()
        )
    })?;
    Ok(destination.to_owned())
}

#[cfg(unix)]
fn make_executable(path: &Path) -> Result<()> {
    use std::os::unix::fs::PermissionsExt;

    let mut permissions = path
        .metadata()
        .with_context(|| format!("read Rivet Engine metadata {}", path.display()))?
        .permissions();
    permissions.set_mode(0o755);
    fs::set_permissions(path, permissions)
        .with_context(|| format!("make Rivet Engine executable {}", path.display()))
}

#[cfg(not(unix))]
fn make_executable(_path: &Path) -> Result<()> {
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::TempDir;

    #[test]
    fn installs_and_repairs_an_engine_asset() -> Result<()> {
        let temp = TempDir::new()?;
        let destination = temp.path().join("nested/rivet-engine");

        assert_eq!(install_asset(&destination, b"engine")?, destination.clone());
        assert_eq!(fs::read(&destination)?, b"engine");

        fs::write(&destination, b"bad")?;
        install_asset(&destination, b"engine")?;
        assert_eq!(fs::read(&destination)?, b"engine");
        Ok(())
    }

    #[test]
    fn auto_manages_only_local_endpoints() -> Result<()> {
        let config = |endpoint: &str| ServeConfig {
            endpoint: endpoint.to_owned(),
            engine_spawn: EngineSpawnMode::Auto,
            ..ServeConfig::default()
        };

        assert!(manages_local_engine(&config("http://127.0.0.1:6420"))?);
        assert!(manages_local_engine(&config("http://localhost:6420"))?);
        assert!(manages_local_engine(&config("http://[::1]:6420"))?);
        assert!(!manages_local_engine(&config("https://rivet.example"))?);
        Ok(())
    }
}
