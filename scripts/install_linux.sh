#!/usr/bin/env bash
# install_linux.sh — fetch the right Conduit Linux package for this host
# (deb / rpm / apk) from a GitHub release, install it, optionally seed
# /etc/conduit/conduit.env, and start the service.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/conduit-obs/conduit-agent/main/scripts/install_linux.sh \
#     | sudo bash -s -- --api-key=YOUR_KEY --service-name=edge-gw
#
# Or download then run:
#   sudo ./install_linux.sh --version=0.1.0 --api-key=$KEY
#
# Flags:
#   --api-key=KEY           Honeycomb ingest key (HONEYCOMB_API_KEY).
#   --service-name=NAME     service.name on emitted signals. When omitted, the
#                           agent uses the profile-shaped default ("linux-host"
#                           on the linux profile) so checked-in dashboards
#                           target a known dataset out of the box. See ADR-0021.
#   --deployment-env=ENV    deployment.environment (default: production).
#   --version=VER           Conduit version to install (default: latest GitHub release).
#   --with-obi              Install the systemd drop-in that grants the eBPF
#                           capabilities the OBI receiver needs (ADR-0020). Required
#                           when conduit.yaml has obi.enabled: true on a Linux host.
#                           Idempotent — re-running the installer regenerates the
#                           drop-in instead of appending.
#   --no-start              Install but do not enable/start the systemd unit.
#   --help                  Print this help and exit.
#
# The script auto-detects the package manager (apt/dnf/yum/apk) and CPU
# architecture (amd64/arm64). It is safe to re-run: package managers handle
# upgrade-in-place, and the env file is only rewritten if --api-key was passed.
set -euo pipefail

REPO="conduit-obs/conduit-agent"
VERSION=""
API_KEY=""
SERVICE_NAME=""
DEPLOYMENT_ENV="production"
START=1
WITH_OBI=0

usage() {
    sed -n '2,21p' "$0" | sed 's/^# \{0,1\}//'
}

die() {
    echo "install_linux.sh: error: $*" >&2
    exit 1
}

require_root() {
    if [ "${EUID:-$(id -u)}" -ne 0 ]; then
        die "must run as root (try: sudo $0 $*)"
    fi
}

# ---- arg parsing -----------------------------------------------------------

while [ $# -gt 0 ]; do
    case "$1" in
        --api-key=*)         API_KEY="${1#*=}" ;;
        --api-key)           API_KEY="${2:-}"; shift ;;
        --service-name=*)    SERVICE_NAME="${1#*=}" ;;
        --service-name)      SERVICE_NAME="${2:-}"; shift ;;
        --deployment-env=*)  DEPLOYMENT_ENV="${1#*=}" ;;
        --deployment-env)    DEPLOYMENT_ENV="${2:-}"; shift ;;
        --version=*)         VERSION="${1#*=}" ;;
        --version)           VERSION="${2:-}"; shift ;;
        --with-obi)          WITH_OBI=1 ;;
        --no-start)          START=0 ;;
        --help|-h)           usage; exit 0 ;;
        *) die "unknown flag: $1 (try --help)" ;;
    esac
    shift
done

require_root "$@"

# ---- detect platform -------------------------------------------------------

if [ ! -f /etc/os-release ]; then
    die "/etc/os-release missing; cannot detect distribution"
fi

ARCH=""
case "$(uname -m)" in
    x86_64|amd64) ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64 ;;
    *) die "unsupported architecture: $(uname -m)" ;;
esac

PKG=""
INSTALL_CMD=""
if   command -v apt-get >/dev/null 2>&1; then PKG=deb; INSTALL_CMD="apt-get install -y"
elif command -v dnf     >/dev/null 2>&1; then PKG=rpm; INSTALL_CMD="dnf install -y"
elif command -v yum     >/dev/null 2>&1; then PKG=rpm; INSTALL_CMD="yum install -y"
elif command -v pacman  >/dev/null 2>&1; then PKG="pkg.tar.zst"; INSTALL_CMD="pacman -U --noconfirm"
else
    die "no supported package manager (apt/dnf/yum/pacman) on PATH; an Alpine .apk build is a post-M3.B follow-up"
fi

# ---- resolve version -------------------------------------------------------

if [ -z "$VERSION" ]; then
    if ! command -v curl >/dev/null 2>&1; then
        die "curl is required to discover the latest release; install curl or pass --version"
    fi
    VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
              | grep '"tag_name"' \
              | head -n1 \
              | sed -E 's/.*"tag_name": *"v?([^"]+)".*/\1/')
    if [ -z "$VERSION" ]; then
        die "could not resolve latest version from GitHub API; pass --version explicitly"
    fi
fi

# ---- download + install ----------------------------------------------------

ASSET="conduit_${VERSION}_linux_${ARCH}.${PKG}"
URL="https://github.com/${REPO}/releases/download/v${VERSION}/${ASSET}"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

