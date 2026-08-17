use anyhow::{Context, Result, bail};
use std::{
    collections::HashSet,
    env,
    ffi::{OsStr, OsString},
    path::{Path, PathBuf},
    process::{Child, Command, Stdio},
    time::{Duration, SystemTime, UNIX_EPOCH},
};

const RUNNER_PROBE_TIMEOUT: Duration = Duration::from_millis(750);
const RUNNER_START_TIMEOUT: Duration = Duration::from_secs(15);
const RUNNER_PROBE_INTERVAL: Duration = Duration::from_millis(50);
const RUNNER_HEARTBEAT_MAX_AGE: Duration = Duration::from_secs(10);
const SHELL_PATH_BEGIN: &str = "__AETHERFLOW_PATH_BEGIN__";
const SHELL_PATH_END: &str = "__AETHERFLOW_PATH_END__";
const SHELL_PATH_PROBE: &str =
    "printf '__AETHERFLOW_PATH_BEGIN__%s__AETHERFLOW_PATH_END__' \"$PATH\"";

#[derive(Clone)]
pub struct DaemonTarget {
    pub endpoint: String,
    pub token: String,
    pub namespace: String,
    pub pool: String,
}

pub struct RunnerSnapshot {
    keys: HashSet<String>,
    fresh_keys: HashSet<String>,
}

impl RunnerSnapshot {
    pub fn is_ready(&self) -> bool {
        !self.fresh_keys.is_empty()
    }

    pub fn keys(&self) -> &HashSet<String> {
        &self.keys
    }
}

/// Keeps the child handle alive so the desktop process remains its parent.
///
/// Dropping a `Child` handle does not terminate the daemon. This is intentional:
/// prompts may continue after the last desktop window closes, and the next app or
/// CLI invocation can reconnect to the same daemon.
pub struct ManagedDaemon {
    child: Child,
}

impl ManagedDaemon {
    pub fn is_running(&mut self) -> Result<bool> {
        Ok(self
            .child
            .try_wait()
            .context("check whether the desktop-managed daemon is still running")?
            .is_none())
    }
}

pub fn launch() -> Result<ManagedDaemon> {
    let executable = daemon_executable()?;
    let runtime_path = match login_shell_runtime_path() {
        Ok(path) => path,
        Err(error) => {
            eprintln!("Aetherflow desktop could not read the login-shell PATH: {error:#}");
            None
        }
    };
    let child = daemon_command(&executable, runtime_path.as_deref())
        .stdin(Stdio::null())
        .stdout(Stdio::inherit())
        .stderr(Stdio::inherit())
        .spawn()
        .with_context(|| format!("launch Aetherflow daemon {}", executable.display()))?;

    eprintln!(
        "Aetherflow desktop launched {} (pid {})",
        executable.display(),
        child.id()
    );
    Ok(ManagedDaemon { child })
}

fn daemon_command(executable: &Path, runtime_path: Option<&OsStr>) -> Command {
    let mut command = Command::new(executable);
    if let Some(runtime_path) = runtime_path {
        command.env("PATH", runtime_path);
    }
    command
}

fn login_shell_runtime_path() -> Result<Option<OsString>> {
    let shell = env::var_os("SHELL")
        .map(PathBuf::from)
        .filter(|shell| shell.is_file())
        .or_else(|| {
            [PathBuf::from("/bin/zsh"), PathBuf::from("/bin/sh")]
                .into_iter()
                .find(|shell| shell.is_file())
        });
    shell
        .as_deref()
        .map(|shell| runtime_path_from_login_shell(shell, SHELL_PATH_PROBE))
        .transpose()
        .map(Option::flatten)
}

fn runtime_path_from_login_shell(shell: &Path, probe: &str) -> Result<Option<OsString>> {
    let output = Command::new(shell)
        .arg("-lic")
        .arg(probe)
        .stdin(Stdio::null())
        .stderr(Stdio::null())
        .output()
        .with_context(|| format!("launch login shell {}", shell.display()))?;
    if !output.status.success() {
        bail!(
            "login shell {} exited with {}",
            shell.display(),
            output.status
        );
    }

    let output = String::from_utf8(output.stdout).context("login shell PATH was not UTF-8")?;
    let Some(start) = output.rfind(SHELL_PATH_BEGIN) else {
        bail!("login shell output omitted the PATH marker");
    };
    let path_start = start + SHELL_PATH_BEGIN.len();
    let Some(path_end) = output[path_start..].find(SHELL_PATH_END) else {
        bail!("login shell output omitted the closing PATH marker");
    };
    let path = &output[path_start..path_start + path_end];
    if path.is_empty() {
        return Ok(None);
    }
    Ok(Some(OsString::from(path)))
}

