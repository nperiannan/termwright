# Roadmap

## 0.1 (this release)
SSH2 / Telnet / Serial / Local / Raw, session manager, SecureCRT .ini/XML/zip import-export, ssh-config import, logging,
scrollback capture, command window (send to all), button bar, logon actions, keychain passwords, known_hosts.

## 0.2: daily-driver parity (done)
- [x] SSH port forwarding: local (-L), remote (-R), dynamic SOCKS5 (-D), per session
- [ ] Convert SecureCRT `Port Forward Table V2` (needs a real sample file; the table is preserved meanwhile)
- [x] Jump host (chainable, any saved SSH2 session, stored as `Firewall Name=Session:<path>`), SOCKS5 and HTTP CONNECT proxies
- [x] Session fonts, Backspace mode (^? / ^H); imported color-scheme names mapped to built-in schemes
- [ ] Full key mapping editor (moved to 0.3)
- [x] Keyword highlighting (named keyword sets, regex, colors; default "Network" set), triggers (pattern -> send) on any output
- [x] Tiled split views: side by side, stacked, grid. Detaching tabs into new windows needs multi-window support (Wails v3) and is deferred.
- [x] Session lock (one or all tabs, password), auto reconnect with back-off
- [x] X11 forwarding (XQuartz / VcXsrv / X server), SSH agent forwarding
- [x] Connect Bar (`ssh -p 2222 user@host`, `telnet host 23`, `serial COM3 115200`, `raw host port`, or a saved session name)

## 0.3: file transfer & automation (done)
- [x] SFTP browser tab (dual pane, reuses the SSH connection - no second login): upload/download files and folders, queue with progress and cancel, mkdir, rename, delete
- [x] XMODEM-CRC / XMODEM-1K upload (e.g. firmware to a boot loader over serial)
- [x] JavaScript scripting (term.send / waitFor / waitForAny / readUntil, openSession, prompt...), logon scripts, script recorder
- [x] Key map editor: key combination -> send text / app action / script
- [ ] SCP, ZMODEM, XMODEM/YMODEM receive (moved to 0.4)
- [ ] Folder synchronisation (moved to 0.4)

## 0.4: transfers (done)
- [x] ZMODEM: automatic download when the remote runs `sz` (saved to the Downloads folder), upload when it runs `rz` (file picker); verified against lrzsz
- [x] XMODEM-CRC receive (verified against lrzsz `sx`)
- [x] SCP upload/download of single files for devices without SFTP
- [x] SFTP folder sync (new/changed files, either direction, never deletes)
- [x] Button bar buttons that run an action or a script
- [x] Port Forward Table V2 import (local + dynamic; remote via Reverse Forward Table V2), pending confirmation with real files
- [ ] Multi-window (detach tab) once on Wails v3

## 0.5: polish (done)
- [x] Themes: follow-system UI theme, accent color, 12 built-in color schemes, custom scheme editor with import/export
- [x] Reopen last sessions on startup, Send Break, Send Text File, Save Selection, DTR/RTS options, description tooltips
- [x] Packaging: macOS DMG, Windows NSIS installer, Linux .deb (amd64/arm64)
- [x] User Guide (docs/USER_GUIDE.md), Help → User Guide

## 0.6: logon automation & security (done)
- [x] Logon Actions: Automate logon, Send initial CR, Expect/Send table (Hide, trailing CR, credentials), ▲▼ ordering
- [x] Send escapes `\s` `\w` `\v` `\p` `\e` `\b`; named Credentials; logon script arguments
- [x] SSH2 Remote command (e.g. jump station → `ssh -t -i key user@target`)
- [x] Hardware (RTS/CTS) and software (XON/XOFF) serial flow control
- [x] YMODEM batch send/receive, SCP of folders, resume of interrupted SFTP transfers
- [x] Master password, encrypted password vault export/import