echo "==> downloading ${ASSET}"
if command -v curl >/dev/null 2>&1; then
    curl -fsSL -o "${TMP}/${ASSET}" "$URL"
elif command -v wget >/dev/null 2>&1; then
    wget -qO "${TMP}/${ASSET}" "$URL"
else
    die "neither curl nor wget on PATH"
fi

echo "==> installing ${ASSET} via ${INSTALL_CMD%% *}"
# shellcheck disable=SC2086
$INSTALL_CMD "${TMP}/${ASSET}"

# ---- collect required env --------------------------------------------------
#
# Conduit needs HONEYCOMB_API_KEY before the service can start. service.name
# is supplied either by the operator (via --service-name or by editing
# conduit.yaml after install) OR by the profile-shaped default that
# applyDefaults() fills in at startup ("linux-host" on the linux profile;
# see ADR-0021). Three ways to supply both pieces, in order of preference:
#
#   1. --api-key / --service-name flags        (CI-friendly, scripted installs)
#   2. interactive prompts                     (sudo ./install_linux.sh on a TTY)
#   3. operator hand-edits /etc/conduit/conduit.{env,yaml} after the fact
#
# Path 3 is what postinstall.sh documents in the package's own message; the
# install script's job is to make 1 + 2 ergonomic and to refuse to enable +
# start a service guaranteed to crash-loop on a missing API key.

if [ -t 0 ] && [ -t 1 ]; then
    if [ -z "$API_KEY" ]; then
        printf '\nConduit needs a Honeycomb ingest key to export.\n'
        printf 'HONEYCOMB_API_KEY (input hidden): '
        if stty -echo 2>/dev/null; then
            read -r API_KEY || true
            stty echo 2>/dev/null
            printf '\n'
        else
            read -r API_KEY || true
        fi
    fi
    if [ -z "$SERVICE_NAME" ]; then
        # Default value matches the profile-shaped default applyDefaults
        # would fill in if we wrote nothing here. Operators on multi-host
        # fleets typically take the default and slice by host.name in
        # boards; operators running multiple workloads per host override.
        DEFAULT_NAME="linux-host"
        printf 'service.name [%s]: ' "$DEFAULT_NAME"
        read -r SERVICE_NAME || true
        SERVICE_NAME="${SERVICE_NAME:-$DEFAULT_NAME}"
    fi
fi

# ---- seed env file (HONEYCOMB_API_KEY only) --------------------------------
#
# Per ADR-0021, CONDUIT_SERVICE_NAME is no longer seeded by the installer —
# service_name lives in conduit.yaml, written below.

if [ -n "$API_KEY" ]; then
    cat > /etc/conduit/conduit.env <<EOF
# Written by install_linux.sh on $(date -u +%FT%TZ).
HONEYCOMB_API_KEY=${API_KEY}
CONDUIT_DEPLOYMENT_ENVIRONMENT=${DEPLOYMENT_ENV}
EOF
    chown root:conduit /etc/conduit/conduit.env
    chmod 0640 /etc/conduit/conduit.env
fi

# ---- write service_name into conduit.yaml ---------------------------------
#
# When the operator supplied --service-name=foo (or accepted the interactive
# prompt with a non-default value), we write `service_name: foo` directly
# into /etc/conduit/conduit.yaml. Idempotent: replaces an existing
# `^service_name:` line, otherwise appends one.
#
# When the operator left service_name empty, we touch nothing — the agent
# fills in the profile-shaped default at startup. Operators who change
# their mind later edit conduit.yaml by hand.

write_service_name() {
    local target="/etc/conduit/conduit.yaml"
    local name="$1"
    if [ ! -f "$target" ]; then
        echo "==> ${target} not found; package install must precede this step." >&2
        return 1
    fi
    if grep -q '^service_name:' "$target"; then
        # Replace existing line in place. Use # as sed delimiter so service
        # names containing / (unlikely but possible) don't break.
        sed -i.bak "s#^service_name:.*#service_name: ${name}#" "$target"
        rm -f "${target}.bak"
    else
        # Append a fresh block at the end of the file. The leading newline
        # keeps the appended line visually separated from whatever the
        # default file ended with.
        printf '\n# Written by install_linux.sh --service-name on %s.\nservice_name: %s\n' \
            "$(date -u +%FT%TZ)" "$name" >> "$target"
    fi
}

if [ -n "$SERVICE_NAME" ]; then
    write_service_name "$SERVICE_NAME"
    echo "==> wrote service_name: ${SERVICE_NAME} to /etc/conduit/conduit.yaml"
fi

