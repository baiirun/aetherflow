use std::{
    fs, io,
    path::{Path, PathBuf},
};

const FNV_OFFSET_BASIS: u64 = 0xcbf29ce484222325;
const FNV_PRIME: u64 = 0x100000001b3;

fn main() -> io::Result<()> {
    let manifest_directory = PathBuf::from(
        std::env::var_os("CARGO_MANIFEST_DIR").expect("Cargo sets CARGO_MANIFEST_DIR"),
    );
    let workspace_root = fs::canonicalize(manifest_directory.join("../.."))?;
    let storage_directory = workspace_root.join("crates/aetherflow-storage");
    let desktop_directory = workspace_root.join("crates/aetherflow-desktop");
    let inputs = [
        workspace_root.join("Cargo.toml"),
        workspace_root.join("Cargo.lock"),
        manifest_directory.join("Cargo.toml"),
        manifest_directory.join("build.rs"),
        manifest_directory.join("src"),
        storage_directory.join("Cargo.toml"),
        storage_directory.join("src"),
        desktop_directory.join("Cargo.toml"),
        desktop_directory.join("src/bin/aetherflowd.rs"),
    ];
    let mut files = Vec::new();
    for input in inputs {
        collect_files(&input, &mut files)?;
    }
    files.sort();

    let mut fingerprint = FNV_OFFSET_BASIS;
    for path in files {
        println!("cargo:rerun-if-changed={}", path.display());
        let relative_path = path.strip_prefix(&workspace_root).unwrap_or(&path);
        fingerprint = hash_bytes(fingerprint, relative_path.as_os_str().as_encoded_bytes());
        fingerprint = hash_bytes(fingerprint, &[0]);
        fingerprint = hash_bytes(fingerprint, &fs::read(path)?);
        fingerprint = hash_bytes(fingerprint, &[0xff]);
    }
    println!("cargo:rustc-env=AETHERFLOW_ACTOR_BUILD_ID={fingerprint:016x}");
    Ok(())
}

fn collect_files(path: &Path, files: &mut Vec<PathBuf>) -> io::Result<()> {
    if path.is_dir() {
        for entry in fs::read_dir(path)? {
            collect_files(&entry?.path(), files)?;
        }
    } else if path.is_file() {
        files.push(path.to_owned());
    }
    Ok(())
}

fn hash_bytes(mut hash: u64, bytes: &[u8]) -> u64 {
    for byte in bytes {
        hash ^= u64::from(*byte);
        hash = hash.wrapping_mul(FNV_PRIME);
    }
    hash
}
