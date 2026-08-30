# Packaging

Installs `provider-daemon` as a background service that starts on login/boot and
reconnects on its own.

| Platform | Mechanism | Files |
|---|---|---|
| macOS | launchd LaunchAgent (per-user) | `macos/dev.ayni.provider.plist`, `macos/install.sh` |
| Linux | systemd user service | `linux/shared-compute-provider.service`, `linux/install.sh` |
| Windows | service + MSI | added in M7 |

Binaries come from GitHub Releases (built by `.github/workflows/release.yml` on a `v*`
tag) or a local `cargo build --release -p provider-daemon --features llama`.

## Configuration

Both installers read `~/.shared-compute/provider.env`:

```
SC_COORDINATOR_URL=wss://api.ayni-ai.com/ws/provider
SC_REGISTRATION_TOKEN=sc_prov_xxxxxxxx
SC_MODEL=qwen2.5-0.5b-instruct-q4_k_m
SC_BACKEND=llama
SC_MANIFEST_URL=https://models.ayni-ai.com
SC_REGISTRY_PUBKEY=p7UUs6aCFebGUfSvFV5Wczh7kYBCEW2tDRO+dV0IpDM=
```

The model is downloaded and SHA-256-verified from the signed registry on first start.

## macOS

```bash
./packaging/macos/install.sh /path/to/provider-daemon
launchctl list | grep ayni.provider
log stream --predicate 'process == "provider-daemon"' --info      # view logs
./packaging/macos/install.sh --uninstall
```

## Linux

```bash
./packaging/linux/install.sh /path/to/provider-daemon
systemctl --user status shared-compute-provider
journalctl --user -u shared-compute-provider -f
loginctl enable-linger "$USER"     # keep running after logout
./packaging/linux/install.sh --uninstall
```
