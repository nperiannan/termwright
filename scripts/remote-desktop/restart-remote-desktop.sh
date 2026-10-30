#!/usr/bin/env bash
# Termwright - restart a remote desktop service (xrdp or TigerVNC).
# Restarting clears a VNC address that TigerVNC blocked after wrong passwords,
# and repairs a service that hangs. Anyone connected to that desktop is
# disconnected. Arguments: the systemd units to restart. Uses sudo only when it
# works without asking, or with the password Termwright passes in TW_SUDO
# (read from the command's standard input, never printed).
set +e
export LC_ALL=C
if [ "$(id -u)" = 0 ]; then M=root
elif sudo -n true 2>/dev/null; then M=nopasswd
elif [ -n "$TW_SUDO" ] && printf '%s\n' "$TW_SUDO" | sudo -S -p '' -v 2>/dev/null; then M=password
else
  echo "sudo is needed to restart a service and is not available. Type the sudo password in Termwright, or run on the machine:"
  echo "  sudo systemctl restart $*"
  exit 3
fi
priv() {
  case $M in
    root) "$@" ;;
    nopasswd) sudo -n "$@" ;;
    password) printf '%s\n' "$TW_SUDO" | sudo -S -p '' "$@" ;;
  esac
}
echo "restarting: $*"
priv systemctl restart "$@"; rc=$?
sleep 3
for u in "$@"; do printf '%s: %s\n' "$u" "$(systemctl is-active "$u" 2>&1)"; done
if [ "$rc" != 0 ]; then echo "systemctl restart failed (status $rc)"; exit 1; fi
echo "restarted"