pub async fn runner_snapshot(target: &DaemonTarget) -> Result<RunnerSnapshot> {
    let response = match reqwest::Client::new()
        .get(format!("{}/envoys", target.endpoint.trim_end_matches('/')))
        .bearer_auth(&target.token)
        .query(&[
            ("namespace", target.namespace.as_str()),
            ("name", target.pool.as_str()),
            ("limit", "100"),
        ])
        .timeout(RUNNER_PROBE_TIMEOUT)
        .send()
        .await
    {
        Ok(response) => response,
        Err(error) if error.is_connect() || error.is_timeout() => {
            return Ok(RunnerSnapshot {
                keys: HashSet::new(),
                fresh_keys: HashSet::new(),
            });
        }
        Err(error) => return Err(error).context("probe Aetherflow daemon runner"),
    };

    let status = response.status();
    let body = response
        .text()
        .await
        .context("read Aetherflow daemon runner probe")?;
    if !status.is_success() {
        bail!("Aetherflow daemon runner probe failed with {status}: {body}");
    }

    decode_runner_snapshot(&body, current_time_ms()?)
}

pub async fn runner_is_ready(target: &DaemonTarget) -> Result<bool> {
    Ok(runner_snapshot(target).await?.is_ready())
}

/// Forces Rivet to retry placement when a durable actor is stranded in the
/// `no_envoys` backoff state. Rescheduling preserves the actor's stored state.
pub async fn recover_stalled_actor(
    target: &DaemonTarget,
    actor_name: &str,
    actor_key: &str,
) -> Result<bool> {
    let client = reqwest::Client::new();
    let response = client
        .get(format!("{}/actors", target.endpoint.trim_end_matches('/')))
        .bearer_auth(&target.token)
        .query(&[
            ("namespace", target.namespace.as_str()),
            ("name", actor_name),
            ("key", actor_key),
            ("limit", "1"),
        ])
        .timeout(RUNNER_PROBE_TIMEOUT)
        .send()
        .await
        .context("inspect durable actor status")?;
    let status = response.status();
    let body = response.text().await.context("read durable actor status")?;
    if !status.is_success() {
        bail!("durable actor status request failed with {status}: {body}");
    }

    let response: serde_json::Value =
        serde_json::from_str(&body).context("decode durable actor status")?;
    let actor = response
        .get("actors")
        .and_then(serde_json::Value::as_array)
        .and_then(|actors| {
            actors.iter().find(|actor| {
                actor.get("name").and_then(serde_json::Value::as_str) == Some(actor_name)
                    && actor.get("key").and_then(serde_json::Value::as_str) == Some(actor_key)
            })
        });
    let Some(actor) = actor else {
        return Ok(false);
    };
    if !actor
        .get("destroy_ts")
        .is_none_or(serde_json::Value::is_null)
        || actor.get("error").and_then(serde_json::Value::as_str) != Some("no_envoys")
    {
        return Ok(false);
    }
    let actor_id = actor
        .get("actor_id")
        .and_then(serde_json::Value::as_str)
        .context("stalled durable actor omitted actor_id")?;

    let response = client
        .post(format!(
            "{}/actors/{actor_id}/reschedule",
            target.endpoint.trim_end_matches('/')
        ))
        .bearer_auth(&target.token)
        .query(&[("namespace", target.namespace.as_str())])
        .json(&serde_json::json!({}))
        .timeout(RUNNER_PROBE_TIMEOUT)
        .send()
        .await
        .context("reschedule stalled durable actor")?;
    let status = response.status();
    let body = response
        .text()
        .await
        .context("read durable actor reschedule response")?;
    if !status.is_success() {
        bail!("durable actor reschedule failed with {status}: {body}");
    }

    eprintln!("Aetherflow desktop rescheduled stalled actor {actor_name}/{actor_key} ({actor_id})");
    Ok(true)
}

fn decode_runner_snapshot(body: &str, now_ms: u64) -> Result<RunnerSnapshot> {
    let response: serde_json::Value =
        serde_json::from_str(body).context("decode Aetherflow daemon runner probe")?;
    let envoys = response
        .get("envoys")
        .and_then(serde_json::Value::as_array)
        .context("Aetherflow daemon runner probe omitted envoys")?;
    let heartbeat_cutoff_ms = now_ms
        .saturating_sub(u64::try_from(RUNNER_HEARTBEAT_MAX_AGE.as_millis()).unwrap_or(u64::MAX));
    let mut keys = HashSet::new();
    let mut fresh_keys = HashSet::new();
    for envoy in envoys {
        if !envoy.get("stop_ts").is_none_or(serde_json::Value::is_null) {
            continue;
        }
        let Some(key) = envoy.get("envoy_key").and_then(serde_json::Value::as_str) else {
            continue;
        };
        keys.insert(key.to_owned());
        if envoy
            .get("last_ping_ts")
            .and_then(serde_json::Value::as_u64)
            .is_some_and(|last_ping_ms| last_ping_ms >= heartbeat_cutoff_ms)
        {
            fresh_keys.insert(key.to_owned());
        }
    }
    Ok(RunnerSnapshot { keys, fresh_keys })
}

