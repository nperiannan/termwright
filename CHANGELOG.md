# Changelog

All notable changes to Termwright. Each release on GitHub shows its section of this file and the list of commits.

## Unreleased

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
* **VBScript** logon scripts (SecureCRT `.vbs`, Windows) with the `crt` object, and `crt.Screen.Get` / `CurrentRow` now read the **rendered screen** (Python and VBScript).

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
* **SecureCRT-style Python scripts**: `.py` logon scripts, Run Script, buttons and key bindings get the familiar `crt` object (`crt.Screen.WaitForString`, `crt.Dialog.Prompt`, `crt.Arguments`, …) and run with your installed Python 3.
* **Terminal colors**: *Auto* scheme follows the light/dark UI, and one scheme can be applied to all sessions (imported SecureCRT defaults now follow the global scheme).
