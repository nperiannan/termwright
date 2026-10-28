# Changelog

All notable changes to Termwright. Each release on GitHub shows its section of this file and the list of commits.

## Unreleased

## 0.21.0 (unreleased)

A cleaner, more modern look. Global Options → General → Layout switches back to the **Classic** layout (toolbar, tab strip and menu bar) at any time.

* **Title bar**: the tabs (with a status dot and badges), a **Search or connect** box (Quick Open), Quick Connect, SFTP, the layout menu, the theme switch, Global Options and a **⋯** menu with File, Edit, View, Transfer, Tools and Help. On macOS the tabs share the title bar with the window buttons (after a restart when the layout is changed).
* **Quick Open connects**: type an address or a connect line (`ssh admin@10.1.1.1`, `voss 10.1.1.1`, `telnet host 2001`, `serial COM3 115200`) and Enter connects.
* **Start page**: a large search-or-connect box, quick actions (Quick Connect, New Session, Local Shell, Import, Discover) and cards for Favourites and Recent sessions with device icons, address and when they were last used.
* **Session Manager**: roomier two-line rows with a device icon (switch, router, firewall, server… from the device type) and the address, session counts on folders and New / Import / Scan buttons at the bottom.
* **Session options in a side panel**: New Session and Session Options open beside the terminal with the everyday fields (name, protocol, address and port or serial port and speed, device type, user name, password, environment); the Advanced rows and **More options…** open the full dialog at that page.
* **Connecting card**: while a session connects for the first time, a card shows each step (opening the connection or the jump host, checking the host key, logging in, starting the shell).
* **Error card**: when a session cannot connect, a card shows the reason, a hint and Try Again, Edit Session, Find by MAC Address and the other fixes.
* The Button Bar is hidden once when updating (View → Button Bar shows it again).
* Fixed: the bar with the error and its fixes (Edit Session, Forget Saved Password, …) was missing on a failed connection since 0.20.1.

## 0.20.9 (2026-10-28)

* **About** shows how many times Termwright was downloaded from GitHub (all files of all releases, as GitHub counts them; looked up when About opens and remembered for six hours, nothing is shown when GitHub cannot be reached).

## 0.20.8 (2026-10-27)

* Start page and About in the **light theme**: the same icon as in the dark theme - the four tiles on the page, without the white tile - so both themes match. The app's icon in the Dock / taskbar is unchanged.

## 0.20.7 (2026-10-27)

* **Interface font and text size** (Global Options → General): System (the default, as before), VS Code (the font VS Code uses on each system), Inter or IBM Plex Sans (both included), and text from 13 px (default) to 17 px for menus, dialogs and the Session Manager.
* **Serial hex**: the send bar sends **hex bytes** (`48 65 6C 0D 0A`, `0x02,0x03`, `4865`; mistakes are marked and not sent) and **View: Hex** shows what the device sends as hex bytes; **Save…** writes the output to a file.
* Start page and About in the dark theme: the icon is the four tiles without the dark tile and its light shade.
* Other programs' names now appear only where sessions are imported or exported; the session file document is `docs/SESSION_FILES.md` with a trademark note, and scripting is described by its `crt` object.

## 0.20.6 (2026-10-27)

* CSV import reads back the **MAC address, environment and jump host** that Termwright's CSV export writes (they were lost on a round trip). The folder, zip and XML exports already keep everything, device type and MAC included - now covered by a test.

## 0.20.5 (2026-10-27)