## 0.7: SecureFX-style & updates (done)
- [x] Standalone SFTP sessions (own login, no shell) and "Connect as: SFTP" for one-session devices (VOSS)
- [x] SFTP bookmarks (per session for server folders, shared for local folders)
- [x] Server-to-server (site-to-site) copy between two SFTP tabs
- [x] Update check: stable / nightly channels from GitHub releases (token for private repos), installer download

## 0.8: keys & migration (done)
- [x] Key login setup: generate (Ed25519/RSA/ECDSA) or reuse a key, install on Linux/macOS/Windows OpenSSH, test, switch the session
- [x] Import PuTTY/KiTTY (.reg, ~/.putty/sessions) and MobaXterm (.mxtsessions) sessions
- [x] About: features, author, repository links

## 0.9: PuTTY keys & Python scripts (done)
- [x] Read PuTTY .ppk keys directly (formats 2 and 3, Argon2id/Argon2i, RSA/Ed25519/ECDSA/DSA)
- [x] Python scripts with the `crt` object (runs with the installed Python 3)
- [x] Terminal scheme *Auto* (follows the UI theme) and *use this scheme for all sessions*

## 0.10: daily network work (done)
- [x] Device types (login profiles) in Quick Connect, Connect Bar, sessions and folder defaults
- [x] Paste confirmation, send to selected tabs, Quick Open palette, output alerts
- [x] Command library with variables, workspaces, folder defaults
- [x] Bulk import: CSV, Ansible (INI/YAML), NetBox, AWS EC2
- [x] Compare outputs (diff of two tabs or files)
- [x] Per-line timestamps, copy as HTML, OSC 8 links, window size/position memory, portable mode


## 0.10.1 (done)

- [x] Device types have their own username and password (keychain), with *No password* for accounts such as EXOS `admin`
- [x] Sessions inherit the device type's login or override it (Session Options and Quick Connect)
- [x] Empty passwords are remembered instead of asked every time
- [x] SFTP tab can copy with SCP (*Copy via SCP*)
- [x] Compare With… on the tab menu, *Ignore prompts* option; import sources grouped by app

## 0.10.2 (done)

- [x] Import saved SecureCRT passwords (Password V2 02/03, Password, plain text) into the OS keychain
- [x] Key passphrases in Passwords & Credentials, remember from the connect prompt, included in the vault export
- [x] SFTP/SCP through the jump station to a remote command's target

## 0.10.3 / 0.10.4 (done)

- [x] SFTP header shows *user@host via jump host*
- [x] Browse as root in the SFTP tab (sudo sftp-server, password only when sudo asks)
- [x] 0.10.6: new app icon, red-on-hover tab close, clearer folder arrows, jump-station data-flow docs
- [x] 0.10.5: Run as any user with a command (sudo su - user, su - user, …), password prompts answered by the user

## 0.11 (done)

- [x] Kerberos / GSSAPI login (kinit ticket or built-in Kerberos login), tested against MIT Kerberos + OpenSSH
- [x] SSH certificates (`-cert.pub` / certificate file) and FIDO2 security keys through ssh-agent
- [x] Password managers: 1Password (`op://`), Bitwarden (`bw://`), HashiCorp Vault (`vault://`), commands (`cmd://`)
- [x] VBScript (.vbs) logon scripts via Windows Script Host
- [x] `crt.Screen.Get` / `CurrentRow` read the rendered terminal screen

## 0.11.1 (done)
- Session Manager on the left or right, hide/show, remembered in settings
- SFTP browser icons: gradient folders, files colored by type, link and hidden markers
- Update window: installed / latest stable with changes / newer nightly with backup step; nightly ordered by commit (compare API); repo and token under Advanced
- Windows file picker: "All files" filter and upper-case extensions

## 0.11.2 (done)
- Working directory detection (OSC 7 / 1337 CurrentDir / 633 Cwd): SFTP opens in the terminal's folder, Follow Terminal Folder, cd terminal to the SFTP folder, one-click bash/zsh setup (this shell or ~/.bashrc / ~/.zshrc)

