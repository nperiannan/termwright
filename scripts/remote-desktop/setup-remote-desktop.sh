#!/usr/bin/env bash
#
# setup-remote-desktop.sh - XFCE desktop with TigerVNC and/or xrdp on Ubuntu 22.04 / 24.04 and Debian 12
#
# Each service is set up only when it is NOT already running. A service that is
# running (or whose port is taken) is reported and left untouched, so running
# the script again never drops a connected user. --force reconfigures anyway.
# By default both services listen on localhost only; reach them through SSH
# (in Termwright: the SSH session as the jump host, address 127.0.0.1).
#
set -Eeuo pipefail
umask 022
export LC_ALL=C

SCRIPT_NAME=${0##*/}
TARGET_USER=${SUDO_USER:-}
VNC_DISPLAY=2
GEOMETRY=1920x1080
DEPTH=24
ALLOWED_CIDR=""
ENABLE_VNC=1
ENABLE_RDP=1
VNC_NETWORK=0        # 1 = listen on all interfaces
RDP_NETWORK=0
ENABLE_UFW=1
GOODIES=0
FORCE=0
CHECK_ONLY=0
PW_STDIN=0
TMP_DIR=""
VNC_PW=""
ACCOUNT_PW=""
RDP_USER=""
VNC_USER=""
CREATE_USERS=0
SECRETS_STDIN=0
JOURNAL=""           # what this run created, for undoing it (see "undo" below)
LASTERR=""           # file: the last error, in words
LASTSTEP=""          # file: the step that was running
STATE_RDP=""         # "", ok or removed
STATE_VNC=""
DO_RDP=0 DO_VNC=0 RDP_RUNNING=0 VNC_RUNNING=0

usage() {
  cat <<EOF
Usage: sudo ./$SCRIPT_NAME [options]

Installs and configures XFCE with TigerVNC (VNC) and xrdp (RDP) on Ubuntu 22.04 and 24.04
and on Debian 12.
A service that is already running is left alone; use --force to reconfigure it.
When a service does not come up, what this run created for it (users, files, units,
firewall rules) is removed again and the other service is kept; installed packages
stay. Exit status: 0 done, 3 done except one service that was removed again, 1 failed.
Both services listen on localhost only unless told otherwise.

Options:
  --user USER             Desktop login user for both services (default: the sudo user)
  --rdp-user USER         Desktop user of RDP only (default: --user)
  --vnc-user USER         Desktop user of VNC only (default: --user)
  --create-users          Create --rdp-user / --vnc-user when they do not exist (an existing
                          user is never changed). Needs the account password (--secrets-stdin)
  --vnc-display N         TigerVNC display number, 1-99 (default: 2; port = 5900+N)
  --geometry WxH          VNC screen size (default: 1920x1080)
  --depth N               VNC colour depth: 16, 24 or 32 (default: 24)
  --vnc-network           VNC listens on all interfaces (default: localhost only)
  --rdp-network           xrdp listens on all interfaces (default: localhost only)
  --allow-cidr CIDR       Open the firewall (UFW) for this network, e.g. 10.234.179.0/24.
                          Only useful with --vnc-network / --rdp-network
  --no-ufw                Never touch UFW
  --no-vnc                Do not set up VNC
  --no-rdp                Do not set up RDP
  --goodies               Also install xfce4-goodies (a large package set)
  --vnc-password-stdin    Read the VNC password (one line) from standard input
  --secrets-stdin         Read passwords from standard input as lines "TWACCOUNT <password>"
                          (password of the new users) and "TWVNC <password>", ended by "TWEND".
                          Other lines are ignored. Passwords never appear on a command line
  --force                 Reconfigure and restart services that are already running
  --check                 Only report what is running and what would be done
  -h, --help              Show this help

Examples:
  sudo ./$SCRIPT_NAME --user nperiannan
  echo 'secret1' | sudo ./$SCRIPT_NAME --user ubuntu --vnc-password-stdin
  sudo ./$SCRIPT_NAME --user ubuntu --rdp-network --allow-cidr 10.234.179.0/24
  ./$SCRIPT_NAME --check --user ubuntu
EOF
}

note_err() { if [[ -n $LASTERR ]]; then printf '%s' "$1" >"$LASTERR" 2>/dev/null || true; fi; return 0; }
log()  { printf '\n[%s] %s\n' "$SCRIPT_NAME" "$*"; if [[ -n $LASTSTEP ]]; then printf '%s' "$*" >"$LASTSTEP" 2>/dev/null || true; fi; return 0; }
note() { printf '  %s\n' "$*"; }
warn() { printf 'WARNING: %s\n' "$*" >&2; }
die()  { printf 'ERROR: %s\n' "$*" >&2; note_err "$*"; exit 1; }


# What this run changes is written to a journal (see "undo" below). When a
# service does not come up, only that service is removed again; when the whole
# run dies, everything it did is. Packages stay installed.
on_exit() {
  local rc=$?
  trap '' PIPE
  set +e
  if [[ $rc -ne 0 && $rc -ne 3 && -n $JOURNAL ]]; then
    if (( DO_RDP )) && [[ -z $STATE_RDP ]]; then rollback_service rdp; STATE_RDP=removed; fi
    if (( DO_VNC )) && [[ -z $STATE_VNC ]]; then rollback_service vnc; STATE_VNC=removed; fi
    rollback_shared_if_unused
  fi
  [[ -n $TMP_DIR && -d $TMP_DIR ]] && rm -rf "$TMP_DIR"
  exit "$rc"
}
trap on_exit EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP
trap 'exit 141' PIPE
trap 'rc=$?; printf "ERROR: line %s: %s (exit %s)\n" "$LINENO" "$BASH_COMMAND" "$rc" >&2; [[ -s ${LASTERR:-/dev/null} ]] || note_err "$(cat "${LASTSTEP:-/dev/null}" 2>/dev/null || true) failed (exit $rc)"' ERR


# ---------------------------------------------------------------- arguments
need_val() { [[ $2 -ge 2 ]] || die "$1 requires a value"; }
while (($#)); do
  case "$1" in
    --user)         need_val "$1" $#; TARGET_USER=$2; shift 2 ;;
    --rdp-user)     need_val "$1" $#; RDP_USER=$2; shift 2 ;;
    --vnc-user)     need_val "$1" $#; VNC_USER=$2; shift 2 ;;
    --create-users) CREATE_USERS=1; shift ;;
    --secrets-stdin) SECRETS_STDIN=1; shift ;;
    --vnc-display)  need_val "$1" $#; VNC_DISPLAY=$2; shift 2 ;;
    --geometry)     need_val "$1" $#; GEOMETRY=$2; shift 2 ;;
    --depth)        need_val "$1" $#; DEPTH=$2; shift 2 ;;
    --allow-cidr)   need_val "$1" $#; ALLOWED_CIDR=$2; shift 2 ;;
    --vnc-network)  VNC_NETWORK=1; shift ;;
    --rdp-network)  RDP_NETWORK=1; shift ;;
    --no-ufw)       ENABLE_UFW=0; shift ;;
    --no-vnc)       ENABLE_VNC=0; shift ;;
    --no-rdp)       ENABLE_RDP=0; shift ;;
    --goodies)      GOODIES=1; shift ;;
    --vnc-password-stdin) PW_STDIN=1; shift ;;
    --force)        FORCE=1; shift ;;
    --check)        CHECK_ONLY=1; shift ;;
    -h|--help)      usage; exit 0 ;;
    *) die "Unknown option: $1 (see --help)" ;;
  esac
