#!/bin/sh
# Conduit post-remove maintainer script. POSIX-sh.
#
# Intentionally conservative: does NOT remove the conduit user, the
# /etc/conduit directory, or /var/lib/conduit / /var/log/conduit. That way
# a re-install (or apt purge follow-up) preserves operator state and config.
# A full purge happens only via `apt purge conduit` (which calls postremove
# with $1 == "purge") — handled below.
set -eu

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload >/dev/null 2>&1 || true
fi

# deb-only: full purge removes config too.
if [ "${1:-}" = "purge" ]; then
    rm -rf /etc/conduit /var/lib/conduit /var/log/conduit
    if getent passwd conduit >/dev/null 2>&1; then
        userdel conduit 2>/dev/null || true
    fi
    if getent group conduit >/dev/null 2>&1; then
        groupdel conduit 2>/dev/null || true
    fi
fi
