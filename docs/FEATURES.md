# Termwright features

Everything Termwright does, one line each. Details and screenshots are in the [User Guide](USER_GUIDE.md); scripting is in [SCRIPTING.md](SCRIPTING.md). This list is for version 0.19.

## Platforms and installation

- **macOS, Windows and Linux**: one app for all three (Apple Silicon Mac; Windows 10/11 x64; Linux x64 and ARM64).
- **Installers**: DMG for macOS, setup.exe for Windows, .deb for Debian/Ubuntu, plus zip / tar.gz archives that run without installing.
- **Portable mode**: a `termwright-data` folder next to the program keeps all settings there (e.g. on a USB stick).
- **Own data folder**: settings, known hosts and logs in one folder (`TERMWRIGHT_HOME` can move it); passwords stay in the OS keychain.
- **Updates**: Help → Check for Updates shows the installed version, the latest stable release with its changes and newer nightly builds; downloads and opens the installer.
- **Update channels**: be told about stable releases, stable and nightly, or nothing; optional check at start-up.
- **Release notes**: every release and nightly build lists its changes (CHANGELOG) and commits.
- **Free and open source**: Apache-2.0.

## Main window

- **Menu bar**: File, Edit, View, Transfer, Script, Options, Tab, Help; on macOS also inside the window (can be turned off).
- **Toolbar**: connect, quick connect, local shell, reconnect, disconnect, copy, paste, find, log, SFTP, scripts, layouts, lock and more.
- **Connect Bar**: type `ssh admin@host`, `telnet host 2003`, `serial COM3 115200`, `raw host 4001`, `rdp host`, `vnc host`, `voss 10.1.1.1` or a session name and press Enter.
- **Tabs**: one per connection with a status dot (connected, connecting, error, disconnected); middle-click closes.
- **Tab badges**: `LOG` (logging), `● REC` (session recording), `SCRIPT` (script recording), `SEL` (in the send group), `PROD` (production).
- **Layouts**: tabbed, tile side by side, tile stacked, tile grid.
- **Status bar**: state, protocol, user@host, server version, route (jump host), transfer progress, terminal size, emulation, connected tabs.
- **Start page**: quick actions (Quick Connect, New Session, Local Shell, Import Sessions, Discover Devices) and recent sessions when no tab is open.
- **Restore tabs**: the open sessions come back at the next start (optional).
- **Window size and position** are remembered.
- **Themes**: dark, light or follow the system, with an accent color.
- **Text kept as typed**: no auto-capitalisation, spelling or quote correction in fields (macOS).
- **About**: version and a Copy Link button to share Termwright.

## Connecting

- **Protocols**: SSH2, Telnet, Serial, Local Shell, Raw TCP, RDP, VNC and Console (a port of a console server).
- **Saved sessions**: double-click, Connect Terminal, Connect SFTP Session, or Connect All in Folder.
- **Quick Connect**: host (`user@host:port`), protocol and optional device type, without saving.
- **Quick Open** (`Mod+G`): find a session, folder, host, open tab, library command or menu action by typing.
- **Workspaces**: save the open tabs with their layout and reopen them with one click.
- **Local Shell**: zsh/bash on macOS and Linux, PowerShell on Windows; custom command and start folder.
- **Clone Session**: a second tab to the same device.
- **Reconnect**: Enter in a disconnected tab, the Reconnect button or `Mod+R`.
- **Auto reconnect**: retries after 2, 5, 10, 20, 30 s… (not after you disconnect or a login fails).
- **Errors that offer the fix**: a failed connection explains why and offers Reconnect, Edit Session, Edit Jump Host, Device Type Login, Forget Saved Password or Find by MAC Address.

## SSH2