done

# ---------------------------------------------------------------- preflight
[[ -f /etc/os-release ]] || die "Cannot identify this operating system."
# shellcheck disable=SC1091
. /etc/os-release
case "${ID:-}:${VERSION_ID:-}" in
  ubuntu:22.04|ubuntu:24.04|debian:12) ;;
  *) die "This script supports Ubuntu 22.04 and 24.04 and Debian 12 only; found ${PRETTY_NAME:-an unknown system}." ;;
esac
(( CHECK_ONLY )) || [[ $EUID -eq 0 ]] || die "Run with sudo or as root (or use --check)."
command -v ss >/dev/null 2>&1 || die "The ss command is missing: install iproute2 first (sudo apt install iproute2)."

RDP_USER=${RDP_USER:-$TARGET_USER}
VNC_USER=${VNC_USER:-$TARGET_USER}
(( ENABLE_RDP )) && [[ -z $RDP_USER ]] && die "Say which user gets the RDP desktop: --user NAME or --rdp-user NAME"
(( ENABLE_VNC )) && [[ -z $VNC_USER ]] && die "Say which user gets the VNC desktop: --user NAME or --vnc-user NAME"
check_user_name() { # role name
  [[ $2 =~ ^[a-z_][a-z0-9_-]{0,31}$ ]] || die "Invalid $1 user name '$2'."
  [[ $2 != root ]] || die "Do not run a graphical session as root."
  if ! id "$2" >/dev/null 2>&1; then
    (( CREATE_USERS )) || die "User '$2' does not exist (use --create-users to create it)."
  fi
}
(( ENABLE_RDP )) && check_user_name RDP "$RDP_USER"
(( ENABLE_VNC )) && check_user_name VNC "$VNC_USER"
[[ $VNC_DISPLAY =~ ^[1-9][0-9]?$ ]] || die "VNC display must be a number from 1 to 99."
[[ $GEOMETRY =~ ^[1-9][0-9]{2,4}x[1-9][0-9]{2,4}$ ]] || die "Geometry must look like 1920x1080."
[[ $DEPTH =~ ^(16|24|32)$ ]] || die "Depth must be 16, 24 or 32."
[[ -z $ALLOWED_CIDR || $ALLOWED_CIDR =~ ^[0-9a-fA-F:.]+(/[0-9]{1,3})?$ ]] || die "Invalid CIDR '$ALLOWED_CIDR'."
(( ENABLE_VNC || ENABLE_RDP )) || die "Nothing to do: both --no-vnc and --no-rdp were given."