* **Theme switch** at the top right: sun (Light), moon (Dark) and screen (follow the system); the tooltip tells the current theme (and what the system shows), a click changes it.
* SuperPuTTY import: **Cygterm and Mintty** sessions become Local Shell sessions (their own command, else Cygwin's bash on Windows, else the normal shell). RLogin stays out (unencrypted, needs administrator rights) and the import says why.

## 0.20.4 (2026-10-27)

* **SuperPuTTY import**: File → Import Sessions → *PuTTY / KiTTY / SuperPuTTY sessions* takes SuperPuTTY's `Sessions.xml` with its folders, host, port, protocol (SSH, Telnet, Raw, Serial), username and the `-i`, `-l`, `-P`, `-A`, `-X` extra arguments. Passwords given with `-pw` go to the OS keychain, not into the session files. Before, a `Sessions.xml` was read as a SecureCRT XML export and nothing was imported.

## 0.20.3 (2026-10-27)

* **Typing and Backspace no longer lag in long sessions**: keyword highlighting marked every match in the whole scrollback, and the terminal redrew all of those marks on every key - after a few thousand lines of output with addresses and up/down (over 10,000 marks) each key cost several times more, and holding Backspace kept deleting after the key was released. Only the rows in view are highlighted now (about a hundred marks, whatever the length of the session); scrolling up highlights the rows as they come into view.
* **Keyword highlighting is off by default** (existing settings switch once; pick a default set such as *Network* in Global Options → Keyword Highlighting, or a set per session).
* Discover Devices: the address count no longer goes past the total (260 / 254); devices checked a second time are counted separately ("6 checked again").

## 0.20.2 (2026-10-27)

* **Save Scrollback As HTML** (File menu, right-click the terminal or a tab): the scrollback as a web page with the Termwright logo, the session, address, the device's model, OS version, serial number and MAC address (from the output on screen, or read in a background connection for VOSS, EXOS, Cisco and Linux device types), device type, protocol, user, connection and the date and time it was saved, then the output with its colors and keyword highlighting as shown in the tab - to open in a browser, attach to a ticket or print to PDF.

## 0.20.1 (2026-10-27)

* **Discover Devices finds the same devices every time**: on macOS a scan could run out of sockets (apps start with a limit of 256) and then miss the ports, SNMP and names of many devices; the limit is raised and the scan stays within it. Devices are woken before the checks (their MAC address is looked up first), devices that are there but showed no login port are checked a second time more slowly, and checks wait and retry when the computer is short of sockets. "stopped" was shown after every scan; it now shows only when Stop was pressed.
* **Terminal bar** under SSH, Telnet, Raw and local shell tabs: Timestamps, Auto scroll and Clear. ✕ hides it for a tab; Global Options → Terminal → *Terminal bar* hides it everywhere.
* Serial tabs show the **port and speed** of the connection at the start of the send bar; the bar wraps in narrow panes.
* **Faster typing** with many sessions: the Session Manager only puts the rows in view on the page (a tree of 1,500 sessions went from about 20,000 page elements to 650), and Command Window suggestions are worked out when the keys pause.
* The UI theme **follows the system** by default (light or dark with the computer); existing settings switch to it once and a theme chosen later stays.
* Start page: a smaller icon in front of the Termwright title; the icon on the start page and in About has a dark version for the dark theme.

## 0.20.0 (2026-10-27)

* **Serial consoles**
  * **Port list** in Session Options and Quick Connect: the ports of this computer in order (`COM2` before `COM10`, `/dev/ttyUSB2` before `/dev/ttyUSB10`, `/dev/cu.*` on macOS) with the USB adapter's name and ID. It updates by itself: a cable plugged in appears as *(new)* and is picked when no port was chosen; *Other port…* types a name.
  * **Detect Speed**: tries the common baud rates and keeps the one where the device answers with readable text; *Detect the speed each time it connects* does it on every connect.
  * **Send bar** under serial tabs: message box (Enter sends, ↑/↓ recall) with CR / LF / CR+LF / no line ending, **Timestamps**, **Auto scroll** and **Clear**. Right-click → *Send Bar* shows it on any tab.
* **Every connection**
  * **Clear Screen** (new eraser button, right-click the terminal or a tab, Edit menu) empties the screen and the scrollback. *Clear Scrollback* now keeps what is on the screen.
  * **Timestamps**: local time with milliseconds at the start of each line; per tab, or on by default for a session (Session Options → Terminal → Display). Off by default.
  * **Auto scroll** on by default; turned off (new arrow button, right-click, Edit menu, or per session) the view stays still while output arrives, with *Turn On* in the corner.
* **Discover Devices**: an address without a mask such as `192.168.1.0` (or `192.168.1`) scans its /24; the address field is shorter with **Scan** next to it.
* **Faster typing** in the Command Window (and other text fields) when the Session Manager shows many sessions: deleting with Backspace no longer lags.

## 0.19.2 (2026-10-27)

* **New app icon**: four tiles for the main features - terminal, remote desktop, device discovery and backups - with file transfer in the middle; on the start page, the About window, the window and taskbar icon and the installers.
* Global Options → Device Types: more space between the device type buttons and the Name field.

## 0.19.1 (2026-10-27)

* Start page: the Termwright title is semi-bold, and a **Discover Devices** button sits next to Import Sessions.

## 0.19.0 (2026-10-27)

* **Export to other programs** (File → Export Sessions…): **PuTTY** and **KiTTY** (.reg for Windows), a **PuTTY sessions folder** (Linux / macOS), **MobaXterm** (.mxtsessions), **Tabby** profiles (YAML), **OpenSSH config** (ssh, VS Code and other SSH tools) and **CSV**, next to the SecureCRT folder / zip / XML.
  * Carried over: name, folder, protocol, address, port, username (also from the device type and folder defaults), key file, jump host (PuTTY 0.77+ SSH proxy, MobaXterm SSH gateway, Tabby jump host, `ProxyJump`), SOCKS / HTTP proxy, port forwards, keep-alive, agent / X11 forwarding, remote command, serial settings.
  * A summary lists the sessions a format cannot hold and the settings that stay in Termwright (device type logins, logon actions, triggers…). Passwords are never exported.
* Import: MobaXterm bookmarks with a space after `=` and their comments are read correctly; PuTTY / KiTTY session names that include their folder keep the plain name.

## 0.18.0 (2026-10-27)

* **Devices that get a new address from DHCP**: sessions remember the device's **MAC address** (saved by Discover Devices; Session Options → Connection → *MAC address*; *Remember MAC Addresses* for older sessions found by a scan).
  * After a scan, Discover Devices lists saved sessions whose device answers at a **new address** (same MAC) and addresses that now belong to **another device**; **Review…** updates the ticked sessions.
  * **On connect** the MAC address of the device that answers is compared with the saved one before anything is sent; if another device answers, Termwright asks: *Find the Device by MAC* or *Connect Anyway*. Background runs never log in to a different device.
  * **Find by MAC Address** (connection error bar) searches the session's network for the device and offers its new address.
  * The SSH host key at the new address is compared with the saved one (*same* / *different*); when it is the same it is trusted at the new address too.
  * Works on this computer's own network (MAC addresses are not visible through routers, VPNs, jump hosts or proxies).

## 0.17.0 (2026-10-27)

* **Session recording and playback**: right-click a tab → **Record Session** (or record every connection of a session, a folder or all sessions). Recordings keep what the device showed with its timing and window size changes - not what you type - one file per connection in the open asciinema format (`.cast`). **Add Marker…** (`Mod+J`) adds a note at that point. File → **Recordings…** lists them; the **player tab** has play / pause, speed 0.5×-8×, seek, *Skip idle*, markers, text search and *Export Text…*. Old recordings can be deleted after N days. Background runs are never recorded.
* **Smart cards (PIV, CAC, PKCS#11)**: Global Options → **Smart Cards** reads the cards through their PKCS#11 library (OpenSC, YubiKey `libykcs11` and others; usual places found automatically) and copies a key's public key for `authorized_keys`. Session Options → SSH2 Authentication → **Smart card** uses any key on the card or a chosen one, also with OpenSSH certificates, jump hosts, SFTP and agent forwarding (card keys offered through the forwarded agent). RSA (rsa-sha2-256/512) and ECDSA keys. The PIN is asked at the first login and kept in memory only until Termwright quits (or asked every time); keys that need the PIN for each signature are supported. Folder Defaults can set the smart card for a folder.
* The script recorder is now *Start / Stop Script Recording* (Script menu) and its tab badge *SCRIPT*, so it is not confused with session recordings.

## 0.16.0 (2026-10-27)

* **Run and Collect** (File → Run and Collect…, or right-click a session, folder or selection): run commands on many devices in the background (four at a time, saved logins, nothing asked) and see values from their output in one table - columns are a name, a device type and a pattern; inventory columns (model, version, serial number, uptime) are suggested for the built-in device types. **Copy Table**, **Export CSV…**, the full output per device and earlier runs are kept.
* **Console servers**: protocol **Console** opens a device through the port of a console server (SSH as `user:portNN` or `user:ttySN`, SSH to 3000 + N, Telnet to 2000 + N or 7000 + N, or custom), using the console server's session for the address, jump host and SSH login and the device type for the login on the device. Right-click a console server's session → **Console Server Ports…** creates the sessions for a range or list of ports in one go. Console sessions work with backups, checklists and Run and Collect.
* **Change Checklist…** on the Session Manager menu of a session, a folder or a selection: opens the checklist of exactly those devices, or starts a new one with them (also File → Change Checklists… and Quick Open).
* Session Options no longer logs an Angular error when it shows the device type's login.

## 0.15.0 (2026-10-27)

* **Change checklists** (File → Change Checklists…): show commands per device type, run on a set of devices before and after a change in the background; each device is marked *same* or *changed* (times, dates and uptime counters ignored) and **Compare** shows before and after. Every change is kept with its date and an optional ticket / note.
* **Command History** (Script → Command History…, `Mod+Y`): search every command typed in terminals and the Command Window (session, device type, time, how often); type it in the tab, run it or put it in the Command Window. The command is read from the terminal line without the prompt. Commands that may carry a secret (password, secret, community, key-string, pre-shared, tokens) are never remembered; history can be turned off and cleared.
* **Suggestions in the Command Window** from history and the Command Library, the active tab's device type first (never another device type's commands); Tab takes one.
* **Backups and checklists with `config terminal`** in a device type's after-login steps: the prompt the device shows afterwards (`sw:1(config)#`) is learned, so the run no longer fails with *no prompt after after-login step*. The built-in VOSS prompt pattern also accepts configuration modes.
* Compare Outputs names checklist files *Before:* / *After:*.

## 0.14.1 (2026-10-26)

* **Configuration backups of VOSS switches**: the device type's after-login steps (`enable`, `terminal more disable`) now run one at a time, each only when the device shows the prompt it expects - `enable` is skipped for accounts that start at `#`, and nothing is typed before the switch is ready. Before, a backup could wait for a `#` that never came and fail with *no prompt after logging in*.
* A failed backup keeps **what the device sent** (*device output* link next to the error, `last-error.txt` in the device's backup folder), and the error shows the last line and the prompt pattern it was checked against.

## 0.14.0 (2026-10-26)

* **Configuration backups** (File → Configuration Backups…): back up the configuration of sessions and folders on demand or every day / on weekdays at a set time, four devices at a time in the background, with the session's login (device type, saved passwords, keys, jump hosts) and without asking anything. Copies are kept per device (newest N), saved only when the configuration changed (timestamps and similar lines ignored), and **What Changed** opens the last two in Compare Outputs. Device types have **Backup commands** (EXOS `show configuration`, VOSS / Cisco IOS `show running-config`). Right-click a session, folder or selection → *Back Up Configuration(s)*.
* **RDP sessions**: open in the computer's Remote Desktop client - Remote Desktop Connection on Windows (saved password handed over), Windows App on macOS (*Copy Password*), FreeRDP or Remmina on Linux - with domain, full screen and window size options. Through a jump host or proxy Termwright opens a local tunnel for the client.
* **VNC sessions** in a built-in viewer tab (noVNC): VNC servers, Raspberry Pi, Linux desktops and macOS Screen Sharing; Ctrl+Alt+Del, clipboard both ways, view only, fit to tab; also through jump hosts and proxies.
* Connect Bar: `rdp [user@]host[:port]`, `vnc host[:port]`. Discover Devices adds computers with only RDP or VNC as RDP / VNC sessions (VNC recognised by its `RFB` greeting).
* Compare Outputs: *Ignore timestamps* also ignores day and month names (`Mon Oct 26`).
* Roadmap updated with the planned change checklists, command history search, run-and-collect, console servers and more.

## 0.13.1 (2026-10-26)

* **Session filter finds the exact device**: when a host name, address or session name equals the filter text (e.g. `10.127.34.2`), only those sessions are listed; *N more sessions containing …* shows the others (`10.127.34.231`, `10.127.34.28` …). Other results are sorted: starting with the text first.

## 0.13.0 (2026-10-26)

* **Edit many sessions at once**: select sessions with Cmd/Ctrl-click or Shift-click (or right-click a folder → *Edit All Sessions in Folder…*) and change folder, device type, username, password, protocol, port, jump host, environment, color scheme, keyword set or auto reconnect in one go. Selections can also be connected, deleted, added to Favourites or dragged to a folder together.
* **Favourites and Recent** at the top of the Session Manager, with when each session was last connected (kept when sessions or folders are renamed or moved).
* **Hover card** on sessions: target, jump host, device type, password state (saved / asked / no password / from the device type), last connected, description.
* **Environments**: Production, Staging, Test / Lab, Development per session or folder (Folder Defaults). Coloured tags in the Session Manager and a coloured line on tabs; production sessions ask before a multi-line paste and before a send to several tabs.
* **Which devices answer**: optional dot per session from a quick TCP check of its port every minute (pulse button in the Session Manager header).
* **Search in Global Options**: find an option by name and jump to it, highlighted.
* **Errors that offer the fix**: a failed connection shows what went wrong with *Reconnect*, *Edit Session*, *Edit Jump Host*, *Device Type Login* or *Forget Saved Password*; a failed SFTP "browse as root" offers *Run As Command…*; a failed open offers *Try Again* / *Edit Session*.
* **Device-type-aware commands**: Command Library commands can be marked for device types (or take it from their folder name); the active tab's commands are listed first, can be filtered, rank first in Quick Open, and the new **Commands ▾** menu in the Command Window puts them in the box to check before sending.
* **Discover Devices**:
  * **Connect without saving**: double-click a device or right-click → *Connect*.
  * **IoT and embedded boards** get their own kind and icon: ESP32/ESP8266 (ESPHome, Tasmota, WLED), Arduino, STM32, RP2040, nRF, Shelly, Sonoff, Tuya, RTOS firmware (FreeRTOS, Zephyr, ThreadX, NuttX, Mbed, MicroPython, lwIP). New checks: unicast mDNS service list (DNS-SD), CoAP (UDP 5683), ESPHome API (6053), MQTT (1883), Modbus/TCP (502); iPhones and Android phones are also recognised by their mDNS services.

## 0.12.6 (2026-10-26)

* **Discover Devices recognises the network OS** (EXOS, VOSS, BOSS, IOS/IOS XE/IOS XR, NX-OS, Junos, EOS, FortiOS, PAN-OS, RouterOS, Comware, ArubaOS) from any evidence - SNMP, web certificate, Telnet greeting - and picks that device type. An EXOS switch that runs OpenSSH and answers no SNMP (certificate *Extreme Networks, Inc., EXOS*, Telnet *telnet session telnet0 on /dev/ptyb0*) is now EXOS, not Linux, and its system column says *EXOS*.
* **Choose what Add saves**: *all selected devices* (as before) or *only the selected devices of these device types* - tick one or more device types; each shows how many devices it has. **A subfolder per device type** puts them in e.g. *Discovered/EXOS* and *Discovered/VOSS*. The choices are remembered.
* New network switch icon (switch arrows over a row of ports).
* Builds retry downloading Go modules and the Wails tool when the Go module or checksum server drops the connection.

## 0.12.5 (2026-10-26)

* **Device type passwords**: the password prompt during a Telnet/Serial login offers *Remember for device type …*; an empty password with the box ticked records that the device has none, so it logs in by itself from then on. The prompt names the user (`Password for admin@10.0.0.5`).
* Session Options and Quick Connect show the device type's password state next to *Use the device type's username and password*: *no password*, *password saved* or *password asked when connecting*.
* Discover Devices ignores web page titles that are unfilled templates (e.g. `{{pageTitle}}`) instead of using them as the system description.

## 0.12.4 (2026-10-26)

* Neutral wording: the start page says **Import Sessions** (sessions can come from several terminal programs and inventories); README, About and Options no longer single out one product.
* **Mobile phones and tablets** in Discover Devices: recognised from their mDNS (Bonjour) or DNS name (iPhone, Galaxy, Pixel …), a phone maker's MAC address, or a private (random) MAC address with no open ports - which Android and iOS use on Wi-Fi, so no vendor list can name them. New mobile icon; devices are also named from mDNS.
* `scripts/watch-build.sh [tag]` follows a GitHub build from the terminal: every job, its current step and time (GitHub CLI if installed, otherwise the public API).

## 0.12.3 (2026-10-26)

(0.12.2 was not published: its Linux build timed out on a slow Ubuntu mirror. Its changes are in 0.12.3.)

* **Text is kept exactly as typed**: macOS no longer capitalises the first letter (e.g. user name `rwa` became `Rwa`), corrects spelling or replaces quotes and dashes in any field - user names, passwords, prompts, commands.
* Release notes: every release and nightly build now lists its changes and commits ([CHANGELOG.md](CHANGELOG.md)).
* Build: more reliable nightly publishing; Linux build packages are kept in the build cache, so a slow Ubuntu mirror no longer breaks the Linux build.

## 0.12.1 (2026-10-26)

* **Much better device identification** in Discover Devices: web page title, server and certificate; Telnet banner and login prompt (with proper option negotiation); SIP over UDP for phones; MAC address and maker (IEEE vendor list) from the ARP table; this computer listed; product names win over generic words (a TerraMaster NAS is a NAS, not Windows; EXOS with OpenSSH is a switch, Mocana SSH is network gear).
* **ⓘ evidence** per device: everything the result is based on.
* **NAS / storage** kind; device type suggested from the Telnet login prompt; **Show / Hide** for the SNMP community; a note when no device answers SNMP.

## 0.12.0 (2026-10-26)

* **Discover devices** (File → Discover Devices…): scan a network or address range, see each device with an icon for its kind (switch, router, firewall, access point, server, computer, phone, printer), its name, vendor and system (SNMP v2c, DNS, SSH greeting), and add the selected ones - or all - as sessions with a matching device type. It never tries to log in. See the [User Guide](docs/USER_GUIDE.md#discover-devices).

  ![Discover Devices](docs/screenshots/discover.png)

## 0.11.5 (2026-10-26)

* **SFTP follows the terminal's user**: after `sudo su -` in the terminal (also on a host behind a bastion), the SFTP tab opened from it browses as root and opens the terminal's folder (configurable).

## 0.11.4 (2026-10-25)

* Upgrade code for pre-release installs removed.
* Password vault files use a new format version: export them again after updating (older vault files are not read).

## 0.11.3 (2026-10-25)

* **Working directory detection on by default** (configurable in Global Options → General) - Termwright turns folder reporting on in bash/zsh by itself when an SFTP tab opens from a terminal, also on a host behind a bastion; SFTP tabs follow the terminal folder.
* **Hidden Session Manager** leaves a slim *SESSIONS* strip on its side to bring it back.
* **About** shows the GitHub link with a *Copy Link* button to share Termwright.

## 0.11.2 (2026-10-25)

* **Working directory detection** - an SFTP tab opened from a terminal starts in the terminal's folder, can **follow** every `cd`, and can `cd` the terminal to the folder shown in SFTP. One click turns on folder reporting for bash/zsh (this shell or permanently); OSC 7, iTerm2 and VS Code sequences are understood.

## 0.11.1 (2026-10-25)

* **Session Manager left or right, shown or hidden** - header buttons, View menu or command palette; the choice is remembered.
* **Colored SFTP browser** - the same folder icons as the Session Manager, and files colored by type (text, archive, image, firmware, script, key), links and hidden files marked.
* **Clearer updates** - the update window shows the installed version, the latest stable release with its changes, and a newer nightly build (if any) with a warning and a **Back Up Now** step; an older nightly is no longer offered as an update.
* **Windows import** - the file picker also offers *All files (\*.\*)*, so an `.XML` export always shows.

## 0.11.0 (2026-10-25)

* **Kerberos (GSSAPI) login** - uses your `kinit` ticket, or logs in to Kerberos itself (principal + password); works with OpenSSH's `gssapi-with-mic`.
* **SSH certificates** (`key-cert.pub` or any certificate file) and **FIDO2 / U2F security keys** (`sk-ed25519`, `sk-ecdsa`, through ssh-agent).
* **Password managers** - any password field can point to **1Password** (`op://…`), **Bitwarden** (`bw://…`), **HashiCorp Vault** (`vault://…`) or a command (`cmd://…`); the secret is read when connecting and never stored.
* **VBScript** logon scripts (`.vbs`, Windows) with the `crt` object, and `crt.Screen.Get` / `CurrentRow` now read the **rendered screen** (Python and VBScript).

## 0.10.6

* New application icon (terminal + file transfer), red-on-hover tab close buttons, clearer folder arrows.
* User guide: [how SFTP through a jump station works](docs/USER_GUIDE.md#sftp-through-a-jump-station-bastion---how-the-files-travel) - files stream from the AWS host through the jump station's memory straight to your computer.

## 0.10.5 (includes 0.10.4)

* **Run as** in the SFTP tab: browse as root (`sudo`) or any user with your own command (`sudo su - oracle`, `su - admin`, `sudo -iu postgres`); password prompts are shown so you can answer them. Works on hosts behind a jump station too.

## 0.10.2

* **SecureCRT passwords are imported**: saved session passwords (encrypted or plain) move into the OS keychain during import; optional SecureCRT configuration passphrase.
* **Key passphrases** are listed in Passwords & Credentials (change / forget / add), can be remembered from the connect prompt, and are part of the password-vault export.
* SFTP and SCP on the target of an `ssh … host` remote command, through the jump station.

## 0.10.0

* **Device types** - login profiles for EXOS, VOSS, Cisco IOS and Linux (add your own) with their **own username and password** (keychain, or *No password* for EXOS `admin`). Connect with just an address: `voss 10.1.1.1` in the Connect Bar, or pick the type in Quick Connect, a session or a folder. Sessions inherit the device type's login or untick *Use the device type's username and password* to override it.
* **SFTP through a jump station** - when a session's remote command is `ssh -t -i key user@host`, open SFTP (and SCP) on that host using the key stored on the jump station: tab or Session Manager menu, or set *SFTP opens on* in the session.
* **SCP from the SFTP tab** - browse with SFTP, copy with SCP (*Copy via SCP*).
* **Compare With…** on the tab menu (pick another tab or a file), with optional *Ignore prompts*; import sources grouped by SecureCRT / other apps / inventories.
* User guide: new [SSH keys: generate, install and store](docs/USER_GUIDE.md#ssh-keys-generate-install-and-store) section.
* **Command library** with `{{variables}}`, **send to selected tabs**, **Quick Open** (`Cmd+G`), **workspaces** and **folder defaults**.
* **Output alerts** across all tabs (notice, tab badge, sound, desktop notification) and **paste confirmation** for multi-line pastes.
* **Compare outputs** of two tabs or files; **bulk import** from CSV, Ansible, NetBox and AWS EC2.
* Per-line timestamps, copy as HTML with colors, OSC 8 links, remembered window size, portable mode.

## 0.9.0

* **PuTTY `.ppk` keys** work directly as identity files (formats 2 and 3, encrypted or not) - no PuTTYgen conversion.
* **Python scripts with the `crt` object**: `.py` logon scripts, Run Script, buttons and key bindings get the familiar `crt` object (`crt.Screen.WaitForString`, `crt.Dialog.Prompt`, `crt.Arguments`, …) and run with your installed Python 3.
* **Terminal colors**: *Auto* scheme follows the light/dark UI, and one scheme can be applied to all sessions (imported default schemes now follow the global scheme).
