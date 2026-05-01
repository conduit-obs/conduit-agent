#!/bin/sh
# Conduit post-install maintainer script. Runs after files are unpacked on
# every package manager nfpm targets. POSIX-sh.
set -eu

CONDUIT_USER=conduit
CONDUIT_GROUP=conduit

mkdir -p /var/lib/conduit /var/log/conduit
chown -R "$CONDUIT_USER:$CONDUIT_GROUP" /var/lib/conduit /var/log/conduit
chmod 0750 /var/lib/conduit /var/log/conduit

# Lock down config + secrets to root:conduit 0640. We unconditionally set
# the mode so existing files are tightened on upgrade if they had drifted.
if [ -f /etc/conduit/conduit.yaml ]; then
    chown root:"$CONDUIT_GROUP" /etc/conduit/conduit.yaml
    chmod 0640 /etc/conduit/conduit.yaml
fi
if [ -f /etc/conduit/conduit.env ]; then
    chown root:"$CONDUIT_GROUP" /etc/conduit/conduit.env
    chmod 0640 /etc/conduit/conduit.env
fi

# Reload systemd so the unit shipped at /lib/systemd/system/conduit.service
# (or /usr/lib/systemd/system/ on rpm-flavored distros) is visible. We do
# NOT enable or start on a fresh install — the user must populate the API
# key first. On upgrades, restart only if the service is already running.
if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload >/dev/null 2>&1 || true

    # Detect "fresh install" vs "upgrade" across nfpm targets:
    #   deb : $1 == "configure" and $2 unset (fresh) / set (upgrade)
    #   rpm : $1 == "1" (fresh) / "2" (upgrade)
    #   apk : $1 == "" (no arg)            -> treat as fresh
    #   arch: no args                      -> treat as fresh
    fresh_install=1
    case "${1:-}" in
        configure)
            if [ -n "${2:-}" ]; then fresh_install=0; fi ;;
        2|3|4|5)
            fresh_install=0 ;;
    esac

    if [ "$fresh_install" -eq 0 ]; then
        if systemctl is-active --quiet conduit 2>/dev/null; then
            systemctl restart conduit || true
        fi
    else
        cat <<'EOF'

Conduit installed. To finish setup:

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
fi