# home and group of a desktop user (a user that is still to be created gets /home/NAME)
home_of()  { local h; h=$(getent passwd "$1" | cut -d: -f6 || true); printf '%s' "${h:-/home/$1}"; }
group_of() { id -gn "$1" 2>/dev/null || printf '%s' "$1"; }
VNC_PORT=$((5900 + VNC_DISPLAY))
VNC_UNIT="tigervnc@${VNC_DISPLAY}.service"
RDP_PORT=3389

is_active()  { systemctl is-active --quiet "$1" 2>/dev/null; }
listing()    { ss -Hlnt "sport = :$1" 2>/dev/null || true; }
listening()  { local o; o=$(listing "$1"); [[ -n $o ]]; }
exposed()    { local o; o=$(listing "$1" | awk '{print $4}'); grep -Eq '^(0\.0\.0\.0|\*|\[::\]):' <<<"$o"; }
# a live X server (not ours) already uses this display number
display_busy() {
  local lock=/tmp/.X${VNC_DISPLAY}-lock pid
  [[ -r $lock ]] || return 1
  pid=$(tr -d '[:space:]' <"$lock" 2>/dev/null || true)
  [[ $pid =~ ^[0-9]+$ ]] && kill -0 "$pid" 2>/dev/null
}

# ------------------------------------------- what is running now (before apt!)
# Installing xrdp starts it with its default settings, so the state has to be
# read first.
RDP_RUNNING=0 VNC_RUNNING=0 DO_RDP=0 DO_VNC=0
if (( ENABLE_RDP )) && { is_active xrdp || is_active xrdp-sesman || listening "$RDP_PORT"; }; then RDP_RUNNING=1; fi
if (( ENABLE_VNC )) && { is_active "$VNC_UNIT" || listening "$VNC_PORT"; }; then VNC_RUNNING=1; fi
if (( ENABLE_RDP && (! RDP_RUNNING || FORCE) )); then DO_RDP=1; fi
if (( ENABLE_VNC && (! VNC_RUNNING || FORCE) )); then DO_VNC=1; fi

if (( DO_VNC )) && ! is_active "$VNC_UNIT"; then
  display_busy && die "Display :$VNC_DISPLAY is used by another X server. Choose another with --vnc-display."
  listening "$VNC_PORT" && die "TCP port $VNC_PORT is used by another program. Choose another --vnc-display."
fi

local_login() { # user
  command -v loginctl >/dev/null 2>&1 && \
    loginctl list-sessions --no-legend 2>/dev/null | awk -v u="$1" '$3 == u && $4 == "seat0" { f = 1 } END { exit !f }'
}
user_note() { # role user
  if id "$2" >/dev/null 2>&1; then
    note "$1 user $2: exists (not changed)"
    local_login "$2" && warn "$2 is logged in at this computer's own screen: a remote desktop for the same user often stays black. Use another user or log out locally first."
  else
    note "$1 user $2: will be created (no administrator rights)"
  fi
  return 0
}

report() {
  local what
  printf '\nDesktop users on %s\n' "$(hostname)"
  (( ENABLE_RDP )) && user_note RDP "$RDP_USER"
  (( ENABLE_VNC )) && user_note VNC "$VNC_USER"
  if (( ENABLE_RDP )); then
    if (( RDP_RUNNING )); then note "RDP  : already running (port $RDP_PORT) - $( (( FORCE )) && echo 'will be reconfigured (--force)' || echo 'left untouched')"
    else note "RDP  : not running - will be set up"; fi
  fi
  if (( ENABLE_VNC )); then
    if (( VNC_RUNNING )); then
      what="already running"; is_active "$VNC_UNIT" || what="port $VNC_PORT is in use by another program"
      note "VNC  : $what - $( (( FORCE )) && echo 'will be reconfigured (--force)' || echo 'left untouched')"
    else note "VNC  : not running - will be set up (display :$VNC_DISPLAY, port $VNC_PORT)"; fi
  fi
}

