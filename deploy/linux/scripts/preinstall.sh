#!/bin/sh
# Conduit pre-install maintainer script.
#
# Runs on Debian/Ubuntu (apt invokes preinst), RHEL/Fedora (rpm invokes %pre),
# Alpine (apk invokes pre-install), and Arch (pacman invokes pre_install).
# Stays POSIX-shell so it works under dash on Debian.
#
# Idempotent. Safe to run on upgrades — it skips work that's already done.
set -eu

CONDUIT_USER=conduit
CONDUIT_GROUP=conduit

if ! getent group "$CONDUIT_GROUP" >/dev/null 2>&1; then
    groupadd --system "$CONDUIT_GROUP"
fi

if ! getent passwd "$CONDUIT_USER" >/dev/null 2>&1; then
    useradd --system \
        --gid "$CONDUIT_GROUP" \
        --no-create-home \
        --home-dir /var/lib/conduit \
        --shell /usr/sbin/nologin \
        --comment "Conduit OTel agent" \
        "$CONDUIT_USER"
fi

# Add conduit to groups that grant read access to the platform-default log
# sources. Both groups are optional; suppress failures so non-systemd or
# minimal distros (e.g. Alpine) still install cleanly.
if getent group adm >/dev/null 2>&1; then
    usermod --append --groups adm "$CONDUIT_USER" 2>/dev/null || true
fi
if getent group systemd-journal >/dev/null 2>&1; then
    usermod --append --groups systemd-journal "$CONDUIT_USER" 2>/dev/null || true
fi
