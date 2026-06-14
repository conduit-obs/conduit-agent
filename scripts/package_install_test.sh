#!/usr/bin/env bash
# package_install_test.sh — install a built Conduit Linux package, assert the
# packaging contract (binary, dedicated user/group, config + unit layout,
# shipped config validity, real OTLP roundtrip), then uninstall cleanly.
#
# Runs the lifecycle a real operator hits, minus the systemd start (which
# needs a systemd-enabled host — the deb path in the integration workflow
# covers that on the native runner; this script is what the rpm / archlinux
# container jobs and `make {deb,rpm}-install-test` use).
#
# Usage:
#   scripts/package_install_test.sh <package-glob> <apt|dnf|pacman>
#
# Example:
#   scripts/package_install_test.sh 'dist/conduit_*_linux_amd64.deb' apt
set -euo pipefail

PKG_GLOB="${1:?usage: package_install_test.sh <package-glob> <apt|dnf|pacman>}"
PKG_MGR="${2:?usage: package_install_test.sh <package-glob> <apt|dnf|pacman>}"

# Resolve the glob to a single concrete file.
# shellcheck disable=SC2206
matches=( $PKG_GLOB )
if [ ${#matches[@]} -ne 1 ] || [ ! -f "${matches[0]}" ]; then
    echo "package_install_test: expected exactly one package matching '$PKG_GLOB', found ${#matches[@]}" >&2
    printf '  %s\n' "${matches[@]:-<none>}" >&2
    exit 1
fi
PKG="${matches[0]}"
echo "==> testing package: $PKG (manager: $PKG_MGR)"

fail() { echo "package_install_test: FAIL: $*" >&2; exit 1; }

# ---- install ---------------------------------------------------------------
case "$PKG_MGR" in
    apt)    apt-get update -qq || true; apt-get install -y "./$PKG" 2>/dev/null || apt-get install -y "$(readlink -f "$PKG")" ;;
    dnf)    dnf install -y "$PKG" ;;
    pacman) pacman -Sy --noconfirm; pacman -U --noconfirm "$PKG" ;;
    *)      fail "unknown package manager: $PKG_MGR" ;;
esac

# ---- assert the packaging contract ----------------------------------------
[ -x /usr/bin/conduit ] || fail "/usr/bin/conduit missing or not executable"
/usr/bin/conduit version || fail "conduit version did not run"

getent group conduit  >/dev/null 2>&1 || fail "system group 'conduit' was not created"
getent passwd conduit >/dev/null 2>&1 || fail "system user 'conduit' was not created"

[ -f /etc/conduit/conduit.yaml ] || fail "/etc/conduit/conduit.yaml missing"
[ -f /etc/conduit/conduit.env ]  || fail "/etc/conduit/conduit.env missing"
if [ ! -f /lib/systemd/system/conduit.service ] && [ ! -f /usr/lib/systemd/system/conduit.service ]; then
    fail "conduit.service systemd unit not installed"
fi

# ---- shipped config validates ---------------------------------------------
export HONEYCOMB_API_KEY="${HONEYCOMB_API_KEY:-smoke-dummy-key}"
export CONDUIT_DEPLOYMENT_ENVIRONMENT="${CONDUIT_DEPLOYMENT_ENVIRONMENT:-ci}"
/usr/bin/conduit config --validate -c /etc/conduit/conduit.yaml \
    || fail "shipped /etc/conduit/conduit.yaml failed validation"

# ---- real OTLP roundtrip through the installed binary ----------------------
# Reuse the shared smoke (it renders its own profile.mode=none config, so it
# is independent of the host-profile receivers the shipped config enables).
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
"$SCRIPT_DIR/smoke_otlp.sh" /usr/bin/conduit

# ---- uninstall -------------------------------------------------------------
case "$PKG_MGR" in
    apt)    apt-get remove -y conduit ;;
    dnf)    dnf remove -y conduit ;;
    pacman) pacman -R --noconfirm conduit ;;
esac
[ ! -e /usr/bin/conduit ] || fail "/usr/bin/conduit still present after uninstall"

echo "==> package_install_test: ok ($PKG)"