pub async fn wait_for_runner(target: &DaemonTarget) -> Result<()> {
    let deadline = tokio::time::Instant::now() + RUNNER_START_TIMEOUT;
    loop {
        if runner_is_ready(target).await? {
            return Ok(());
        }
        if tokio::time::Instant::now() >= deadline {
            bail!(
                "Aetherflow daemon did not register pool {} within {} seconds",
                target.pool,
                RUNNER_START_TIMEOUT.as_secs()
            );
        }
        tokio::time::sleep(RUNNER_PROBE_INTERVAL).await;
    }
}

pub async fn wait_for_launched_runner(
    target: &DaemonTarget,
    daemon: &mut ManagedDaemon,
    previous_runner_keys: &HashSet<String>,
) -> Result<()> {
    let deadline = tokio::time::Instant::now() + RUNNER_START_TIMEOUT;
    loop {
        let snapshot = runner_snapshot(target).await?;
        if snapshot
            .fresh_keys
            .iter()
            .any(|key| !previous_runner_keys.contains(key))
        {
            return Ok(());
        }
        if !daemon.is_running()? {
            bail!("Aetherflow daemon exited before its runner registered");
        }
        if tokio::time::Instant::now() >= deadline {
            bail!(
                "Aetherflow daemon did not register pool {} within {} seconds",
                target.pool,
                RUNNER_START_TIMEOUT.as_secs()
            );
        }
        tokio::time::sleep(RUNNER_PROBE_INTERVAL).await;
    }
}

fn current_time_ms() -> Result<u64> {
    let millis = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .context("system clock is before the Unix epoch")?
        .as_millis();
    u64::try_from(millis).context("system time does not fit in milliseconds")
}

fn daemon_executable() -> Result<PathBuf> {
    if let Some(configured) = env::var_os("AETHERFLOWD_PATH") {
        let configured = PathBuf::from(configured);
        if configured.is_file() {
            return Ok(configured);
        }
        bail!(
            "AETHERFLOWD_PATH does not point to a file: {}",
            configured.display()
        );
    }

    let current_executable = env::current_exe().context("locate Aetherflow desktop executable")?;
    if let Some(candidate) = daemon_candidates(&current_executable)
        .into_iter()
        .find(|candidate| candidate.is_file())
    {
        return Ok(candidate);
    }

    // Development installs may put both binaries on PATH instead of in an app
    // bundle. `Command` performs the PATH lookup when given a bare filename.
    Ok(PathBuf::from("aetherflowd"))
}