if (( ! DO_RDP && ! DO_VNC )); then
  report
  log "Nothing to do - the services are already running. Use --force to reconfigure them."
  exit 0
fi
if (( CHECK_ONLY )); then
  report
  log "Check only: nothing was changed."
  exit 0
fi

# Passwords arrive on standard input (never on a command line), after the
# line sudo may have read for itself.
read_secrets() {
  local line i
  for ((i = 0; i < 20; i++)); do
    IFS= read -r -t 30 line || break
    case $line in
      "TWACCOUNT "*) ACCOUNT_PW=${line#TWACCOUNT } ;;
      "TWVNC "*)     VNC_PW=${line#TWVNC } ;;
      TWEND)         return 0 ;;
    esac
  done
  die "--secrets-stdin: the passwords did not arrive."
}
(( SECRETS_STDIN )) && read_secrets

# VNC password: settle it before anything is installed
VNC_HOME=$(home_of "$VNC_USER")
VNC_PASSWD_FILE=$VNC_HOME/.vnc/passwd
if (( DO_VNC )); then
  if [[ -n $VNC_PW ]] || (( PW_STDIN )); then
    if [[ -z $VNC_PW ]]; then
      IFS= read -r VNC_PW || [[ -n $VNC_PW ]] || die "--vnc-password-stdin: no password on standard input."
    fi
    (( ${#VNC_PW} >= 6 )) || die "The VNC password must have at least 6 characters."
    (( ${#VNC_PW} <= 8 )) || warn "VNC passwords are limited to 8 characters; only the first 8 are used."
  elif [[ ! -s $VNC_PASSWD_FILE ]] && ! [[ -t 0 && -t 1 ]]; then
    die "No VNC password for $VNC_USER and no terminal to ask. Use --vnc-password-stdin, --secrets-stdin, or run: sudo -u $VNC_USER vncpasswd"
  fi
fi

exec 9>/run/lock/setup-remote-desktop.lock
flock -n 9 || die "Another run of this script is in progress."

# ---------------------------------------------------------------- undo
# Everything this run creates or replaces is written to a journal, one line
# "kind|service|a|b" each. rollback undoes the lines of one service, newest
# first. Only what THIS run made is touched: users that already existed, and
# files that were already as wanted, are not in the journal.
JSVC=shared
journal() { local IFS='|'; printf '%s\n' "$*" >>"$JOURNAL"; }

remove_user() { # a user this run created
  local u=$1 uid
  [[ $u != root ]] || return 0
  uid=$(id -u "$u" 2>/dev/null) || return 0
  (( uid >= 1000 )) || return 0
  loginctl terminate-user "$u" >/dev/null 2>&1 || true
  pkill -KILL -u "$u" >/dev/null 2>&1 || true
  sleep 1
  userdel -r -f "$u" >/dev/null 2>&1 || warn "Could not remove the user $u."
  return 0
}

rollback() { # rdp | vnc | shared
  local svc=$1 kind s a b
  local -a units
  [[ -n $JOURNAL && -s $JOURNAL ]] || return 0
  while IFS='|' read -r -u 8 kind s a b; do
    [[ $s == "$svc" ]] || continue
    case $kind in
      units)  read -ra units <<<"$a"; systemctl disable --now "${units[@]}" </dev/null >/dev/null 2>&1 || true ;;
      file)   rm -f -- "$a" || warn "Could not remove $a." ;;
      backup) if [[ -e $b ]]; then mv -f -- "$b" "$a" || warn "Could not restore $a."; fi ;;
      tree)   if [[ $a == /*/.vnc ]]; then rm -rf -- "$a" || warn "Could not remove $a."; fi ;;
      ufw)    ufw delete allow from "$a" to any port "$b" proto tcp </dev/null >/dev/null 2>&1 || warn "Could not remove the firewall rule for port $b." ;;
      user)   remove_user "$a" ;;
    esac
  done 8< <(tac "$JOURNAL")
  # what was undone is forgotten, so undoing twice does nothing
  grep -v -E "^[^|]+\|$svc\|" "$JOURNAL" >"$JOURNAL.new" || true
  mv -f "$JOURNAL.new" "$JOURNAL"
  return 0
}

rollback_service() { # rdp | vnc
  local svc=$1
  set +e
  log "Removing what this run set up for ${svc^^}"
  printf 'TWREMOVED %s\n' "$svc"
  rollback "$svc"
  systemctl daemon-reload >/dev/null 2>&1
  # a service that ran before (--force) goes back to its old settings
  if [[ $svc == rdp ]] && (( RDP_RUNNING )); then systemctl restart xrdp-sesman xrdp >/dev/null 2>&1; fi
  if [[ $svc == vnc ]] && (( VNC_RUNNING )); then systemctl restart "$VNC_UNIT" >/dev/null 2>&1; fi
  set -e
  return 0
}

# the session launcher is shared: it goes only when no service of this run is left
rollback_shared_if_unused() {
  [[ $STATE_RDP == ok || $STATE_VNC == ok ]] && return 0
  rollback shared
  return 0
}

# ------------------------------------------------------------------- users
create_user() { # name service
  local u=$1
  if id "$u" >/dev/null 2>&1; then note "User $u exists - not changed"; return 0; fi
  [[ -n $ACCOUNT_PW ]] || die "No password was given for the new user $u."
  log "Creating user $u"
  useradd -m -s /bin/bash "$u" || die "Could not create user $u."
  journal user "$2" "$u"
  printf '%s:%s\n' "$u" "$ACCOUNT_PW" | chpasswd || die "Could not set the password of $u."
}

# Put a file in place; return 0 when it changed (old copy kept as .backup.*).
install_file() {
  local src=$1 dst=$2 mode=$3 owner=$4 group=$5 changed=1 bak
  if [[ -e $dst ]] && cmp -s "$src" "$dst"; then
    changed=0
  elif [[ -e $dst ]]; then
    bak="$dst.backup.$(date +%Y%m%d%H%M%S)"
    cp -a "$dst" "$bak"
    journal backup "$JSVC" "$dst" "$bak"
  else
    journal file "$JSVC" "$dst"
  fi
  (( changed )) && install -D -m "$mode" -o "$owner" -g "$group" "$src" "$dst"
  chmod "$mode" "$dst"
  chown "$owner:$group" "$dst"
  (( changed ))
}

# Polkit 0.106 and later (Ubuntu 24.04, Debian 12) reads JavaScript rules from
# /etc/polkit-1/rules.d; the 0.105 of Ubuntu 22.04 only knows .pkla files.
polkit_uses_rules() {
  local v
  v=$(pkaction --version 2>/dev/null | awk '{print $NF}')
  [[ -n $v ]] || return 0
  if [[ $v == 0.* ]]; then (( ${v#0.} >= 106 )); else return 0; fi
}

# Remote XFCE sessions otherwise keep asking for a password for colour
# profiles and package-source refreshes. Only for the desktop users.
install_polkit() { # user
  if ! polkit_uses_rules; then
    cat > "$TMP_DIR/polkit-$1" <<EOF
[Termwright remote desktop: no authentication pop-ups in the remote session]
Identity=unix-user:$1
Action=org.freedesktop.color-manager.*;org.freedesktop.packagekit.system-sources-refresh
ResultAny=yes
ResultInactive=yes
ResultActive=yes
EOF
    install_file "$TMP_DIR/polkit-$1" "/etc/polkit-1/localauthority/50-local.d/45-remote-desktop-$1.pkla" 0644 root root || true
    return 0
  fi
  cat > "$TMP_DIR/polkit-$1" <<EOF
// Termwright remote desktop: no authentication pop-ups in the remote session.
polkit.addRule(function(action, subject) {
    if (subject.user == "$1" &&
        (action.id.indexOf("org.freedesktop.color-manager.") == 0 ||
         action.id == "org.freedesktop.packagekit.system-sources-refresh")) {
        return polkit.Result.YES;
    }
});
EOF
  install_file "$TMP_DIR/polkit-$1" "/etc/polkit-1/rules.d/45-remote-desktop-$1.rules" 0644 root root || true
}

open_firewall() { # port network(0|1)
  local port=$1 net=$2 out
  (( ENABLE_UFW )) || return 0
  [[ -n $ALLOWED_CIDR ]] && (( net )) || return 0
  command -v ufw >/dev/null 2>&1 || "${APT[@]}" install ufw
  log "Firewall (UFW)"
  out=$(ufw allow from "$ALLOWED_CIDR" to any port "$port" proto tcp)
  printf '%s\n' "$out"
  grep -q 'Skipping' <<<"$out" || journal ufw "$JSVC" "$ALLOWED_CIDR" "$port"
  if ufw status | head -1 | grep -qi inactive; then
    warn "UFW is inactive: the rules are saved but not enforced. Enable it yourself (sudo ufw enable) after allowing SSH."
  fi
  return 0
}

# --------------------------------------------------------------- services
# Each service is set up in a subshell (a failure there stops that service
# only), checked, and removed again when it does not work.
setup_rdp() {
  JSVC=rdp
  if (( ! RDP_RUNNING )); then journal units rdp "xrdp xrdp-sesman"; fi
  if (( CREATE_USERS )); then create_user "$RDP_USER" rdp; fi
  RDP_HOME=$(home_of "$RDP_USER"); RDP_GROUP=$(group_of "$RDP_USER")
  [[ -d $RDP_HOME ]] || die "Home folder '$RDP_HOME' of $RDP_USER does not exist."
  log "Installing packages for RDP"
  "${APT[@]}" install xrdp xorgxrdp
  install_polkit "$RDP_USER"

  log "Configuring xrdp"
  printf '#!/bin/sh\nexec /usr/local/bin/start-remote-xfce\n' > "$TMP_DIR/xsession"
  install_file "$TMP_DIR/xsession" "$RDP_HOME/.xsession" 0755 "$RDP_USER" "$RDP_GROUP" || true

  adduser xrdp ssl-cert >/dev/null 2>&1 || true

  local want bak
  want="port=3389"
  (( RDP_NETWORK )) || want="port=tcp://127.0.0.1:3389"
  if ! grep -qxF "$want" /etc/xrdp/xrdp.ini; then
    bak="/etc/xrdp/xrdp.ini.backup.$(date +%Y%m%d%H%M%S)"
    cp -a /etc/xrdp/xrdp.ini "$bak"
    journal backup rdp /etc/xrdp/xrdp.ini "$bak"
    sed -i -E "0,/^port=(3389|tcp:\/\/[^[:space:]]*:3389)\$/s//${want//\//\\/}/" /etc/xrdp/xrdp.ini
    grep -qxF "$want" /etc/xrdp/xrdp.ini || die "Could not set '$want' in /etc/xrdp/xrdp.ini."
  fi
  systemctl enable xrdp xrdp-sesman >/dev/null 2>&1
  log "Starting xrdp"
  systemctl restart xrdp-sesman xrdp
  open_firewall "$RDP_PORT" "$RDP_NETWORK"
}

setup_vnc() {
  JSVC=vnc
  if (( ! VNC_RUNNING )); then journal units vnc "$VNC_UNIT"; fi
  if (( CREATE_USERS )); then create_user "$VNC_USER" vnc; fi
  VNC_HOME=$(home_of "$VNC_USER"); VNC_GROUP=$(group_of "$VNC_USER")
  [[ -d $VNC_HOME ]] || die "Home folder '$VNC_HOME' of $VNC_USER does not exist."
  log "Installing packages for VNC"
  "${APT[@]}" install tigervnc-standalone-server tigervnc-common
  install_polkit "$VNC_USER"

  log "Configuring TigerVNC display :$VNC_DISPLAY (TCP $VNC_PORT)"
  [[ -d $VNC_HOME/.vnc ]] || journal tree vnc "$VNC_HOME/.vnc"
  install -d -m 0700 -o "$VNC_USER" -g "$VNC_GROUP" "$VNC_HOME/.vnc"
  printf '#!/bin/sh\nexec /usr/local/bin/start-remote-xfce\n' > "$TMP_DIR/xstartup"
  install_file "$TMP_DIR/xstartup" "$VNC_HOME/.vnc/xstartup" 0755 "$VNC_USER" "$VNC_GROUP" || true

  local bak
  if [[ -n $VNC_PW ]]; then   # from --vnc-password-stdin or --secrets-stdin
    printf '%s\n' "$VNC_PW" | runuser -u "$VNC_USER" -- vncpasswd -f > "$TMP_DIR/vncpw"
    [[ -s $TMP_DIR/vncpw ]] || die "vncpasswd produced no password file."
    if [[ -e $VNC_PASSWD_FILE ]]; then
      bak="$VNC_PASSWD_FILE.backup.$(date +%Y%m%d%H%M%S)"
      cp -a "$VNC_PASSWD_FILE" "$bak"
      journal backup vnc "$VNC_PASSWD_FILE" "$bak"
    else
      journal file vnc "$VNC_PASSWD_FILE"
    fi
    install -D -m 0600 -o "$VNC_USER" -g "$VNC_GROUP" "$TMP_DIR/vncpw" "$VNC_PASSWD_FILE"
  elif [[ ! -s $VNC_PASSWD_FILE ]]; then
    log "Set a VNC password for $VNC_USER"
    journal file vnc "$VNC_PASSWD_FILE"
    runuser -u "$VNC_USER" -- vncpasswd
  fi
  chmod 0600 "$VNC_PASSWD_FILE"
  chown "$VNC_USER:$VNC_GROUP" "$VNC_PASSWD_FILE"

  local localhost_arg=yes
  (( VNC_NETWORK )) && localhost_arg=no
  cat > "$TMP_DIR/vnc-unit" <<EOF
[Unit]
Description=TigerVNC server for $VNC_USER on display :%i
After=network-online.target
Wants=network-online.target

[Service]
Type=forking
User=$VNC_USER
Group=$VNC_GROUP
WorkingDirectory=$VNC_HOME
Environment=HOME=$VNC_HOME
TimeoutStartSec=60
ExecStartPre=-/usr/bin/vncserver -kill :%i
ExecStart=/usr/bin/vncserver :%i -geometry $GEOMETRY -depth $DEPTH -localhost $localhost_arg -SecurityTypes VncAuth
ExecStop=-/usr/bin/vncserver -kill :%i
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF
  install_file "$TMP_DIR/vnc-unit" /etc/systemd/system/tigervnc@.service 0644 root root || true
  systemctl daemon-reload
  systemctl enable "$VNC_UNIT" >/dev/null 2>&1
  log "Starting $VNC_UNIT"
  systemctl restart "$VNC_UNIT"
  open_firewall "$VNC_PORT" "$VNC_NETWORK"
}

wait_for() { local n=$1 i; shift; for ((i = 0; i < n; i++)); do "$@" && return 0; sleep 1; done; return 1; }
check_port() { # name port network(0|1) unit...
  local name=$1 port=$2 net=$3; shift 3
  local u
  for u in "$@"; do
    wait_for 20 is_active "$u" || {
      echo "$name: $u is not active" >&2
      systemctl --no-pager --full status "$u" 2>&1 | tail -15 >&2 || true
      note_err "$name: $u did not start"; return 1; }
  done
  wait_for 20 listening "$port" || { echo "$name: TCP $port is not listening" >&2; note_err "$name: TCP $port is not listening"; return 1; }
  if (( ! net )) && exposed "$port"; then
    echo "$name: TCP $port is reachable from the network, expected localhost only" >&2
    note_err "$name: TCP $port is reachable from the network, expected localhost only"; return 1
  fi
  note "$name: OK (port $port, $( (( net )) && echo 'all interfaces' || echo 'localhost only'))"
}
login_warn() { # user
  if local_login "$1"; then
    warn "$1 is logged in at this computer's own screen. A remote desktop for the same user can show a black screen: log out locally first, or use another user."
  fi
  return 0
}
validate_rdp() { log "Checking RDP"; check_port RDP "$RDP_PORT" "$RDP_NETWORK" xrdp xrdp-sesman || return 1; login_warn "$RDP_USER"; }
validate_vnc() { log "Checking VNC"; check_port VNC "$VNC_PORT" "$VNC_NETWORK" "$VNC_UNIT" || return 1; login_warn "$VNC_USER"; }

FAILED_NOTES=()
# shellcheck disable=SC2034  # st is a nameref: it sets STATE_RDP / STATE_VNC
run_service() { # rdp | vnc
  local svc=$1 rc=0 reason
  local -n st=STATE_${svc^^}
  : >"$LASTERR"; : >"$LASTSTEP"
  set +e
  ( set -e; "setup_$svc" )
  rc=$?
  if (( rc == 0 )); then "validate_$svc"; rc=$?; fi
  set -e
  if (( rc == 0 )); then st=ok; return 0; fi
  reason=$(cat "$LASTERR" 2>/dev/null || true)
  [[ -n $reason ]] || reason="$(cat "$LASTSTEP" 2>/dev/null || true) failed"
  [[ $reason == "${svc^^}:"* ]] || reason="${svc^^}: $reason"
  FAILED_NOTES+=("$reason")
  rollback_service "$svc"
  st=removed
  return 0
}

# ------------------------------------------------------------------- run
TMP_DIR=$(mktemp -d)
JOURNAL=$TMP_DIR/journal; LASTERR=$TMP_DIR/lasterr; LASTSTEP=$TMP_DIR/laststep
: >"$JOURNAL"

# ----------------------------------------------------------------- packages
log "Installing packages"
export DEBIAN_FRONTEND=noninteractive
APT=(apt-get -o DPkg::Lock::Timeout=120 -y)
"${APT[@]}" update || warn "apt-get update failed; trying with the package lists already here."
packages=(xfce4 dbus-x11)
(( GOODIES )) && packages+=(xfce4-goodies)
"${APT[@]}" install "${packages[@]}"

# ------------------------------------------------------- desktop launcher
log "Creating the XFCE session launcher"
cat > "$TMP_DIR/start-remote-xfce" <<'EOF'
#!/bin/sh
# Starts XFCE on X11, cleaned of any Wayland / GNOME values of a local login.
unset WAYLAND_DISPLAY GNOME_SETUP_DISPLAY SESSION_MANAGER DBUS_SESSION_BUS_ADDRESS XDG_SESSION_ID
export XDG_SESSION_TYPE=x11
export XDG_CURRENT_DESKTOP=XFCE
export XDG_SESSION_DESKTOP=xfce
export DESKTOP_SESSION=xfce
export XDG_CONFIG_DIRS=/etc/xdg/xdg-xfce:/etc/xdg

# Not "systemctl --user import-environment": the user manager is ONE per user,
# shared by the RDP and the VNC desktop, so one session would hand its DISPLAY
# to the other (a black screen). Each session has its own D-Bus session below.

# No window compositing unless the user chose it: with it the remote XFCE
# often shows a black screen over RDP (first start only; later changes stay).
xf=${XDG_CONFIG_HOME:-$HOME/.config}/xfce4/xfconf/xfce-perchannel-xml/xfwm4.xml
if [ ! -e "$xf" ]; then
    mkdir -p "${xf%/*}" 2>/dev/null &&
    printf '%s\n' '<?xml version="1.0" encoding="UTF-8"?>' \
        '<channel name="xfwm4" version="1.0"><property name="general" type="empty"><property name="use_compositing" type="bool" value="false"/></property></channel>' \
        > "$xf" 2>/dev/null || true
fi

exec env -u WAYLAND_DISPLAY -u GNOME_SETUP_DISPLAY dbus-run-session -- startxfce4
EOF
JSVC=shared
install_file "$TMP_DIR/start-remote-xfce" /usr/local/bin/start-remote-xfce 0755 root root || true

if (( DO_RDP )); then run_service rdp; fi
if (( DO_VNC )); then run_service vnc; fi
ACCOUNT_PW=""; VNC_PW=""

if (( ENABLE_UFW && (RDP_NETWORK || VNC_NETWORK) )) && [[ -z $ALLOWED_CIDR ]]; then
  note "Network access is on but no --allow-cidr was given: the firewall was not changed."
fi

# ----------------------------------------------------------------- result
tried=$(( DO_RDP + DO_VNC ))
failed=${#FAILED_NOTES[@]}
if (( failed >= tried )); then
  rollback_shared_if_unused
  die "Setup failed, and what this run had set up was removed again. ${FAILED_NOTES[*]}"
fi

if (( DO_RDP && DO_VNC )) && [[ $RDP_USER == "$VNC_USER" ]]; then
  warn "RDP and VNC share one user ($RDP_USER); using both at once can overwrite each other's XFCE settings. Use one user per service."
fi
report_done() {
  printf '\nSummary - %s (%s)\n' "$(hostname)" "$(hostname -I 2>/dev/null | xargs || true)"
  if [[ $STATE_RDP == ok ]]; then note "RDP: 127.0.0.1:$RDP_PORT  user $RDP_USER (Xorg session)"; fi
  if [[ $STATE_VNC == ok ]]; then note "VNC: 127.0.0.1:$VNC_PORT  user $VNC_USER (display :$VNC_DISPLAY), $GEOMETRY"; fi
  if (( ! RDP_NETWORK && ! VNC_NETWORK )); then
    note "Reach them through SSH: ssh -L 3389:127.0.0.1:$RDP_PORT -L $VNC_PORT:127.0.0.1:$VNC_PORT <user>@<this host>, or in Termwright use this SSH session as the jump host with address 127.0.0.1."
  fi
  note "Logs: journalctl -u xrdp -u xrdp-sesman -u $VNC_UNIT ; ~/.xsession-errors ; ~/.vnc/*.log"
}
report_done
if (( failed > 0 )); then
  for n in "${FAILED_NOTES[@]}"; do warn "$n - it was removed again; the other service was kept."; done
  log "Done, but not everything: see the warnings above"
  exit 3
fi
log "Done"
