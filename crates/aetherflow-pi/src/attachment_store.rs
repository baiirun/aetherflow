use crate::PiMessage;
use aetherflow_storage::{AttachmentId, AttachmentRef};
use anyhow::{Context, Result, bail};
use base64::Engine as _;
use serde_json::Value;
use sha2::{Digest, Sha256};
use std::{
    collections::HashMap,
    fs::{self, OpenOptions},
    io::Write,
    path::{Path, PathBuf},
    sync::atomic::{AtomicU64, Ordering},
};

pub const MAX_ATTACHMENT_BYTES: usize = 25 * 1024 * 1024;
const ATTACHMENT_ID_FIELD: &str = "attachmentId";
static NEXT_TEMPORARY_ID: AtomicU64 = AtomicU64::new(0);

#[derive(Clone, Debug)]
pub struct LocalAttachmentStore {
    root: PathBuf,
}

impl LocalAttachmentStore {
    pub fn from_env() -> Result<Self> {
        Ok(Self::new(default_attachment_directory()?))
    }

    pub fn new(root: impl Into<PathBuf>) -> Self {
        Self { root: root.into() }
    }

    pub fn root(&self) -> &Path {
        &self.root
    }

    pub fn put(&self, media_type: &str, bytes: &[u8]) -> Result<AttachmentRef> {
        validate_media_type(media_type)?;
        if bytes.is_empty() {
            bail!("attachment is empty");
        }
        if bytes.len() > MAX_ATTACHMENT_BYTES {
            bail!(
                "attachment is {} bytes; maximum is {MAX_ATTACHMENT_BYTES}",
                bytes.len()
            );
        }

        let id = AttachmentId::from_sha256(Sha256::digest(bytes).into());
        let attachment = AttachmentRef {
            id,
            media_type: media_type.to_owned(),
            byte_len: u64::try_from(bytes.len()).context("attachment length exceeds u64")?,
        };
        let directory = self.directory_for(&attachment.id);
        fs::create_dir_all(&directory)
            .with_context(|| format!("create attachment directory {}", directory.display()))?;

        let blob_path = self.blob_path(&attachment.id);
        if !blob_path.is_file() {
            write_atomic(&blob_path, bytes)?;
        }
        let metadata_path = self.metadata_path(&attachment.id);
        if !metadata_path.is_file() {
            write_atomic(&metadata_path, &serde_json::to_vec(&attachment)?)?;
        }
        Ok(attachment)
    }

    pub fn read(&self, attachment: &AttachmentRef) -> Result<Vec<u8>> {
        let bytes = fs::read(self.blob_path(&attachment.id))
            .with_context(|| format!("read attachment {}", attachment.id))?;
        validate_attachment(attachment, &bytes)?;
        Ok(bytes)
    }

    pub fn get(&self, id: &AttachmentId) -> Result<(AttachmentRef, Vec<u8>)> {
        let metadata = fs::read(self.metadata_path(id))
            .with_context(|| format!("read metadata for attachment {id}"))?;
        let attachment: AttachmentRef =
            serde_json::from_slice(&metadata).context("decode attachment metadata")?;
        if &attachment.id != id {
            bail!("attachment metadata identity does not match requested ID");
        }
        let bytes = self.read(&attachment)?;
        Ok((attachment, bytes))
    }

    fn directory_for(&self, id: &AttachmentId) -> PathBuf {
        self.root.join(&id.to_string()[..2])
    }

    fn blob_path(&self, id: &AttachmentId) -> PathBuf {
        self.directory_for(id).join(format!("{id}.blob"))
    }

    fn metadata_path(&self, id: &AttachmentId) -> PathBuf {
        self.directory_for(id).join(format!("{id}.json"))
    }
}

pub(crate) fn externalize_pi_message(
    store: &LocalAttachmentStore,
    message: PiMessage,
) -> Result<PiMessage> {
    let mut value = serde_json::to_value(message)?;
    externalize_value(store, &mut value)?;
    serde_json::from_value(value).context("decode Pi message after externalizing attachments")
}

