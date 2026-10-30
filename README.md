<p align="center"><img src="docs/assets/termwright-icon-256.png" width="112" alt="Termwright" /></p>

<h1 align="center">Termwright</h1>

<p align="center">
  <b>A session manager and terminal for network and systems engineers.</b><br/>
  SSH2 · Telnet · Serial · SFTP · RDP · VNC, on Windows, macOS and Linux.
</p>

<p align="center">
  <a href="https://github.com/nperiannan/termwright/releases"><img alt="Release" src="https://img.shields.io/github/v/release/nperiannan/termwright?include_prereleases&sort=semver&label=release"></a>
  <a href="LICENSE"><img alt="License: Apache 2.0" src="https://img.shields.io/badge/license-Apache%202.0-blue"></a>
  <img alt="Platforms" src="https://img.shields.io/badge/platforms-Windows%20%7C%20macOS%20%7C%20Linux-555">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.24%2B-00ADD8">
</p>

<p align="center">
  <a href="docs/USER_GUIDE.md">User Guide</a> ·
  <a href="docs/FEATURES.md">Feature List</a> ·
  <a href="docs/SCRIPTING.md">Scripting</a> ·
  <a href="docs/SESSION_FILES.md">Session Files</a> ·
  <a href="CHANGELOG.md">Changelog</a> ·
  <a href="docs/ROADMAP.md">Roadmap</a>
</p>

![Termwright main window](docs/screenshots/main.png)

## Overview

Termwright is a free, open-source desktop application for people who spend their day on the command line of switches, routers, firewalls, servers and cloud hosts. It keeps hundreds of sessions organised, logs in the way each kind of device expects, moves files to and from them, and automates the repetitive parts of change windows: backups, checklists, collecting the same values from many devices.

It is a native desktop program, not a web service. It is written in Go, with an Angular and xterm.js interface, and ships as a single binary for each platform. Sessions are plain-text files that you own. Passwords are kept in the operating system's keychain, never in session files. Nothing is sent anywhere except to the hosts you connect to.

Existing sessions can be imported from SecureCRT, PuTTY, KiTTY, SuperPuTTY, MobaXterm and OpenSSH, or from CSV, Ansible, NetBox and AWS inventories. They can be exported back to those formats at any time.

## Highlights

- **Connections**: SSH2, Telnet, serial consoles, raw TCP and local shells in tabs or tiles; RDP and VNC alongside them.
- **Authentication**: passwords, keys (including PuTTY `.ppk`), certificates, FIDO2 security keys, smart cards, Kerberos and password managers. **Accounts** let many sessions, folders and device types share one login, so a password is changed in one place.
- **Device types**: one login profile per platform (EXOS, VOSS, Cisco IOS, Linux, or your own). It holds the login steps, after-login commands and credentials, so a new device needs only an address.
- **Device facts**: model, version, serial number, MAC address and uptime are read in a background login and shown wherever the session appears.
- **Session Manager**: folders with defaults, favourites, recents, environments (production is marked and guarded), reachability, bulk edit, a session list in the title bar and network discovery.
- **Files**:
  - a dual-pane SFTP browser with sync, compare, resume, bookmarks, server-to-server copy, SCP and *Run as root*;
  - a **Files panel** beside the terminal that follows the shell's working directory;
  - ZMODEM, YMODEM and XMODEM for consoles and boot loaders.
- **Automation**:
  - expect/send logon actions and output triggers;
  - Python, VBScript and JavaScript scripts;
  - a command window that sends to many tabs, a command library with variables, and searchable command history;
  - configuration backups with change detection, change checklists, and Run and Collect.
- **Networks in between**: jump hosts (chainable), SOCKS5 and HTTP proxies, port forwarding, and SFTP to a host behind a bastion through the bastion's own key. Web interfaces of devices behind a jump host open in a browser window whose traffic goes through the SSH connection.

## What's new in 0.23.0

