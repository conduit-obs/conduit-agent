# deploy/linux

Linux packaging payload for the Conduit agent. Everything in this directory
ends up shipped via [`nfpms` in `.goreleaser.yaml`](../../.goreleaser.yaml)
on `make release-snapshot` / a real release build.

## Files

| Path                         | Installed to                                | Mode | Notes                                                           |
| ---------------------------- | ------------------------------------------- | ---- | --------------------------------------------------------------- |
| `conduit.service`            | `/lib/systemd/system/conduit.service` (deb) `/usr/lib/systemd/system/conduit.service` (rpm, arch) | 0644 | systemd unit; `Type=simple`, runs as `conduit:conduit`. |
| `conduit.yaml.default`       | `/etc/conduit/conduit.yaml`                 | 0640 root:conduit | Default Conduit config; references env vars from `conduit.env`. Marked `config|noreplace`. |
| `conduit.env.default`        | `/etc/conduit/conduit.env`                  | 0640 root:conduit | Env file consumed by the systemd unit. Marked `config|noreplace`. |
| `scripts/preinstall.sh`      | (maintainer script)                         | 0755 | Creates `conduit:conduit`; adds `conduit` to `adm` and `systemd-journal` if present. |
| `scripts/postinstall.sh`     | (maintainer script)                         | 0755 | `chown` / `chmod` config files; `systemctl daemon-reload`; restart on upgrade. |
| `scripts/preremove.sh`       | (maintainer script)                         | 0755 | Stops the unit; disables it on uninstall (not on upgrade). |
| `scripts/postremove.sh`      | (maintainer script)                         | 0755 | `daemon-reload`; on `apt purge`, removes `conduit` user / `/etc/conduit`. |

## Install paths created at runtime

| Path                | Owner             | Mode | Purpose                                                |
| ------------------- | ----------------- | ---- | ------------------------------------------------------ |
| `/usr/bin/conduit`  | root:root         | 0755 | The agent binary.                                      |
| `/etc/conduit/`     | root:root         | 0755 | Holds `conduit.yaml` and `conduit.env`.                |
| `/var/lib/conduit/` | conduit:conduit   | 0750 | Reserved for filestorage-extension queues (M10).       |
| `/var/log/conduit/` | conduit:conduit   | 0750 | Reserved for stderr capture if we ever stop journaling.|

The default config is env-var driven so deployments only need to populate
`/etc/conduit/conduit.env`:

```sh
HONEYCOMB_API_KEY=hcaik_xxx
CONDUIT_SERVICE_NAME=edge-gateway
CONDUIT_DEPLOYMENT_ENVIRONMENT=production
```

## Quick install

```sh
curl -fsSL https://raw.githubusercontent.com/conduit-obs/conduit-agent/main/scripts/install_linux.sh \
  | sudo bash -s -- --api-key="$HONEYCOMB_API_KEY" --service-name=edge-gateway
```

The installer downloads the right `.deb` / `.rpm` / `.apk` for the host's
distro and architecture, installs it, seeds `/etc/conduit/conduit.env`,
and `systemctl enable --now conduit`s. Re-running it is safe (`apt-get
install` / `dnf install` do upgrade-in-place).

## Manual install (release artifact)

```sh
# Debian / Ubuntu
sudo apt-get install -y ./conduit_X.Y.Z_linux_amd64.deb

# RHEL / Fedora / Amazon Linux
sudo dnf install -y ./conduit_X.Y.Z_linux_amd64.rpm

# Arch / pacman
sudo pacman -U ./conduit_X.Y.Z_linux_amd64.pkg.tar.zst
```

> **Alpine note**: an `.apk` build is on the M3.B follow-up list, not in
> this milestone. Alpine ships busybox `adduser` / `addgroup` instead of
> `useradd` / `groupadd`, uses `/sbin/nologin`, and runs OpenRC instead
> of systemd, all of which the maintainer scripts here assume. The
> binary itself runs fine on Alpine — for now, drop the `linux_amd64.tar.gz`
> archive in place and write your own OpenRC service file.

## Acceptance behavior (M3)

- Re-installing the same version preserves operator edits to
  `/etc/conduit/conduit.yaml` and `conduit.env` (`config|noreplace`).
- Default config requires only `HONEYCOMB_API_KEY` and
  `CONDUIT_SERVICE_NAME` to start.
- The systemd unit logs to journald via the default `StandardOutput=journal`.
- `/etc/conduit/conduit.yaml` and `conduit.env` are 0640 root:conduit.

References:

- [`conduit-agent-plan/04-milestone-plan.md`](../../conduit-agent-plan/04-milestone-plan.md) §M3.
- [`conduit-agent-plan/06-work-breakdown-structure.md`](../../conduit-agent-plan/06-work-breakdown-structure.md) EP-4.