- **Password and keyboard-interactive** logins, with the password saved in the OS keychain if you want.
- **Public keys**: identity file, encrypted keys, default `~/.ssh/id_*` keys and the SSH agent.
- **PuTTY `.ppk` keys** used directly (formats 2 and 3), no conversion.
- **SSH certificates**: `key-cert.pub` next to the key or any certificate file.
- **FIDO2 / U2F security keys** (`sk-ed25519`, `sk-ecdsa`) through ssh-agent.
- **Smart cards** (PIV, CAC, YubiKey, other PKCS#11 tokens): any key on the card or a chosen one; PIN asked once and kept in memory only.
- **Kerberos / GSSAPI**: uses your `kinit` ticket, or logs in to Kerberos itself.
- **Host key checking**: known_hosts (Termwright's and `~/.ssh/known_hosts`), prompts for new and changed keys.
- **Keep-alive** at a chosen interval.
- **Remote command**: run a command instead of a shell, e.g. `ssh -t` from a jump station to a cloud host.
- **Agent forwarding** (including smart card keys) and **X11 forwarding**.
- **Send Break**: SSH break request (also serial break and Telnet BREAK).

## Key login setup

- **Generate keys**: Ed25519, RSA 4096 or ECDSA, or reuse an existing key.
- **Install the key** on Linux, macOS, BSD or Windows OpenSSH servers (incl. `administrators_authorized_keys`).
- **Test and switch**: tries a key-only login, then switches the session to the key.
- **Key passphrases** remembered in the keychain, listed, changed or forgotten in Passwords & Credentials.

## Telnet, Serial and Raw

- **Telnet** with option negotiation (window size, terminal type, echo, suppress go-ahead).
- **Serial**: COM ports / tty devices listed in order with the USB adapter's name, updated by itself when a cable is plugged in; baud rate, data bits, parity, stop bits.
- **Detect Speed**: finds the baud rate of a console (Enter at each common speed, the readable answer wins); optionally on every connect.
- **Send bar** on serial tabs: port and speed of the connection, message box with CR / LF / CR+LF / no line ending, ↑/↓ recall, Timestamps, Auto scroll and Clear; available on any tab from the right-click menu.
- **Serial flow control**: none, RTS/CTS or XON/XOFF; DTR/RTS on open; break.
- **Raw TCP** for terminal servers and devices with a plain TCP console.
- **Send initial carriage return** to wake up consoles.

## Remote desktops

- **RDP** opens in the computer's Remote Desktop client (Remote Desktop Connection on Windows, Windows App on macOS, FreeRDP or Remmina on Linux).
- **RDP options**: Windows domain, full screen or window size; saved password handed over (Windows) or Copy Password (macOS).
- **VNC** in a built-in viewer tab: VNC servers, Raspberry Pi, Linux desktops, macOS Screen Sharing.
- **VNC options**: view only, fit to tab, Ctrl+Alt+Del, clipboard both ways.
- **Through jump hosts and proxies**: Termwright opens a local tunnel for RDP and VNC.

## Console servers

- **Console sessions**: open a device through a console server's port, using the console server's session for address, jump host and SSH login.
- **Access methods**: SSH as `user:portNN` (Opengear-style), SSH as `user:ttySN` (Avocent-style), SSH to 3000 + N, Telnet to 2000 + N or 7000 + N, or custom.
- **Device login over the console**: the device type's login steps run after the console server login.
- **Console Server Ports…**: create sessions for a range or list of ports (`1-16`, `5 rack4-dist1`) with a preview.

## Device types (login profiles)

- **Built-in profiles**: EXOS, VOSS, Cisco IOS and Linux; add your own.
- **Own username and password** per device type (keychain, password manager, or *No password*).
- **Login steps** for Telnet / Serial / Raw and **after-login steps** for every protocol (`enable`, paging off).
- **Remember for device type**: a password typed at a login prompt can be saved for the whole device type.
- **Inherit or override**: sessions use the device type's login unless they set their own.
- **Prompt pattern** per device type, used by background runs and paced sends.
- **Backup commands** per device type (EXOS `show configuration`, VOSS / Cisco `show running-config`).
- **Password state shown** (saved, asked, no password) in Session Options and Quick Connect.

## Session Manager

- **Folder tree** with filter: an exact match on address or name shows only that device, the rest on request.
- **Favourites and Recent** sections, with when each session was last connected.
- **Hover card**: target, jump host, device type, MAC address, password state, last connected, description.
- **Multi-select** (Cmd/Ctrl-click, Shift-click): connect, edit, back up, delete, add to favourites or drag together.
- **Edit many sessions at once**: folder, device type, username, password, protocol, port, jump host, environment, color scheme, keyword set, auto reconnect.
- **Drag and drop** sessions and folders; new, edit, duplicate, rename, delete.
- **Folder defaults**: username, device type, jump host, identity file, keyword set, color scheme, environment, smart card and recording for a whole folder.
- **Environments**: Production, Staging, Test / Lab, Development; coloured tags and tab lines; production asks before multi-line pastes and multi-tab sends.
- **Which devices answer**: optional dot per session from a TCP check every minute.
- **Left or right, shown or hidden**: a slim SESSIONS strip brings it back.
- **Session icons** per protocol (SSH, SFTP, Telnet, Serial, Console, Local, Raw, RDP, VNC) with a via-badge for jump targets.

## Discover devices

- **Scan a network or range** (up to a /22) without ever logging in; `192.168.1.0` without a mask scans its /24. Dependable on busy networks: devices are woken (ARP) before the checks, live devices are checked a second time more slowly, and checks wait when the computer runs short of sockets.
- **What it checks**: login and service ports, SSH and Telnet greetings, web page title / server / certificate, SIP (phones), SNMP v2c name and model, DNS, mDNS, CoAP, ESPHome, MQTT, Modbus.
- **MAC address and maker** from the ARP table, with the downloadable IEEE vendor list.
- **Kinds with icons**: switch, router, firewall, access point, server, NAS, computer, phone, mobile, printer, IoT board.
- **Network OS recognised**: EXOS, VOSS, BOSS, IOS / IOS XE / IOS XR, NX-OS, Junos, EOS, FortiOS, PAN-OS, RouterOS, Comware, ArubaOS.
- **IoT boards**: ESP32/ESP8266 (ESPHome, Tasmota, WLED), Arduino, STM32, RP2040, nRF, Shelly, Sonoff, Tuya, RTOS firmware.
- **Evidence (ⓘ)** behind every result.
- **Add as sessions** with a suggested device type: all selected, or only chosen device types, optionally one subfolder per type.
- **Connect without saving** by double-clicking a result.
- **Already in sessions** marked, including devices that moved to a new address.

## Devices that get a new address (DHCP)

- **MAC address saved** in sessions added by Discover Devices (and editable in Session Options).
- **Remember MAC Addresses** for older sessions a scan finds at their address.
- **Moved devices found**: a scan lists saved sessions whose device answers at a new address, and addresses now used by another device.
- **Review and update**: tick the sessions to move to the new address.
- **MAC check before logging in**: if another device answers at the saved address, Termwright stops before sending anything (Find the Device by MAC / Connect Anyway).
- **Find by MAC Address**: searches the local network for the device and offers its new address.
- **SSH host key check** at the new address (same / different); trusted at the new address when it is the same.

## Terminal

- **Emulation**: xterm-256color with true color, VT100/102/220, ANSI, Linux; Unicode.
- **Scrollback** with search (`Mod+F`), clear scrollback, reset terminal.
- **Terminal bar** under SSH, Telnet, Raw and local tabs: Timestamps, Auto scroll and Clear (can be hidden per tab or everywhere).
- **Clear Screen** (toolbar eraser, terminal bar, right-click, tab menu, Edit menu): empties the screen and the scrollback on any connection.
- **Auto scroll** on by default; turn it off (toolbar, right-click, Edit menu or per session) to keep the view still while output arrives.
- **Copy / paste**: copy on select, paste on right-click, Shift+Insert / Ctrl+Insert, copy as HTML with colors.
- **Paste safety**: preview and confirm multi-line pastes; paste line delay for slow devices.
- **Keyword highlighting**: sets of words or regular expressions with colors, per session or default (built-in network set: up/down, errors…).
- **Clickable links** and OSC 8 hyperlinks.
- **Timestamps**: local time with milliseconds in front of each output line (display only), per tab or on by default for a session.
- **Zoom** (`Mod +`, `Mod -`, `Mod 0`), per-session font and size, cursor style and blink.
- **Backspace** sends DEL or BS per session.
- **Lock Session / Lock All Sessions** with a password.
- **Send Text File** line by line.
- **Compare With…**: diff this tab with another tab or a file.

## Command Window, Button Bar, Command Library and History

- **Command Window**: type once, send to the active tab, selected tabs or all connected tabs; multi-line; character mode.
- **Send group**: pick tabs with Choose… or Add to Send Group.
- **Suggestions** while typing, from history and the Command Library, the tab's device type first.
- **Button Bar**: buttons that send text (with escapes), run an action and/or a script; to one or all tabs.
- **Command Library**: saved commands in folders, multi-line, `{{variables}}` with defaults, for chosen device types.
- **Commands ▾**: library commands for the active tab's device type, put in the box to check before sending.
- **Command History** (`Mod+Y`): every typed command with session, device type and time; search, type, run or reuse.
- **Secrets never remembered**: commands with passwords, secrets, communities or keys, and anything typed at a password prompt.
- **Key Map**: any key combination sends text, runs an action and/or a script.

## Logging and recording

- **Session logs**: plain text or raw, append, start on connect, file name tokens (`%H %S %Y%M%D…`), per-line timestamp prefix.
- **Save Scrollback / Save Selection** to a file.
- **Session recording**: what the device showed with its timing, per tab, session, folder or all sessions; asciinema `.cast` files.
- **Markers** (`Mod+J`) with a note at any point of a recording.
- **Player tab**: play / pause, speed 0.5×-8×, seek, skip idle time, markers, find text, export as plain text.
- **Recordings list**: by session and date, with length, markers and size; delete after N days.

## Configuration backups, checklists and Run and Collect

- **Configuration backups**: on demand or daily / on weekdays at a set time, four devices at a time in the background, no tabs.
- **Kept only when changed** (timestamps ignored), newest N copies per device, What Changed compares the last two.
- **Failure details**: what the device sent is kept next to the error.
- **Change checklists**: show commands per device type, run before and after a change; each device marked *same* or *changed*, side-by-side compare.
- **Run and Collect**: commands on many devices, values pulled into one table by patterns (model, version, serial, uptime…), Copy Table and Export CSV.
- **From the Session Manager**: right-click a session, folder or selection → Back Up, Change Checklist, Run and Collect.
- **Background logins** use saved passwords, keys, device types, jump hosts and console servers, and never ask anything.

## File transfer

- **SFTP browser**: dual pane (this computer / server), over the terminal's login or as its own SFTP session.
- **Transfers**: files and whole folders, queue with progress, cancel, **resume** of failed copies.
- **Sync** new or changed files in either direction (nothing is deleted).
- **Bookmarks** for local and server folders.
- **Server-to-server copy** between two SFTP tabs.
- **Copy via SCP** from the SFTP browser for devices with a weak SFTP server.
- **Run as root or another user** (sudo, su, any command) in the SFTP tab.
- **Through a jump station**: SFTP / SCP on the target of an `ssh … host` remote command.
- **Working directory detection**: SFTP opens in the terminal's folder, follows every `cd`, and can `cd` the terminal; also follows `sudo su -`.
- **Colored files**: folders and files colored by type, links and hidden files marked.
- **SCP** upload and download of files and folders.
- **ZMODEM** (`sz` / `rz` detected automatically), **YMODEM** batch and **XMODEM** for serial consoles and boot loaders.

## Tunnels and routing

- **Port forwarding**: local, remote and dynamic (SOCKS5).
- **Jump hosts**: through another saved SSH2 session, chainable, for SSH2, Telnet, Raw, SFTP, RDP and VNC.
- **Proxies**: SOCKS5 and HTTP CONNECT with optional login.

## Automation and scripting

- **Logon actions**: Expect/Send table with hide, trailing CR, named credentials and escapes (`\s` user, `\w` password, `\v` clipboard, `\p` pause…).
- **Triggers**: answer output patterns automatically (e.g. `--More--` → space), loop-protected.
- **Alerts**: a pattern in any tab gives a notice, a tab badge, a sound and a desktop notification.
- **Python scripts** (SecureCRT style, `crt` object) with your installed Python 3.
- **VBScript scripts** (SecureCRT style, Windows).
- **JavaScript scripts** (built in): `term.waitFor`, `readUntil`, `openSession` and more.
- **Logon scripts** with arguments; Run Script; scripts on buttons and keys.
- **Reading the screen** as rendered (`crt.Screen.Get`, `CurrentRow`).
- **Script recorder**: type commands, get a script that waits for each prompt.

## Passwords and security

- **OS keychain** for passwords and passphrases; never in session files.
- **Password managers**: 1Password (`op://`), Bitwarden (`bw://`), HashiCorp Vault (`vault://`) or any command (`cmd://`); read when connecting, never stored.
- **Named credentials** shared by many sessions.
- **Master password** asked at start-up before any saved secret is used.
- **Password vault export / import**: one file encrypted with AES-256-GCM (scrypt).
- **Forget Saved Password** per session.

## Import and export

- **Native storage** is the SecureCRT session layout (`Config/Sessions/*.ini`); unknown keys are kept.
- **Import from SecureCRT**: Config folder, zip, XML export or single `.ini`, with saved passwords moved to the keychain.
- **Import from other programs**: PuTTY / KiTTY (`.reg`, `~/.putty/sessions`), MobaXterm (`.mxtsessions`, `MobaXterm.ini`), OpenSSH `~/.ssh/config`.
- **Import from inventories**: CSV, Ansible (INI / YAML), NetBox, AWS EC2.
- **Export**: SecureCRT folder, zip or XML (everything kept).
- **Export to other programs**: PuTTY and KiTTY (.reg), PuTTY sessions folder, MobaXterm (.mxtsessions), Tabby profiles, OpenSSH config and CSV.
- **Export summary**: which sessions a format cannot hold and which settings stay in Termwright; passwords are never exported.
- **Color schemes**: import and export as JSON.

## Appearance

- **12 built-in color schemes** (Solarized, Dracula, Nord, Gruvbox, Catppuccin…), *Auto* follows the UI theme.
- **Custom scheme editor** with live preview.
- **One scheme for all sessions** or per session.
- **Global Options search**: find any option by name.

## Keyboard shortcuts

- `Mod` = Cmd on macOS, Ctrl+Shift on Windows/Linux: `O` sessions, `Q` quick connect, `L` local shell, `R` reconnect, `W` close tab, `P` SFTP tab, `F` find, `S` save scrollback, `M` session manager, `K` command window, `B` button bar, `E` command library, `Y` command history, `J` recording marker, `G` Quick Open.