## 0.11.3 (done)
- Working directory detection automatic and quiet when the SFTP tab opens (login shell probed over a separate channel, also through a jump station); Follow on by default; settings in General
- Hidden Session Manager leaves a SESSIONS strip; About: GitHub link with Copy Link

## 0.11.4 (done)
- Removed upgrade code for pre-release installs (old data folder, keychain service, session key prefix, update repository)
- Password vault format version 2

## 0.11.5 (done)
- Folder reporting also reports the shell user (OSC 1337 RemoteHost); SFTP tabs switch to root (sudo) or `sudo su - <user>` to match the terminal; folder reporting is re-sent on every SFTP open so a new `sudo su -` shell reports too; setting `sftpFollowUser`

## 0.12 (done)
- Discover Devices: address ranges up to a /22, TCP probes (SSH, Telnet, HTTP(S), SMB, RDP, printing, SIP), SSH/Telnet greeting, reverse DNS, SNMP v2c system group (own minimal client), device kind classification with icons, device type guess, add selected as sessions

## 0.12.1 (done)
- Discovery identification: web fingerprint (title, Server, realm, certificate, product names), Telnet negotiation for banners, SIP OPTIONS over UDP, ARP MAC + IEEE vendor list (download), this computer, two-pass rules (product names before generic words), NAS kind, evidence list (ⓘ), device type from the Telnet prompt, SNMP community Show/Hide and "no SNMP answer" note

## 0.12.2 – 0.12.6 (done)
- Discovery: NAS and mobile kinds, mDNS names, private MAC addresses, OUI download, network OS from any evidence (EXOS with OpenSSH), add only chosen device types, a subfolder per device type, new switch icon
- Device type passwords remembered at the logon prompt (empty = no password); text fields keep what is typed; CI caches and retries

## 0.13 (done)
- Edit many sessions at once, Favourites and Recent, hover card, environments (production confirmations), reachability dots
- Search in Global Options, errors that offer the fix, device-type-aware commands (library, Command Window, Quick Open)
- Discovery: connect without saving, IoT / embedded boards (ESP32, Arduino, STM32, RTOS) via mDNS services, CoAP, ESPHome, MQTT, Modbus
- 0.13.1: the session filter lists the exact match first

## 0.14 (done)
- [x] **Configuration backups**: back up the running configuration of chosen sessions or folders on demand or on a schedule (daily / weekdays), using each device type's backup command; keep N copies, save only when changed, see what changed (Compare), from the Session Manager too
- [x] **RDP**: Remote Desktop sessions opened in the computer's RDP client (Windows Remote Desktop, Windows App on macOS, FreeRDP on Linux), through jump hosts and proxies via a local tunnel
- [x] **VNC**: built-in VNC viewer tab (noVNC), through jump hosts and proxies; VNC and macOS Screen Sharing logins
- [x] Discovery: RDP and VNC devices can be added as sessions

## 0.15 (done)
- [x] **Change checklists**: run a set of show commands on chosen devices before a change and again after it, then compare each device automatically
- [x] **Command history search** across all tabs and sessions (filter by device type) and **autocomplete** from history and the Command Library while typing in the Command Window

## 0.16 (done)
- [x] **Run and collect**: run commands on many devices and get one report (file per device, and a table such as versions / serial numbers with CSV export)
- [x] **Console servers**: open the right port of a console server from a session, port sessions created in one go

## 0.17 (done)
- [x] **Session recording and playback**: asciinema v2 `.cast` per connection (output, timing, window size, markers), per session / folder / all sessions, player tab (speed, seek, skip idle, markers, text search, export text), Recordings list, keep N days
- [x] **Smart cards: PIV / CAC / PKCS#11 SSH keys**: Smart Cards options (libraries, test, copy public key), per session / folder, PIN in memory only, certificates, jump hosts, SFTP, agent forwarding; tested with SoftHSM2

