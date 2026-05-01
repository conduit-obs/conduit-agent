#!/bin/sh
# Conduit pre-remove maintainer script. POSIX-sh.
#
# Stops the service before files disappear. Disables the service only on
# a true uninstall (not on upgrade), to avoid breaking auto-start across
# package upgrades.
set -eu

if ! command -v systemctl >/dev/null 2>&1; then
    exit 0
fi

# Detect "uninstall" vs "upgrade" across nfpm targets:
#   deb : $1 == "remove" (uninstall) / "upgrade" (upgrade)
#   rpm : $1 == "0" (uninstall)      / "1"       (upgrade)
#   apk : no arg                     -> treat as uninstall
#   arch: no arg                     -> treat as uninstall
uninstalling=1
case "${1:-}" in
    upgrade|1|2)
        uninstalling=0 ;;
esac

if systemctl is-active --quiet conduit 2>/dev/null; then
    systemctl stop conduit || true
fi

if [ "$uninstalling" -eq 1 ]; then
    if systemctl is-enabled --quiet conduit 2>/dev/null; then
        systemctl disable conduit || true
    fi
fi