pub(crate) fn referenced_attachments(message: &PiMessage) -> Result<Vec<AttachmentRef>> {
    let value = serde_json::to_value(message)?;
    let mut attachments = HashMap::new();
    collect_references(&value, &mut attachments)?;
    Ok(attachments.into_values().collect())
}

pub(crate) fn hydrate_pi_message(
    message: &mut PiMessage,
    attachments: &HashMap<AttachmentId, Vec<u8>>,
) -> Result<()> {
    let mut value = serde_json::to_value(&*message)?;
    hydrate_value(&mut value, attachments)?;
    *message =
        serde_json::from_value(value).context("decode Pi message after hydrating attachments")?;
    Ok(())
}

fn externalize_value(store: &LocalAttachmentStore, value: &mut Value) -> Result<()> {
    match value {
        Value::Array(values) => {
            for value in values {
                externalize_value(store, value)?;
            }
        }
        Value::Object(object) => {
            let is_inline_image = object.get("type").and_then(Value::as_str) == Some("image")
                && object.get("data").and_then(Value::as_str).is_some();
            if is_inline_image {
                let media_type = object
                    .get("mimeType")
                    .and_then(Value::as_str)
                    .context("Pi image content is missing mimeType")?;
                let data = object
                    .get("data")
                    .and_then(Value::as_str)
                    .context("Pi image content is missing data")?;
                let bytes = base64::engine::general_purpose::STANDARD
                    .decode(data)
                    .context("decode Pi image content")?;
                let attachment = store.put(media_type, &bytes)?;
                object.remove("data");
                object.insert(
                    ATTACHMENT_ID_FIELD.to_owned(),
                    Value::String(attachment.id.to_string()),
                );
                object.insert("byteLength".to_owned(), Value::from(attachment.byte_len));
            } else {
                for value in object.values_mut() {
                    externalize_value(store, value)?;
                }
            }
        }
        _ => {}
    }
    Ok(())
}

fn collect_references(
    value: &Value,
    attachments: &mut HashMap<AttachmentId, AttachmentRef>,
) -> Result<()> {
    match value {
        Value::Array(values) => {
            for value in values {
                collect_references(value, attachments)?;
            }
        }
        Value::Object(object) => {
            if object.get("type").and_then(Value::as_str) == Some("image")
                && let Some(id) = object.get(ATTACHMENT_ID_FIELD).and_then(Value::as_str)
            {
                let attachment = AttachmentRef {
                    id: id.parse().context("parse attachment ID in Pi message")?,
                    media_type: object
                        .get("mimeType")
                        .and_then(Value::as_str)
                        .context("attachment reference is missing mimeType")?
                        .to_owned(),
                    byte_len: object
                        .get("byteLength")
                        .and_then(Value::as_u64)
                        .context("attachment reference is missing byteLength")?,
                };
                attachments.insert(attachment.id.clone(), attachment);
            }
            for value in object.values() {
                collect_references(value, attachments)?;
            }
        }
        _ => {}
    }
    Ok(())
}

fn hydrate_value(value: &mut Value, attachments: &HashMap<AttachmentId, Vec<u8>>) -> Result<()> {
    match value {
        Value::Array(values) => {
            for value in values {
                hydrate_value(value, attachments)?;
            }
        }
        Value::Object(object) => {
            if object.get("type").and_then(Value::as_str) == Some("image")
                && let Some(id) = object.get(ATTACHMENT_ID_FIELD).and_then(Value::as_str)
            {
                let id: AttachmentId = id.parse().context("parse attachment ID in Pi message")?;
                let bytes = attachments
                    .get(&id)
                    .with_context(|| format!("attachment {id} was not downloaded"))?;
                object.insert(
                    "data".to_owned(),
                    Value::String(base64::engine::general_purpose::STANDARD.encode(bytes)),
                );
            }
            for value in object.values_mut() {
                hydrate_value(value, attachments)?;
            }
        }
        _ => {}
    }
    Ok(())
}