- **Set Up Remote Desktop.** Right-click an SSH session on an Ubuntu 22.04, Ubuntu 24.04 or Debian 12 machine to install the XFCE desktop with RDP (xrdp) and/or VNC (TigerVNC) in the background over the SSH connection. Each service gets a user of its own, a failed service is removed again, and the new RDP and VNC sessions are created next to the SSH one with their user names and passwords stored.
- **Old SSH devices** ([#4](https://github.com/nperiannan/termwright/issues/4)). *Allow legacy algorithms* in Session Options → Authentication connects to devices that only offer `diffie-hellman-group1-sha1`, `aes128-cbc` or `hmac-sha1`; key exchange, cipher, MAC and host key lists can also be set by hand in the OpenSSH syntax.
- **mRemoteNG import** ([#3](https://github.com/nperiannan/termwright/issues/3)). Import `confCons.xml` with folders, inherited user names, ports and protocols (SSH, Telnet, RAW, RDP, VNC); saved passwords go to the OS keychain.
- **Smaller changes.** A Sessions button in the title bar, a Files panel that follows `sudo su -`, and a macOS title bar that lines up with the window buttons.

## What's new in 0.22.0

- **Accounts** ([#1](https://github.com/nperiannan/termwright/issues/1)). A username and password kept once (in the OS keychain or a password manager) and chosen in a session, a folder's defaults or a device type. Changing an account's password updates every session that uses it; *Convert Sessions…* turns sessions that share a login into one account.
- **Settings travel with the export.** A zip or folder export carries device types, accounts (without passwords), Folder Defaults, the command library, checklists, jobs and the Global Options. Import Sessions shows what is in the file and imports the parts you choose, following the target folder.
- **Exports keep everything new.** Accounts, the Files panel, *SFTP opens on* and Wake-on-LAN sessions survive Termwright's own exports; the CSV gains account, network and Files panel columns and reads RDP, VNC and Wake-on-LAN sessions back with their protocol.

## What's new in 0.21.0

- **Modern interface, now the default.** Tabs and a session list share the title bar. A start page shows favourite and recent sessions, New Session and Properties open in a side panel, and sessions have device icons and colour-coded folders. Every dialog has been redesigned. The classic layout (menu bar, toolbar and tab strip) remains one click away.
- **Device facts.** Read after connecting without typing in your tab, and shown on the tab, status bar, cards, hover card and side panel. To teach a new fact, select its value in the output; facts can be edited, removed or added by hand in Session Options.
- **Files panel.** A file browser beside the SSH terminal, opposite the Session Manager:
  - it follows the terminal's folder;
  - opening a folder in the panel `cd`s the terminal there;
  - a double-clicked file opens in its usual program on your computer, and saved changes are offered for upload.
- **Wake-on-LAN.** Wake-on-LAN sessions with their own icon: the magic packet goes to the device's network only. If its IP address is unknown, the network is searched for its MAC address. A green or orange dot shows from a regular ping whether it is up. Any session with a MAC address can be woken too, from its jump host when it sits behind one.
- **Web pages through a session.** Open a device's web interface (firewall, IPMI, NAS, switch) behind a jump host, or any page as seen from an SSH server. A separate browser window with a temporary profile sends all its traffic and name lookups through the SSH connection, and the profile is deleted when the window closes.
- **SFTP.**
  - Clickable paths and a bookmark bar.
  - Multi-select with check boxes and the keyboard.
  - New File and Delete on both sides (local files go to the Trash).
  - Copy, Sync and Diff between the panes; Compare reports when two files are identical.
  - Transfer speed is shown.
  - Through a jump station, SFTP opens on the host the terminal is on.
- **Security.** An optional master password, set during first-run setup with a strength meter (four characters minimum), and an unlock window at start-up.
- **Everyday details.**
  - Keyword highlighting on by default.
  - Command History with per-command removal and clearing by device type.
  - Save Screen As Image.
  - Keyboard shortcuts for nearly every menu command.
  - Timestamps that start on the first line.

## Capabilities

| Area | Summary |
|---|---|
| Protocols | SSH2; Telnet (NAWS, TTYPE, ECHO, SGA); serial (flow control, DTR/RTS, break, speed detection); raw TCP; local shell (PTY on macOS and Linux, ConPTY on Windows); RDP through the system's client; VNC in a built-in viewer; Wake-on-LAN sessions. |
| SSH authentication | Password and keyboard-interactive. Public keys: OpenSSH, encrypted, and PuTTY `.ppk`. SSH certificates, FIDO2 security keys, PIV / PKCS#11 smart cards, Kerberos (GSSAPI), the SSH agent. `known_hosts` verification with prompts for new and changed keys. |
| Key management | Generate Ed25519, RSA 4096 or ECDSA keys. Install them on Linux, macOS, BSD and Windows OpenSSH servers, test the key-only login, then switch the session to it. |
| Device types | Per-platform login profiles: login steps for Telnet and serial, after-login steps for every protocol (for example `enable`, paging off), and credentials inherited or overridden per session. |
| Device facts | Model, version, serial number, MAC address, uptime and custom values, read in a background login. Built-in rules for VOSS, EXOS, Cisco IOS and Linux. |
| Session management | Wake-on-LAN (also through a jump host); folders with defaults; favourites and recents; environments, with confirmation before multi-tab sends to production; reachability checks; bulk edit; workspaces; Quick Open; drag and drop. |
| Discovery | Scans a network or range (up to /22): open ports, SSH and Telnet banners, web servers and certificates, SIP, SNMP v2c, DNS, MAC vendor. Each result shows its evidence; selected devices are added as sessions. Discovery never logs in. |
| Import and export | Sessions are plain-text `.ini` files, and unknown keys are preserved byte for byte. Import from SecureCRT (Config folder, zip, XML), PuTTY / KiTTY, SuperPuTTY, MobaXterm, mRemoteNG, OpenSSH config, CSV, Ansible, NetBox and AWS EC2. Export to the same formats plus Tabby. |
| Terminal | Tabs and tiled layouts; xterm-256color and true colour; Unicode; scrollback search; keyword highlighting; timestamps; auto-scroll control; clickable links; session lock; zoom; per-session fonts and colour schemes. |
| File transfer | SFTP browser with folder transfers, sync, resume, bookmarks, compare, server-to-server copy, SCP, sudo / su and jump-station targets. Files panel beside the terminal. SCP. ZMODEM (automatic `sz` / `rz`), YMODEM batch, XMODEM. |
| Automation | Expect/send logon actions with named credentials; output triggers; logon scripts; Python, VBScript (Windows) and JavaScript scripting through a `crt`-style object; script recorder; key map; button bar; output alerts. |
| Network operations | Command Window that sends to the active, selected or all tabs; command library with `{{variables}}`; command history; scheduled configuration backups kept only when changed; change checklists; Run and Collect to CSV; Compare. |
| Logging and recording | Raw or plain-text session logs with per-line timestamps and filename tokens; scrollback to text or HTML; session recordings with markers and playback. |
| Security | OS keychain for passwords and passphrases; 1Password, Bitwarden, HashiCorp Vault and command-based password managers; master password with unlock at start-up; encrypted password export (AES-256-GCM, scrypt). |
| Networking | Web pages and device web interfaces through a session (browser window with a temporary profile); local, remote and dynamic (SOCKS5) port forwarding; chainable jump hosts; SOCKS5 and HTTP CONNECT proxies; X11 and agent forwarding; automatic reconnect. |
| Updates | Stable and nightly channels from GitHub Releases, with the installer downloaded and opened from Help → Check for Updates. |

The complete list is in [docs/FEATURES.md](docs/FEATURES.md).

## Screenshots

| | |
|---|---|
| ![Start page](docs/screenshots/home.png) <br/>*Start page: search or connect, favourites and recent sessions* | ![Session side panel](docs/screenshots/session-panel.png) <br/>*Session properties beside the terminal, with device facts* |
| ![Files panel](docs/screenshots/files-panel.png) <br/>*Files panel following the terminal's folder* | ![SFTP](docs/screenshots/sftp.png) <br/>*SFTP browser: bookmarks, multi-select, Copy / Sync / Diff* |
| ![Compare](docs/screenshots/sftp-compare.png) <br/>*Comparing two files, with the verdict at the top* | ![Device facts](docs/screenshots/device-facts.png) <br/>*Device facts: read, edit, teach* |
| ![Tiled sessions](docs/screenshots/grid.png) <br/>*Tiled sessions with keyword highlighting* | ![Session Manager](docs/screenshots/session-manager.png) <br/>*Session Manager with the hover card* |
| ![Quick Connect](docs/screenshots/quickconnect.png) <br/>*Quick Connect with a device type* | ![Settings](docs/screenshots/options.png) <br/>*Settings* |
| ![Discover Devices](docs/screenshots/discover.png) <br/>*Network discovery with evidence per device* | ![Configuration backups](docs/screenshots/backups.png) <br/>*Configuration backups* |
| ![Change checklist](docs/screenshots/checklist.png) <br/>*Change checklist: same or changed per device* | ![Run and Collect](docs/screenshots/collect.png) <br/>*Run and Collect into one table* |
| ![Open Web Interface](docs/screenshots/web-open.png) <br/>*A device's web interface through its jump station* | ![Command History](docs/screenshots/history.png) <br/>*Command History* |
| ![Light theme](docs/screenshots/light.png) <br/>*Light theme* | ![Files panel on the left](docs/screenshots/files-panel-right.png) <br/>*Files panel on the left when the Session Manager is on the right* |

<details>
<summary>More screenshots</summary>

| | |
|---|---|
| ![Unlock window](docs/screenshots/unlock.png) <br/>*Unlock window at start-up* | ![Welcome](docs/screenshots/welcome-security.png) <br/>*First-run setup with an optional master password* |
| ![Connection error](docs/screenshots/fix-bar.png) <br/>*A failed connection explained, with fixes* | ![Teach a device fact](docs/screenshots/fact-teach.png) <br/>*Teach a fact by selecting its value* |
| ![Logon actions](docs/screenshots/logon.png) <br/>*Logon actions and remote command* | ![Key login setup](docs/screenshots/keysetup.png) <br/>*Key login setup* |
| ![Import](docs/screenshots/import.png) <br/>*Import from other programs* | ![Export](docs/screenshots/export-formats.png) <br/>*Export to other programs* |
| ![Device types](docs/screenshots/devicetypes.png) <br/>*Device types* | ![Command library](docs/screenshots/commands.png) <br/>*Command library with variables* |
| ![Send to selected tabs](docs/screenshots/sendgroup.png) <br/>*Send to selected tabs* | ![Compare With](docs/screenshots/tab-compare.png) <br/>*Compare a tab with another tab* |
| ![SFTP on the target](docs/screenshots/sftp-hop.png) <br/>*SFTP on a host behind a jump station* | ![SFTP Run as](docs/screenshots/sftp-runas-menu.png) <br/>*SFTP as root or another user* |
| ![Authentication](docs/screenshots/auth.png) <br/>*Kerberos, certificates and security keys* | ![Password managers](docs/screenshots/passwords-managers.png) <br/>*Password managers* |
| ![Workspaces](docs/screenshots/workspaces.png) <br/>*Workspaces* | ![Alerts](docs/screenshots/alerts.png) <br/>*Output alerts* |
| ![Devices at new addresses](docs/screenshots/discover-moved.png) <br/>*Devices found at new addresses by MAC* | ![Another device at the address](docs/screenshots/mac-warning.png) <br/>*Stops before logging in to a different device* |
| ![Session playback](docs/screenshots/player.png) <br/>*Session playback with markers* | ![Smart cards](docs/screenshots/smartcards.png) <br/>*Smart card keys* |
| ![Console Server Ports](docs/screenshots/console-ports.png) <br/>*Console server ports* | ![Edit Sessions](docs/screenshots/bulk-edit.png) <br/>*Edit many sessions at once* |
| ![Serial console](docs/screenshots/serial-tab.png) <br/>*Serial console with send bar* | ![HTML report](docs/screenshots/scrollback-html.png) <br/>*Scrollback saved as HTML* |
| ![RDP tab](docs/screenshots/rdp.png) <br/>*RDP through a jump host* | ![VNC viewer](docs/screenshots/vnc.png) <br/>*VNC viewer* |

</details>

## Installation

Pre-built packages are published on the [Releases](https://github.com/nperiannan/termwright/releases) page. Tagged versions are stable; the *nightly* pre-release is rebuilt from every change to `main`.

| Platform | Package | Notes |
|---|---|---|
| macOS (Apple Silicon) | `.dmg`, `.zip` | Not notarized. After installing, run `xattr -dr com.apple.quarantine /Applications/Termwright.app` once. |
| Windows 10 / 11 (x64) | installer `.exe`, `.zip` | Requires the WebView2 runtime, which is included in current Windows versions. SmartScreen may warn about an unsigned file. |
| Debian / Ubuntu (amd64, arm64) | `.deb` | `sudo apt install ./termwright_*.deb` |
| Other Linux (amd64, arm64) | `.tar.gz` | Requires GTK 3 and WebKitGTK 4.1. |

Installation, first start and every feature are described in the [User Guide](docs/USER_GUIDE.md).

## Migrating existing sessions

**File → Import Sessions…** (or the first-run setup) lists the sessions it finds in the usual places and imports them, including folders, logon actions and colours where the source has them.

- **SecureCRT**: choose the Config folder:
  - Windows: `%APPDATA%\VanDyke\Config`
  - macOS: `~/Library/Application Support/VanDyke/SecureCRT/Config`
  - Linux: `~/.vandyke/SecureCRT/Config`

  An XML or zip export also works. Session files are copied unchanged, and saved passwords are asked for once and stored in the OS keychain.
- **PuTTY / KiTTY**: a registry export (`reg export HKCU\Software\SimonTatham\PuTTY\Sessions putty.reg`) or `~/.putty/sessions`. **SuperPuTTY**: `Sessions.xml`.
- **MobaXterm**: *User sessions → Export all sessions to file*.
- **mRemoteNG**: *File → Save Connection File As…* (`confCons.xml`); folders, inherited user names and saved passwords are kept.
- **OpenSSH**: `~/.ssh/config` is read directly.

**File → Export Sessions…** writes the same formats back, so you can always return to another program. The key-by-key mapping is documented in [docs/SESSION_FILES.md](docs/SESSION_FILES.md).

## Building from source

Requirements:
- Go 1.24 or later
- Node.js 24 LTS (22.22+ and 26+ also work)
- the Wails CLI v2.10.2

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.2
wails doctor          # verifies platform dependencies
```

Platform dependencies:
- **Windows**: WebView2 runtime.
- **macOS**: Xcode command-line tools (`xcode-select --install`).
- **Linux**: `libgtk-3-dev` and `libwebkit2gtk-4.1-dev`; build with `-tags webkit2_41`.

```bash
make build            # build/bin/termwright(.exe) or build/bin/Termwright.app
make dev              # development build with live reload
make test             # Go unit and integration tests, including an end-to-end SSH test
```

Without `make`:

```bash
GOTOOLCHAIN=go1.24.7 wails build -clean -skipbindings
```

On Linux, add `-tags webkit2_41`. `GOTOOLCHAIN` is pinned because the Wails 2.10 CLI cannot read packages built for newer Go releases.

Release builds for Windows (amd64), Linux (amd64, arm64) and macOS (arm64) are produced by GitHub Actions ([.github/workflows/build.yml](.github/workflows/build.yml)).

## Data and privacy

| Item | Location |
|---|---|
| Settings, sessions, logs, backups | `%APPDATA%\Termwright`, `~/Library/Application Support/Termwright` or `~/.config/Termwright` (override with `TERMWRIGHT_HOME`) |
| Passwords and key passphrases | The operating system's keychain (Keychain, Credential Manager, Secret Service) or your password manager |
| Command history | `history.json` in the data folder; commands that look like they contain a secret are never recorded |

Termwright has no telemetry. Apart from the hosts you connect to (and the reachability checks and background logins you enable for them), it contacts only GitHub: for the update check and the download count shown in About.

## Project structure

```
main.go, app.go, *_app.go   application entry point, native menu, methods bound to the UI
internal/session            session model, folder store, import and export
internal/sessionfile        lossless .ini / XML session file reader and writer
internal/term               SSH2, Telnet, serial, local PTY, SFTP/SCP, file transfer protocols, scripting
internal/config             data directory and global settings
internal/secrets            OS keychain integration
internal/secretref          password manager integration
internal/history            command history
frontend/                   Angular application (standalone components, signals, zoneless) with xterm.js
docs/                       user guide, feature list, scripting and session file references
```

## Contributing

Bug reports and feature requests are welcome in [GitHub Issues](https://github.com/nperiannan/termwright/issues). Please include:
- the version (Help → About) and operating system;
- the steps to reproduce the problem;
- for connection problems, the message shown in the error card.

Pull requests should keep `make test` passing, and should update the User Guide and the feature list for user-visible changes.

## License

Termwright is licensed under the [Apache License 2.0](LICENSE). Third-party notices are listed in [NOTICE](NOTICE).

---

<sub>SecureCRT® is a registered trademark of VanDyke Software, Inc. PuTTY, KiTTY, SuperPuTTY, MobaXterm, mRemoteNG and other product names are the property of their respective owners. Termwright is an independent open-source project, not affiliated with or endorsed by any of them; these names are used only to identify the file formats Termwright can import and export.</sub>
