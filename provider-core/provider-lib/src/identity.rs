//! Persistent provider identity: a long-term X25519 keypair on disk (Tier 0).
//! Tier 1+ replaces the on-disk secret with a hardware-bound key (StrongBox / TPM / VBS).

use anyhow::{Context, Result};
use crypto_box::SecretKey;
use sc_crypto::Keypair;
use std::path::{Path, PathBuf};

/// Default identity location: `$HOME/.shared-compute/identity.key` (32 raw bytes).
pub fn default_path() -> PathBuf {
    let home = std::env::var_os("HOME")
        .or_else(|| std::env::var_os("USERPROFILE"))
        .map(PathBuf::from)
        .unwrap_or_else(|| PathBuf::from("."));
    home.join(".shared-compute").join("identity.key")
}

/// Load the keypair at `path`, or create and persist a fresh one.
pub fn load_or_create(path: &Path) -> Result<Keypair> {
    if path.is_file() {
        let raw =
            std::fs::read(path).with_context(|| format!("read identity {}", path.display()))?;
        let arr: [u8; 32] = raw
            .as_slice()
            .try_into()
            .map_err(|_| anyhow::anyhow!("identity file must be exactly 32 bytes"))?;
        let secret = SecretKey::from(arr);
        let public = secret.public_key();
        return Ok(Keypair { public, secret });
    }

    let kp = Keypair::generate();
    if let Some(dir) = path.parent() {
        std::fs::create_dir_all(dir).with_context(|| format!("mkdir {}", dir.display()))?;
    }
    std::fs::write(path, kp.secret.to_bytes())
        .with_context(|| format!("write {}", path.display()))?;
    harden_perms(path);
    Ok(kp)
}

#[cfg(unix)]
fn harden_perms(path: &Path) {
    use std::os::unix::fs::PermissionsExt;
    let _ = std::fs::set_permissions(path, std::fs::Permissions::from_mode(0o600));
}

#[cfg(not(unix))]
fn harden_perms(_path: &Path) {}

#[cfg(test)]
mod tests {
    use super::*;
    use sc_crypto::encode_public;

    #[test]
    fn persists_and_reloads() {
        let dir = std::env::temp_dir().join(format!("sc-id-{}", std::process::id()));
        let path = dir.join("identity.key");
        let _ = std::fs::remove_dir_all(&dir);

        let a = load_or_create(&path).unwrap();
        let b = load_or_create(&path).unwrap();
        assert_eq!(encode_public(&a.public), encode_public(&b.public));

        let _ = std::fs::remove_dir_all(&dir);
    }
}