fn validate_attachment(attachment: &AttachmentRef, bytes: &[u8]) -> Result<()> {
    if u64::try_from(bytes.len()).ok() != Some(attachment.byte_len) {
        bail!(
            "attachment {} length does not match metadata",
            attachment.id
        );
    }
    let actual = AttachmentId::from_sha256(Sha256::digest(bytes).into());
    if actual != attachment.id {
        bail!(
            "attachment {} content digest does not match its ID",
            attachment.id
        );
    }
    validate_media_type(&attachment.media_type)
}

fn validate_media_type(media_type: &str) -> Result<()> {
    match media_type {
        "image/png" | "image/jpeg" | "image/gif" | "image/webp" => Ok(()),
        _ => bail!("unsupported attachment media type {media_type}"),
    }
}

fn write_atomic(destination: &Path, bytes: &[u8]) -> Result<()> {
    let parent = destination
        .parent()
        .context("attachment destination has no parent")?;
    let temporary = parent.join(format!(
        ".attachment-{}-{}.tmp",
        std::process::id(),
        NEXT_TEMPORARY_ID.fetch_add(1, Ordering::Relaxed)
    ));
    let result = (|| -> Result<()> {
        let mut file = OpenOptions::new()
            .create_new(true)
            .write(true)
            .open(&temporary)
            .with_context(|| format!("create temporary attachment {}", temporary.display()))?;
        file.write_all(bytes)
            .with_context(|| format!("write temporary attachment {}", temporary.display()))?;
        file.sync_all()
            .with_context(|| format!("sync temporary attachment {}", temporary.display()))?;
        fs::rename(&temporary, destination).with_context(|| {
            format!(
                "move temporary attachment {} to {}",
                temporary.display(),
                destination.display()
            )
        })
    })();
    if result.is_err() {
        let _ = fs::remove_file(&temporary);
    }
    result
}

fn default_attachment_directory() -> Result<PathBuf> {
    if let Some(path) = std::env::var_os("AETHERFLOW_DATA_DIR") {
        return Ok(PathBuf::from(path).join("attachments"));
    }
    let home = std::env::var_os("HOME")
        .context("HOME is not set; set AETHERFLOW_DATA_DIR to choose attachment storage")?;
    Ok(PathBuf::from(home).join(".aetherflow/attachments"))
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn stores_large_images_once_and_validates_reads() -> Result<()> {
        let temp = tempfile::tempdir()?;
        let store = LocalAttachmentStore::new(temp.path());
        let bytes = vec![7; 96 * 1024];

        let first = store.put("image/png", &bytes)?;
        let second = store.put("image/png", &bytes)?;

        assert_eq!(first, second);
        assert_eq!(store.read(&first)?, bytes);
        assert_eq!(store.get(&first.id)?.0, first);
        Ok(())
    }

    #[test]
    fn externalized_events_carry_references_instead_of_base64() -> Result<()> {
        let temp = tempfile::tempdir()?;
        let store = LocalAttachmentStore::new(temp.path());
        let data = base64::engine::general_purpose::STANDARD.encode(vec![3; 64 * 1024]);
        let message: PiMessage = serde_json::from_value(json!({
            "type": "message_end",
            "message": {
                "role": "user",
                "content": [{ "type": "image", "mimeType": "image/png", "data": data }]
            }
        }))?;

        let mut message = externalize_pi_message(&store, message)?;
        let encoded = serde_json::to_value(&message)?;
        assert!(encoded.to_string().len() < 1_000);
        assert!(encoded.to_string().contains(ATTACHMENT_ID_FIELD));
        assert!(!encoded.to_string().contains(&data));

        let references = referenced_attachments(&message)?;
        let attachment = references.first().context("attachment reference")?;
        hydrate_pi_message(
            &mut message,
            &HashMap::from([(attachment.id.clone(), store.read(attachment)?)]),
        )?;
        assert_eq!(
            serde_json::to_value(message)?,
            serde_json::from_value::<Value>(json!({
                "type": "message_end",
                "message": {
                    "role": "user",
                    "content": [{
                        "type": "image",
                        "mimeType": "image/png",
                        "attachmentId": attachment.id,
                        "byteLength": attachment.byte_len,
                        "data": data
                    }]
                }
            }))?
        );
        Ok(())
    }
}