fn daemon_candidates(current_executable: &Path) -> Vec<PathBuf> {
    let Some(executable_directory) = current_executable.parent() else {
        return Vec::new();
    };

    let mut candidates = Vec::with_capacity(2);
    if executable_directory
        .file_name()
        .is_some_and(|name| name == "MacOS")
        && let Some(contents_directory) = executable_directory.parent()
    {
        candidates.push(contents_directory.join("Helpers/aetherflowd"));
    }
    candidates.push(executable_directory.join("aetherflowd"));
    candidates
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::{
        io::{Read, Write},
        net::TcpListener,
        sync::mpsc,
    };

    #[test]
    fn login_shell_path_is_forwarded_to_the_daemon() {
        let probe = format!(
            "printf '{}%s{}' '/runtime/bin:/usr/bin'",
            SHELL_PATH_BEGIN, SHELL_PATH_END
        );
        let runtime_path = runtime_path_from_login_shell(Path::new("/bin/sh"), &probe)
            .expect("read login shell PATH")
            .expect("login shell should return PATH");
        let command = daemon_command(Path::new("/tmp/aetherflowd"), Some(&runtime_path));
        let configured_path = command
            .get_envs()
            .find_map(|(key, value)| (key == "PATH").then_some(value).flatten());

        assert_eq!(configured_path, Some(OsStr::new("/runtime/bin:/usr/bin")));
    }

    #[test]
    fn app_bundle_prefers_the_helpers_directory() {
        let candidates = daemon_candidates(Path::new(
            "/Applications/Aetherflow.app/Contents/MacOS/aetherflow-desktop",
        ));

        assert_eq!(
            candidates,
            vec![
                PathBuf::from("/Applications/Aetherflow.app/Contents/Helpers/aetherflowd"),
                PathBuf::from("/Applications/Aetherflow.app/Contents/MacOS/aetherflowd"),
            ]
        );
    }

    #[test]
    fn development_binary_uses_a_sibling_daemon() {
        assert_eq!(
            daemon_candidates(Path::new("/repo/target/debug/aetherflow-desktop")),
            vec![PathBuf::from("/repo/target/debug/aetherflowd")]
        );
    }

    #[tokio::test]
    async fn runner_probe_uses_envoy_status_without_touching_actors() {
        let listener = TcpListener::bind("127.0.0.1:0").expect("bind probe server");
        let endpoint = format!(
            "http://{}",
            listener.local_addr().expect("read probe server address")
        );
        let (request_tx, request_rx) = mpsc::channel();
        let server = std::thread::spawn(move || {
            let (mut stream, _) = listener.accept().expect("accept runner probe");
            let mut request = [0_u8; 4096];
            let bytes_read = stream.read(&mut request).expect("read runner probe");
            request_tx
                .send(String::from_utf8_lossy(&request[..bytes_read]).into_owned())
                .expect("record runner probe");
            let body = format!(
                r#"{{"envoys":[{{"envoy_key":"runner-1","pool_name":"rivetkit-rust","stop_ts":null,"last_ping_ts":{}}}]}}"#,
                current_time_ms().expect("read test time")
            );
            write!(
                stream,
                "HTTP/1.1 200 OK\r\ncontent-type: application/json\r\ncontent-length: {}\r\nconnection: close\r\n\r\n{body}",
                body.len(),
            )
            .expect("write runner probe response");
        });
        let target = DaemonTarget {
            endpoint,
            token: "dev".to_owned(),
            namespace: "default".to_owned(),
            pool: "rivetkit-rust".to_owned(),
        };

        assert!(runner_is_ready(&target).await.expect("probe runner"));
        let request = request_rx.recv().expect("receive runner probe");
        assert!(request.starts_with("GET /envoys?"), "{request}");
        assert!(!request.contains("/gateway/"), "{request}");
        server.join().expect("join probe server");
    }

    #[tokio::test]
    async fn stalled_actor_is_rescheduled_without_deleting_its_state() {
        let listener = TcpListener::bind("127.0.0.1:0").expect("bind recovery server");
        let endpoint = format!(
            "http://{}",
            listener.local_addr().expect("read recovery server address")
        );
        let (request_tx, request_rx) = mpsc::channel();
        let server = std::thread::spawn(move || {
            for response_body in [
                r#"{"actors":[{"actor_id":"stalled-actor","name":"session_directory","key":"sessions-v2","error":"no_envoys","connectable_ts":null,"destroy_ts":null}]}"#,
                "{}",
            ] {
                let (mut stream, _) = listener.accept().expect("accept recovery request");
                let mut request = [0_u8; 4096];
                let bytes_read = stream.read(&mut request).expect("read recovery request");
                request_tx
                    .send(String::from_utf8_lossy(&request[..bytes_read]).into_owned())
                    .expect("record recovery request");
                write!(
                    stream,
                    "HTTP/1.1 200 OK\r\ncontent-type: application/json\r\ncontent-length: {}\r\nconnection: close\r\n\r\n{response_body}",
                    response_body.len(),
                )
                .expect("write recovery response");
            }
        });
        let target = DaemonTarget {
            endpoint,
            token: "dev".to_owned(),
            namespace: "default".to_owned(),
            pool: "rivetkit-rust".to_owned(),
        };

        assert!(
            recover_stalled_actor(&target, "session_directory", "sessions-v2")
                .await
                .expect("recover stalled actor")
        );
        let inspect_request = request_rx.recv().expect("receive actor inspection");
        assert!(
            inspect_request.starts_with("GET /actors?"),
            "{inspect_request}"
        );
        assert!(
            inspect_request.contains("name=session_directory"),
            "{inspect_request}"
        );
        assert!(
            inspect_request.contains("key=sessions-v2"),
            "{inspect_request}"
        );
        let recover_request = request_rx.recv().expect("receive actor recovery");
        assert!(
            recover_request.starts_with("POST /actors/stalled-actor/reschedule?"),
            "{recover_request}"
        );
        assert!(!recover_request.contains("DELETE"), "{recover_request}");
        server.join().expect("join recovery server");
    }

    #[test]
    fn stale_runner_heartbeat_is_not_ready() {
        let body = r#"{"envoys":[{"envoy_key":"stale","stop_ts":null,"last_ping_ts":1000}]}"#;

        let snapshot = decode_runner_snapshot(body, 20_000).expect("decode runner snapshot");

        assert_eq!(snapshot.keys(), &HashSet::from(["stale".to_owned()]));
        assert!(!snapshot.is_ready());
    }
}
