#!/usr/bin/env bash
# Termwright - Collect Remote Desktop Logs.
# Read-only: prints the state of the RDP (xrdp) and/or VNC (TigerVNC) service of
# this machine, for finding out why a remote desktop session does not connect.
# It changes nothing and prints no passwords (the VNC password file is only
# listed with its permissions). Commands that need root use sudo only when it
# works without asking, or with the password Termwright passes in TW_SUDO.
#
#   diagnose-remote-desktop.sh [--rdp PORT] [--vnc PORT] [--rdp-user USER] [--vnc-user USER]
set +e
export LC_ALL=C

RDP_PORT=; VNC_PORT=; RDP_USER=; VNC_USER=
while [ $# -gt 0 ]; do
  case $1 in
    --rdp) RDP_PORT=$2; shift 2 ;;
    --vnc) VNC_PORT=$2; shift 2 ;;
    --rdp-user) RDP_USER=$2; shift 2 ;;
    --vnc-user) VNC_USER=$2; shift 2 ;;
    *) shift ;;
  esac
done

SUDO_MODE=none
if [ "$(id -u)" = 0 ]; then SUDO_MODE=root
elif command -v sudo >/dev/null 2>&1; then
  if sudo -n true 2>/dev/null; then SUDO_MODE=nopasswd
  elif [ -n "$TW_SUDO" ]; then
    if printf '%s\n' "$TW_SUDO" | sudo -S -p '' -v 2>/dev/null; then SUDO_MODE=password; fi
  fi
fi

# priv CMD...: run as root when that is possible; status 126 when it is not
priv() {
  case $SUDO_MODE in
    root) "$@" ;;
    nopasswd) sudo -n "$@" ;;
    password) printf '%s\n' "$TW_SUDO" | sudo -S -p '' "$@" ;;
    *) return 126 ;;
  esac
}
# show TITLE CMD...: prints a titled block; says so when root was needed and is not available
show() {
  local title=$1; shift
  printf -- '--- %s\n' "$title"
  local out rc
  out=$("$@" 2>&1 </dev/null); rc=$?
  if [ "$rc" = 126 ] && [ "$SUDO_MODE" = none ]; then
    cmdline="$*"; echo "(needs sudo - run on the machine: sudo ${cmdline#priv })"
  else
    [ -n "$out" ] && printf '%s\n' "$out" | head -n 60
    [ -z "$out" ] && echo "(nothing)"
  fi
}
section() { printf '\n== %s ==\n' "$1"; }
home_of() { local h; h=$(getent passwd "$1" 2>/dev/null | cut -d: -f6); printf '%s' "${h:-/home/$1}"; }

# can this machine reach its own port, and what does it say
local_banner() {
  local port=$1
  ( exec 3<>"/dev/tcp/127.0.0.1/$port" || exit 1
    timeout 3 head -c 12 <&3 | tr -d '\r\n\0' ; echo ) 2>&1 || echo "no connection to 127.0.0.1:$port"
}
local_tcp() { ( exec 3<>"/dev/tcp/127.0.0.1/$1" ) 2>/dev/null && echo "127.0.0.1:$1 accepts connections" || echo "127.0.0.1:$1 refuses connections"; }

section "Machine"
printf 'host:   %s\n' "$(hostname 2>/dev/null)"
. /etc/os-release 2>/dev/null && printf 'os:     %s\n' "$PRETTY_NAME"
printf 'kernel: %s\n' "$(uname -r)"
printf 'uptime: %s\n' "$(uptime -p 2>/dev/null || uptime)"
printf 'login:  %s\n' "$(id -un)"
case $SUDO_MODE in
  root) echo "sudo:   running as root" ;;
  nopasswd) echo "sudo:   works without a password" ;;
  password) echo "sudo:   works with the password given" ;;
  *) echo "sudo:   not available (logs that need root are skipped; the commands to run are shown)" ;;
esac
printf 'systemd: %s\n' "$(systemctl is-system-running 2>&1 | head -n 1)"
show "Listening TCP ports of the remote desktop services" sh -c "ss -ltnH 2>/dev/null | awk '{print \$4}' | grep -E ':(3389|3350|59[0-9][0-9]|${RDP_PORT:-0}|${VNC_PORT:-0})\$' | sort -u"
show "Who listens (process names)" priv ss -ltnp
SSHD=$(priv sshd -T 2>/dev/null | grep -iE '^(allowtcpforwarding|permitopen|disableforwarding|gatewayports) ' | tr '\n' ' ')
echo "--- sshd forwarding (needed to reach 127.0.0.1 through this SSH session)"
if [ -n "$SSHD" ]; then echo "$SSHD"; else
  grep -rihE '^[[:space:]]*(AllowTcpForwarding|PermitOpen|DisableForwarding)' /etc/ssh/sshd_config /etc/ssh/sshd_config.d/ 2>/dev/null | head -n 10
  echo "(settings in the files above; none listed = the default, forwarding allowed)"
fi
show "Firewall" priv ufw status

