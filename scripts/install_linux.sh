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
#   --service-name=NAME     service.name on emitted signals (defaults to hostname).
#   --deployment-env=ENV    deployment.environment (default: production).
#   --version=VER           Conduit version to install (default: latest GitHub release).
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
# Conduit needs HONEYCOMB_API_KEY + CONDUIT_SERVICE_NAME before the
# service can start. Three ways to supply them, in order of preference:
#
#   1. --api-key / --service-name flags        (CI-friendly, scripted installs)
#   2. interactive prompts                     (sudo ./install_linux.sh on a TTY)
#   3. operator hand-edits /etc/conduit/conduit.env after the fact
#
# Path 3 is what postinstall.sh already documents in the package's own
# message; the install script's job is to make 1 + 2 ergonomic and to
# refuse to enable+start a service that's guaranteed to crash-loop.

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
        DEFAULT_NAME="$(hostname -s 2>/dev/null || hostname || echo conduit)"
        printf 'CONDUIT_SERVICE_NAME [%s]: ' "$DEFAULT_NAME"
        read -r SERVICE_NAME || true
        SERVICE_NAME="${SERVICE_NAME:-$DEFAULT_NAME}"
    fi
fi

# ---- seed env file if we have anything to seed -----------------------------

if [ -n "$API_KEY" ] || [ -n "$SERVICE_NAME" ]; then
    if [ -z "$SERVICE_NAME" ]; then
        SERVICE_NAME="$(hostname -s 2>/dev/null || hostname)"
    fi
    cat > /etc/conduit/conduit.env <<EOF
# Written by install_linux.sh on $(date -u +%FT%TZ).
HONEYCOMB_API_KEY=${API_KEY}
CONDUIT_SERVICE_NAME=${SERVICE_NAME}
CONDUIT_DEPLOYMENT_ENVIRONMENT=${DEPLOYMENT_ENV}
EOF
    chown root:conduit /etc/conduit/conduit.env
    chmod 0640 /etc/conduit/conduit.env
fi

# ---- enable + start (only if env is actually complete) ---------------------
#
# We deliberately refuse to enable+start when the required env is empty.
# Earlier versions emitted a soft warning and started the service anyway,
# which left every fresh install in a Restart=on-failure crash loop and
# buried the real "you need to set these vars" message under a misleading
# "==> conduit is running" line. Fail loud, exit clean.

env_is_complete() {
    grep -q '^HONEYCOMB_API_KEY=..*' /etc/conduit/conduit.env 2>/dev/null \
        && grep -q '^CONDUIT_SERVICE_NAME=..*' /etc/conduit/conduit.env 2>/dev/null
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

==> Conduit installed but NOT started — required environment is unset.

   1. Edit /etc/conduit/conduit.env and set:
        HONEYCOMB_API_KEY                  (required)
        CONDUIT_SERVICE_NAME               (required)
        CONDUIT_DEPLOYMENT_ENVIRONMENT     (defaults to "production")

   2. Validate the config:
        sudo -u conduit conduit config --validate -c /etc/conduit/conduit.yaml

   3. Enable and start the agent:
        sudo systemctl enable --now conduit

   4. Watch the logs:
        sudo journalctl -u conduit -f

EOF
fi