## Real-world readiness (next, before more features)
Features alone do not make a tool people rely on every day. These come first:
- [ ] **Field testing**: features verified so far only with test servers or software tokens, on real equipment
  - RDP on Windows (Remote Desktop Connection)
  - smart cards with a real PIV / CAC card and a YubiKey, on macOS, Windows and Linux
  - the Windows build with smart card support (first release built with cgo)
  - console servers and Run and Collect on real devices
- [ ] **Beta users**: 5-10 network engineers using Termwright daily for a few weeks; fix what they report
- [ ] **Device test pass**: VOSS, EXOS, Cisco IOS / NX-OS, Junos, Linux and serial consoles - logins, prompts, paging, full-screen tools, long outputs, slow links
- [ ] **Terminal accuracy**: vttest and full-screen apps (vi, top, menus of devices) compared with other terminals
- [ ] **Code signing and notarization**: Windows (Authenticode) and macOS (Developer ID), so installers run without warnings and pass company policies
- [ ] **Packages**: winget, Homebrew cask, MSI for company deployment, Linux packages (deb / rpm / Flatpak)
- [ ] **Security review**: an outside look at password storage, secret handling, host key checks and the update path
- [ ] **Team sharing and sync** (from *Later*): shared session libraries for teams
- [ ] **Community**: issue templates, a short contributing guide, release announcements in network engineering communities

## 0.18 (done)
- [x] **Devices with a new address (DHCP)**: MAC address saved in sessions, new addresses found by Discover Devices and on connect, MAC check before logging in, SSH host key check, *Find by MAC Address*

## 0.19 (done)
- [x] **Export to other programs**: PuTTY / KiTTY, MobaXterm, Tabby, OpenSSH config, CSV, with a summary of what each format cannot hold

## 0.20 (done)
- [x] **Serial consoles**: port list in order with the adapter's name, updated when a cable is plugged in; Detect Speed (also on every connect); send bar with line ending, Timestamps, Auto scroll and Clear
- [x] **Clear Screen, Timestamps and Auto scroll** for every connection (toolbar, right-click, tab menu, Edit menu, per session)
- [x] Discover Devices: `192.168.1.0` without a mask scans its /24; shorter address field with Scan next to it
- [x] Faster typing in the Command Window with a large Session Manager tree

## 0.21 (planned)
- [ ] MAC addresses of devices behind routers: read the router's ARP table over SNMP
- [ ] Smart cards on Windows without a PKCS#11 library: keys of the Windows certificate store (CNG)
- [ ] Import the smart card settings of SecureCRT sessions (needs sample exports)
- [ ] Optional recording of typed input in session recordings (off by default)
- [ ] Export to Royal TS, Remote Desktop Manager and Termius (if users ask)

## Later
- [ ] **Team sharing and sync** (see Real-world readiness): shared session library (Git repository or shared folder), passwords stay per user or in a password manager
- [ ] Split panes inside a tab (nested splits, keyboard driven)
- [ ] Cloud inventory imports: Azure, Google Cloud
- [ ] Plugin / extension API
- [ ] Mosh for unreliable links; inline images (sixel, iTerm2) in the terminal
- [ ] Mobile apps (iOS, Android)
- [ ] *Test Login* button in sessions and device types; first-run welcome that finds other terminal programs' sessions; rescan shows new / gone / changed devices
- [ ] Discovery: SNMP v3, more ports in an advanced section, LLDP neighbours via SNMP
- [ ] Code signing + notarization (needs Apple Developer ID / Windows certificate)

## Release engineering
- [ ] Code signing: Windows Authenticode, macOS Developer ID + notarization
- [ ] Installers: NSIS/MSIX (Windows), DMG (macOS), AppImage/deb/rpm (Linux)
- [x] Auto-update channel (check + download installer; silent self-replace needs code signing)
- [x] Release notes from CHANGELOG.md + commit list for every release and nightly (`scripts/release-notes.sh`); before tagging, rename the *Unreleased* section to the version; *Actions → release notes* rewrites notes of published releases
- [ ] Round-trip compatibility test suite with real SecureCRT 9.x files