if [ -n "$RDP_PORT" ]; then
  section "RDP (xrdp) - port $RDP_PORT, user ${RDP_USER:-?}"
  echo "this machine to itself: $(local_tcp "$RDP_PORT")"
  echo "--- packages"; dpkg-query -W -f='${Package} ${Version}\n' xrdp xorgxrdp 2>&1 | head -n 4
  echo "--- services"
  for u in xrdp xrdp-sesman; do printf '%s: %s, %s\n' "$u" "$(systemctl is-active $u 2>&1)" "$(systemctl is-enabled $u 2>&1)"; done
  show "xrdp.ini (port, address, security)" grep -E '^(port|address|security_layer|crypt_level|tls_ciphers|ssl_protocols)=' /etc/xrdp/xrdp.ini
  show "journal of xrdp and xrdp-sesman" priv journalctl -u xrdp -u xrdp-sesman -n 40 --no-pager
  show "/var/log/xrdp.log" priv tail -n 30 /var/log/xrdp.log
  show "/var/log/xrdp-sesman.log" priv tail -n 30 /var/log/xrdp-sesman.log
  if [ -n "$RDP_USER" ]; then
    echo "--- user $RDP_USER"
    if id "$RDP_USER" >/dev/null 2>&1; then
      H=$(home_of "$RDP_USER")
      printf 'id: %s\nhome: %s (%s)\nshell: %s\n' "$(id "$RDP_USER")" "$H" "$([ -d "$H" ] && echo exists || echo MISSING)" "$(getent passwd "$RDP_USER" | cut -d: -f7)"
      printf 'locked: %s\n' "$(priv passwd -S "$RDP_USER" 2>&1 | awk '{print $2}')"
      show "startup files of $RDP_USER" priv ls -la "$H/.xsession" "$H/.xsessionrc" "$H/.xsession-errors"
      show "$H/.xsession-errors (last lines)" priv tail -n 20 "$H/.xsession-errors"
    else echo "the user $RDP_USER does not exist on this machine"; fi
    show "desktop processes of $RDP_USER" sh -c "ps -u '$RDP_USER' -o pid,etime,args --no-headers 2>/dev/null | cut -c1-140 | head -n 15"
  fi
  show "Login sessions (who is at the machine)" loginctl list-sessions --no-pager
fi

if [ -n "$VNC_PORT" ]; then
  DISP=$((VNC_PORT - 5900))
  UNIT="tigervnc@${DISP}.service"
  section "VNC (TigerVNC) - port $VNC_PORT (display :$DISP), user ${VNC_USER:-?}"
  echo "this machine to itself: $(local_tcp "$VNC_PORT")"
  echo "what the server says first: $(local_banner "$VNC_PORT")   (a working VNC server says RFB 003.008; RFB 003.003 means it has blocked this address after wrong passwords - restart the service or wait)"
  echo "--- packages"; dpkg-query -W -f='${Package} ${Version}\n' tigervnc-standalone-server tigervnc-common 2>&1 | head -n 4
  echo "--- service"; printf '%s: %s, %s\n' "$UNIT" "$(systemctl is-active "$UNIT" 2>&1)" "$(systemctl is-enabled "$UNIT" 2>&1)"
  show "systemctl status $UNIT" systemctl status "$UNIT" --no-pager -n 15
  show "journal of $UNIT" priv journalctl -u "$UNIT" -n 40 --no-pager
  show "unit file" cat /etc/systemd/system/tigervnc@.service
  show "VNC and X server processes" sh -c "ps -eo user,pid,etime,args --no-headers 2>/dev/null | grep -E '[X]tigervnc|[X]vnc|[v]ncserver' | cut -c1-200 | head -n 10"
  if [ -n "$VNC_USER" ]; then
    echo "--- user $VNC_USER"
    if id "$VNC_USER" >/dev/null 2>&1; then
      H=$(home_of "$VNC_USER")
      printf 'id: %s\nhome: %s (%s)\nshell: %s\n' "$(id "$VNC_USER")" "$H" "$([ -d "$H" ] && echo exists || echo MISSING)" "$(getent passwd "$VNC_USER" | cut -d: -f7)"
      # TigerVNC 1.13+ (Ubuntu 24.04) keeps its files in ~/.config/tigervnc and its logs in
      # ~/.local/state/tigervnc; older versions use ~/.vnc. Look in all three.
      for d in "$H/.vnc" "$H/.config/tigervnc" "$H/.local/state/tigervnc"; do
        show "$d (names and permissions only)" priv ls -la "$d"
      done
      for f in "$H/.vnc/xstartup" "$H/.config/tigervnc/xstartup"; do
        priv test -f "$f" && show "$f" priv head -n 20 "$f"
      done
      for d in "$H/.vnc" "$H/.config/tigervnc" "$H/.local/state/tigervnc"; do
        for f in $(priv sh -c "ls '$d'/*.log 2>/dev/null"); do show "VNC server log $f" priv tail -n 40 "$f"; done
      done
    else echo "the user $VNC_USER does not exist on this machine"; fi
  fi
  show "Other VNC displays in use" sh -c "ls /tmp/.X11-unix 2>/dev/null; ls /tmp/.X*-lock 2>/dev/null"
fi

section "End"
echo "collected $(date '+%Y-%m-%d %H:%M:%S %z')"
exit 0