# ---- OBI systemd drop-in (--with-obi) --------------------------------------
#
# The OBI receiver (ADR-0020) instruments processes via eBPF, which
# requires a non-default capability set. We grant the upstream-documented
# caps (CAP_SYS_ADMIN, CAP_DAC_READ_SEARCH, CAP_NET_RAW, CAP_SYS_PTRACE,
# CAP_PERFMON, CAP_BPF) via a drop-in so the operator's `--with-obi`
# decision is auditable and reversible (`rm` the file + daemon-reload).
#
# Writing both CapabilityBoundingSet= and AmbientCapabilities= is the
# defensive shape: the bounding set defines the upper limit a process can
# ever drop into, and the ambient set propagates the caps across exec
# boundaries. Without the bounding set the ambient set has no effect.
#
# Idempotent: rewrites the file every run. Operators reverting OBI
# remove the file with `sudo rm /etc/systemd/system/conduit.service.d/obi.conf`
# and reload.

obi_kernel_supports_obi() {
    [ -r /proc/sys/kernel/osrelease ] || return 1
    local rel major minor
    rel=$(cat /proc/sys/kernel/osrelease)
    major=$(printf '%s' "$rel" | cut -d. -f1)
    minor=$(printf '%s' "$rel" | cut -d. -f2 | grep -oE '^[0-9]+' || echo 0)
    if [ "$major" -gt 5 ] || { [ "$major" -eq 5 ] && [ "$minor" -ge 8 ]; }; then
        return 0
    fi
    if [ "$major" -ge 4 ] && [ -r /etc/os-release ] && grep -qiE 'id_like=.*rhel|^id="?(rhel|centos|rocky|almalinux|ol)' /etc/os-release; then
        [ "$major" -gt 4 ] || [ "$minor" -ge 18 ]
        return $?
    fi
    return 1
}

write_obi_dropin() {
    local dir="/etc/systemd/system/conduit.service.d"
    local file="${dir}/obi.conf"
    mkdir -p "$dir"
    cat > "$file" <<'EOF'
# Written by install_linux.sh --with-obi.
# Grants the eBPF capabilities the OBI receiver needs (ADR-0020).
# Remove this file + run `sudo systemctl daemon-reload` to revert.
[Service]
CapabilityBoundingSet=CAP_SYS_ADMIN CAP_DAC_READ_SEARCH CAP_NET_RAW CAP_SYS_PTRACE CAP_PERFMON CAP_BPF
AmbientCapabilities=CAP_SYS_ADMIN CAP_DAC_READ_SEARCH CAP_NET_RAW CAP_SYS_PTRACE CAP_PERFMON CAP_BPF
EOF
    chmod 0644 "$file"
}

if [ "$WITH_OBI" -eq 1 ]; then
    if ! obi_kernel_supports_obi; then
        cat >&2 <<EOF
==> --with-obi requested, but the running kernel does not look like it
    supports OBI (need ≥ 5.8 mainline, or ≥ 4.18 RHEL-family with backports).
    Skipping the drop-in. If you believe the kernel is supported, install
    manually: see docs/getting-started/obi.md.
EOF
    elif ! command -v systemctl >/dev/null 2>&1; then
        echo "==> --with-obi requested but systemctl is not on PATH; skipping drop-in." >&2
    else
        write_obi_dropin
        systemctl daemon-reload
        echo "==> wrote /etc/systemd/system/conduit.service.d/obi.conf granting OBI eBPF caps."
    fi
fi

# ---- enable + start (only if env is actually complete) ---------------------
#
# We deliberately refuse to enable+start when the required env is empty.
# Earlier versions emitted a soft warning and started the service anyway,
# which left every fresh install in a Restart=on-failure crash loop and
# buried the real "you need to set these vars" message under a misleading
# "==> conduit is running" line. Fail loud, exit clean.

# env_is_complete checks that the only key the agent absolutely needs from
# the environment file (HONEYCOMB_API_KEY) is non-empty. service.name is now
# resolved from conduit.yaml + profile defaults, not the env file (ADR-0021).
env_is_complete() {
    grep -q '^HONEYCOMB_API_KEY=..*' /etc/conduit/conduit.env 2>/dev/null
}

if [ "$START" -eq 0 ]; then
    echo "==> install complete. Start when ready: sudo systemctl enable --now conduit"
elif ! command -v systemctl >/dev/null 2>&1; then
    echo "==> install complete. systemctl not found; start conduit via your init system."
elif env_is_complete; then
    systemctl enable --now conduit
    echo "==> conduit is running. Tail logs with: sudo journalctl -u conduit -f"
else
    cat <<'EOF'

==> Conduit installed but NOT started — HONEYCOMB_API_KEY is unset.

   1. Edit /etc/conduit/conduit.env and set:
        HONEYCOMB_API_KEY                  (required)
        CONDUIT_DEPLOYMENT_ENVIRONMENT     (defaults to "production")

   2. (Optional) Override the profile-shaped service.name by editing
      /etc/conduit/conduit.yaml:
        service_name: my-edge-gateway

      Default is "linux-host" on the linux profile (ADR-0021).

   3. Validate the config:
        sudo -u conduit conduit config --validate -c /etc/conduit/conduit.yaml

   4. Enable and start the agent:
        sudo systemctl enable --now conduit

   5. Watch the logs:
        sudo journalctl -u conduit -f

EOF
fi
