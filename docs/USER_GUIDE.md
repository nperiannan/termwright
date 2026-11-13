# Termwright User Guide

Termwright is a terminal emulator and session manager for **SSH2, Telnet, Serial, Raw TCP and local shells**, with SFTP/SCP/ZMODEM/YMODEM/XMODEM file transfer, logon automation, key-login setup (including PuTTY `.ppk` keys) Kerberos, SSH certificates and FIDO2 keys, password-manager integration, and Python, VBScript or JavaScript scripting. It runs on macOS (Apple Silicon), Windows and Linux, and imports and exports the sessions of other terminal programs (SecureCRT®, PuTTY, KiTTY, SuperPuTTY, MobaXterm, OpenSSH).

![The start page: favourites and recent sessions as cards](screenshots/main.png)

| | |
|---|---|
| ![A switch with the AI assistant beside it](screenshots/ai-terminal.png) <br/>*A terminal with the [AI assistant](#ai-assistant-optional) beside it* | ![A VNC desktop in a tab](screenshots/vnc.png) <br/>*A [VNC](#remote-desktops-rdp-and-vnc) desktop in a tab* |

> **New in 0.33:** an [AI assistant and agent](#ai-assistant-optional) beside the terminal (your own Claude login, Claude, OpenAI-compatible services or Ollama) that can connect to sessions and run commands - production always asks first - built-in [TFTP, FTP and HTTP servers](#file-servers-tftp-ftp-http), single sign-on for teams, start page buttons in three styles, and backup and restore in Settings.
>
> **New in 0.32:** [sync your own computers](#syncing-your-own-computers) through a shared folder, [Mosh](#mosh-roaming-ssh), Telnet over TLS and local echo, Escape and Tab in every dialog with screen reader labels, and for [teams](#teams-who-uses-which-device): *Tell Me When It Is Free*, handing the team over, backup and restore and a searchable activity log.
>
> **New in 0.21:** a **modern layout**: tabs and search in the title bar, a [start page](#2-the-main-window) with your favourite and recent sessions, a [session side panel](#4-sessions-and-the-session-manager), new device icons and folders coloured by level, and every dialog redone. Also: [device facts](#device-facts) (model, version, MAC, uptime read after connecting - taught in two clicks), a [welcome flow](#first-start-welcome-and-master-password) with an optional **master password** and an **unlock window**, [SFTP](#sftp-browser) with clickable paths, a bookmark bar, check boxes for several files, **New File** and **Delete** on both sides, keyword highlighting on by default, **Save Screen As Image** and a shortcut for nearly every menu command.
>
> **New in 0.11:** [Kerberos (GSSAPI) login](#kerberos-gssapi-login), [SSH certificates and FIDO2 security keys](#ssh-certificates-and-fido2-security-keys), [password managers](#password-managers) (1Password, Bitwarden, HashiCorp Vault, any command), **VBScript** logon scripts, and `crt.Screen.Get` / `CurrentRow` read the rendered screen.
>
> Older changes are in the [changelog](../CHANGELOG.md).

**Contents**

1. [Install](#1-install)
2. [The main window](#2-the-main-window)
3. [Connecting](#3-connecting) - device types, first SSH connection, [SSH keys: generate, install and store](#ssh-keys-generate-install-and-store), [old devices: legacy SSH algorithms](#old-devices-legacy-ssh-algorithms)
4. [Sessions and the Session Manager](#4-sessions-and-the-session-manager) - [device facts](#device-facts), [Wake-on-LAN](#wake-on-lan)
5. [Session Options reference](#5-session-options-reference) - [Mosh](#mosh-roaming-ssh), Telnet over TLS, local echo
6. [Working in the terminal](#6-working-in-the-terminal) - [AI assistant](#ai-assistant-optional)
7. [Command Window, Button Bar and Connect Bar](#7-command-window-button-bar-and-connect-bar)
8. [Logging and capturing output](#8-logging-and-capturing-output)
9. [File transfer](#9-file-transfer)
10. [Automation: logon actions, triggers, scripts](#10-automation-logon-actions-triggers-scripts)
11. [Tunnels, jump hosts and proxies](#11-tunnels-jump-hosts-and-proxies) - [web pages through a session](#web-pages-through-a-session)
12. [Appearance: themes, color schemes, highlighting](#12-appearance-themes-color-schemes-highlighting)
13. [Global Options reference](#13-global-options-reference)
14. [Migrating from SecureCRT, PuTTY or MobaXterm](#14-migrating-from-securecrt-putty-or-mobaxterm) - [syncing your own computers](#syncing-your-own-computers)
15. [Keyboard shortcuts](#15-keyboard-shortcuts) - Escape, Tab and screen readers in dialogs
16. [Files and folders Termwright uses](#16-files-and-folders-termwright-uses)
17. [Troubleshooting](#17-troubleshooting)

---

## 1. Install

Download the latest build from the repository's **Releases → nightly** page (or a version tag).

| OS | File | How to install |
|---|---|---|
| macOS (M1–M5) | `Termwright-…-macos-arm64.dmg` or `termwright-macos-arm64.zip` | Open the DMG and drag **Termwright** to **Applications**. The build is not notarized: the first time run `xattr -dr com.apple.quarantine /Applications/Termwright.app` in Terminal (or right-click → Open). |
| Windows 10/11 | `Termwright-…-windows-amd64-setup.exe` or `termwright-windows-amd64.zip` | Run the installer, or unzip and run `termwright.exe`. Needs the WebView2 runtime (built into Windows 11 and current Windows 10). SmartScreen may warn because the file isn't code-signed: **More info → Run anyway**. |
| Debian/Ubuntu | `termwright_…_amd64.deb` / `…_arm64.deb` | `sudo apt install ./termwright_*.deb` |
| Other Linux | `termwright-linux-amd64.tar.gz` / `…-arm64.tar.gz` | Unpack and run `./termwright`. Requires GTK 3 and WebKitGTK 4.1 (`libwebkit2gtk-4.1-0`). |

Optional: install `lrzsz` on the *remote* hosts to use ZMODEM (`sz`/`rz`).

To build from source see the [README](../README.md#build).

### First start: welcome and master password

The first time Termwright starts (and once after updating to a new version; **Help → Welcome…** shows it again) a short welcome flow opens:

1. **Look** - theme (Light, Dark or System), text size, folder colours and the start page buttons.
2. **Sessions** - sessions of SecureCRT, PuTTY, SuperPuTTY, MobaXterm and OpenSSH found in their usual places are listed with an **Import** button; export files (SecureCRT `.xml`/`.zip` or Config folder, PuTTY/KiTTY `.reg`, SuperPuTTY `Sessions.xml`, MobaXterm, CSV) are one click away.
3. **Security** - an optional **master password**. Type one (any 4 or more characters; a meter shows how strong it is - capitals, numbers and symbols help but are never required) and confirm it, or press **Skip**. It can be set, changed or removed later in Options → Passwords & Accounts → Master Password.
4. **Done** - a few tips, then *Start Using Termwright* or *Discover Devices*.

| Welcome | Master password (optional) |
|---|---|
| ![Welcome](screenshots/welcome.png) | ![Welcome: master password](screenshots/welcome-security.png) |

With a master password set, Termwright starts with an **unlock window**: type the master password and press **Unlock** to use your saved passwords. **Not now** closes it; saved passwords stay locked and are asked when connecting (unlock later in Passwords & Accounts). The passwords themselves stay in the OS keychain - the master password only lets Termwright use them. **Lock Now** (Passwords & Accounts → Master Password) locks them again and shows the unlock window.

![Unlock window](screenshots/unlock.png)

## 2. The main window

Termwright has one layout: tabs, the Sessions list, search and the menus (**⋯**) in the title bar. (The Classic layout with a toolbar, tab strip and menu bar was removed in 0.33; settings that chose it open the same window.)

| Area (modern layout) | What it does |
|---|---|
| **Title bar** | The **tabs** (a dot shows the state: green connected, amber connecting, red error, grey disconnected; badges for logging and recording; a running script shows as a pill with Stop), **+** for a new session, the **Sessions** button (a list of every session with its small device icon, grouped by folder with favourites first - type to filter, ↵ connects, Alt+↵ opens SFTP, an address that is no session connects to it), Quick Connect (⚡), SFTP, the layout menu, the theme switch (Light / Dark / System), Settings (⚙) and the **⋯** menu with File, Edit, View, Transfer, Tools and Help. On macOS the tabs share the title bar with the window buttons. Many tabs scroll sideways (mouse wheel or trackpad). |
| **Start page** | Shown when no tab is open: a large search-or-connect box (type `voss 10.1.1.1`, `ssh admin@host`, a session name …), quick actions (Quick Connect, New Session, Local Shell, Import, Discover Devices - as *Raised* buttons, *Keycaps* with their shortcut or *Glow cards*: Settings → General → Start page buttons) and cards for your **Favourites** and **Recent** sessions with their device icon, address, device facts and when you last used them - at most 12 cards in all, favourites first. Click a card to connect; right-click for more; *Clear* empties the list. |
| **Session Manager** (left by default) | Folders and sessions with a filter box. Folders are coloured by level (top level amber, sub-folders purple, deeper blue, production light red), sessions show a **device icon** (switch, router, firewall, access point, server …) - View → Tile / Glyph Icons switches between coloured tiles and plain glyphs. **New**, **Shell** (a local shell) and **Scan** buttons at the bottom, in the colours of the start page buttons; the **pulse** button beside the filter shows **which devices answer** (a green or red dot from a quick TCP check of each session's port, every minute) and the **⋯** menu has Clear Recent / Favourites, Compact rows and the side. |
| **Session side panel** | New Session and right-click → Properties… open beside the terminal with the everyday fields and the device facts; *Advanced* and **All session options…** open the full dialog. |
| **Command Window / Button Bar** | View → Command Window / Button Bar (or `Mod+K` / `Mod+B`). |
| **Status bar** | State, protocol · user@host:port, device type and facts (e.g. *VOSS 8.10.0.0 · up 41 days*), transfers, terminal size. |

![Sessions list in the title bar](screenshots/sessions-menu.png)

| Start page | Session side panel |
|---|---|
| ![Start page](screenshots/home.png) | ![Session side panel](screenshots/session-panel.png) |

Every command of the old menu bar is in the **⋯** menu of the title bar (on macOS also in the top-of-screen menu), with the same keyboard shortcuts.

The Session Manager can move to the **other side** or be **hidden** (View → Session Manager Left / Right, View → Session Manager, `Mod+M`); position, visibility and width are remembered.

![Session Manager on the right](screenshots/session-manager-right.png)

![Session Manager hidden - the SESSIONS strip brings it back](screenshots/session-manager-hidden.png)

## 3. Connecting

There are several ways to open a connection:

1. **Double-click a saved session** in the Session Manager (or right-click → *Connect Terminal* / *Connect SFTP Session*, or select a folder → right-click → *Connect All in Folder*), or click a card on the start page.
2. **Quick Connect** (⚡ in the title bar, `File → Quick Connect`, `Mod+Q`): type a connect line in the top box - `voss admin@10.20.1.11`, `ssh user@host:2222`, `telnet host 23`, `serial COM3 115200` - and the fields below fill in (protocol, address and port, device type, user name), or fill in the fields yourself. A device type's chip fills the protocol, port and login steps; its username is used unless you type another. Switch on **Save as a session** to keep it in the Session Manager (in the selected folder); the switch remembers your last choice, and Settings → General → *Save Quick Connect sessions* turns it on for every Quick Connect. **More options…** opens the full Session Options.

   | Inherit the device type's login | Override it for this session |
   |---|---|
   | ![Quick Connect with device type](screenshots/quickconnect.png) | ![Quick Connect with own login](screenshots/quickconnect-override.png) |

3. **Connect Bar** (toolbar text box): type and press Enter
   - `ssh admin@10.1.1.1`, `ssh -p 2222 -l admin core-sw1`, `ssh host:2222`
   - `telnet 10.1.1.20` or `telnet console-server 2003`
   - `serial COM3 115200` or `serial /dev/ttyUSB0 9600`
   - `raw 10.0.0.5 4001`
   - `rdp admin@10.0.0.10` (Remote Desktop) or `vnc 192.168.1.40` / `vnc host:5901`
   - a **device type and an address**: `voss 10.20.1.11`, `exos 10.1.1.5`, `telnet cisco-ios 10.0.0.1 2003`
   - a saved session name or path, e.g. `Lab/lab-sw01`
4. **Local Shell** (toolbar `>_`): your login shell (zsh/bash on macOS/Linux, PowerShell on Windows).
5. **Recent sessions** on the start page.
6. **Quick Open** (`Cmd+G` / `Ctrl+Shift+G`): type part of a session, folder, host, open tab, library command or menu action and press Enter (Alt+Enter opens a session as SFTP).

   ![Quick Open](screenshots/palette.png)
7. **Workspaces** (File → Workspaces…): reopen a saved set of sessions in its layout with one click; File → Save Workspace… saves the current tabs.

### Device types (login profiles)

Devices log in differently: EXOS is ready at `#` after username and password, VOSS starts at `>` and needs `enable`, Cisco IOS wants `enable` and `terminal length 0`. Instead of building logon actions for every session, define a **device type** once in Global Options → **Device Types** and pick it in Quick Connect, the Connect Bar, a session's Connection page or a folder's defaults.

![Device Types](screenshots/devicetypes.png)

- Built-in: **EXOS**, **VOSS**, **Cisco IOS**, **Linux** - edit them or add your own (*New*, *Duplicate*).
- **Icon**: the icon of the sessions with this device type - *From the name* (the default: EXOS and VOSS get a switch, Linux a server …) or one you pick: switch, router, firewall, access point, server, NAS, computer, phone, IP phone, IoT board, TV / media player, camera, printer. Device types made in Discover Devices (*+ New device type…*) get their icon there.
- **Login steps** answer `login:` / `Password:` prompts; they run for Telnet, Serial and Raw (SSH authenticates before the shell starts).
- **After-login steps** run for every protocol, e.g. `>` → `enable`, `#` → `terminal more disable`.
- **Username and Password**: the device type's own login, with the password stored in the OS keychain (never in `settings.json`). Leave the password empty to be asked at connect time - the session's Login box then says *password asked when connecting*, and the password prompt offers **Remember for device type …**: tick it to save what you type, or leave the password empty and tick it to record that the device has none. Tick **No password** for devices whose account has an empty password (e.g. EXOS `admin` out of the box) - Termwright then sends an empty password and never asks. *Forget* removes a saved password.
- Sessions with a device type **inherit** its username and password; the box shows what will happen, e.g. *(admin, no password)*, *(admin, password saved)* or *(admin, password asked when connecting)*. In Session Options → Connection (or Quick Connect) untick **Use the device type's username and password** to give that session its own username and password (or *No password*); the device type's login and after-login steps still run.
- In *Send*, `\s` is the username and `\w` the password: from the device type (or the session's own login when it overrides it), or asked once when needed.
- Optional defaults: protocol, port, initial carriage return and a prompt pattern. Background runs (backups, checklists) also recognise the prompt the device shows after the after-login steps, e.g. `sw:1(config)#` after `config terminal`.
- **Backup commands**: what prints the configuration for [Configuration Backups](#configuration-backups). Built-in: EXOS `show configuration`, VOSS and Cisco IOS `show running-config`; add your own for other device types (one command per line).
- A session's own Logon Actions still run, after the device type's steps.
- **No device type, no logon actions**: a Telnet or serial session with a username still logs in by itself - it answers a prompt ending in `login:`, `username:`, `user name:` or `user:` (any case) with the username, and the `password:` that follows with the saved password (or a password-only prompt). To type the login yourself, leave the session's username empty. If you type something before the login prompt appears (the console was still logged in), it stands down for that connection, so `exit` is not followed by a new login; pressing Enter alone, to wake a console, does not count.
- **Log in again when the device restarts** (Session Options → Logon Actions, Telnet, serial and console sessions): a console server keeps the connection open while the device behind it restarts, so the session does not reconnect; with this on, a login prompt that comes back by itself - after a restart, a crash or an idle logout - is answered again (device type login, logon actions or the saved username and password). A prompt that follows your own `exit` or `logout` is left alone, and it runs at most once every half minute, so a refused password does not loop. (A direct connection to the device that drops during the restart is a job for *Reconnect automatically*, which logs in on the new connection.)

![Session with a device type](screenshots/session-device.png)

### First SSH connection

![New host key](screenshots/hostkey.png)

- **New host key**: Termwright shows the key type and SHA256 fingerprint. *Accept & Save* stores it in Termwright's `known_hosts`; *Accept Once* connects without saving. Your `~/.ssh/known_hosts` is also trusted (read-only).
- **Changed host key**: a red warning. Only choose *Replace & Save* if you know the device was re-keyed.
- **Empty passwords**: an account with no password (EXOS `admin`) is remembered too - press Enter at the password prompt with *Remember* ticked, set *No password* on the session, or on its device type.
- **Password**: tick *Save password (OS keychain)* to store it in the macOS Keychain / Windows Credential Manager / Secret Service. Passwords are never written to session files. Remove one with right-click → *Forget Saved Password*.
- **Keys**: Termwright tries the session's identity file, then a running SSH agent (`ssh-agent`, Windows OpenSSH agent), then `~/.ssh/id_ed25519`, `id_ecdsa`, `id_rsa` (unencrypted). The identity file can be an OpenSSH/PEM key or a **PuTTY `.ppk`** key. Encrypted identity files prompt for the passphrase (or use one saved in the keychain).

### Kerberos (GSSAPI) login

For hosts joined to Active Directory / MIT Kerberos (OpenSSH `GSSAPIAuthentication yes`): Session Options → **SSH2 Authentication** → tick **gssapi (Kerberos)** (it is tried first).

![SSH2 Authentication with Kerberos and certificate](screenshots/auth.png)

- **With a ticket from `kinit`** (file credential cache, `KRB5CCNAME=FILE:…` or `/tmp/krb5cc_<uid>`), Termwright logs in without asking anything.
- **Without a ticket**, Termwright logs in to Kerberos itself: the **Kerberos principal** (e.g. `alice@CORP.EXAMPLE.COM`; blank = the session's user name) and its password, asked once per run.
- The realm's KDCs come from `krb5.conf` (`KRB5_CONFIG`, `/etc/krb5.conf`, Windows `krb5.ini`), or from DNS SRV records when the principal includes the realm. **Service name** defaults to `host@<hostname>` (`%H` = hostname).
- Mutual authentication is used, as OpenSSH requires. macOS's built-in `API:` / Windows logon tickets are not read directly - run `kinit -c FILE:/tmp/krb5cc_$(id -u)` or let Termwright ask for the password.

### SSH certificates and FIDO2 security keys

- **OpenSSH certificates** (keys signed by your organisation's SSH CA): put `<key>-cert.pub` next to the identity file - Termwright offers the certificate first, then the plain key. A certificate stored elsewhere goes in **Certificate** (SSH2 Authentication page). Certificates loaded in your ssh-agent are used too.
- **FIDO2 / U2F hardware keys** (`sk-ssh-ed25519@openssh.com`, `sk-ecdsa-sha2-nistp256@openssh.com`, e.g. YubiKey): the private key never leaves the device, so Termwright signs through **ssh-agent**. Set the `id_ed25519_sk` file as identity file and load it once with `ssh-add ~/.ssh/id_ed25519_sk` (touch the key / enter its PIN). If it is not loaded, Termwright tells you and offers **Retry** after you ran `ssh-add`. Each login may ask you to touch the key. On macOS use an OpenSSH with security-key support (e.g. Homebrew `openssh`) for `ssh-add`.

### Smart cards: PIV, CAC and PKCS#11 keys

SSH keys on smart cards and tokens - PIV and CAC cards, YubiKey (PIV), SafeNet eToken, HSMs - are used through the card's **PKCS#11 library**. The private key never leaves the card; the card signs after you enter its PIN.

1. Install the card's middleware. For most cards that is **OpenSC** (free; macOS, Windows, Linux); YubiKey also offers `libykcs11`. Termwright finds both in their usual places.
2. **Global Options → Smart Cards**: *Test / Read Cards* lists the cards and their keys. Add other libraries with *Add Library…* (e.g. your organisation's middleware). **Copy Public Key** gives the line for the server's `~/.ssh/authorized_keys` (or for your SSH CA).

   ![Smart Cards in Global Options](screenshots/smartcards.png)
3. **Session Options → SSH2 Authentication → Smart card**: *Any key on the inserted cards*, or one key. Folder Defaults can set it for a whole folder.

   ![Smart card in Session Options](screenshots/smartcard-session.png)

- The **PIN** is asked at the first login - only when the server accepts the card's key - and kept in memory until Termwright quits (never saved). *Ask the PIN for every login* changes that; *Forget Entered PINs* asks again. Keys that need the PIN for every signature (e.g. PIV slot 9c) are handled.
- Works with **OpenSSH certificates** (the session's *Certificate* file is used when it belongs to the card key), **jump hosts** (set the smart card on the jump session too), SFTP, and **agent forwarding**: the card's keys are offered to the server next to your local agent's keys.
- RSA (with `rsa-sha2-256` / `rsa-sha2-512`) and ECDSA P-256 / P-384 / P-521 keys.
- Background runs (backups, checklists, Run and Collect) use the card only once its PIN was entered in a tab; otherwise they fail with *smart card PIN needed*.
- After removing and re-inserting the card the PIN is asked again.

### Old devices: legacy SSH algorithms

A switch or appliance with old firmware may offer only weak algorithms (for example `diffie-hellman-group1-sha1`, `aes128-cbc` or `hmac-sha1`). Termwright does not use them by default, so the connection fails with *ssh: handshake failed … no common algorithm for key exchange*; the error then tells you what to do. In Session Options → **Authentication** → **Algorithms**, tick **Allow legacy algorithms (for old devices)** for that session only. When you create the session, the **New Session** side panel has the same switch among its SSH switches: *Allow legacy algorithms*.

The same switch is in **Quick Connect** (SSH: *Allow legacy algorithms*), for a connection without a saved session. For many sessions at once - e.g. a folder of old switches - right-click the folder → **Edit All Sessions in Folder…** (or select sessions → *Edit N Sessions…*), tick **Old SSH algorithms** and *Allow legacy algorithms*; only SSH sessions are changed ([#4](https://github.com/nperiannan/termwright/issues/4)).

![Allow legacy algorithms](screenshots/algorithms.png)

| New Session panel | Quick Connect | Edit All Sessions in Folder |
|---|---|---|
| ![Legacy switch in the New Session panel](screenshots/session-panel-legacy.png) | ![Legacy switch in Quick Connect](screenshots/quickconnect-legacy.png) | ![Old SSH algorithms in Edit Sessions](screenshots/bulk-legacy.png) |

The old algorithms are added at the **end** of each list, so a device that also offers modern ones still gets them. To control the lists yourself, fill in **Key exchange**, **Ciphers**, **MACs** and **Host key** with names separated by commas, in the OpenSSH way:

| Write | Meaning |
|---|---|
| `a,b` | exactly these, in this order |
| `+a,b` | add to the end of the usual list |
| `^a` | put first in the usual list |
| `-a*` | remove (a name can end in `*`, e.g. `-hmac-sha2-512*`) |

![Custom algorithm lists](screenshots/algorithms-custom.png)

An unknown name is reported by name when you connect. When the host key is already recorded in `known_hosts`, its algorithm is tried first so the saved key still matches. Use these options only on networks you trust: the weak algorithms are weak for a reason.

### Log in with: password, key, or key then password

An SSH session's side panel (New Session, or right-click → *Properties…*) and Session Options → Connection have **Log in with**:

- **Password** (the default): the username and password - the session's own, an account's, a password manager's or the device type's - also for devices that ask through keyboard-interactive prompts. Keys from the SSH agent and `~/.ssh` are tried quietly first, as OpenSSH does; choosing *Password* again turns that off.
- **Key**: an SSH key only. *Private key* is empty for the default keys in `~/.ssh` and the SSH agent; **Choose…** picks a key file; **+ Generate a new key** makes an Ed25519 key for this server (`~/.ssh/termwright_<host>_ed25519`, or reuses it when it is already there).
- **Key, then password**: the key first, and the password when the server refuses the key - handy while a key is being rolled out.

Tick **Copy the key to the server at the first login** and the first connection asks for the password once; when it is connected, Key Login Setup opens with the key filled in, adds the public key to the server and checks it. The password stays as a fallback (the session is set to *Key, then password*).

| Session panel | Session Options → Connection |
|---|---|
| ![Log in with a key in the session panel](screenshots/session-login-key.png) | ![Log in with in Session Options](screenshots/session-options-login.png) |

### SSH keys: generate, install and store

Termwright can create SSH keys, put the public key on a server for you and switch a session from password to key login. Everything is in one dialog:

- **With a connected SSH2 tab** (password login): right-click the tab → **Set Up Key Login…**. The key is generated (or reused) and installed on that server.
- **Without a connection**: **Options → Set Up Key Login / Generate Key…** only creates a key pair, so you can copy the public key to a device yourself.

![Tab menu](screenshots/tab-menu.png)

**1. Generate or choose a key**
- **Generate a new key**: *Ed25519* (recommended), *RSA 4096* (older devices) or *ECDSA P-256*. *Save as* defaults to `~/.ssh/termwright_<host>_<type>`; *Comment* is written into the public key (e.g. `you@laptop`).
- **Use an existing key on this computer**: keys found in `~/.ssh` are listed (OpenSSH/PEM or PuTTY `.ppk`); *Browse…* picks one elsewhere.
- **Passphrase** (optional, asked twice for a new key) encrypts the private key file.

**2. Install, test and use it** (tick what you want under *Then*)
- **Add the public key to the server**: appended to `~/.ssh/authorized_keys` on Linux/macOS/BSD (folder and file permissions fixed). On Windows OpenSSH it goes to `%USERPROFILE%\.ssh\authorized_keys`, or for administrators to `%ProgramData%\ssh\administrators_authorized_keys` with the ACL sshd requires. A key that is already there is not added twice.
- **Test a key-only login**: opens a second connection that may use only that key, so you know it works before relying on it.
- **Use this key for the session from the next connection**: sets the session's identity file (Session Options → SSH2 Authentication) and tries public key first; the password stays as a fallback.

**3. Where things are stored**

| What | Where |
|---|---|
| Private key | the *Save as* file, permissions `600` (owner only) |
| Public key | the same name + `.pub` (shown in the dialog with a *Copy* button) |
| Passphrase | OS keychain, only if you tick *Save the passphrase in the OS keychain*; otherwise asked when connecting |
| Which key a session uses | the session file (*Identity file*), never the key itself |

Saved passphrases are listed in **Options → Passwords & Accounts → Key Passphrases**: see which sessions use each key, *Change…* (the new passphrase is checked against the key file), *Forget*, or *Add…* one for any key file. When Termwright asks for a key's passphrase while connecting, tick *Save passphrase (OS keychain)* to remember it. Passphrases are included in the password-vault export; back up the key files themselves separately.

![Key Login Setup](screenshots/keysetup.png)

Network devices without a Unix/Windows shell (switches, routers) can't be set up automatically: generate the key, click *Copy* next to the public key and paste it into the device's own command (e.g. `ip ssh pubkey-chain` on Cisco IOS; see the device's SSH user-key commands for EXOS and VOSS).

### Reconnecting

When a connection fails, a box at the top of the terminal explains the error and offers the buttons that fix it: **Reconnect**, **Edit Session**, **Edit Jump Host** (the jump host could not be reached), **Device Type Login** (the login of a device type was refused) or **Forget Saved Password** (a saved password was refused - you are asked for it again). ✕ closes the box.

![Connection error with fixes](screenshots/fix-bar.png)

If a "browse as root" in an SFTP tab fails (*not in the sudoers file*, *a terminal is required* …), the message offers *Run As Command…* to switch user another way.

When a connection drops the tab shows `[Disconnected] Press Enter to reconnect`. Press **Enter**, use **Reconnect** (`Cmd/Ctrl+Shift+R`), or enable **Auto reconnect** in the session (retries after 2, 5, 10, 20, 30 s…, up to 20 times; never after you disconnect yourself or after a login failure).

### Remote desktops: RDP and VNC

Choose **RDP** or **VNC** as the protocol of a session (or type `rdp host` / `vnc host` in the Connect Bar). Both work through the session's **jump host or proxy** (Session Options → Jump Host / Proxy), so a Windows server or a lab desktop behind a jump station opens with a double-click like any switch.

**RDP** opens in this computer's Remote Desktop client and the tab shows what was opened:

| | Client | Password |
|---|---|---|
| Windows | Remote Desktop Connection (`mstsc`) | the session's saved password is handed over (for that connection only) |
| macOS | **Windows App** (formerly Microsoft Remote Desktop, free in the Mac App Store) | the client asks; **Copy Password** in the tab puts it on the clipboard for 60 seconds |
| Linux | FreeRDP (`xfreerdp`, `xfreerdp3`, `sdl-freerdp`), otherwise Remmina | given to FreeRDP on its input (never on the command line) |

- Session Options → **Remote Desktop**: Windows **domain**, **full screen** or a window **size** (e.g. `1600x900`).
- Through a jump host or proxy Termwright opens a **local tunnel** (`127.0.0.1:<port>` on this computer) and points the client at it. The tab shows the route and the open connections; keep it open while you work - closing it closes the tunnel. *Open Remote Desktop Again* restarts the client.

![RDP tab](screenshots/rdp.png)

![Remote Desktop options](screenshots/rdp-options.png)

**VNC** opens in Termwright's own viewer tab (noVNC): VNC servers (TightVNC, TigerVNC, RealVNC, x11vnc), Raspberry Pi, Linux desktops and **macOS Screen Sharing** (enter your Mac account's user name and password). The saved password is used, otherwise you are asked. The toolbar has **Ctrl+Alt+Del**, **Send Clipboard** (this computer's clipboard to the remote one; copying on the remote computer comes back automatically), **View Only**, **Fit** (scale the remote screen to the tab) and **Reconnect**. Session Options → VNC → *View only* starts every connection in view-only mode.

![VNC viewer tab](screenshots/vnc.png)

Discover Devices finds computers with RDP (3389) or VNC (5900) open and can add them as RDP or VNC sessions.

### Set up a remote desktop on a Linux machine

Right-click an **SSH2** session → **Set Up Remote Desktop…** (or, in an open SSH tab, right-click the tab or use **Options → Set Up Remote Desktop…**). Termwright installs the XFCE desktop with **xrdp** (RDP, port 3389) and/or **TigerVNC** (VNC, display :2, port 5902) on that machine. It runs a script on the machine **in the background**: nothing is typed in your terminal, and the dialog shows one line of progress (*Show details* has the full output).

![Set Up Remote Desktop](screenshots/remote-desktop.png)

To type the passwords instead, untick the box and open *More options*:

![Passwords and more options](screenshots/remote-desktop-passwords.png)

- **Install**: RDP, VNC or both. A service that is **already running is left alone**, so running it again never disconnects anyone.
- **Desktop users**: each service gets a **user of its own**, by default `<your user>-rdp` and `<your user>-vnc`. They are created when they do not exist, without administrator rights; a user that exists is used as it is and its password is not changed. Two users mean the two desktops never disturb each other, and neither disturbs the user who is logged in at the machine's own screen (a remote desktop for that same user often stays black).
- **Passwords**: one box, **Use the password of this SSH login for the new users, sudo and VNC**, is on by default. It works whether the login's password was saved or typed when you connected (a typed one is kept in memory for that tab only). Untick it to type the password of the new users (twice), the sudo password (or *sudo needs no password*) and a separate VNC password. VNC only looks at the **first 8 characters**. The passwords travel on the SSH connection to the script's input, never on a command line, and are never shown or written to a file.
- **Who can connect**, chosen **for RDP and for VNC separately** (under *More options*): *Through this SSH session* (recommended: the service listens on the machine's own address only, nothing is opened to the network, and Termwright reaches it through the SSH connection) or *Directly from the network* (all interfaces; add the network to open in the firewall). VNC is not encrypted and its password has at most 8 characters, so the dialog warns when VNC is opened directly; RDP is encrypted (TLS) either way. The sessions are saved to match: *through the SSH session* = address `127.0.0.1` with the SSH session as jump host (`127.0.0.1` is then **the machine itself**, as seen from the SSH session, not your computer; the Session Manager shows it as `127.0.0.1 (on name)`), *directly* = the machine's real address and no jump host.
![Who can connect, chosen for RDP and VNC separately](screenshots/remote-desktop-access.png)

- **Create the sessions afterwards** (under *More options*, together with *Who can connect* and *Reconfigure*): when the setup ends **without errors**, Termwright makes `name (RDP)` and `name (VNC)` next to the SSH session (ports 3389 / 5902; address as chosen in *Who can connect*) with the **user name and the password** of the user it created, the password in the OS keychain like any saved password. For a user that already existed nothing is stored, because its password is not known to Termwright. The same holds for a VNC service that already ran and was not reconfigured (its password was not changed); the dialog says which session has no saved password. The first time you open it, the login asks for the password, and after it works a toast offers **Save Password**. The dialog and a toast have **Open RDP** and **Open VNC** buttons.
- **Check First** only reports what is running and which users would be created; it changes nothing.
- **Reconfigure services that already run** rewrites their settings and restarts them, so anyone connected is disconnected. Use it to repair a desktop.
- **If it fails, it cleans up after itself.** What the setup created for a service that did not come up (its new user, files, service and firewall rule) is removed again, and only for that service: when VNC works and RDP fails, RDP is removed and VNC stays, with a note and only the working session created. Users that already existed and installed packages are never removed. **Copy log** (next to *Show details*) puts the error and the whole output, passwords masked, on the clipboard.
- **Hide** lets the setup go on while you work; you are told when it ends. **Cancel setup** stops it.
- Supported: **Ubuntu 22.04**, **Ubuntu 24.04** and **Debian 12**. The dialog shows the machine's version, and the script stops before changing anything on other systems. Ubuntu 22.04 uses the older polkit rule format automatically. Debian needs `sudo` installed for the login (or log in as root). More systems (the RHEL family) are planned. The script is in `scripts/remote-desktop/` of the source.

Wrong sudo password? The dialog says *The sudo password was not accepted*; **Back** returns to the form.

![Setup progress](screenshots/remote-desktop-progress.png)

![Remote desktop ready](screenshots/remote-desktop-done.png)

A user who is logged in at the machine's own screen is flagged before you start:

![Desktop user logged in locally](screenshots/remote-desktop-local.png)

**When a desktop does not connect: Diagnose.** Right-click an **RDP** or **VNC** session (*Diagnose (why it does not connect)…*) or its **SSH** session (*Diagnose Remote Desktop…*). One click runs it, and the dialog **names the problem in a line or two** - red for what is wrong, with what to do about it - or says *No problem found* in green:

![The VNC server has blocked this address](screenshots/remote-desktop-diagnose-problem.png)

![No problem found](screenshots/remote-desktop-diagnose-ok.png)

What it checks:

- the connection **from this computer**, through the jump host just as when the session is opened, with the first bytes of the handshake: for VNC what the server says (`RFB 003.008`) and which security types it offers (and whether Termwright's viewer can use them); for RDP whether the service confirms the connection and which security it chose (TLS, NLA…). A server that answers `RFB 003.003` with *Too many security failures* has blocked the address after wrong passwords (through the SSH session every login comes from 127.0.0.1, so it blocks all of them);
- **on the machine**, over the SSH session, that service only (an SSH session checks both): whether the port listens and the service runs, the service status and journal, `xrdp.log`, the VNC server log (TigerVNC 1.13+ keeps them in `~/.local/state/tigervnc`; failed logins show up there), the desktop user (exists, home folder, locked), the `sshd` forwarding settings (`AllowTcpForwarding no` is named), the listening ports and the firewall.

**Show details** opens the green and red list of everything that was checked, and under it the full report, to **copy** or **save** as a text file for someone else:

![What was checked](screenshots/remote-desktop-diagnose-details.png)

Some checks (the service journal, the VNC and RDP logs) need `sudo`: the result then says so, and you type the sudo password in the box and choose *Diagnose Again* (it is sent to the command and never shown or stored). **Nothing is changed on the machine and no password is in the report.**

**Restart a service.** The box under the result has **Restart VNC service** / **Restart RDP service** (an SSH session offers both); it is highlighted when the diagnosis says a restart should fix the problem. It asks first, because anyone connected to that desktop is disconnected, then restarts `tigervnc@<display>` or `xrdp` (with `xrdp-sesman`) on the machine over the SSH session and **diagnoses again** so you see the result. For a blocked VNC address the restart clears the block; set the password again before connecting (*Reconfigure services that already run* in Set Up Remote Desktop, or `sudo -u <vnc user> vncpasswd` on the machine), so it does not return.

![After the restart the server answers normally](screenshots/remote-desktop-diagnose-fixed.png)

**One desktop at a time.** RDP and VNC to the same machine **as the same user** are two views of one desktop and one set of settings, so Termwright does not let both stay open (sessions made by *Set Up Remote Desktop* use two different users, so they never meet this):

- Opening RDP while a VNC tab to the same machine is open (or the other way round) asks *"VNC is already open … Shall I close it and open RDP?"*. **Yes** closes that tab and opens the new one; **Cancel** opens nothing.

  ![RDP or VNC already open to the same machine](screenshots/remote-desktop-conflict.png)
- When **someone else** is connected to the machine's RDP or VNC (Termwright asks the machine over the SSH session), it says so and asks whether to open anyway. It never closes other people's sessions.

  ![Someone else is connected](screenshots/remote-desktop-others.png)


### Console servers

Devices cabled to a console server (terminal server) get a session of protocol **Console**: Termwright connects to the console server's port for the device and then logs in to the device itself, so the console of a switch in another building opens with a double-click.

1. Make a normal **SSH2** or **Telnet** session for the console server (address, username, key or saved password, jump host if needed).
2. Right-click it → **Console Server Ports…**: choose how the console server gives access to a port, the ports (`1-16`, `20, 24`, or one `port name` per line, e.g. `5 rack4-dist1`), how sessions are named, the folder and the **device type** of the devices. The preview shows each session and the command it corresponds to; sessions that already exist are skipped.

   ![Console Server Ports](screenshots/console-ports.png)
3. Or make one by hand: Session Options → Protocol **Console**, then the console server's session, the **port** and the **access**:

| Access | Connects with |
|---|---|
| SSH as user:portNN | `ssh admin:port05@console` (Opengear and similar) |
| SSH as user:ttySN | `ssh admin:ttyS5@console` (Avocent ACS and similar) |
| SSH to port 3000 + N | `ssh -p 3005 admin@console` |
| Telnet to port 2000 + N | `telnet console 2005` (Lantronix, Cisco and others) |
| Telnet to port 7000 + N | `telnet console 7005` (Avocent, Cyclades) |
| Custom | SSH2 or Telnet, any base TCP port, any user pattern (`{user}`, `{port}`, `{nn}`) |

![Console session](screenshots/console-session.png)

- The **console server's session** gives the address, the jump host or proxy and the SSH login (its key or saved password); change it once and every port follows.
- The **device type** (or the session's own username / password) logs in to the device at the console - over SSH too - and Enter is sent after connecting to wake the console line up.
- Console sessions can be backed up, used in change checklists and in Run and Collect like any other device. With the Session Manager's *Show which devices answer*, they are not checked (the console server is).

## 4. Sessions and the Session Manager

- **New session**: **+** after the tabs, *New* at the bottom of the Session Manager, `Mod+N`, or right-click a folder → *New Session* (goes into the selected folder). It opens in the **session side panel** with the everyday fields; **All session options…** opens the full dialog.
- **New folder**: folder-plus icon or right-click → *New Folder*. Folders can be nested.
- **Edit**: right-click → *Properties…* (or *Options → Session Options* for the active tab). *Properties…* is always the last item of a session's menu, below *Delete*; for a folder the last item is *Folder Defaults…*.

  ![A session's right-click menu with Properties at the end](screenshots/session-menu.png)
- **Duplicate / Rename / Delete**: right-click a session. Deleting a folder deletes the sessions inside it (you are asked first).
- **Move**: drag a session or folder onto another folder (or onto a session to drop into its folder; onto empty space for the top level). Termwright asks first - *Move "core-sw2" from "Datacenter/Core" to "Cloud"?* - so a slip of the mouse does not move anything; Settings → General → *Confirm before moving sessions* turns the question off.
- **Copy, Cut and Paste**: right-click a session, folder or selection → **Copy** (`Cmd/Ctrl+C`) or **Cut** (`Cmd/Ctrl+X`), then right-click a folder → **Paste** (`Cmd/Ctrl+V`; on a session it pastes into that session's folder; the ⋯ menu pastes at the top level). Paste after Cut moves; after Copy it makes copies - a folder with all its sub-folders and sessions - keeping the names when they are free there (otherwise *name (2)*). Saved passwords are copied with the sessions.
- **Undo** (⋯ menu → *Undo: …*, `Cmd/Ctrl+Z` in the Session Manager, or *Undo* on the notice after the change): undoes the last move, paste or delete - up to **three** steps back. It first says exactly what will happen, e.g. *Session "core-sw2" will be moved from "Cloud" back to "Datacenter/Core"*, *Session "core-sw2 (2)" in "Lab" will be deleted* or *Folder "Lab" will be re-created with its 6 sessions* (passwords of deleted sessions are asked again when connecting).

  | Paste on a folder | Confirm a move | Undo says what it does |
  |---|---|---|
  | ![Copy, Cut and Paste](screenshots/tree-paste-menu.png) | ![Move confirmation](screenshots/move-confirm.png) | ![Undo](screenshots/undo-confirm.png) |
- **Filter**: type in *Filter sessions* to search names, hosts, users and paths. When a host, address or name equals what you typed (e.g. `10.127.34.2`), only that session is listed; *N more sessions containing …* shows the partial matches too.
- **Favourites and Recent**: right-click a session → *Add to Favourites*; favourites (★) and the five most recent sessions are listed at the top of the Session Manager, with when you last connected. Click *Favourites* or *Recent* to fold a section.
- **Hover card**: rest the pointer on a session for a moment to see its device icon, name, environment and address, then *Via* (jump host or target), device type, the device facts (model, version, uptime …), MAC address, where the login comes from (saved password, asked, or the device type's login) and the port check, when you last connected and the description.
- **Aligned rows**: the address column is wide enough for any IPv4 address, so the device types line up under each other.
- **Select several sessions**: `Cmd`-click (macOS) or `Ctrl`-click adds or removes one, `Shift`-click selects a range. Right-click the selection to **Connect**, **Edit**, add to Favourites or **Delete** them all; drag them onto a folder to move them together.
- **Delete**: right-click → *Delete*, or select sessions (or a folder) and press **Delete** (on a Mac the delete key). Termwright asks first unless *Settings → General → Confirm before deleting sessions* is off; *Undo* in the ⋯ menu re-creates what was deleted (saved passwords are asked again).
- **Jump hosts in use**: a session that is the jump host of other sessions (SSH, RDP or VNC) is known to them by its path. When you **move or rename** it (drag, cut and paste, *Rename*, *Edit Sessions*, Session Options), Termwright lists the sessions - and the folders whose default jump host it is - that use it and asks whether to **update** them to the new location; Undo of the move updates them back. When you **delete** it, it lists them and asks whether to **delete them too** (for example the RDP and VNC sessions of a machine reached through that SSH session) or only the jump host. These questions are always asked, also when *Confirm before moving sessions* is turned off.
- **Edit Sessions** (right-click a selection → *Edit N Sessions…*, or a folder → *Edit All Sessions in Folder…*): tick what to change - folder, device type, username, password (or *No password*), protocol, port, jump host, environment, color scheme, keyword set, auto reconnect - and click *Apply*. Everything else stays as it is in each session. Handy after Discover Devices added many switches.

  ![Several sessions selected](screenshots/multi-menu.png)

  ![Edit Sessions](screenshots/bulk-edit.png)
- **Environments**: mark a session (Session Options → Connection → *Environment*) or a whole folder (Folder Defaults) as **Production**, Staging, Test / Lab or Development. A coloured tag shows in the Session Manager and a coloured line on the tab. **Production** sessions ask before a multi-line paste and before the Command Window, Button Bar or Command Library sends to several tabs including them (once a minute for the same tabs).
- **Your own environments**: **Settings → Environments** renames and recolours the four, or adds more (e.g. *Pre-production*, *Customer A*): a name, a short tag (up to 6 letters) and one of ten environment colours. *Like production* gives an environment production's paste questions and its tag on the tab. A folder in an environment - and everything in it, subfolders and sessions - takes its colour and tag unless a subfolder or session has an environment of its own.
- **Expand All / Collapse All Folders**: the Session Manager's ⋯ menu.
- **Which devices answer**: select a folder first to check only the sessions in it (and its subfolders); with no folder selected every session is checked. The pulse button in the header checks every minute whether each session's host answers on its port (a TCP connection that is closed at once - nothing is sent). Green = answers, red = no answer, grey = not checked (serial, local shell, or through a jump host or proxy).

  ![Session Manager with favourites, recent sessions, environments and the hover card](screenshots/session-manager.png)
- **Icons** tell the session type at a glance:

  | Icon | Session |
  |---|---|
  | <img src="assets/icon-ssh.svg" width="16"> green `›_` | SSH2 terminal |
  | <img src="assets/icon-sftp.svg" width="16"> blue `⇅` | SSH2 session that opens as an SFTP file browser |
  | <img src="assets/icon-telnet.svg" width="16"> orange `T` | Telnet (not encrypted) |
  | <img src="assets/icon-serial.svg" width="16"> purple connector | Serial console |
  | <img src="assets/icon-shell.svg" width="16"> slate `$` | Local shell |
  | <img src="assets/icon-raw.svg" width="16"> pink `<>` | Raw TCP |
  | small arrow badge | reached through a jump station (remote command `ssh … host`) |
- **Folder colours and production folders**: folders are coloured by level - top level amber, then violet, blue, teal, pink and lime for five levels of subfolders. A folder in an environment takes the environment's colour instead (the environment colours are never level colours). Right-click a folder (or a session) → **Environment…** lists the environments with their tags and colours: pick one, *None* to follow the parent folder, or *New environment…* to make one there and then (name, tag, colour, *Like production*). A folder's environment applies to everything in it that has none of its own.
- **Compact Session Manager** (Global Options → General, or ⋯ → *Compact Session Manager*): one line per session instead of two, for long lists; folders keep their colours and counts, a little smaller. With compact rows the Session Manager's header has a **two-line rows** button that turns them back off, next to the pulse (which devices answer) and ⋯ buttons. View → *Tile / Glyph Icons* chooses coloured tiles or plain device symbols.
- **Session side panel** (New Session, or right-click → *Properties…*): the everyday fields - name, folder, protocol, address and port, open as terminal or SFTP, device type, username, environment and password - plus two switches for SSH sessions, **Files panel beside the terminal** and **Reconnect automatically**, a **More Options…** button that opens every page of the session, the session's device facts (*Refresh* reads them now, *Edit* opens the Device Facts page) and an *Advanced* list (jump host, port forwarding, logon actions, terminal and appearance, logging, MAC address, keys) that opens the full Session Options at that page. *Save* keeps it, *Save & Connect* connects too, Esc closes the panel.
- **Default session**: a session file named `Default` at the top level (e.g. an imported one) is used as the template for new sessions.
- **Create SFTP Session / Create SSH Session**: right-click an SSH2 session (Session Manager, start page card or tab) to save a copy that opens the SFTP browser - or, for an SFTP session, a terminal - with the same address, login and saved password.
- **Clear Recent / Clear Favourites**: right-click the Recent or Favourites heading, the Session Manager's ⋯ menu, or *Clear* on the start page; right-click a recent session → *Remove from Recent*.

### Device facts

Termwright reads a device's **model, OS version, serial number, MAC address and uptime** (and any value you add) after connecting, in a **background login** - nothing is typed in your tab. Version and uptime are read at every connect, the others once. They show when you hover over a tab, in the status bar (*VOSS 8.10.0.0 · up 41 days*), on the start page cards, in the Session Manager hover card, in the session side panel and in the HTML scrollback report. A MAC address found is saved with the session, so *Find by MAC Address* works. VOSS, EXOS, Cisco IOS and Linux come with the rules; right-click a session → **Refresh Device Facts** reads them now.

**Session Options → Device Facts** lists every fact - its value for this session, the command it comes from and whether it is read at every connect or once - with:

- **Edit** (✎): name, the value for this session, the command, *Every connect* / *Once* and the pattern (a regular expression whose first `( )` group is the value). Name, command and pattern belong to the device type (all its sessions); the value only to this session.
- **Remove** (🗑): stops reading the fact for the device type and removes the value saved for this session.
- **Add by Hand…**: a value without a command, e.g. the rack location.
- **Read Now**: reads all facts again.
- **Teach a new fact**: type a command (e.g. `show sys-info`) and press **Run** - it runs in a background login - then select the value in the output with the mouse and press **Save Selection as Fact…**. Termwright works out the pattern from the output, shows what it finds, and saves it with the device type.

You can also teach from the terminal: select the value in a tab, then **Edit → Save Selection as Device Fact…** (`Cmd+Shift+6` / `Alt+Shift+6`) - or Shift+right-click (right-click pastes by default) → *Save Selection as Device Fact…*. The session side panel's *Device facts* box has **Refresh** and **Edit** (opens this page).

| Device facts | Teach a fact from the output |
|---|---|
| ![Device facts](screenshots/device-facts.png) | ![Save as Device Fact](screenshots/fact-teach.png) |

### Name After Devices

Sessions named by their address (`10.127.35.13`) can take the name the device gives itself - its **hostname**, read as a device fact (EXOS `show switch`, VOSS `show sys-info`, Cisco `show version`, Linux `hostname`).

- **Right-click a folder or a selection → Name After Devices…** (or one session → *Name After Device…*): Termwright reads each device's hostname and model in the background with the session's own saved login, a few at a time, and lists them. Pick the format - **Hostname**, **Hostname (model)** or **Hostname · last octet** - edit any name, untick what should stay, then **Rename**. Sessions without a saved login or a hostname rule are shown and left alone. **Undo** in the notice renames them back.
- **After connecting**: when a session named by its address connects, Termwright offers the device's name in a notice - *Rename to core-vsp-1*, the name with the model, or *Keep* (it does not ask again for that session). With **Settings → General → Sessions named by an address → Rename automatically** it renames at once, in the chosen format, with **Undo**; *Off* stops both.
- The address stays the session's host, open tabs follow the new name, and sessions that use it as a jump host are asked to follow too. A name that was not the address is kept in the description (*Was named …*).
- **Team devices** are named by the team: Termwright tells the team server the hostname and model it read, and the owner renames them for everyone on the admin page (**Inventory → select → Name After Devices**).

### Discover devices

**File → Discover Devices…** (also in the command palette) finds the devices in a network, tells what each one is, and adds the ones you pick as sessions. It **never tries to log in**.

**Wake-on-LAN sessions**: a device that answered no login port but has a MAC address can still be ticked - it is saved as a Wake-on-LAN session (see Wake-on-LAN below) with its address and MAC address. These are not ticked automatically.

**Copying from the results**: right-click a device → *Copy IP Address*, *Copy MAC Address*, *Copy Vendor / System Info* (with everything it is based on) or *Copy All Details*; or select any text in the table and press Cmd/Ctrl+C.

![Discover Devices - before the first scan](screenshots/discover-start.png)

1. **Addresses**: the page opens with **Networks of this computer** - every network this computer has an address on (wired, Wi-Fi, VPN), one row each with the interface, its address, the network and how many addresses a scan covers. Click a row to fill the box (a network larger than a /22 gives the /24 around this computer; a VPN row is marked because the networks behind a VPN are not known here - type them yourself). *Refresh* reads them again after connecting a VPN. Or type your own: a network (`10.20.0.0/24`, or just `10.20.0.0` for its /24), a range (`10.20.0.10-80` or `10.20.0.10-10.20.1.20`) or single addresses, separated by commas or spaces, then **Scan** next to it (or Enter). Up to 1,024 addresses (a /22) per scan.
2. **SNMP** (on by default): a read-only SNMP v2c community (`public` is filled in). **Show / Hide** next to it reveals what you typed. The community is used for this scan only and is not saved. Devices that answer report their system name, model and software - the most reliable information. If *no* device answers, a note says so: check the community, that SNMP is enabled, and that the devices' SNMP access list allows your computer.
3. **Scan**. For every address Termwright collects, in parallel:
   - **Open ports**: SSH (22), Telnet (23), HTTP (80), HTTPS (443), SMB (445), RDP (3389), printing (9100, 631), SIP (5060).
   - **SSH greeting** (e.g. `OpenSSH_8.1`, `Mocana SSH`) and the **Telnet banner and login prompt** (Termwright answers the Telnet option negotiation, so devices show their banner, e.g. `ExtremeXOS (X440G2…) version 31.7 / login:`).
   - **Web interface**: title of the start page, `Server` header, login realm, certificate owner, and product names on the page (ExtremeXOS, TerraMaster, Yealink, LaserJet, …).
   - **SIP** (UDP 5060): phones answer with their model and firmware, e.g. `Yealink SIP-T46S 66.86.0.15`.
   - **SNMP** system name, description and vendor number; **DNS** name (home routers often register phone names such as `Galaxy-S22`).
   - **mDNS (Bonjour) name**: many devices answer with the name they give themselves, e.g. `Natarajans-iPhone`, a printer or NAS name.
   - **MAC address** of devices on your own subnet (from your computer's ARP table) - this also finds live devices with no open port. With the **MAC vendor list** (*Download the MAC vendor list*: the IEEE registry, about 3 MB, saved in the data folder) the maker is shown too.
   - **This computer** is listed and marked *this computer*.

   Devices appear while the scan runs, sorted by address; **Stop** ends it early.
4. **Results**: each device has an icon for its kind, its name (SNMP name, DNS name or address), MAC address, vendor and system, and the services that answered. Click **ⓘ** to see *what the result is based on* - every piece of evidence that was found.

   ![Discover Devices - results with evidence](screenshots/discover.png)

   ![More kinds: phones, mobiles and IoT boards (ESP32, Shelly, STM32)](screenshots/discover-kinds.png)

   | Icon | Kind | Recognised by |
   |---|---|---|
   | <img src="assets/device-switch.svg" width="16"> | Network switch | SNMP, Telnet banner or web page naming VOSS, Fabric Engine, ExtremeXOS/Switch Engine, IOS, NX-OS, Junos, EOS …; network vendor MAC; embedded SSH servers (Mocana); devices with Telnet open |
   | <img src="assets/device-router.svg" width="16"> | Router | ISR/ASR, IOS-XR, RouterOS, VyOS, FRITZ!Box, OpenWrt … |
   | <img src="assets/device-firewall.svg" width="16"> | Firewall | FortiGate, PAN-OS, ASA/Firepower, Check Point, SonicWall, pfSense/OPNsense |
   | <img src="assets/device-wireless.svg" width="16"> | Wireless access point | "access point", AP models, UniFi, Aironet, Ruckus |
   | <img src="assets/device-server.svg" width="16"> | Server (Linux/Unix) | Linux/BSD in SNMP or the SSH greeting, ESXi/Proxmox, iDRAC/iLO; SSH with file sharing |
   | <img src="assets/device-nas.svg" width="16"> | NAS / storage | TerraMaster, Synology, QNAP, TrueNAS, ReadyNAS, ASUSTOR, WD My Cloud (web page, SNMP or MAC) |
   | <img src="assets/device-computer.svg" width="16"> | Computer | Windows (SNMP, RDP, SMB without SSH), Apple MAC addresses, this computer |
   | <img src="assets/device-mobile.svg" width="16"> | Mobile phone / tablet | names such as iPhone, iPad, Galaxy, Pixel, Redmi, OnePlus (mDNS or DNS); a phone maker's MAC address with no open ports; a **private (random) MAC address** with no open ports - Android and iOS use one per Wi-Fi network, so the maker cannot be read from it (no vendor list can name it) |
   | <img src="assets/device-iot.svg" width="16"> | IoT / embedded board | ESP32/ESP8266 (Espressif MAC, ESPHome, Tasmota, WLED, `esp32-…` names), Arduino, STM32, Raspberry Pi Pico/RP2040, Nordic nRF, Shelly, Sonoff, Tuya; RTOS and embedded network stacks (FreeRTOS, Zephyr, ThreadX, NuttX, Mbed OS, MicroPython, **lwIP** web servers); the **ESPHome API** port (6053), **CoAP** (UDP 5683), Modbus/TCP or MQTT without a computer's services; mDNS services such as `_esphomelib`, `_arduino`, `_hap` (HomeKit) |
   | <img src="assets/device-phone.svg" width="16"> | IP phone | SIP answer, phone models (Yealink, Poly/Polycom, Grandstream, Snom, Mitel, Cisco, Avaya …), phone vendor MAC |
   | <img src="assets/device-printer.svg" width="16"> | Printer | printer models in SNMP or the web page (LaserJet, Brother, Canon, Epson …), printing ports |
   | <img src="assets/device-unknown.svg" width="16"> | Unknown device | nothing named it - open **ⓘ** to see what answered |

   Besides the login and web ports, the scan asks each address over unicast mDNS which services it offers (DNS-SD) and tries CoAP, the ESPHome API, MQTT and Modbus/TCP, so microcontroller boards without SSH or a web page are found too. An iPhone offering `_apple-mobdev2` or an Android phone offering wireless debugging (`_adb-tls-connect`) is recognised the same way.

   **Connect without saving**: every device has a **Connect ▾** button. The button connects the recommended way (SSH before Telnet, then RDP, VNC or the web interface); **▾** lists every way the device answered - SSH, SFTP (files), Telnet, RDP, VNC, the web interface (HTTP or HTTPS). Double-click a device does the same as the button; right-click lists every way too (*Connect: SSH - recommended*, …). The tab opens with the suggested device type and the window stays open.

   ![Connect ▾ lists every way a device answered](screenshots/discover-connect.png)

   **Best guesses**: a device that nothing named gets a guess with its reason in the *Type* column, e.g. *Router / access point - likely: TP-Link, web admin only*, a TV or media player (Samsung, LG, Roku, Chromecast …), a camera (Hikvision, Dahua, Axis, Reolink …) or the gateway (the network's `.1` with a web page).

   **Another device at a saved address**: when a session's address now answers with a different MAC address, *10.20.0.40 is now another device* names the session and both MAC addresses: **Keep** leaves the session as it is, **Update the session** saves the new MAC address.

   Product names win over generic words: a NAS whose SNMP says "Linux" but whose web page says "TerraMaster" is shown as a NAS.
5. **Add**: devices with SSH or Telnet are ticked (those already in the Session Manager are marked *in sessions* and left unticked; this computer is never ticked). Devices without a login port can be added when they have a MAC address (as Wake-on-LAN sessions); tick *Only devices you can connect to* to hide the others.

   Pick a **device type** per device - also for phones, IoT boards, TVs and other devices without SSH or Telnet, where it sets the session's icon. Termwright suggests one from the network OS that any evidence names (an SNMP description, the web certificate *Extreme Networks, Inc., EXOS* or the EXOS Telnet greeting *telnet session telnet0 on /dev/pty…* all mean EXOS, even when the switch runs OpenSSH), then from what the device reports (VOSS, Cisco IOS, Linux …) or, failing that, from the **Telnet login prompt** compared with the first login step of your device types (e.g. EXOS `login:`, VOSS `Login:`). **+ New device type…** at the end of the list makes a device type right there: a name and an icon (phone, IoT, TV, camera, router …); login steps can be added later in Options → Device Types.

   ![+ New device type… with its icon](screenshots/discover-newtype.png)

   What to add sits in the bar at the bottom, next to the button (greyed out until devices are selected): type or choose a **folder** (default *Discovered*) and click **Add N Sessions**.
   - **Add** *all selected devices* (the default) or *only the selected devices of these device types*: tick one or more device types (each shows how many selected devices it has; *(no device type)* covers devices without one).
   - **A subfolder per device type** files the sessions under the folder by device type, e.g. *Discovered/EXOS*, *Discovered/VOSS* (devices without a type go to *Other*).
   - These choices are remembered for the next scan. Sessions are named after the device and get its vendor and system as description; folder defaults apply as usual.

   ![Discovered devices added as sessions](screenshots/discover-added.png)

Scanning connects to many addresses and can be noticed by security monitoring: scan only networks you are allowed to scan, such as your lab. Termwright asks you to confirm this before the first scan.

**Folder Defaults** (right-click a folder → *Folder Defaults…*): username, device type, jump session, identity file, keyword set, color scheme and environment that every session in the folder (and sub-folders) uses when its own field is empty. The nearest folder wins.

![Folder Defaults](screenshots/folder-defaults.png)

**Workspaces** (File → Workspaces…, File → Save Workspace…): a named set of sessions with their layout (tabs, side by side, stacked, grid). *Open* reconnects them all; *Rename* and ✕ manage the list.

![Workspaces](screenshots/workspaces.png)

### Wake-on-LAN

Termwright can wake a sleeping or powered-off computer, server or lab device by sending a Wake-on-LAN *magic packet* to its MAC address - either as a **Wake-on-LAN session** of its own, or from any saved session that has a MAC address.

#### Wake-on-LAN sessions

New Session → Protocol → *More…* → **Wake-on-LAN** makes a session whose only job is to wake a device and show whether it is up. It has its own power icon in the Session Manager.

| Field | What it does |
|---|---|
| **MAC address** (required) | the device's wired network port that is set to wake (`00:11:22:33:44:55`, `00-11-…`, `0011.2233.4455` or `001122334455`) |
| **Network** | the device's network, e.g. `192.168.1.0/24`; `192.168.1.0`, `192.168.1` or an address in it (`192.168.1.41`) mean its /24. The magic packet is **advertised on that network**: to its broadcast address and to `255.255.255.255`, out of this computer's interface on it. |
| **IP address** | the device's address: it is pinged to show whether it is up. Without a network, the packet goes to the broadcast address of the network this address is on. **The network or the IP address is required** (one of them). |
| **UDP port** | 9 by default (some devices use 7) |
| **Broadcast** | only when the right broadcast address differs from the network's (e.g. a router that forwards a directed broadcast) |
| **After waking** | how many times to search the network for the MAC address, and how many seconds apart (4 searches, 30 s apart by default) |
| **Ping** | how often the device is pinged to show up or down - every 2 minutes by default, or 30 s, 1, 5, 10 or 30 minutes, or never |
| **Send from** | with a jump host (Jump Host / Proxy page): send from the jump host - for a device on the jump host's network - or from this computer |

**Double-click** the session (or *Save & Wake*, *Wake Now*) to wake it:

1. The magic packet is sent (three times) to the network's broadcast address and to the limited broadcast `255.255.255.255`, out of the network interface that is on the device's network (several routers and switches drop a directed broadcast, and macOS and Windows send `255.255.255.255` out of one interface only, so each interface is addressed by its own address). When this computer is on no such network, every interface is used.
2. **With an IP address**, the device is pinged every 5 seconds until it answers (at least 2 minutes, or as long as the searches would take). When it answers, its **NetBIOS name** is read too (Windows and Samba answer), and the dot turns **green**. If it does not answer in time, the dot turns **orange** and a notice says it is down, not woken or not present.
3. **With only a network**, Termwright waits 30 seconds, then searches the network for the MAC address (it makes this computer look up every address of the network, as Discover Devices does, and reads the answers' MAC addresses). It repeats this up to 4 times (both are settings). When the MAC address is found, its **IP address is saved in the session** and the device is pinged; if it is not found after the last search, the device is marked **not present / down** and the searches stop. Only a network this computer is directly on can be searched.

**Macs with Apple silicon**: they cannot be woken when shut down, only from sleep, and they often ignore the magic packet itself. They wake through a *Bonjour Sleep Proxy* instead: with *Wake for network access* on (System Settings → Battery → Options; laptops on the power adapter, lid open or with an external display) the sleeping Mac hands its address to an Apple TV, HomePod or AirPort on the network, which wakes it when a connection comes in for a service the Mac offers. Enter those ports under **Wake a Mac through its sleep proxy** (Session Options → Connection, e.g. `22, 5900`) with the Mac's IP address: after the magic packet Termwright connects to them - from the jump host when the packet goes from there. Several MAC addresses can be entered, separated by commas (the Mac's Wi-Fi and its dock's Ethernet); each gets the packet.

**Sending from a jump host**: Termwright runs a one-line command over SSH that sends the packet with `python3`, or `perl` when there is no python3 (as on many NAS systems), or `wakeonlan`. No root rights are needed (an ordinary UDP broadcast socket).

**Wake on a schedule**: *Every day* or *Monday to Friday* at a time (the session panel, or Session Options → Connection) sends the magic packet at that time while Termwright is open - for example to start a lab server before the working day. A wake missed by more than 15 minutes (the computer was asleep, Termwright was closed) is skipped, not sent late; a note says when a scheduled packet went out.

Afterwards every Wake-on-LAN session with an IP address is **pinged every 2 minutes** (its *Ping* setting): the dot in the Session Manager is **green** when it answers and **orange** when it does not; while it is being woken the dot blinks. The hover card shows the MAC address, network, state, when it was checked and the NetBIOS name. Right-click → *Check Now (ping)* checks at once. To wake a device and connect to it in one step, right-click the session you want to open (SSH, RDP, VNC… with the device's MAC address saved) → **Wake Up and Connect**. While a device is up, *Wake Up* is greyed out. A session changed to Wake-on-LAN keeps the power icon (its device type is cleared).

| Session Manager | Wake-on-LAN session options |
|---|---|
| ![Wake-on-LAN sessions with their state](screenshots/wol-tree.png) | ![Wake-on-LAN session options](screenshots/wol-session-options.png) |

![New Wake-on-LAN session in the side panel](screenshots/wol-new.png)

#### Waking the device of any session

- **The MAC address** is saved with the session: Discover Devices and device facts fill it in, or type it in Session Options → Connection → *MAC address* (`00:11:22:33:44:55`, `00-11-…`, `0011.2233.4455` or `001122334455`). Use the MAC address of the wired port that is set to wake.
- **Wake up**: right-click the session → **Wake Up (Wake-on-LAN)**, or **Wake Up and Connect** to connect as soon as it answers. Also on the tab menu, Transfer → Wake Up, and - when a connection times out - the error card's **Wake Up** button, which reconnects the tab when the device answers. Right-click a folder → *Wake Up All in Folder*, or a selection → *Wake Up N Sessions*, sends to every session with a MAC address.
- **Where the packet is sent from**: Wake-on-LAN packets are broadcasts that do not cross routers, so they must start on the device's network. *Automatic* (the default) sends from **this computer** when it has a network interface on the device's network - like a phone app on the same Wi-Fi - and otherwise from the session's **jump host** (or the jump station of an `ssh … host` command): Termwright logs in and sends the packet with `python3`, or the `wakeonlan` tool if it is installed. Session Options → Connection can force either one. The notice after sending tells where it was sent from and to which addresses. On macOS, allow Termwright under System Settings → Privacy & Security → **Local Network**, or the system silently drops the broadcast.
- **Options** (Session Options → Connection, below the MAC address): the **broadcast address** (empty = the broadcast address of the network the session's address is on - the packet goes to that network only; enter another one only when needed, e.g. `192.168.20.255` for a router that forwards directed broadcasts), the **UDP port** (9 by default, sometimes 7), **send from** (automatic / this computer / the jump host), and **Wake Now** to try the settings before saving.
- **Did it wake?** After sending, Termwright watches the session's port (through the jump host when there is one) for up to three minutes and tells you when the device answers - with a *Connect* button - or that it did not, with what to check: Wake-on-LAN enabled in the BIOS/UEFI and the network card's settings, the right MAC address, and the broadcast address for other subnets.

| Session menu | Settings | It answered |
|---|---|---|
| ![Wake Up menu](screenshots/wol-menu.png) | ![Wake-on-LAN settings](screenshots/wol-options.png) | ![Device awake](screenshots/wol-awake.png) |

### Devices that get a new address (DHCP)

Switches that take their management address from DHCP, lab equipment, access points and IoT boards can get **another IP address** when their lease runs out - and their old address can be given to **another device**. Termwright recognises devices by their **MAC address** so that you neither lose them nor log in to the wrong one.

- **Discover Devices saves the MAC address** in the sessions it adds (Session Options → Connection → *MAC address*; the Session Manager's hover card shows it). For sessions you made earlier, a scan offers **Remember MAC Addresses** for the sessions it found at their address.
- **After a scan**, Discover Devices tells you which saved sessions answer at a **new address** (same MAC address) and which addresses now belong to **another device**. **Review…** lists the moves; tick the sessions and **Update Sessions**.

  ![Discover Devices finds saved devices at new addresses](screenshots/discover-moved.png)

  ![Reviewing the new addresses](screenshots/moved.png)
- **When you connect**, Termwright compares the MAC address of the device that answers with the saved one - before anything is sent. If it differs, it stops and asks: **Find the Device by MAC** or *Connect Anyway*. Background runs (backups, checklists, Run and Collect) never log in to a different device.

  ![Another device answers at the saved address](screenshots/mac-warning.png)
- **Find by MAC Address** (also on the connection error bar when a device does not answer) searches the session's network for the MAC address and offers the new address; **Reconnect Tab** connects to it.

  ![Found by its MAC address](screenshots/find-mac.png)

**The SSH host key is checked too**: *same SSH host key* means the device at the new address is the one you logged in to before, and its key is trusted at the new address as well. *Different SSH host key* (same MAC) is not ticked: the device was reinstalled or it is not yours. Nothing changes until you update the sessions.

Limits: MAC addresses are only visible on **this computer's own network** (the same subnet / VLAN). Devices behind a router, VPN, jump host or proxy cannot be checked this way. Where you can, a **DHCP reservation** or a **DNS name** in the session is the cleaner fix; the MAC address check is the safety net for networks where you do not control DHCP.

### Teams: who uses which device

A team that shares a lab can run its own small **team server** (a Docker container on a Linux host of the lab) and connect every member's Termwright to it. **Options → Teams…** is the place for it, and it is optional: until you join or set up a team, nothing changes and nothing is sent anywhere.

- **Set Up a Team Server…**: pick the SSH session of the host and give the team a name. Termwright checks Docker, picks a free port from 7443 (never one in use), starts `termwright-team-<name>` with its own data volume, and makes you the **owner**. If the host has no internet, the image is copied over SSH.
- **Join a Team…**: paste the invite the owner sent you. Termwright talks only to the server whose certificate matches the invite. A team with **single sign-on** shows *Join with Microsoft* (or its own name): the sign-in opens in your browser; every few weeks Termwright asks you to sign in again.
- **The team**: who is in it and who is online. Owners and co-owners see the invite (*Copy*, *New Invite* - the old one stops working), make members co-owners and remove members. *You appear as* sets the name the team sees.

- **The team folder**: the team's inventory appears as a folder named after the team and follows the server within seconds. Name, folder, address, port, protocol, device type and MAC address come from the team; your login on each device stays yours.
- **Push to Team…** (right-click a session, a folder or a selection; also in Discover Devices): offers your own sessions to the team. Devices the team has already - under any name - are shown as *already in the team as …* and left out. Owners and co-owners add directly; what members push waits for their approval.
- **Update Server…** (owners and co-owners): when the team server is older than your Termwright, the team shows *Version X is available for the team server*. Termwright logs in to the host, gets the new version and restarts the container with the same port, network and data; the team, history and invite stay, and the old version comes back if the new one does not answer.
- **Reinstall or Update…** (owners and co-owners): shown when the server runs this Termwright's version. Install the *Nightly build* (the newest build of main: fixes before they are released) or the *Latest release*, the same safe way as an update.
- **Open Admin Page** (owners and co-owners): the team's inventory, approvals, scans from the lab host, members and activity in your browser, signed in by Termwright - no passwords on the server.

Only names, user and computer names and the device inventory are shared; never passwords, usernames, keys or terminal output. The [team server guide](TEAM_SERVER.md) covers firewalls, installing by hand, backups and updates.

| Teams | Set Up a Team Server |
|---|---|
| ![Teams](screenshots/teams.png) | ![Set Up a Team Server](screenshots/teams-setup.png) |

**In use and reserved** (network devices only: switches, routers, firewalls, access points and the like reached by SSH or Telnet - not computers, Linux machines, phones, printers or IoT boards, which take several logins at once):

- A team device someone else has open shows **●** with their initials at the end of its row; a reserved one shows **🔒** (*🔒 you* when it is yours). The start page cards show first names.
- **Hover the badge** for the team card: who reserved it, from, until, how long and the note; who has it open and since when; when it was last used and how long it has been free. The device card (hover the device's name) shows the same at the bottom.
- **Right-click → Reserve…** keeps a device until a time (2 or 4 hours, today, tomorrow or Friday 18:00, or a date and time you pick) with a note. **Change Reservation…** and **Release Reservation** are on the same menu; owners and co-owners can release anyone's.
- **Connecting to a device someone else reserved** asks first and sends them a notice. A device someone else has open only shows a short message, since several people can be logged in.
- A laptop that sleeps or closes drops out of *in use* within about a minute and a half.
- Only devices with a device type of a network device count; give the others a type in the team inventory. A serial or console server session named after a team device (*core-sw1 console*) shows that device's status and counts as having it open.
- Members with your name are you on another computer: your reservation does not warn you there, and you can change or release it from either.
- **Tell Me When It Is Free** (right-click a device someone else has open or reserved): you get a notice as soon as nobody else has it open or reserved. *Stop Waiting for It* is on the same menu; a wait ends by itself after a day.
- **Hand the team over**: the owner chooses **Make owner…** next to a member in the Teams window (or on the admin page's Members). That member becomes the owner and you stay as co-owner.

| Team card | Device card | Reserve |
|---|---|---|
| ![Team card](screenshots/team-card.png) | ![Device card with the team's status](screenshots/team-device-card.png) | ![Reserve a device](screenshots/team-reserve.png) |

**The admin page** (owners and co-owners, **Options → Teams… → Open Admin Page**) shows how the lab is used over 7, 30, 90 days or a year (utilization over all hours or working hours): device hours open, utilization, when the lab is busy, the most and least used devices, reservations that sat idle, and insights such as device types in demand (the team may need more) or devices nobody used (candidates to remove). **Usage reports** list every device, member, reservation and session, with CSV export; the **Now** column shows which devices are free, in use or reserved, and **Only free now** lists the ones you can take right away. The admin page also manages the inventory, approvals, scans from the lab, members and the activity log; it follows the system's light or dark theme or the one you pick. See the [team server guide](TEAM_SERVER.md#the-admin-page).

| Overview | Usage reports |
|---|---|
| ![Team overview](screenshots/team-admin.png) | ![Usage reports](screenshots/team-reports.png) |

The admin page's **Activity** keeps the last 5000 events - who added, changed, approved, reserved, released, removed, handed over or signed in - with a filter and **Export CSV**. **Backup & restore** (owner) downloads the team's data in one file and puts a backup back.

| Tell Me When It Is Free | Make owner | Activity | Backup & restore |
|---|---|---|---|
| ![Tell Me When It Is Free](screenshots/team-watch.png) | ![Make owner](screenshots/team-owner.png) | ![Activity](screenshots/team-activity.png) | ![Backup and restore](screenshots/team-backup.png) |

## 5. Session Options reference

![Session Options](screenshots/session.png)

| Page | Settings |
|---|---|
| **Connection** | Name, folder, protocol (SSH2 / Telnet / Serial / Local Shell / Raw), device type, **Connect as** (SSH2: terminal or SFTP file browser only), hostname, port, username (blank = ask), password (saved, *No password*, or from a **password manager**), description. **Mosh** (SSH2): connect with the mosh client of this computer - see [Mosh](#mosh-roaming-ssh). **Telnet over TLS** (Telnet): telnets, port 992 by default; *Accept any certificate* for self-signed appliances. |
| **SSH2 Authentication** | Auth methods to try (**gssapi / Kerberos**, public key, keyboard-interactive, password), Kerberos principal and service name, identity file, **certificate**, *save password in OS keychain*, **agent forwarding**, **X11 forwarding**, keep-alive interval. **Algorithms**: *Allow legacy algorithms* lets this session talk to an old device that only offers weak algorithms (`diffie-hellman-group1-sha1`, `aes128-cbc`, `hmac-sha1`, `ssh-rsa`); they are added at the end of each list, so modern ones are still preferred. Key exchange, Ciphers, MACs and Host key take lists in the OpenSSH syntax: `a,b` exactly these, `+a` add to the usual list, `-a*` remove, `^a` put first. Use it only on networks you trust. |
| **Port Forwarding** (SSH2) | Local (`-L`), Remote (`-R`) and Dynamic SOCKS5 (`-D`) forwards; see [§11](#11-tunnels-jump-hosts-and-proxies). |
| **Jump Host / Proxy** | Connect via another saved SSH2 session (chainable) or through a SOCKS5 / HTTP CONNECT proxy. |
| **Serial** | Port (picked from the ports of this computer, see [Serial consoles](#serial-consoles)), baud with **Detect Speed**, data bits, parity, stop bits, **flow control** (none, RTS/CTS, XON/XOFF), assert DTR/RTS on open. |
| **Local Shell** | Command to run (blank = login shell) and start directory. |
| **Logon Actions & Triggers** | Automate logon (Expect/Send table with hide, trailing CR and credentials), send initial CR, logon **script** with arguments, SSH2 **remote command**, and triggers that answer prompts at any time; see [§10](#10-automation-logon-actions-triggers-scripts). |
| **Terminal** | Emulation (Xterm, VT100/102/220, ANSI, Linux), TERM override, scrollback lines, color scheme, font and size, Backspace sends DEL (^?) or BS (^H), keyword set, auto reconnect, **timestamps** at the start of each line (off by default) and **auto scroll** (on by default). **Local echo** shows what you type, for Telnet, raw and serial devices that do not echo it. |
| **Log File** | File name with tokens, start on connect, append, raw log, per-line prefix; see [§8](#8-logging-and-capturing-output). |

*Save & Connect* saves and opens a tab. Changes to an open session apply on the next (re)connect.

| Serial | Terminal |
|---|---|
| ![Serial page](screenshots/serial.png) | ![Terminal page](screenshots/session-terminal.png) |

### Mosh (roaming SSH)

For an SSH session over Wi-Fi, a VPN or a laptop that sleeps, tick **Connect with Mosh** in Session Options → Connection. Termwright then runs the *mosh* program of your computer with the session's address, port, username and key: mosh logs in with ssh, then keeps the session alive over UDP, so it survives sleep, a new Wi-Fi network or address, and shows what you type at once on a slow link.

- Install mosh on your computer (macOS: `brew install mosh`; Debian/Ubuntu: `sudo apt install mosh`; Fedora/RHEL: `sudo dnf install mosh`; Windows: mosh inside WSL) and **mosh-server** on the host; UDP ports 60000-61000 must be open to the host.
- The password or key passphrase is typed in the terminal, as with ssh. SFTP tabs, the Files panel and port forwarding of the session use Termwright's own SSH.
- Without mosh on your computer the tab says how to install it.

![Mosh in Session Options](screenshots/session-mosh.png)

### Serial consoles

- **Port**: the list shows the serial ports of this computer in order - `COM1, COM2 … COM10` on Windows, `/dev/ttyACM0, /dev/ttyUSB0, /dev/ttyUSB1 …` on Linux, `/dev/cu.*` on macOS - with the adapter's name and USB ID when the system reports them (e.g. *FT232R USB UART (0403:6001)*). The list **updates by itself** about every second and a half: plug in a console cable and its port appears, marked *(new)*, and is picked when no port was chosen yet. A saved port that is not plugged in shows as *(not connected)*. *Other port…* types a name by hand. Quick Connect has the same list.
- **Detect Speed**: sends Enter at each common speed (9600, 115200, 19200, 38400, 57600, 4800, 2400, 230400, 1200) and keeps the one where the device answers with readable text. Close a tab that has the port open first. *Detect the speed each time it connects* does this on every connect; the status bar then shows the speed with *(detected)*.
- **Send bar** (under serial tabs): it starts with the **port and speed** of the connection (`/dev/cu.usbserial-A10K · 115200 8N1`, green while connected; *(detected)* when the speed was detected). Type a message and press **Enter** or **Send**; ↑/↓ recall earlier messages. The line ending is added after each message: **CR** (default, what consoles expect), **LF**, **CR+LF** or none. **Timestamps** puts the local time (`[10:41:07.512]`) at the start of every line, **Auto scroll** follows new output, **Clear** empties the screen and the scrollback, **Save…** writes the output to a text file. ✕ hides the bar; right-click the terminal → *Send Bar* shows it again, on any tab.
- **Hex**: the first list switches the message box between **Text** and **Hex**. In Hex mode type bytes as `48 65 6C 6C 6F 0D 0A`, `0x02,0x10,0x03` or `48656C6C6F`; they are sent exactly as typed (no line ending is added - put `0D 0A` in yourself). A typo such as `1G` is marked in red and nothing is sent. **View: Hex** shows what the device sends as hex bytes - a new line after each line feed (`0A`) or 16 bytes - with timestamps if they are on; **View: Text** goes back to normal output.

![Serial tab sending and showing hex bytes](screenshots/serial-hex.png)

![Serial tab with the send bar, timestamps and auto scroll off](screenshots/serial-tab.png)

## 6. Working in the terminal

- **Copy / Paste**: select text (copied automatically when *Copy on select* is on). Paste with right-click (when *Paste on right-click* is on; Shift+right-click opens the menu), `Cmd+V` on macOS, `Ctrl+Shift+V` on Windows/Linux, or `Shift+Insert`. Copy also with `Ctrl+Insert`.
- **Find**: `Cmd/Ctrl+Shift+F` searches the scrollback; Enter = next, Shift+Enter = previous, Esc closes.
- **Zoom**: `Cmd/Ctrl +`, `Cmd/Ctrl -`, `Cmd/Ctrl 0` (back to 14).
- **Font, size and line spacing**: Global Options → Terminal & Appearance. The default is Menlo / Consolas / DejaVu Sans Mono at 14 with *Normal* line spacing (the font's own, as most terminal programs use); *Line spacing* adds 10, 20 or 35 % between lines. A session can have its own font and size (Session Options → Terminal → Appearance).
- **Save Screen As Image** (File menu, tab menu, `Mod+⇧P`): the visible screen as a PNG, with its colours and highlighting - handy for tickets and chat.
- **Files panel**: View → *Files Panel* (`Mod+⇧1`), the title-bar button or the tab's right-click menu shows a file browser beside an SSH terminal that follows its folder - see [Files panel beside the terminal](#files-panel-beside-the-terminal).
- **Terminal bar** (under every SSH, Telnet, Raw and local shell tab): **Timestamps**, **Auto scroll** and **Clear** one click away. ✕ hides it for that tab (right-click → *Terminal Bar* shows it again); Global Options → Terminal → *Terminal bar* hides it for every tab.
- **Clear Screen** (the eraser button, the terminal bar, right-click the terminal or a tab, Edit menu) empties the screen and the scrollback; the line with the cursor stays. *Clear Scrollback* keeps what is on the screen; *Reset Terminal* resets the emulator.
- **Timestamps**: right-click the terminal → *Timestamps* (or Edit → Timestamps On / Off) puts the local time with milliseconds at the start of each line, on any connection. It is display only: logs have their own per-line prefix. Session Options → Terminal → *Display* turns it on for every connection of a session (off by default).
- **Auto scroll** (on by default): the arrow button, right-click → *Auto Scroll* or Edit → Auto Scroll On / Off. When it is off the view stays where it is while output arrives, so you can read; *Auto scroll is off · Turn On* in the corner brings it back. Session Options → Terminal → *Display* can keep it off for a session.
- **Links**: URLs in the output open in your browser when clicked.
- **Send Break**: Tab → Send Break (serial break, telnet BREAK or SSH break request, e.g. to reach a boot loader / ROMMON).
- **Send Text File**: Transfer → Send Text File… pastes a file line by line (respects *Paste line delay*).
- **Layouts**: View → Tabbed / Tile Side by Side / Tile Stacked / Tile Grid, or the four layout buttons. In tiled views click a pane to make it active.

![Tiled view with keyword highlighting](screenshots/grid.png)

- **Lock**: Tab → Lock Session (or the lock button) blocks keyboard input to a tab until you enter the password you chose; *Lock All Sessions* locks every tab.
- **Tab menu** (right-click a tab): Reconnect, Disconnect, Clone, Add to Send Group, Open SFTP Tab, **Files Panel**, Set Up Key Login, Connect SFTP Session, Log, Save Scrollback, Timestamps, Auto Scroll, Send Bar, Clear Screen, Session Options, Rename Tab, Close, Close Other Tabs. Middle-click closes a tab.

### AI assistant (optional)

Off until you choose an AI service in **Settings → AI Assistant**: **Claude** (your Anthropic API key), an **OpenAI-compatible** service (OpenAI, Azure OpenAI or a company gateway, by address and key), **Ollama** (a model on your computer or a lab server - nothing leaves your network) or **Claude Code** - the Claude Code program on your computer, signed in with your own Claude account, so no API key is needed (install it from claude.com/claude-code and run `claude` once in a terminal to sign in; **Test** finds it and shows where; Termwright runs it without tools, so it only answers). Keys are kept in the OS keychain. **Model** is a drop-down list of the models the service offers, loaded when you open it - for Claude Code the newest of each family (*Opus 5.5 · newest* and so on) and then every version the installed Claude Code knows; you can also type a model name, and *Default* uses the service's own choice. For Claude Code, **Effort** sets how hard the model thinks (Low … Max; higher is slower and uses more of your allowance). Both can also be changed from the model chip at the top of the AI panel.

- **The AI panel** - the sparkles button in the title bar (also **Options → AI Assistant…**, or right-click in a terminal → *Explain with AI* / *Ask AI for a Command*) opens the AI assistant as a chat beside the terminal. Until an AI service is set up the button shows a small amber dot and opens these settings.
  - **Dock it** on the right (the default) or the left, or let it **float** as its own window - the buttons in its header; drag its edge to make it wider. Termwright remembers your choice.
  - **A conversation**: ask follow-up questions; answers scroll like a chat. **New Chat** (the button under the header, or **+** in the header) starts a new chat; the chat's title (or the clock button) opens **Chats** - earlier chats, kept on this computer (the device output sent with them is not kept); click one to continue it.
  - **Model**: the chip next to *AI Assistant* shows the model that answers (*Sonnet 5.5*); click it to choose another (for Claude Code: Sonnet, Opus or Haiku) or *Default*. Each answer shows the model that wrote it.
  - **Tokens**: each answer shows **↑ sent ↓ received** as the AI service counted them, the bar under the header shows the total for the chat (**⇅**), and next to Send you see about how many tokens the next question will send. Every question sends the chat so far, so long chats cost more - start a new chat for a new problem. With Claude Code about 14k of each question are Claude Code's own instructions (mostly cached by Claude).
  - **What goes with a question** - shown as chips above the typing area: the **selected text** of the terminal (chosen when you have a selection), otherwise the **whole terminal**; **+ Add context** (or type **@**) adds copied text, the whole terminal, another tab, or a picture or text file. A long terminal shows how many tokens it is, with a warning and *keep the last 200 / 500 / 1,000 lines*.
  - **Pictures**: paste a screenshot (⌘V / Ctrl+V), drop it, or attach it - up to 4 per message, 5 MB each. Claude, OpenAI-compatible services and Ollama vision models read them; Claude Code reads them from a temporary folder (it may read only those files).
  - **Typing**: a large box that grows as you type; Enter sends, Shift+Enter starts a new line, Esc stops.

  ![The AI panel](screenshots/ai-ask.png)

- **Agent mode** (switch *Ask* to *Agent* under the typing area): the AI may run commands in your tabs, find saved sessions and connect to them to find a problem - or fix or configure something if you ask - and reads the results itself. Every step shows in the chat (which device, which command, its output). The button next to the mode chooses what it may do without asking (the colour shows the level):
  - **Fully autonomous** (orange): commands and configuration changes run without asking - for example *Configure MLAG on the EXOS switches in Building B*. Termwright asks you to confirm once when you choose it. Even then, **reboot, reload, erase** and similar always ask.
  - **Changes ask first** (yellow, the default): commands on the device type's *read-only* list run by themselves; anything else shows a card with the exact command and device - **Run** (you can edit the command first), **Skip** or **Stop**.
  - **Ask before every command** (yellow).
  - **Read only** (white): it never runs anything that is not on the read-only list; changes come back as text for you.

  **Production is always guarded:** on a session in a **production** environment, every command and every connection waits for you - in every mode, fully autonomous included. The card is marked **PROD**.

  ![What the agent may do](screenshots/ai-agent-perms.png)

  **Groups of devices**: the agent finds saved sessions by folder (a location such as *Building B* or *Datacenter/Rack 4*), name, address or device type, and connects by name, folder/name or IP address. It logs in with the credentials saved in Termwright; passwords are never sent to the AI. If a session cannot connect (no saved login, the device does not answer, another device at the address), the agent is told and stops trying that one. Under each answer, a chip per device shows how it is going - *working*, *waiting* (for you), ✓ commands that ran, ✗ commands the device refused (the step shows *The device reported an error*).

  The **wrench** button (with how many are on) chooses the tools the agent may use - turn one off and the AI is not offered it:
  - **Run commands in tabs**.
  - **Connect to saved sessions** (find and open).
  - **Logger**: logs a tab to a file in your log folder (`AI-<session>-<date>.log`).
  - **Recorder**: records a tab as a replayable session recording, with a marker for each command the agent runs.

  Logger and Recorder only write files on this computer, so they never ask and work in Read only too; the agent turns them on before it debugs or changes a device and off at the end, and tells you the files. **Tools used** under each answer counts what it did (*Run commands ×6 · Logger ×2*); each step card shows the tab, the command and whether it ran by itself (*auto*), after you allowed it, or was skipped.

  The read-only list is per device type (**Settings → Device Types → AI agent: read-only**, one command beginning per line); empty = `show`, `display`, `ping`, `traceroute`… for network devices and the usual read-only commands for Linux. A command with `;`, `&&`, redirection or a pipe into anything but a filter (include, grep…) always asks (except in fully autonomous mode). The commands are typed in the visible tab, so they appear there and in its log like your own.

  ![The agent configures a switch, with a status per device](screenshots/ai-agent.png)

- Each command has **Paste** (types it at the prompt **without Enter** and closes the window - you check it and press Enter), **Copy** and **Save** (to the Command Library, for that device type). Nothing the AI answers is ever run by itself.
- **Privacy**: nothing is sent until you ask. Passwords, secrets, keys, SNMP communities and tokens are removed first; *Also replace IP addresses, MAC addresses and host names* hides those too. *What was sent* shows exactly what left your computer. Check your company's policy before sending device output to an outside service.

#### Use Termwright from Claude Desktop or Claude Code

Termwright can also work the other way round: as a **connector** (an MCP server) that Claude Desktop and Claude Code use, so you can ask in a Claude chat *"look at my access-sw7 tab - why is port 3 down?"* or *"which of my saved sessions are EXOS switches?"*.

1. **Settings → AI Assistant → Use Termwright from Claude**: switch on *Let Claude use Termwright* and press **Save**.
2. **Add** next to *Claude Desktop* (then restart Claude Desktop) and/or next to *Claude Code* - or *Set up by hand*: Claude Code `claude mcp add --scope user termwright -- <Termwright program> mcp`; Claude Desktop: *Settings → Developer → Edit Config*, a server with the Termwright program as the command and `mcp` as its argument (the page shows the exact path).
3. Keep Termwright open while Claude uses it.

![Use Termwright from Claude](screenshots/ai-connector.png)

What Claude can do: **list the open tabs** and **the saved sessions**, **read a tab** (the end of its screen and scrollback - passwords, keys and SNMP communities removed), and - only when you allow it in Termwright - **open a session** and **type a command**, getting the output back. *Commands: Never* keeps Claude to reading. Every command shows in Termwright first; *Allow in This Tab for 15 Minutes* saves asking again for read-only commands, and commands that may change the device (configure, delete, reboot, save…) are always asked, with a warning:

**Events.** Each line has its level (OK, INFO, WARN, FAIL) and its trigger: USER for what you did, AI for what an agent did through the connector, SYSTEM for what Termwright did on its own.

**QEMU versions.** Keep every host on the same QEMU version and a switch moves live anywhere. A running switch moves live to a host with the same or a newer QEMU, not an older one; moved with its disk to another version, it boots there. *Images → On the Hosts* warns when the versions differ.

**Traffic generator.** Right-click a switch → **Add Test Port…** (or *Traffic → ＋ Test Port*) and pick a free port and an address: the test port appears beside the switch, answers ARP and ping, and sends and counts frames. *＋ Stream* sends frames from one test port to another (or to every other one, or to an address) - UDP, ICMP, TCP SYN, ARP or raw, with a VLAN, size, rate and duration - and sets when it passes (loss, latency). *Start All* runs them; the Traffic tab shows sent, received, loss, latency and PASS or FAIL, and *Report…* saves it as HTML or CSV. Test ports run on the host (tens of thousands of frames a second): for forwarding, VLANs, LAGs, ACLs and failover, not line rate.

**Traffic tests.** *Traffic → Tests → ＋ Test* saves a test that Termwright runs as a series of trials on two test ports (both ways if you tick it): **Throughput** (RFC 2544: the highest rate without loss per frame size, found by halving between nothing and the top rate), **Latency** (min, average, maximum and jitter at that rate, or at a rate you give), **Frame loss** (the loss at 100 %, 90 % … of the top rate, until two steps in a row lose nothing), **Back-to-back** (the longest burst at the top rate without loss), **MAC learning** (RFC 2889: frames from more and more source MACs, then frames to each of them - the most that are learned before any floods to a third test port, the monitor), and **Convergence** (traffic at a steady rate while, at a time you choose, a cable is pulled - a LAG member for a LAG failover - or a switch stops or restarts, and after a hold the cable is plugged in or the switch started again: the frames lost and the outage in milliseconds, each way). ▶ runs a test; its results fill in as each step ends, every trial is in its log, *Stop* ends it (a pulled cable is plugged in again) and *Save Report…* saves HTML or CSV. One test runs at a time, and not while other traffic runs. The test ports send tens of thousands of frames a second at most and the emulated switches forward less, so the numbers measure the emulated switches' software, not line rate.

**Richer streams.** In a stream: *Frame size* fixed, stepping or random between two sizes, or IMIX; *QoS* sets DSCP (and the 802.1p priority on a VLAN); *Burst* sends frames back to back while keeping the average rate; *Changing fields* make one stream many flows - its source or destination MAC or address, VLAN or ports stepping through, or picking at random among, a number of values (for hashing on LAGs and ECMP, MAC learning, ACLs). A test port can have an IPv6 address (streams to it can go over IPv6) and multicast groups it joins (IGMP/MLD); a stream to a group is counted at the test ports that joined it. The table shows out-of-order and duplicate frames, latency with jitter (hover for min/avg/max) and a small graph of the sent and received rate; a stream can also fail on jitter.

**Packet capture.** Right-click a cable → **Capture Packets**: its traffic, both ways, goes into a pcap file on the host while the switches keep running (blue dashes run along the cable and a blue dot pulses at the captured port; the cable keeps its colour). The **Captures** tab in the bottom pane lists the captures and shows the packets live with a filter; *Save .pcap…* copies the file to this computer, *Wireshark* opens it in Wireshark (live while it captures) and *Delete* removes a stopped capture and its file. To see a switch's management traffic (SSH, SNMP, DHCP, its cloud connection), right-click the running switch → **Capture the Management Port**. Click a packet to see its fields and its bytes in hex under the list - click a field to mark its bytes, or a byte to find its field. Tick packets (Shift-click for a range, ⌘/Ctrl-click for one more) to **Copy Selected** as text or **Save Selected as .pcap**. Double-click a packet (or *Open Packet ↗*) to open it in a window of its own: ◀ ▶ (or the arrow keys) step through the capture, and *Copy as Text*, *Copy as Hex* and *Save as .pcap…* take just that packet.

**Local API and command line.** Settings → AI Assistant → *Local API* turns on a REST API for the emulated lab on this computer only, for your own scripts, test frameworks and CI; nothing it does is asked first, and its changes show as *API* in the Events. The address and a *Copy Key* button are there (send the key as `Authorization: Bearer <key>`; a new key each time Termwright starts, kept with the address in `mcp.json` in the data folder). `GET /api/v1/lab` is the lab, `GET /api/v1/lab/tools` lists the tools and their arguments, `POST /api/v1/lab/tools/<tool>` runs one (`{"action": "start", …}`), and the traffic generator has its own resources: `GET /api/v1/lab/traffic` (live results), `POST`/`PATCH`/`DELETE …/traffic/ports`, `POST`/`PUT`/`DELETE …/traffic/streams`, `POST …/traffic/start`, `POST …/traffic/stop` and `GET …/traffic/report?format=json|csv|html|text`. It is described by an OpenAPI 3.2.1 document - `docs/api/openapi.yaml`, and served by the app at `/api/v1/openapi.json` and `/api/v1/openapi.yaml` for any OpenAPI viewer or client generator; errors are RFC 9457 problem details. [The API guide](API.md) has curl and Python examples. The same from a terminal: `termwright lab help`, for example `termwright lab traffic add_stream name=voice from="Test port 1" to="Test port 2" pps=1000 seconds=30 dscp=46`, `termwright lab traffic start voice` and `termwright lab traffic report --csv > report.csv`.

**The emulated lab from Claude.** Claude can also run a test case on the emulated lab from start to end: see the lab and its images (*show_lab*, *lab_images*), add switches and connect cables (*lab_add_switches*, *lab_cable*), start them and wait until they are ready (*lab_power*, *lab_wait*), type configuration and show commands on a switch's console and get the output (*lab_console*), check the cabling against LLDP (*lab_check_cabling*), capture a port's packets (*lab_capture*), run traffic and read its pass/fail report (*lab_traffic*), move a switch to another host (*lab_move*), watch a switch's network cards in QEMU (*lab_link_check*), show its QEMU command line (*lab_qemu_command*) and read the Events (*lab_events*). Every change is asked in Termwright first, a whole list at once - for example *"build two 5520s with two cables between them as a LACP LAG, configure it, and show me that both ports are in the LAG"*.

![Claude asks before it types](screenshots/ai-approve.png)

The connector listens only on this computer (127.0.0.1) with a random key kept in Termwright's data folder for your user only; web pages cannot reach it.

![AI Assistant settings](screenshots/ai-connector.png)

## 7. Command Window, Button Bar and Connect Bar

**Command Window** (View → Command Window, `Cmd/Ctrl+Shift+K`)
- Enter sends the text plus Enter; Shift+Enter adds another line (each line is sent with Enter).
- Send to the **Active tab**, **All connected tabs**, or **Selected tabs** - tick tabs with *Choose…* or right-click a tab → *Add to Send Group* (a `SEL` badge marks them).

  ![Send to selected tabs](screenshots/sendgroup.png)
- *Send characters immediately* sends each key as you type.
- ↑/↓ recall history.

**Button Bar** (View → Button Bar)
- `+` adds a button. Right-click a button to edit, move, delete or toggle *Send to All Tabs*.
- In *Options → Global Options → Button Bar* each button can **send text** (escapes: `\r` Enter, `\n`, `\t`, `\e` Esc, `\x03` Ctrl+C), run an **action** (e.g. *Open SFTP Tab*) and/or run a **script**.

**Connect Bar**: **Quick Open** (`Cmd+G` / `Ctrl+Shift+G`), the start page's box and the title bar's **Sessions** list connect the same way (View → Connect Bar opens Quick Open).

**Command History** (Script → Command History…, `Cmd+Y` / `Ctrl+Shift+Y`, or *History…* in the Command Window): every command you type in a terminal or send from the Command Window is remembered with the session, device type and time (up to 5000).
- Type words to search (`isis adj`); *Only VOSS* (the active tab's device type) narrows it down.
- **Enter** types the command in the active tab so you can check or edit it before pressing Enter; **Shift+Enter** / *Run* runs it; *To Command Window* puts it in the Command Window.
- The command is taken from the terminal line without the prompt, so commands completed with Tab or recalled with ↑ are remembered as the device shows them.
- Commands that may contain a secret - `password`, `secret`, `community`, `key-string`, `pre-shared`, tokens - are **never** remembered, nor anything typed at a password prompt. Global Options → General → *Remember typed commands* turns history off; the trash button on a command (or Shift+Delete) forgets that one command - for example one that was mistyped or that a device no longer supports; with *Only <device type>* ticked only on that device type. **Clear History…** asks which device types to forget (the active tab's is ticked; *All device types* forgets everything). History stays on this computer (`history.json` in the data folder).

![Command History](screenshots/history.png)

![Clear Command History](screenshots/history-clear.png)

**Suggestions in the Command Window**: after two characters the Command Window suggests commands from your history and the Command Library - those of the active tab's device type first, never another device type's. ↑/↓ chooses, **Tab** (or Enter on a chosen one) takes it, Esc closes; library commands with `{{values}}` ask for them.

![Command Window suggestions](screenshots/autocomplete.png)

**Command Library** (Script → Command Library…, `Cmd+E` / `Ctrl+Shift+E`)

![Command Library](screenshots/commands.png)

- Save commands in folders (per device type, troubleshooting, config snippets); several lines are sent one after another.
- `{{name}}` or `{{name:default}}` placeholders are asked for when sending, e.g. `show vlan {{vlan_id:10}}`.
- Send to the active tab, the selected tabs or all connected tabs; double-click sends. Library commands also appear in Quick Open.
- **For device types**: tick the device types a command is for (or name its folder after one, e.g. *VOSS / Troubleshooting*). In a tab of that device type its commands are listed first (*For VOSS (this tab)*), *Only commands for VOSS* hides those meant for other device types, and Quick Open ranks them first.
- **Commands ▾** in the Command Window lists the library, those for the active tab's device type first; the chosen command (with its `{{values}}` filled in) goes into the box so you can check it before pressing Enter.

**Paste safety**: pasting more than one line asks first and shows a preview (Global Options → General → *Confirm before pasting more than one line*). The paste line delay (Terminal & Appearance) paces slow devices.

**Compare Outputs** (Tab → Compare Outputs…, or right-click a tab → **Compare With…** and pick another open tab or a file): side-by-side diff of two tabs' scrollback or two files - e.g. `show running-config` before and after a change, or the same command on two switches. Options ignore whitespace, timestamps/uptime counters and - optionally - **prompts** (`core-sw1:1#` vs `core-sw2:1#`, editable pattern), or show only the differences.

![Compare With from the tab menu](screenshots/tab-compare.png)

![Compare Outputs](screenshots/compare.png)

**Timestamps and copying**: right-click a tab (or the terminal) → *Timestamps* prefixes each new output line with the local time (display only; logs have their own setting). **Copy as HTML (with colors)** (right-click in the terminal; Shift+right-click when right-click paste is on) copies the selected text with its colors and bold text, so you can paste colored output into e-mail, Word, Teams or a ticket; plain-text editors receive the plain text. Links printed by devices (including OSC 8 hyperlinks) are clickable.

![Terminal menu with Copy as HTML](screenshots/term-menu.png)

**Copy the output**: **Copy Scrollback** (tab menu, the terminal's right-click menu, File) puts the whole output on the clipboard as text; **Copy Scrollback as HTML Report** copies the same report as below - colours, highlighting and the device header - to paste into a mail or a document.

**HTML report of a session**: File → **Save Scrollback As HTML…** (also right-click the terminal or a tab) saves the whole scrollback as one web page: a header with the Termwright logo, the session, its address, the device's **model** (e.g. `5720-48MXW`), **OS version**, **serial number** and **MAC address**, the device type, protocol, user, connection details, the date and time it was saved and the number of lines, then the output with its colors, bold text and **keyword highlighting** exactly as in the tab. Open it in any browser, attach it to a ticket or e-mail, or print it to PDF. Model, version, serial and MAC are taken from output already on the screen (`show sys-info`, `show switch`, `show version`, `/etc/os-release`); when something is missing and the tab is an SSH or Telnet session with a VOSS, EXOS, Cisco or Linux device type, Termwright reads them in a background connection - nothing is typed in your tab. The MAC address saved by Discover Devices is used when the device does not show one. *Save Scrollback As…* still saves plain text. For a long session turn on a session log as well: the scrollback keeps the last 10,000 lines (Session Options → Terminal → *Scrollback lines*).

![HTML report saved from a tab](screenshots/scrollback-html.png)

## 8. Logging and capturing output

- **Start/stop a log**: toolbar *Log*, File → Log Session…, or the tab menu. The tab shows a `LOG` badge.
- **Automatic logging**: Session Options → Log File → *Start log upon connect*.
- **File name tokens**: `%H` host, `%S` session, `%P` port, `%U` user, `%R` protocol, `%Y %M %D` date, `%h %m %s` time, `%t` ms, `%%` percent. Example: `~/logs/%S-%Y%M%D-%h%m%s.log`. With no name, logs go to the Logs folder (Global Options).
- **Plain vs raw**: by default escape sequences and colors are removed; *Raw log* keeps everything exactly as received.
- **Line prefix**: e.g. `[%h:%m:%s] ` timestamps every line.
- **Save Scrollback As…** (`Cmd/Ctrl+Shift+S`) writes the whole scrollback to a text file; **Save Selection As…** saves only the selected text.

![Log File page](screenshots/log-file.png)

### Session recording and playback

A recording keeps everything a device showed **with its timing**, so a session can be replayed later - for audits, change records, training or "what exactly happened at 02:14".

- **Record a tab**: right-click the tab → **Record Session** (or File → Record Session). The tab shows a blinking `● REC` badge; *Stop Recording* ends it. One recording is made per connection.
- **Record automatically**: Session Options → Log File → *Record every connection*, Folder Defaults → *Recording*, or **Record every session** (File → Recordings… or Global Options → General).
- **Markers**: **Add Marker…** (`Mod+J`, or the tab menu) puts a note at this point of the recording, e.g. *CHG-1234 start*.
- What you **type is not recorded**, only what the device shows (passwords you type are not echoed by devices).
- Background runs (backups, checklists, Run and Collect) are never recorded.

![Recording a tab](screenshots/recording-menu.png)

File → **Recordings…** lists them by session and date with length, markers and size. **Play** opens a player tab:

- play / pause (`Space`), speed 0.5× to 8×, the seek bar, `←` / `→` 5 seconds, `Home` / `End`
- **Skip idle** shortens pauses longer than 1.5 seconds
- **Markers** jump to a marker; **Find text** jumps to the next point where a text appeared
- **Export Text…** saves everything the device showed as plain text; **Show** opens the file's folder

![Recordings](screenshots/recordings.png)

![Player tab](screenshots/player.png)

Recordings are saved in *Documents/Termwright Recordings/<session>/<date>_<time>.cast* (Global Options → General → *Recordings folder*), in the open **asciinema** format: they also play with the asciinema player and tools. *Delete recordings after N days* removes old ones (0 = keep them). *Play File…* opens a `.cast` file from anywhere.

### Configuration backups

File → **Configuration Backups…** keeps dated copies of the configuration of switches, routers and firewalls, and shows what changed.

![Configuration Backups](screenshots/backups.png)

- **Backup jobs** (left): *+ New backup job*, a name, the **sessions** - folders (e.g. *Datacenter*, every session inside) or single sessions; *Add Selected* takes what is selected in the Session Manager.
- **Schedule**: *On demand* (Run Now), *Every day* or *Monday to Friday* at a time. Scheduled jobs run while Termwright is open; a run that was missed (Termwright closed at that time) starts when it opens.
- **Keep** N copies per device (default 30, oldest removed). By default a copy is saved **only when the configuration changed** - lines that change on every run (timestamps in comments, *Last configuration change at …*, *ntp clock-period* …) are ignored; tick *Save a copy every run* to keep one each time.
- **Commands**: each device type's **Backup commands** (Global Options → Device Types; EXOS `show configuration`, VOSS and Cisco IOS `show running-config`). Sessions without a device type use the job's *Commands*.
- **Run Now** backs up four devices at a time, in the background - no tabs open. The login is the one the session uses (device type, saved passwords, keys, jump hosts), but **nothing is asked**: a device whose password is not saved, or whose host key was never accepted, is reported as failed (*no saved password for admin@10.10.7.7*) - connect to it once and save the password.
- The table shows each device's **last backup**, the **result** (*no change*, *changed*, or the error - with a *device output* link to what the device sent, also saved as `last-error.txt`) and the number of copies. **What Changed** compares the newest copy with the one before in Compare Outputs; **Open** opens the copy, **Show** shows it in Finder / Explorer.
- Copies are saved in *Documents/Termwright Config Backups/<session path>/<date>_<time>.txt* (*Change…* picks another folder, e.g. a shared drive or a Git repository).
- **From the Session Manager**: right-click a session, a folder or a selection → **Back Up Configuration(s)** runs a one-off backup.

![Backing up from the Session Manager](screenshots/backups-menu.png)

![What changed between two backups](screenshots/backups-diff.png)

### Change checklists

File → **Change Checklists…** (or right-click a session, folder or selection in the Session Manager → **Change Checklist…**, or Quick Open → *checklist*) runs the same show commands on a set of devices **before and after a change** and tells you which devices changed.

1. **+ New checklist**: a name, the **devices** (folders or sessions, *Add Selected* takes the Session Manager selection) and the **commands per device type** - e.g. VOSS `show isis adjacency`, `show vlan basic`, EXOS `show vlan`, `show ports no-refresh`; *Any other device* covers the rest. New checklists start with suggestions for the built-in device types.

   ![Checklist setup](screenshots/checklist-setup.png)
2. Before the change: **New Change: Take Before** (optionally with a ticket number or note). The commands run on every device in the background, four at a time, with the session's usual login - nothing is asked; a device whose password is not saved is reported as failed with its *device output*.
3. Make the change.
4. **Take After**: the same commands again. Each device is marked **same** or **changed** - times of day, dates and uptime counters are ignored - and **Compare** shows before and after side by side (*Before* / *After* open the files).

   ![Checklist after a change](screenshots/checklist.png)

   ![Before and after of one device](screenshots/checklist-compare.png)

- Each change is kept: pick an earlier one in *Change* to look at it again; *Retake Before* repeats the before snapshot of the selected change.
- The outputs are saved in *Documents/Termwright Checklists/<checklist>/<date>_<time>/before|after/<session path>.txt* (each command under a `### command` line).

### Run and collect

File → **Run and Collect…** (or right-click a session, folder or selection → **Run and Collect…**, or Quick Open → *collect*) runs commands on many devices and puts values from their output in **one table** - an inventory of models, versions, serial numbers and uptimes, or anything a pattern can find.

1. **+ New collection**: the **devices** (folders or sessions), the **commands per device type** and the **columns**. New collections start with inventory commands and columns (*Model*, *Version*, *Serial*, *Uptime*) for the built-in device types; **+ Inventory Columns** adds them later.

   ![Run and Collect setup](screenshots/collect-setup.png)
2. A column has a **name**, a **device type** (or *Any device*) and a **pattern** (a regular expression, case-insensitive; the first `( )` group is the value, e.g. `Serial#\s*:\s*(\S+)`). Several rows with the same name and different device types fill one column - each device type has its own pattern.
3. **Run Now**: the commands run in the background on every device, four at a time, with the session's usual login (device type, saved passwords, keys, jump hosts, console servers) - nothing is asked.

   ![Run and Collect results](screenshots/collect.png)

- **Copy Table** copies it for a spreadsheet, **Export CSV…** saves a `.csv` file; *Output* opens a device's full output, *Device Output* what a failed device sent.
- Editing the columns updates the table at once - there is no need to run again. Earlier runs stay in *Run*.
- The outputs are saved in *Documents/Termwright Collected/<collection>/<date>_<time>/output/<session path>.txt*.

## 9. File transfer

### SFTP browser

![SFTP tab](screenshots/sftp.png)

Open with the SFTP button in the title bar, **Transfer → Open SFTP Tab** or `Mod+P` while an SSH2 tab is connected. It reuses that login (no second password).

In the modern layout the bar at the top shows the session, **Run as ▾**, the terminal-folder button, and the copy protocol (**SFTP / SCP**). Between the panes are the copy arrows (→ upload, ← download), **Sync** → / ← (make the other side match) and **Diff**: select two files - one on each side, or both on the same side - and Diff opens **Compare** with both files side by side, with line numbers and the options to ignore whitespace and timestamps. The bar at the top says whether the files are identical (green) or how many lines differ. Each pane has a **path you can click** (each folder opens with one click; click beside the path to type one), ↑, Refresh, **New Folder**, **New File**, **Delete** and the bookmark star, a **bookmark bar** below it, and a `..` row. Files show a coloured type badge (CFG, LOG, IMG, TXT, KEY …), folders are amber, and *Modified* reads *Today 09:41* or *12 Oct* (hover for the full date and permissions).

![SFTP Compare](screenshots/sftp-compare.png)

**Working directory detection** (SFTP tab opened from a terminal tab):

![Working directory detection](screenshots/sftp-cwd-hint.png)

Working directory detection is **on by default**. When you open an SFTP tab from a terminal tab (toolbar SFTP button, Transfer → Open SFTP Tab, or the tab's right-click menu), Termwright checks over a separate channel that the login shell is **bash or zsh**, and if the terminal is sitting at a prompt it turns folder reporting on in that shell for you - the typed line is hidden, the screen stays as it was. From then on the SFTP tab opens in, and follows, the terminal's folder. This also works **through a jump station**, e.g. an AWS host reached through a bastion: open *SFTP Tab on <target> (through this login)* and the check and the folder reporting happen on the target. Network devices are left alone (their CLI is not bash or zsh). **The terminal's user is followed too:** after `sudo su -` (or `sudo -i`) in the terminal, the SFTP tab switches to browsing **as root** (through sudo, like *Run as ▾ → root*) and opens root's folder; after `sudo su - oracle` it browses as `oracle` (*sudo su - oracle*); after `exit` it goes back to the login user. If sudo asks for a password, Termwright asks for it once. Each part can be turned off in **Options → Global Options → General → SFTP from a terminal**: *Working directory detection*, *SFTP tabs follow the terminal folder* and *SFTP tabs browse as the terminal's user*. The **>_ Terminal ▾** button in the server pane offers:

![Terminal folder menu](screenshots/sftp-terminal-folder.png)

- **Go to Terminal Folder** - jump to the folder the shell is in now.
- **Jump station sessions**: an SFTP tab browses either the jump station or the target. **Open SFTP Tab** (the SFTP button, ⌘P) opens it on the host the terminal is on - normally the target; right-click the terminal tab → Open SFTP Tab on … chooses. It follows the terminal only while the terminal's shell is on that host - Termwright reads the host from the shell's folder report, so after `exit` from the target the jump station's SFTP tab follows again. When the terminal is on the other host, the tab shows **>_ On <host> ▾** with *Switch This Tab to <host>*.
- **SFTP sessions with their own login** (Connect SFTP Session) follow an open terminal tab of the same session, as a tab opened from the terminal does.
- **Becoming root on a jump station**: when you run `sudo su -`, `sudo -i`, `su` or start a new shell in the terminal, the SFTP tab that follows it reads the folder and user again once the new prompt appears, so an SFTP tab opened before becoming root follows root.
- **Follow Terminal Folder** - the SFTP pane changes folder whenever you `cd` in the terminal (the button then reads *Following*). On by default; switch it off here for this tab, or for all tabs in Global Options.
- **Open This Folder in the Terminal (cd)** - the reverse: the terminal changes to the folder shown in SFTP. Also on the right-click menu of a remote folder.
- **Enable Folder Detection in This Shell** - types one line into the terminal (bash or zsh) that makes the shell report its folder after every prompt, until you log out.
- **Enable Folder Detection Permanently…** - after a confirmation, adds the same line to `~/.bashrc` (or `~/.zshrc`) on that server, once.

If the shell still does not report its folder (detection turned off, another shell, or the terminal was busy), a blue *Working directory detection* bar offers the same two buttons; close it with ✕ (remembered). Termwright understands the standard escape sequences OSC 7 (`file://host/path`), OSC 1337 `CurrentDir=` / `RemoteHost=user@host` and OSC 633 `P;Cwd=`, so shells already set up for iTerm2, VS Code, WezTerm or Tabby work as they are. For **fish**: `function _tw_cwd --on-variable PWD; printf '\e]7;file://%s%s\a' (hostname) $PWD; end` in `~/.config/fish/config.fish`. Network devices (VOSS, EXOS, …) do not report a folder. 

Folders use the same colored folder icon as the Session Manager. Files are colored by type so they stand out at a glance: text and configuration files blue, archives amber, images pink, firmware and binaries purple, scripts green, keys and certificates red, everything else gray. Symbolic links carry a small arrow, and hidden (dot) files are dimmed.

**Standalone SFTP session** (its own login, no terminal): right-click an SSH2 session in the Session Manager → **Connect SFTP Session**, or right-click a terminal tab → **Connect SFTP Session (new login)**. Use this for devices such as VOSS that refuse a second channel on an existing SSH connection. To make a session always open this way, set Session Options → Connection → **Connect as: SFTP file browser only**; right-click → **Connect Terminal** still opens a shell. The tab right-click menu also offers **Open SFTP Tab (same login)**.

- Left pane = this computer, right pane = the server. Type a path and press Enter, use ↑ (parent), Backspace, or double-click folders.
- **Keyboard**: click in a list, then ↑ / ↓ move (Shift+↑ / ↓ selects a range), Home / End and Page Up / Down jump, Enter opens a folder or file, Backspace goes up, Delete deletes.
- **Select several files**: tick the **check boxes** (they appear when you point at a row, and on every row once something is selected; the box in the header selects all or none), or Ctrl/Cmd+click, Shift+click for a range, Ctrl/Cmd+A. The footer shows how many are selected with *Clear*. **→** uploads, **←** downloads, double-clicking a file transfers it to the other side. Whole folders are copied recursively.
- **New File** (page-plus icon) makes an empty file in the pane's folder - on this computer or the server; it never replaces an existing file. **New Folder** next to it.
- **Delete** (bin icon, the Delete key, or right-click): on the **server** the selected files and folders are deleted after you confirm (folders with their contents). On **this computer** they are moved to the **Trash** (Recycle Bin on Windows), so they can be put back.

  ![New File](screenshots/sftp-newfile.png)
- Right-click (server side): Download, Rename, Delete, New Folder, Refresh. Right-click (this computer): Upload, Move to Trash, New Folder, Refresh.
- **Sync ⇉ / ⇇** copies only new or changed files (size or newer time) from the current local folder to the current remote folder or back. Nothing is deleted.
- The **transfer bar** at the bottom shows the current copy with progress, speed and time left, *Cancel*, and *N more* for the other transfers (Resume and Clear for finished ones). **✕ closes it** - copies that are running go on in the background (the status bar still shows them) and the bar comes back for the next transfer.
- **Bookmarks**: the **bookmark bar** under each path lists the bookmarks of that side - click one to open it, right-click to remove it, **+ Bookmark** saves the current folder (the ☆ in the header does the same and lists them). Server bookmarks belong to their session; local bookmarks are shared by all sessions.
- **Server-to-server copy**: with two SFTP tabs open (e.g. two switches), right-click files or folders on one server → **Copy to <other server>: <its current folder>**. The data streams through this computer; nothing is stored locally. The ⇄ entry in the queue shows progress.
- **Copy via SFTP / SCP** (the SFTP / SCP selector in the top bar - between the panes in the classic layout; the choice is remembered per session): browsing always uses SFTP, but → / ← (and double-click, Upload/Download in the right-click menu) can copy with **SCP** over the same login - useful when a device's SFTP server is slow or limited. Folders are copied recursively; the queue shows `↑ SCP` / `↓ SCP`. Sync, resume and server-to-server copy stay SFTP.

  ![SFTP tab copying with SCP](screenshots/sftp-scp.png)
- **Run as another user** (*Run as ▾* in the server pane header): browse the server as root or any other user, like switching user in the terminal.
  - **root (sudo)**: starts the SFTP server with `sudo` over the same connection and opens `/root`. If sudo asks for a password you type it once for the tab (kept in memory only).
  - **Other Command…**: type the command you would use in the terminal - `sudo su - oracle`, `su - admin`, `sudo -iu postgres`, `pbrun …`. Termwright runs it on a terminal, shows every **password prompt** it prints (sudo, su, one-time codes) so you can answer, waits for the new shell, then replaces that shell with the SFTP server - so files are read and written as that user. The last six commands are listed in the menu for the session.
  - The button turns red (*as oracle ▾*), the header reads *oracle @ host*, and **Back to &lt;login user&gt;** returns to the normal login. Works on a host behind a jump station too.
  - Requirements: the OpenSSH `sftp-server` program on the host (standard on Linux) and a POSIX shell for the user. Errors (wrong password, *not in the sudoers file*, *must be run from a terminal*) are shown with the command's last output. Copies use SFTP in this mode (*Copy via SCP* is disabled).

  ![Run as menu](screenshots/sftp-runas-menu.png)
  ![Run as command](screenshots/sftp-runas-cmd.png)
  ![Browsing as another user](screenshots/sftp-root.png)
- **Resume** (⟳ on a failed or cancelled SFTP copy): restarts the same copy; partially copied files continue from where they stopped and files that are already complete are skipped.

### Files panel beside the terminal

Choosing a **Linux** device type (Linux, Ubuntu, Debian, RHEL, Rocky, …) for an SSH session turns the Files panel on for it; untick it in Session Options if you do not want it.

![Files panel](screenshots/files-panel.png)

A small file browser beside an SSH terminal, on the side opposite the Session Manager (with the Session Manager on the right it sits on the left). Turn it on for a session with the **Files panel beside the terminal** switch in the session side panel or in Session Options → Connection, or for one tab with **View → Files Panel**, the title-bar button, the tab's right-click menu or ⌘⇧1 (Alt+Shift+1).

- **Follows the terminal**: the panel shows the folder the shell is in (bash or zsh report it; Termwright turns this on, as for SFTP tabs). *Following* in the header turns it off and on. Through a jump station it opens on the host the terminal is on; when the terminal moves to the other host, the panel says so and *Show … here* reopens it there.
- **Follows the terminal's user**: after `sudo su -` (or `sudo -i`, `su - oracle` …) the panel browses as that user too - a red **root** badge shows it - so `/root` and other protected folders open instead of *permission denied*; after `exit` it browses as the login user again. It uses `sudo` on that host (asking for the password if sudo needs one), also through a jump station. ⋯ → *Browse as root (sudo)* / *Browse as …* switches by hand; Global Options → General → *SFTP tabs and the Files panel browse as the terminal's user* turns this off.
- **And the other way round**: open a folder in the panel (double-click, the path, ↑ or ..) and the terminal `cd`s there - only when the shell is at a prompt, so nothing is typed into a running program. ⋯ → *cd the Terminal When I Open a Folder Here* turns this off.
- **Open a file on this computer**: double-click a file (or right-click → Open). It is copied to a private temporary folder and opened with the program your system uses for its type. When you save it, the panel asks **Upload it to …?** - *Upload Now*, *Not Now* or *Stop Watching*. Programs and shortcuts (.exe, .bat, .app, .command …) are never opened from a server; use Download.
- **Upload** (choose files), **Download…** (choose a folder), **New Folder**, **Rename**, **Delete**, **Copy Path**, **cd Here in the Terminal**, *Show Hidden Files*, and a button that opens a full SFTP tab here. Drag the panel's edge to make it wider.

![Files panel on the left](screenshots/files-panel-right.png)

### SFTP through a jump station (bastion) - how the files travel

Many cloud hosts can only be reached from a jump station, often with a private key that is stored **on the jump station** (a session whose remote command is `ssh -t -i ~/.ssh/aws_key user@aws-host`, see [§10](#10-automation-logon-actions-triggers-scripts)). For such a session, *Open SFTP Tab on user@aws-host (through this login)* browses the AWS host directly:

```mermaid
flowchart LR
  PC["Your computer<br/>Termwright"] -- "SSH connection<br/>(your login to the jump station)" --> J["Jump station<br/>runs: ssh -T -o BatchMode=yes -i key -s user@aws-host sftp"]
  J -- "SSH from the jump station<br/>(key stays on the jump station)" --> A["AWS host<br/>sftp-server<br/>(optionally via sudo / Run as)"]
```

- **One live pipe, no copies in between.** Termwright opens one extra channel on the SSH connection it already has to the jump station and runs the jump station's own `ssh` there to start the SFTP server on the AWS host. Uploads and downloads are streamed through that pipe: **from the AWS host, through the jump station's memory, straight to your Desktop** (and the reverse for uploads). Nothing is written to disk on the jump station, and no temporary copy is needed.
- **No second login, no key copying.** Your computer never sees the AWS key; the jump station uses it exactly as when you type the command in the terminal. Your existing login (password, MFA) is reused.
- **Speed** is limited by the slower of the two links (your computer ↔ jump station, jump station ↔ AWS host); both are encrypted SSH.
- **Copy via SCP** in this tab also goes to the AWS host through the same pipe (`scp -t` / `scp -f` run on the AWS host).
- **Run as / root** (*Run as ▾* in the header, below) switches the AWS side to root or another user; the pipe is the same, only the SFTP server on the AWS host runs as that user.
- The header shows *user@aws-host via jump-station*, and the status bar *SFTP to … through …*, so you always know which machine you are looking at. Both machines may have the same home path (e.g. `/home/you`); the listing is the AWS host's.

![SFTP on the AWS host through the jump station](screenshots/sftp-hop.png)

### ZMODEM (sz / rz)

- **Download**: on the remote host run `sz file1 file2…`. Termwright detects it and saves the files to your **Downloads folder** (Global Options → General); a notification shows where.
- **Upload**: run `rz` on the remote host; Termwright opens a file picker. Cancel aborts the transfer cleanly.
- Requires `lrzsz` on the remote host. Works over SSH, Telnet, serial and local shells.

### XMODEM (serial / boot loaders)

- **Transfer → Send File with XMODEM…**: start the device's XMODEM receive command first (or right after). Choose 1K blocks (XMODEM-1K) or classic 128-byte blocks if the receiver is old.
- **Transfer → Receive File with XMODEM…**: choose where to save, then start the send on the device. Trailing padding (0x1A) is removed.

### YMODEM (serial / boot loaders)

- **Transfer → Send Files with YMODEM…**: pick one or more files, then start the receiver on the device (`rb`, or the boot loader's `loady`). File names and sizes are sent in the batch header.
- **Transfer → Receive Files with YMODEM**: start the send on the device (`sb file…`); files land in your **Downloads folder**.

### SCP

To copy with SCP while browsing with SFTP, set **Via: SCP** in the SFTP tab (above). For devices that offer SCP but not SFTP: **Transfer → SCP Upload File…** or **SCP Upload Folder…** (pick, then the remote folder/path) and **SCP Download…** (remote file *or folder* path, then a local folder). Folders are copied recursively (`scp -r`).

### Simulated lab

**Transfer → Labs (Simulated, Emulated)…** (or the flask and cube buttons at the top right) opens the Lab window; its *Simulated lab* runs simulated switches on this computer - for trying commands, training, testing scripts, backups, Network Check or the AI agent without real hardware. Each switch is a small SSH server on `127.0.0.1` (a few MB, no virtual machine) with a command line: abbreviations, `?` help, Tab completion, error messages, VLANs, ports, IP interfaces, save, reboot, and ping, traceroute and LLDP between the simulated switches over the cables you draw. It is **not** a real operating system: protocols (STP, LACP, OSPF…) are not run.

- **Device packs**: Termwright itself carries no switch models. What a switch is - its models, port layout, operating system, commands, outputs, error messages, serial number and MAC formats - comes from a **device pack**, one `.vm` file. **Load Device Pack (.vm)…** in the Lab window shows the pack's name, version, publisher, contents, terms and SHA-256 checksum; it is loaded only after you tick *I am allowed to use this pack and agree to its terms*. The pack is copied to Termwright's data folder on this computer, never sent anywhere, and can be removed or replaced by a newer version (*Load another…*). A lab whose pack is missing keeps its switches until the pack is loaded again.
- **Port layout from the model**: the pack names the port types, and a model's name sets its ports - for example a model `…-16MW-8T` gets 16 ports of type *MW* and 8 of type *T*, and `…-48MXW` gets 48 ports of type *MXW*. Uplink port types come after them.
- **New Lab…** walks through four steps: where it runs (this computer, or a Linux KVM host - see [Emulated lab](#emulated-lab-real-vm-images-on-a-kvm-host)), which switches (family, model, its operating system when the model runs more than one, how many, names like `lab-{n}`, management network (`10.99.0.0/16` by default, room for the 500 switches a lab holds; a lab made with the old `/24` grows to the `/16` when it is full), an optional password - without one any password is taken), the cabling (**Leaf-spine** with the first two switches as the core pair, **Chain**, **Ring** or none, with a preview; switches added to a lab go on from its last switch in a chain or ring, and new names are numbered on past the names in use) and a review.
- Every switch gets a session in the **Simulated Lab** folder (address `127.0.0.1` and its own port, the device type the pack names, user `admin`); its SSH host key is trusted already. The folder shows how many switches run and has **Start Lab**, **Stop Lab**, **Open Lab Window…** and **Add Switches…** on its right-click menu; each session has a dot for running or stopped.
- The **Lab window** lists the switches (state, logins, management address, Connect, Start / Stop), draws the **Topology** (drag to arrange; click a switch for its details, cables and configuration; double-click to connect) and lists the **Cables** (up or down). **Cable** connects two switches: click them in the topology, then pick the ports. **Remove** removes switches with their sessions.
- A switch keeps what was saved and forgets the rest when it stops or reboots - like a real one. The lab starts again with Termwright when it was running.
- **Configuration the pack does not model is kept** when the pack says so: the command is taken, kept per interface and shown in the configuration, so scripts and templates run without errors. The simulator does not *run* what they configure.
- **Serial numbers and MAC addresses** are made in the pack's format per model and can be changed: ✎ next to the MAC in the Topology inspector, or **Serial Numbers…** for a list (one per line, given in order to the ticked switches or all of them). *New in the Model's Format* makes new ones in the pack's format (for example to replace the placeholder serials of an older lab).
- A switch whose session was deleted (or a lab from an older version) gets its session again when the lab starts or the Lab window opens.
- **Command Outputs**: paste the output of a show command from a real switch and the simulator prints it with each switch's values filled in: `{hostname}`, `{serial}`, `{mac}`, `{mgmtAddr}`, `{model}`, `{version}`, `{uptime}`, and from its configuration `{word:prefix|default}`, `{line:prefix|default}` or `{if:prefix|yes|no}`.
- In-band ping works when a VLAN with the same tag reaches the other switch over enabled, cabled ports; the management addresses answer in the management VR / VRF the pack names.

**Making a device pack**: a `.vm` file is a zip with `pack.json` (id, name, version, publisher, terms), `ports.json` (port types), `models.json` (models, port groups, serial and MAC formats such as `AB{YY}{WW}{N:4}`), and per operating system `os/<id>/os.json` (prompts, modes, commands with their actions and outputs, error messages) with Go text templates for the outputs. `tests/*.txt` are transcripts (`> command` and the expected output) that `TW_PACK=<pack> go test ./internal/sim -run TestPackFromEnv` checks. A small example is in `internal/sim/testdata/demo`.

![Simulated lab](screenshots/sim-lab.png)

### Emulated lab (real VM images on a KVM host)

The **emulated lab** (Lab window → *Emulated lab (KVM host)* at the top right) boots the vendors' **own VM images** - the free `.qcow2` images several network operating systems offer - with QEMU/KVM on a **Linux server you reach over SSH**. The real operating system runs, so protocols, APIs and cloud agents work as on a real switch. Your computer only drives the host; nothing of Termwright is installed there.

**The lab workspace.** The cube button at the top right (and the flask for the simulated lab) opens the lab as a **tab**:

- **Left - the palette:** the devices the images can be (grouped by OS, with the version and modules), other images and, in the simulated lab, the device pack's models. Drag one onto the canvas, or double-click it, to add a switch (serial number and MAC in the model's format); on an empty lab it is made on your KVM host straight away when there is one host (or the lab's), else the New Lab panel opens with that device.
- **Centre - the canvas:** each switch with a state dot (green running, amber booting, red a problem, grey stopped), its model and address, and a *persona ✓* or *no persona* badge. Drag switches to arrange them. Cables show the port at each end: green when up, dotted when the switches are off, red and dashed when pulled. Ports without a cable have their link down in the switch (as in GNS3), so only cabled ports show up as active. Right-click a switch for Start, Stop, Reload, Console, SSH, Configure…, Add Link from Here, Back to Day 0 and Delete; right-click a cable to pull it, plug it in or remove it. **Add Link** in the toolbar: click a switch, pick its port, click the other switch, pick its port (Esc cancels). Zoom with − / + and *Fit*.
- **Right:** the selected switch (model, state, persona and where its file was written, serial number, MAC, management address, console, resources, problems) with Console, SSH and Configure…; the topology summary; the host's vCPU and memory.
- **Bottom - Events:** everything Termwright does to each switch, with the time and the host it happened on (with the host's colour): started, persona written (on which partition) or why not, booted, day-0 typed, persona checked, the address it got, stopped, and any error in red. Click a name to select the switch; the second tab shows the end of the selected switch's console. Drag the pane's top edge (or ▴) to make it taller; **Copy** puts the log (or the console end) on the clipboard and **Clear** empties the log. A failed login or day-0 says what the console showed at that moment; a missing persona says what the check command printed.
- **Toolbar:** in the simulated lab, the device pack it runs and **Load Device Pack (.vm)…** (with the consent step); Start All, Stop All, Reload, Add Link, Console All, Add Switches (New Lab when it is empty), Hosts, Images and Templates, the **Management** of the lab and how many switches run, boot, failed or are stopped.
- **New Lab / Add Switches** opens a panel on the right while the canvas shows the switches it will make, dashed: the KVM host - your hosts as cards with their free vCPU, memory and disk (the one with the most free memory first; the chosen one is marked), the management (the best one for that host is ticked, with what it means), one card per device with − / + and names, the module, the first serial number and MAC address (filled in in the model's format, ↻ for new ones), the cabling (leaf-spine, chain, ring, none), what the host has free, and *Start now*. Added switches go on from the last switch in a chain or ring.
- **Configure…** (right-click a switch, or the button on the right) has the tabs *General*; *Persona and Serial* - the device model, standalone or a stack with its slots (model, serial number, module, primary, port map), the persona's values, the base MAC, *↻ New Serials in the Model's Format*, a live preview of the persona file and whether it is in the switch (where it was written, what the check saw); *Resources* (vCPU, memory, disk); and the image's network, day-0 and options. **Save and Apply** writes the persona file and restarts the switch.
- **Add Link** shows the switch's **front panel**: free ports grey, cabled ones green (hover to see where they go), *Next Free Port*.
- **Images** lists the image files by OS and version (what was recognised from the file name, where the file is, whether it is on the host) with the editor: boot (vCPU, memory, disk bus, ports, port names, file), console and first login (ready text, login and password, answers, day 0, address command, device type, QEMU options) and the persona (file in the VM, check command after boot, models, modules). An image's **Files** lists every file it boots with - its disk, more disks, a CD-ROM, a BIOS - with its size, whether it is on this computer, and *Choose…* (or the maker's download page); *Download* fetches the ones with a link (a zipped file is unpacked). Each switch writes to a layer of its own on every disk; a CD-ROM and a BIOS are only read. **Templates** and **Profiles from the device pack** are tabs beside it. Memory is in MB (with the GB it makes), disks in GB (0 = the image's own size), and the disk type (IDE for most network images, SATA, SCSI, virtio) is shown with each image. *More QEMU options* go onto the QEMU command line as they are - for example `-cpu host`, `-machine q35`, `-smp sockets=1,cores=2` or `-device virtio-rng-pci`. A template's first serial number and MAC address are filled in in the model's format, with ↻ for new ones.
- **GNS3 appliances** (*GNS3 Appliance…*): every version in the file becomes an image with its version (from the appliance, so `NOSGNS3.9.4.0.0.qcow2` is 9.4.0.0), size, MD5 sum and download link. Image files next to the appliance file (or downloaded before) are found by themselves; **Download All** (or *Download* on one image) fetches the others to this computer one after the other and checks their MD5 sums: the button fills up as the download goes, turns green when it is there, and the folder they go to is shown; the Events log says when each is there. The **Downloads** box lists what is downloading and what waits, in order: *Next* (⇡), *Earlier* (↑) and *Later* (↓) change the order, ✕ stops a download or takes it out of the queue, and *Stop all* stops them all. Files packed as `.gz`, `.xz` or `.bz2` are unpacked before the MD5 sum is checked (the registry's sum is of the unpacked file). An appliance image the device pack knows by its file name gets the pack's profile.
- **Public appliances** (a tab of Images): ready-made settings from the public GNS3 registry - open-source routers, firewalls and routing suites, Linux switch OSes and switch VMs that can be downloaded freely, small Linux hosts and a traffic generator, and a few vendor lab images you get from the vendor with your own account. **Add** fetches the appliance from the registry and puts its versions in the image list (newer registry files with several settings are read too, with the default login); then **Download All**, or, where the maker wants you to sign in, *Open the download page* and choose the file. An added appliance shows how many of its images are in the list and **Remove**, which takes them out of the list and deletes the files Termwright downloaded for them (files you chose yourself stay; it is refused while a switch or template uses one). **SONiC-VS** (the community SONiC build, versions 202411 and master, login `admin` / `YourPaSsWoRd`) is downloaded from the SONiC build server and unpacked. Appliances with a second (third, fourth) disk, a CD-ROM (for example a cloud-init disk for cloud images) or a BIOS file are listed too; container appliances are not (they do not run on a KVM host).
- **Hosts** lists the KVM hosts with their state, the selected host's vCPU, memory and disk, what it has (QEMU, KVM, sudo, libguestfs, NAT network) and how its switches can be managed (the best one marked), and **Set Up QEMU/KVM** with its steps and live log. *+ Add a Host* takes one of your SSH sessions.

- **KVM Hosts**: pick the SSH session of a Linux server and *Check and Add*. Termwright logs in and checks KVM (`/dev/kvm`), QEMU (`qemu-system-x86_64`, `qemu-img`), free vCPU, memory and disk, and password-less sudo, and says what to fix. **Set Up QEMU/KVM…** (also in New Lab when the check finds a problem) fixes it for you: it shows what it will run, asks for the sudo password when sudo needs one (it goes to sudo on the host only and is not saved) and then, as root, installs QEMU from the distribution's own packages (Ubuntu and Debian `qemu-system-x86 qemu-utils`, Fedora, RHEL / Rocky / Alma, openSUSE, Arch), loads the KVM module when `/dev/kvm` is missing, and adds the user to the `kvm` group - with a live log, and the host checked again afterwards. The switches still run as that user, not as root. Without CPU virtualization (no `/dev/kvm`) the VMs run in software emulation, many times slower.
- **Images**: *Add Image (.qcow2, .img, .vmdk)…* adds an image file of this computer; *Import GNS3 Appliance (.gns3a)…* reads the public appliance files for the settings (and takes the image file when it lies next to the appliance file). Each image has a profile: device type (for the sessions), vCPU and RAM, how many network ports and of which kind (`e1000`, `virtio-net-pci`…; the first port is the management port), port names (`{n}`, `1/{n}`, or counting in steps: `Ethernet{n*4-4}` gives Ethernet0, Ethernet4, …), disk type, what the console shows when the switch can be logged in to (*Ready when*, e.g. `login:`), the login and password (kept in the keychain; the switches' SSH sessions use them, an empty password too, rather than the device type's login), the prompt, the **first-boot (day-0) commands** with `{hostname}` `{mgmtIp}` `{mgmtAddr}` `{mgmtMask}` `{gateway}` `{n}`, **answers** to questions on the console (`[y/N] => n`) and more QEMU options. An image is copied to the host once (`~/termwright-lab/images`, checked by its SHA-256); every switch boots from a small copy-on-write disk on top of it.
- **The order of work** is the tabs': **1 KVM Hosts** (check and set up the host), **2 Images** (the image files, with their version), **3 Templates** and **4 Switches**. A bar above them shows the host: switches running, vCPU and memory they take against the host's, the memory free now, CPU use and load - a stopped switch frees its vCPU and memory at once.
- **Templates**: a template is a device to make switches from - an image, its vCPU, memory and disk size, the model it acts as (when the image has a persona), a module, and where serial numbers and MAC addresses start. Each switch made from it takes the next serial number (the last number counts up: `…-40036`, `…-40037`) and the next block of 64 MAC addresses. *Add Switches from It…* opens New Lab with the template.
- **Personas**: an image profile can bring a *persona file* - a file the image reads at boot to act as another device (its model, serial numbers, modules, stack members). Termwright writes it into the switch's disk with libguestfs on the host while the switch is off (Set Up installs `libguestfs-tools`), then boots it; when the folder only appears on the first boot, it boots once, powers off, writes the file and boots again. The file goes on the partition the image mounts at that folder - from the image's `/etc/fstab`, a partition the profile describes by what is at its top (*fsHas*, for images that mount it from their own start scripts), or a partition labelled like the folder - never into an empty mount-point folder on the root filesystem, and is read back. When none fits, the Events log lists the disk's filesystems with what is at their top. When the profile has a *check* command (for example `show version`), Termwright runs it on the CLI after each boot: a switch that still boots as the plain image shows **running · no persona** with the reason; the switch's panel shows where the file was written and what the check saw. Click a switch in the Switches tab to change its **resources** (vCPU, memory, disk - they apply when it starts next; a disk only grows) and its **persona**: model, serial number and module per slot, stack members with their primary and port mapping (`10:1` = front-panel port 10 on the VM's port 1), and the persona's values; **Save and Apply** writes it (a running switch is powered off for it). The first slot's serial number is the switch's. Image profiles with personas and templates come from a file: **Images → Import Profiles (.json)…**.
- **The workspace.** The header has the topology, the page tabs (Canvas, Hosts, Images, Templates), the health bar (switches up, booting, with a problem, off), the **hosts** button (each host's switches, vCPU, memory and load, its management, the cables between hosts, Add a Host to this Topology) and Start/Stop All. The tools float at the bottom of the canvas: Add Switches, Add Link, Restart (the selected switch), Console All, Check Cabling, zoom and the drawing style. The status bar at the bottom shows each host (KVM, management: click to change), what is happening now, downloads, images, vCPU and memory in use, and the *Fast start* switch.
- **The topology view.** The bar at the top right of the canvas: zoom (− and +, and a menu with *Fit*, *Fill* and 50-200 %; Ctrl/⌘ with the wheel zooms too); *Hosts* (Auto, Areas, Tags, Off: areas round each host for a few hosts, colour tags and a host list to focus one host when there are many); *Arrange by Host*; *Ports* and *Addresses* (labels on or off); *Only problems* (dims what is fine); *Map* (the small map at the bottom right: click to go there).
- **Problems and the Events log.** The side panel shows a switch's problem in three lines at most; its tooltip has the whole text, and **Details in Events ›** opens the Events log, where everything Termwright did to the switches is listed. The log is kept (its last 300 entries) when Termwright starts again; **Clear** empties it.
- **Adding switches the easy way**: in New Lab, each row is a **device** - a model the images can be (for example a model and its OS name, from the persona of the image profiles) or an image as it is - with the **software** to run (the image files of that kind, newest version first; *Add Image File…* when there is none yet), how many, and names. Below it: the module, the **first serial number** and **first MAC address**, filled in with values in the model's real format from the device pack (*↻ New* makes others), and the resources, which come from the model. No field starts empty. An image file is recognised by its name with the device pack's patterns (its kind and version, for example `vm-33.4.1.15.x86_64.qcow2`), and its profile is set up for you.
- **New Lab…** (emulated): the KVM host with its capacity (*Fits about*), the switches (image, how many, names, vCPU and RAM each, with meters for what the host has free), the management and the cabling. **Management** is always the switch's first port: on **the host's network (bridged)** - the default when the host has password-less sudo - it joins the host's bridge (for example `br0`) or, when the host has none, the network of its port through macvtap (the port is put in promiscuous mode, as GNS3's cloud node does) (the host's own network set-up is not changed; not over Wi-Fi), and gets its address from your DHCP server, so this computer and cloud services reach it directly. Termwright learns the address on the console after each boot with the image's *Address command* (for example `show vlan Mgmt`) and points the switch's SSH session there. On a network without DHCP, the bridged host network takes **fixed addresses** too (a first address and the gateway, typed on the console at each start). **The host's network, routed (cloud)** works where bridging does not - a host that is itself a VM whose virtual switch drops other MAC addresses, or a host on Wi-Fi: the switches get fixed addresses on the host's own network, from the *first switch's address* on (pick free ones), with its gateway; Termwright puts them on a bridge of the host and the host answers ARP for their addresses and forwards for them (proxy ARP, sudo), so the network only sees the host's MAC address, and they reach the network and are reached from it with any protocol (SSH, HTTP/HTTPS, FTP, TFTP, SCP - image upgrades included). The address is typed on the switch's console at each start with the image's *Set address* commands (`{mgmtAddr}`, `{mgmtBits}`, `{mgmtMask}`, `{gateway}`; the device pack brings them for its images), also for the lab bridge. The other choices: **the host's NAT network** - like GNS3's NAT node: the first port is on libvirt's `virbr0` on the host (Set Up installs and starts it), gets its address from the host's DHCP server (`192.168.122.x`), reaches out through the host and is reached through it; Termwright reads the address from the host's DHCP leases, so it works with any image, without an address command; the default on a host that is itself a VM - a *lab bridge on the host* (sudo: fixed addresses in the management network, reachable from the host only) or *port forwarding on the host* (no root: `127.0.0.1:<port>` on the host); their sessions use the host session as jump host. A host that is itself a VM must let its virtual switch pass the switches' own MAC addresses for the host's network (on VMware: promiscuous mode, MAC address changes and forged transmits) - the NAT network needs none of that. **Change the management later** in the bar under the tabs (*Management*): pick another way; running switches are stopped and started again with it (their configuration stays), and their sessions follow. For the routed host network a small form asks for the first switch's address and the gateway (the host's own is filled in). The first login's questionnaire of some images (`[y/N/q]:`) is answered with `q` (the defaults).
- **Boot**: each switch shows what it does - *copying the image*, *booting*, *day-0 config 3/5*, *running* or *failed* (with the reason). Termwright follows the boot on the console's log and, the first time, logs in on the console and types the day-0 commands, answering the questions it was given. Click a switch for the last lines of its console.
- **Sessions**: the *Emulated Lab* folder has an SSH session per switch (its management port, through the host) and, under *Consoles*, a Telnet session to each serial console (through the host). The folder shows how many switches are ready.
- **Cables** are virtual wires between the VMs (UDP sockets on the host's 127.0.0.1). New and removed cables take effect when both switches restart (*Restart*); **Pull** (⏻ in the topology) takes a link down at once on both switches and *Plug In* brings it back.
- **Several hosts in one topology.** In *Add Switches* choose another checked host (hosts already in the lab are marked *in the lab*), or use *Move to Host* on a switch's menu: **Live (No Downtime)** moves a running switch with its memory and disks while it keeps running (a ping or two may be lost; both hosts need the same acceleration, KVM or none - otherwise it is not offered - and TCP 29400-29999 between them), **Save, Copy and Resume** (or **Copy Its Disk** when it is stopped) takes its disk along so its configuration and day 0 stay (a minute or so off), and **From Its Image** boots it there from day 0. The Events log opens when a move starts and lists each step, or why it was refused. Moving to a host on another network gives the switch an address there - after a live move it is typed on its console while it keeps running, after Save, Copy and Resume when it starts there (it boots instead of resuming); a host new to the topology on another network gets its own lab bridge, which you can change in Hosts. The first switch on a host copies its image there: Termwright first checks the host has room, a copy that breaks off is tried again from where it stopped (up to four times), and the copy is checked with its checksum before it is used. A host without KVM runs its switches in software emulation (slow). **Set Up Again…** runs every step again on a host that has everything. **Remove Host…** asks what to do on the host first: keep or delete its images and the switches' disks, and take back what Set Up did (its sudo rule, the kvm group, the packages it installed) - or only forget the host. A host whose user has no sudo without a password can only use port forwarding; **Set Up** with the sudo password can add a rule that lets that user run just the network commands the switches need (ip, iptables, sysctl, virsh, arping, chown of /dev/tap*) without one. **Images → On the Hosts** shows which host has which image and copies them so every host has every image (then a move copies only the switch's own disk); click a host there for its numbers, checks and set up, and which other hosts it reaches for cables (UDP) and live moves (TCP). Each host is drawn as an area round its switches with its address, management and use; a cable between switches on two hosts is a UDP tunnel (dashed, purple), so the hosts must reach each other on UDP 20000-29999. Each host has its own management: pick the host in the *Management* chip first. The side panel has a card per host, and the **Inventory** tab of the bottom pane lists every switch under its host with its console (`host:port`), management address and ports, CPU and memory (*Copy* gives a CSV). Drag a switch into another host's area to move it there. **Check UDP Between Hosts** in the side panel tells whether the hosts reach each other for those cables. When you open a topology whose hosts you do not have (a .twlab from a colleague, or a host you removed), Termwright asks which of your hosts to use for each. In the Topology menu, ⧉ duplicates a topology (a lab of its own), ✎ renames it, ⇪ exports it to a .twlab file, and **Import…** adds one.
- **Fast start.** With *Fast start* on (the check box in the lab's bar, on by default), **Stop and Save State** saves a running switch's state into its disk and the next **Start** resumes it in seconds, without a boot or day 0. **Shut Down** (and Reload) stops a switch without saving, so it boots next time. A switch boots normally, and its old state is dropped, when its persona, management, memory, CPUs, disk size or image changed since it was saved. The saved state takes about the switch's memory on the host's disk.
- **The AI agent and the lab.** With the agent tool *Emulated lab* on (AI panel → Agent → tools), the AI sees the emulated lab - switches, models, states, management addresses, consoles, cables, LAGs and the KVM hosts with their CPU and memory - and can start, stop or shut down switches, add or remove cables, pull or plug links, and mark or read LAGs. It also has the same lab tools as Claude through the connector: add switches from an image or template, wait until they are ready, type commands on a switch's console, check the cabling, move a switch to another host, put test ports and streams on the Traffic tab and read the pass/fail report, capture a port's packets, run the link check, show the QEMU command line and read the Events. Each change shows what it does with *Do It*, **Allow for This Chat** (no more questions for lab changes in this chat, except ones that may interrupt traffic), *Skip* or *Stop*. Ask for example *Put test ports on core-1 port 5 and core-2 port 5, run 1000 pps UDP between them for 30 seconds and tell me whether it passes*. Ask for example *Make a LACP LAG of ports 1/2 and 1/3 between core-1 and core-2*: it opens each switch's session, configures both ends and reads the LAG back so the topology shows it. Changes to the lab ask first unless the agent is fully autonomous; in Read only it only looks.
- **From the topology to the switches.** Mark a LAG on the cables between two switches (right-click a cable), then choose **Set the LAG on the Switches…**: Termwright shows the commands for each switch, types them on their consoles (close a switch's console tab first) and reads the LAGs back, so the rings show what the switches really have. **Remove the LAG from the Switches…** removes one the switches have. **Check Cabling** compares the switches' LLDP neighbours with the cables: a cable that does not match turns orange, and its tooltip and the Events say what the port sees. The commands come from the image's device profile (a device pack), so they are right for that switch's CLI.
- **New Topology.** *Topology ▾ → New Topology…* (or *Switches* in the toolbar) opens one page: on the left the models you can make, grouped by operating system (search them, fold a group); click a model to add one more. Termwright picks its image and writes the switch's persona - the model, a serial and a base MAC in the model's format - before it first boots, so it shows as that hardware. In the middle each row has its software version, how many, the names (`{n}` is the number) and its role; *Serial and MAC from* sets where the numbers start. Choose the cabling: **Leaf-spine** (every leaf to every spine; the core-capable models such as 5720 or 7520 are spines by themselves, change it with the Spine/Leaf buttons), **Ring** and **Line** (in the order of the list), **Full mesh**, **Star** (around the device marked Hub) or **No links**, and how many cables between two switches - two or more become a LACP or static LAG on both ends. The right side previews it, shows the vCPU and memory it needs, and where it runs.
- **Add Link.** Click *Add Link*, then the two switches: each click takes that switch's next free port, and the cable shows at once. To choose a port instead, Alt-click the switch, or click *Change Port* in the blue bar after the first click. Right-click a switch → *Add Link from Here* picks the first port from a front panel.
- **The toolbar.** One bar above the canvas: Switches, Link, Cloud, Restart, Consoles, Cabling, zoom, Grid, Ports, Addresses, Problems and Style. *More* (⋯) switches it between icons with labels, icons only, and icons and text, turns the mini-map on or off, and has *Arrange by Host* and how hosts are shown.
- **Grid.** The *Grid* button beside *Ports* shows grid lines on the canvas; a switch you move snaps to them.
- **Running switches at a glance.** The Emulated lab button at the top right shows how many switches run on all KVM hosts and topologies, including topologies in the background (blue), or how many need a look - booting, failed or with a problem (yellow). Hover it for the list by host and topology.
- **How devices are drawn.** Six styles: *Boxes* (default: a rounded box with a small faceplate), *Classic 3-D*, *Classic diagram symbols*, *Isometric hardware*, *Line icons* and *Front panels*. Pick the style on the empty lab or with the menu beside the zoom buttons. Right-click a switch → **Draw As** to pick what it is drawn as (access switch, core, router, firewall, access point, server, KVM host, cloud) and in which style: the choice goes for every switch of that model in the topology, including ones you add later, and is saved with the topology. Cables run to the edge of each drawing. Backups (Settings → Backup) keep the emulated lab, its saved topologies and device packs; images larger than 20 MB and passwords in the system keychain are not in a backup.
- **A data port on the host's network.** Right-click a switch → **Port on the Host's Network…** and give a free data port: when the switch starts, that port is on the KVM host's network (its bridge, or macvtap on its network port), like a GNS3 cloud on a data port - for an uplink to real equipment or a DHCP server. **Take … off the Host's Network** undoes it.
- **Serial numbers and MAC addresses**: ✎ next to the MAC in the Topology panel, or **Serial Numbers…** for a list (or only the first serial number: the others count up from it), a first MAC address (each switch gets a block of 64), or *New in the Model's Format* (the image's *Serial format*, e.g. `AB{YY}{WW}-{N:5}`). A VM gets its serial number as its SMBIOS serial (system, board and chassis) and its MAC on the first port (the others follow) when it starts next; whether the operating system shows that serial depends on the image. The CPU column is the use now, as a share of the switch's vCPUs (hover for host cores).
- **Restart**, **Stop** and **Start** act on the ticked (or clicked) switches; **Back to Day 0** throws a switch's disk away and boots it as new, so the day-0 commands are typed again; **Remove** stops the switches and deletes their disks on the host and their sessions. The VMs keep running on the host when Termwright closes.

![Emulated lab](screenshots/emu-lab.png)

### Network Check (reachability and iPerf3)

**Transfer → Network Check (Reachability, iPerf3)…** answers "who can reach what": this computer and the device tabs you choose, against a list of targets.

- **Targets**: addresses or names separated by commas; `host:port` checks a TCP port. `gateway`, `dns` and `internet` are this computer's own default gateway, name lookup and internet connection (HTTPS when ping is blocked). The open tab's address is added for you.
- **From**: *This computer* always, plus the connected device tabs you tick. For a device, Termwright types the device's own read-only ping in its tab (EXOS `ping count 4 …`, VOSS `ping … count 4`, Cisco and Arista `ping … repeat 4`, Junos, Linux), so you see it there too; for production devices it asks once first. When the default VR or VRF does not answer, the ping is tried again in the management one (EXOS `vr VR-Mgmt`, VOSS `vrf MgmtRouter`, NX-OS `vrf management`), where a switch's management port usually is; the cell then says which one answered, and Traceroute uses it too. A device does not ping its own address (*this device*).
- **The matrix**: one row per target, one column per source, with the time or the failure in each cell. A line under it points at the device that misses what this computer reaches.
- **Click a cell** for the path: **Traceroute** draws each hop from the source to the target and marks where the answers stop, with the output below. **Copy Report** copies the matrix as text.

![Network Check](screenshots/net-check.png)

**Speed (iPerf3)** measures throughput between two points inside your network with the iperf3 program (install it once: `brew install iperf3`, `winget install iperf3` or the iperf3 package on Linux):

- **iPerf3 server on this computer**: start it, then run the command shown (`iperf3 -c <this computer> -t 10`) on a Linux host or a device with iperf3 - *Paste in <tab>* types it for you. The last test's result appears in the window. The server stops after 30 minutes without tests and when Termwright closes; iperf3 is its own program, so the firewall may ask about it once.
- **Test to a server**: the address of an iPerf3 server, 5 to 30 seconds, one way or both ways - the result in Mbit/s each way.

A public internet speed test is left out on purpose: it measures your own line, not the devices.

![Network Check: iPerf3](screenshots/net-check-iperf.png)

### File servers (TFTP, FTP, HTTP)

Most switches and routers fetch software images, licences and configurations *themselves* - you type `download image …` or `copy tftp://…` on the device and it connects to a server. **Transfer → File Servers (TFTP, FTP, HTTP)…** turns this computer into that server, so you need no separate TFTP or FTP program.

![File Servers](screenshots/file-servers.png)

- **Folder**: the files devices can fetch, and where the files they send are saved (`Termwright/Files` in your home folder unless you change it). Nothing outside it can be read or written - a device asking for `../../something` is refused and the refusal is listed under *Transfers*.
- **Listen on**: the address of this computer the servers use. Termwright picks the one that reaches the device of the open tab (marked *reaches …*); *All addresses of this computer* is there when you need it.
- **TFTP** (UDP, port 69), **FTP** (TCP, port 21) and **HTTP** (TCP, port 8080) each have **Start** / **Stop**. TFTP understands the block size and transfer size options, so large images go fast; FTP works in passive and active mode and needs the user name and password shown (the password is made for you and kept in the keychain - **Copy** it, or make a **new** one); HTTP only sends files.
- **Devices can upload files** (TFTP and FTP) lets devices send files, for example a configuration backup. It is off for TFTP until you turn it on.
- **Command for the device**: choose what the device does (*fetches* or *sends*), its type, the protocol and the file, and Termwright writes the command with this computer's address in it - for example `download image 10.1.1.20 summitX-32.6.1.2.xos vr "VR-Mgmt"` for EXOS or `copy tftp://10.1.1.20/image.bin flash:image.bin` for Cisco IOS. **Paste in** types it in the open tab without Enter; **Copy** copies it. Built in: EXOS, Cisco IOS, Cisco NX-OS, Arista EOS, Juniper Junos, Linux and *Any device (URL only)*. **Edit** changes the command for a device type, or adds one for a type that has none (VOSS, for example) - `{url}`, `{ip}`, `{port}`, `{file}`, `{base}`, `{name}`, `{user}` and `{password}` are filled in. Check a command against your device's software version before you rely on it.
![The firewall check in the File Servers window](screenshots/file-servers-firewall.png)

- **Firewall** shows whether this computer's firewall (macOS firewall, Windows Defender Firewall, ufw or firewalld) is on and whether it lets devices reach Termwright. **Allow Termwright…** runs the system's own command after you confirm - the system asks for an administrator password; *Show the command* shows it, to run yourself. TFTP sends each file from a new UDP port, so allow the program rather than only port 69. **Test from <tab>** types a command in the open tab that fetches a tiny `termwright-test.txt` (you press Enter); the panel and a message say whether the device reached this computer - the only check that proves the whole path, firewall and routing included. On Windows, a network marked *Public* blocks more.
- **Transfers** lists every file fetched or sent, with progress, speed and size, and the requests that were refused.
- While a server runs, the status bar shows it (with the progress of a transfer); click it to see the transfers and stop a server:

![File servers in the status bar](screenshots/file-servers-status.png)

**Safety.** TFTP, FTP and HTTP are not encrypted: files and the FTP password cross the network as they are, so use them on lab and management networks. The servers stop on their own after 30 minutes without transfers (10 minutes to 4 hours, or never) and always when Termwright closes. FTP data connections go only to the address the FTP login came from, so the server cannot be used to reach other machines.

**Ports below 1024 on Linux** (TFTP 69, FTP 21) need permission: choose a port above 1024 for the server, or allow it once with `sudo setcap cap_net_bind_service=+ep /path/to/termwright`. On Windows, the firewall asks the first time a server starts; allow it for the network the devices are on. macOS needs nothing.

## 10. Automation: logon actions, triggers, scripts

**Logon Actions** (Session Options → Logon Actions & Triggers):

![Logon Actions](screenshots/logon.png)

- **Automate logon** turns the Expect/Send table on or off; **Send initial carriage return** presses Enter right after connecting (wakes up consoles and terminal servers).
- **Add… / Edit… / Delete** and **▲ ▼** manage ordered steps. Each step waits for its **Expect** text, then sends **Send**. Leave Expect empty to send immediately.
- In the **Expect/Send Properties** dialog (Add… / Edit… or double-click a row): **Hide** masks the Send text in the table, **Send trailing carriage return** adds Enter (on by default), and **Credentials** picks a named credential.
- Send escapes: `\r` CR, `\n` LF, `\b` backspace, `\t` tab, `\e` Escape, `\v` paste clipboard, `\p` pause 1 s, `\s` username and `\w` password (from the chosen credential, otherwise the session's username and saved password), `\\` backslash, `\xHH` any byte.
- Example (Cisco): `Username:` → `\s`, `Password:` → `\w` (Hide), `>` → `enable`, `Password:` → `\w`, `#` → `terminal length 0`.
- **Logon script**: tick it, choose a JavaScript (`.js`) or **Python (`.py`)** script, and optionally give **Arguments** (`scriptArgs` in JavaScript, `crt.Arguments` in Python). `.vbs` scripts run on Windows (see below).

**Remote command** (SSH2 section of the same page): runs a command instead of a login shell; a terminal is still allocated and the tab ends when the command exits. Jump-station example — log in to the jump host and go straight on to an AWS instance with the key that is stored *on the jump host*:

```
ssh -t -i ~/.ssh/aws_key ec2-user@10.0.0.5
```

(Without `-t` the target gets no terminal. Alternatively use Session Options → Firewall → *Jump session*, which tunnels through the jump host but uses a key on *this* computer.)

**SFTP on the target of a remote command.** When the remote command is `ssh … user@host`, SFTP can browse either machine:

- right-click the terminal tab → **Open SFTP Tab on &lt;jump host&gt; (same login)** or **Open SFTP Tab on &lt;user@host&gt; (through this login)**;
- right-click the session in the Session Manager → **Connect SFTP Session to &lt;jump host&gt;** or **… to &lt;user@host&gt; (through &lt;jump host&gt;)**;
- Session Options → Logon Actions → **SFTP opens on**: *Where the terminal is* (the default - the target, or the jump station after `exit`), *Always <jump station>* or *Always <target>*. The SFTP button (Open SFTP Tab), *Connect SFTP Session* and *Connect as: SFTP* use it; without a terminal, *Where the terminal is* opens on the jump station.

![SFTP choices on the tab menu](screenshots/sftp-hop-menu.png)

The server pane's header then reads **user@host via &lt;jump host&gt;**, and the status bar *SFTP to user@host through &lt;jump host&gt;*. If the target's home folder has the same path as the jump station's (e.g. `/home/you` on both), compare the files to tell them apart - the listing is the target's. Through the jump station, Termwright runs `ssh -T -o BatchMode=yes <your options> -s user@host sftp` there, with the same key and options as your command (`-t` is dropped), so the key never leaves the jump station. **Copy via SCP** in that tab also goes to the target. Because of `BatchMode`, the key must work without typing anything (no passphrase, or one held by the jump station's ssh-agent); otherwise the tab shows ssh's error, e.g. *Permission denied (publickey)*.

![SFTP on the target](screenshots/sftp-hop.png)

![SFTP opens on](screenshots/session-hop.png)

![Expect/Send Properties](screenshots/expect-send.png)

**Accounts** in Send strings: `\s` and `\w` send the username and password of the account chosen in the step - see [Accounts](#accounts).

**Alerts** (Global Options → **Alerts**): when output in *any* tab matches a pattern (text or regular expression), Termwright shows a notice, marks the tab with a red dot, can beep and - while it is in the background - shows an operating-system notification. Each pattern fires at most once every 5 seconds per tab. Two disabled examples (syslog severity 0-2 messages, `CRITICAL` / `link down`) are included.

![Alerts](screenshots/alerts.png)

**Triggers**: whenever output contains a pattern (text or regular expression), send a response, e.g. `--More--` → space, `\[y/n\]` (regex) → `y\r`. Limited to 20 per 10 seconds to avoid loops.

**Python scripts with the `crt` object**: `.py` files run with your installed Python 3 and get the `crt` script object (`crt.Screen`, `crt.Session`, `crt.Dialog`…), so existing logon and automation scripts written for it usually work unchanged. Use them as a session's *Logon script*, from Script → Run Script…, a button or a key binding. Set a different interpreter in Global Options → General → *Python interpreter*. Example:

```python
# $language = "Python3"
crt.Screen.WaitForString("login:", 10)
crt.Screen.Send("admin\r")
crt.Screen.WaitForString("Password:")
crt.Screen.Send(crt.Dialog.Prompt("Password", "Login", "", True) + "\r")
for dut in crt.Arguments:            # the Arguments field, e.g. DUT1 DUT2
    crt.Screen.Send("ping " + dut + "\r")
    if crt.Screen.WaitForStrings(["#", "unreachable"], 30) == 2:
        crt.Dialog.MessageBox(dut + " is unreachable", "Ping", ICON_WARN)
```

Supported `crt` members, timeouts and limits are listed in [SCRIPTING.md](SCRIPTING.md#python-scripts-with-the-crt-object).

**VBScript scripts** (Windows): `.vbs` logon scripts with the `crt` object run unchanged with Windows Script Host - the `# $language = "VBScript"` header is accepted, `Sub Main` is called, and `crt.Screen`, `crt.Session`, `crt.Dialog`, `crt.Arguments`, `crt.Sleep` work with their optional arguments. See [SCRIPTING.md](SCRIPTING.md#vbscript-scripts-windows).

**Reading the screen**: in Python and VBScript, `crt.Screen.Get(row1, col1, row2, col2)`, `Get2`, `CurrentRow` and `CurrentColumn` read the screen exactly as it is rendered in the tab, so `crt.Screen.Get(row, 1, row, crt.Screen.CurrentColumn - 1)` returns the current prompt (e.g. `core-sw1:1#`).

**JavaScript scripts**: Script → Run Script…, a session's *Logon script*, a key binding or a button. Full API in [SCRIPTING.md](SCRIPTING.md). Example:

```js
term.sendLine("show vlan 10");
var out = term.readUntil(/# $/m, 10000);
log(out && out.indexOf("not found") < 0 ? "VLAN 10 present" : "VLAN 10 missing");
```

**Record a script**: Script → Start / Stop Script Recording (toolbar ●; the tab shows `SCRIPT`). Type your commands; when you stop, Termwright saves a `.js` that waits for each prompt and replays the commands.

**Key Map** (Global Options → Key Map): click the *Keys* box, press a combination (e.g. `Ctrl+Shift+F2`), then choose text to send, an action and/or a script. Key bindings take priority over the remote program while a terminal has focus.

| Key Map | Button Bar |
|---|---|
| ![Key Map](screenshots/keymap.png) | ![Button Bar](screenshots/buttons.png) |

### Accounts

An **account** is a username and a password that sessions log in with - for example `lab-admin`, your RADIUS / TACACS login, or `ec2-user` for cloud hosts. Many sessions, whole folders and device types can use the same account, so when its password changes you change it **once** and every session follows.

**Options → Passwords & Accounts → Accounts** lists them: the account, its username, where its password is (*Keychain*, *Password manager* or *Asked when connecting*), what uses it (sessions, folders, device types) and when its password was last changed. Click an account to change its name, username, password (or *Password manager…* for 1Password, Bitwarden, HashiCorp Vault or a command) and a note; *Show the sessions* lists the sessions that use it. **New Account**, **Remove Account…** (it says first what uses the account - those sessions then use their own login) and **Save Accounts**. Renaming an account renames it everywhere: sessions, folders, device types and logon actions.

**Accounts with a private key**: *Log in with* → **Private key** and choose the key file (*Browse…*). SSH sessions that use the account - also when it is their jump host, and SFTP - log in with that key unless the session has a key file of its own. The key's passphrase is asked when it is first needed and can be saved (Key Passphrases). The password stays optional: Telnet and serial sessions use it, and SSH uses it when the server refuses the key.

**Quick Connect** has an **Account** list when you have accounts: choose one to log in with it instead of typing a username and password (a typed `user@host` goes back to that username).

![Accounts](screenshots/accounts.png)

**Who logs in** - every session has an **Account** list (Session Options → Connection, and the side panel):

| Choice | Who logs in |
|---|---|
| **Default** (the usual choice) | the account of the session's **folder** (Folder Defaults, the nearest folder first), else the **device type's** account or login, else the session's own username and password. The list says what Default means for this session, e.g. *Default · lab-admin (folder "Datacenter")*. |
| an account | that account, whatever the folder or device type says |
| **Own username and password** | the username and password saved in this session only |
| **+ New Account…** | opens Passwords & Accounts to make one |

Below the list a card says exactly who will log in - *Logs in as admin with the account lab-admin (from the folder "Datacenter")*. **Device type = how it logs in** (prompts, `enable`, paging off), **Account = who logs in**: the two are chosen separately. The account is used for SSH, Telnet, SFTP sessions and tabs, RDP, jump hosts and background logins (device facts, backups, Run and Collect).

![Session Options - Account](screenshots/session-account.png)

- **Folder Defaults** (right-click a folder) has an **Account**, used by every session in the folder that keeps *Default*.
- **Device types** (Global Options → Device Types) have an **Account** too: sessions of that type with *Default* (and no folder account) log in with it, instead of the device type's own username and password.
- **Edit Sessions** (select sessions → *Edit N Sessions…*) can set the Account of many sessions at once.
- **A refused login** with an account shows **Update the Account Password** on the error card: type the new password once and every session that uses the account logs in with it. When a password is asked for, *Save for the account …* saves it for the account rather than for one session.
- **Convert Sessions…** (Accounts page) finds sessions that log in with the **same username and the same saved password** and turns each group into an account in one click: the account takes the username and password, and the sessions' own copies are removed.

![Convert sessions to an account](screenshots/accounts-convert.png)

![Passwords & Accounts](screenshots/passwords.png)

**Master password** (Options → Passwords & Accounts → Master Password, or the welcome flow): when set, Termwright starts with an **unlock window** and asks for it before it uses any saved session, credential or device type password or key passphrase. Any 4 or more characters work - a meter shows how strong it is, and capitals, numbers or symbols are never required. **Not now** keeps the passwords locked (they are asked when connecting); **Lock Now** locks them again. The passwords stay in the OS keychain; the master password only gates their use by Termwright.

![Master password](screenshots/passwords-master.png)

**Key Passphrases** (same dialog): passphrases of encrypted SSH keys saved in the OS keychain - change, forget or add them; see [SSH keys](#ssh-keys-generate-install-and-store).

![Key Passphrases](screenshots/passwords-keys.png)

### Password managers

**1Password, Bitwarden, HashiCorp Vault, or any command**: instead of saving a password, click **Password manager…** next to a session's, device type's or credential's password and enter a reference. Termwright tests it (you only see *OK - got an N-character secret*), stores just the reference, and reads the secret each time it connects - kept in memory for 10 minutes, never written to disk.

| Reference | Reads | Needs |
|---|---|---|
| `op://Network/core-sw1/password` | 1Password item field | 1Password CLI `op`, signed in (desktop-app integration or service account) |
| `bw://core-sw1`, `bw://core-sw1#username` | Bitwarden item password (or another field) | Bitwarden CLI `bw`, logged in; Termwright asks for the master password once to unlock |
| `vault://secret/network/core-sw1#password` | HashiCorp Vault KV v2 (or v1) field | Vault address and token in *Password Managers*, or `VAULT_ADDR` / `VAULT_TOKEN` / `vault login` |
| `cmd://security find-generic-password -s lab -w` | the output of any command | the command |

| Choose a reference | Session using it |
|---|---|
| ![Password manager reference](screenshots/password-ref.png) | ![Session with a reference](screenshots/session-ref.png) |

**Password Managers** (Options → Passwords & Accounts → Password Managers): the reference formats, the Vault address and token (saved in the OS keychain), a *Test* box and *Forget fetched secrets*. If a password manager is unavailable when connecting, Termwright shows why and asks for the password as usual (typing it does not replace the reference). CLIs installed with Homebrew, winget or Scoop are found even when the app was started from the Dock / Start menu.

![Password Managers](screenshots/passwords-managers.png)

Device types can use a reference too, so a whole fleet logs in with a password that lives only in your password manager:

![Device type with a Vault reference](screenshots/devicetypes-ref.png)

**Password vault export/import** (same dialog → Export / Import): writes all saved session passwords, device type passwords, SSH key passphrases and credentials into one `.twvault` file encrypted with AES-256-GCM (scrypt-derived key). Import it on another computer with the same passphrase. Vault files made before 0.11.4 use an older format: export them again from 0.11.4 or later.

## 11. Tunnels, jump hosts and proxies

### Web pages through a session

Many devices have a web interface - a firewall or router UI, an IPMI / iLO / iDRAC console, a NAS, a switch's web manager - that your computer cannot reach because it sits behind a jump host or in a cloud network. Termwright opens such pages in a **browser window of their own whose traffic goes through an SSH connection**, so the page loads as if it were opened on that server.

| Command | Where | What it opens |
|---|---|---|
| **Open Web Page Through This Session…** | right-click an SSH2 session or tab, Transfer menu | any address, out through that session's SSH server - e.g. `http://nas.lan:5000`, `https://10.20.0.1`, or `http://localhost:9090` for a service that listens only on the server itself |
| **Open Web Interface…** | right-click a session (SSH2, Telnet, Raw) or its tab | the device's own address (`https://<address>`, change it as needed), through its route: its **jump host**, the **jump station** of an `ssh … host` remote command, or its proxy. A device without a jump host opens directly in your default browser. |

![Open Web Interface](screenshots/web-open.png)

How it works:

1. Termwright starts a private **SOCKS5 tunnel** on `127.0.0.1` (a random port, only reachable from your computer) whose connections leave from the SSH server - the open tab's connection when there is one, otherwise a login of its own (saved password, key or the usual prompt).
2. It starts **Chrome, Edge, Chromium, Brave or Firefox** (choose in the dialog; the choice is remembered) as a **separate window with a temporary profile** that sends every request - and every name lookup - through the tunnel. Internal names such as `nas.lan` or `fritz.box` are resolved by the server. Your normal browser windows are not affected.
3. The web server sees a connection from the SSH server, not from your computer.
4. **Closing the window** ends the tunnel and its login and **deletes the profile** - no cookies, logins or cache are kept. Quitting Termwright closes the tunnels too.

![Open Web Page Through This Session](screenshots/web-open-session.png)

Notes:

- Safari cannot send one window through a tunnel, so it is not offered; on macOS install Chrome, Edge, Brave or Firefox.
- Devices with a self-signed certificate show the browser's certificate warning; continue there if you trust the device. Termwright never turns certificate checks off.
- Downloads, uploads, audio and typing in any language work as in your normal browser.
- The SSH server must allow TCP forwarding (`AllowTcpForwarding yes`, the OpenSSH default). Network devices usually do not, so for a device use **Open Web Interface**, which goes through its jump host instead.


**Port forwarding** (Session Options → Port Forwarding, SSH2 only); active while the session is connected:

| Type | Listens on | Connects to | Example |
|---|---|---|---|
| Local | this computer (`127.0.0.1` by default) | a host reachable from the server | Listen 8443 → `10.0.0.5:443`, then browse `https://localhost:8443` |
| Remote | the server | a host reachable from this computer | Listen 9000 on server → `127.0.0.1:3000` |
| Dynamic | this computer | anything (SOCKS5 proxy) | Listen 1080, set your browser's SOCKS proxy to `127.0.0.1:1080` |

![Port Forwarding](screenshots/port-forwarding.png)

**Jump host**: pick another saved SSH2 session; Termwright logs into it first and tunnels through (the jump session can itself use a jump host). Works for SSH2, Telnet and Raw sessions.

![Jump Host / Proxy](screenshots/jump-proxy.png)

**Proxy**: SOCKS5 or HTTP CONNECT with optional user (password asked when connecting).

**X11 forwarding** needs a local X server (XQuartz on macOS, VcXsrv/X410 on Windows, built in on Linux). **Agent forwarding** lets the remote host use your local SSH agent keys.

## 12. Appearance: themes, color schemes, highlighting

![Light theme with Solarized Light](screenshots/light.png)

- **UI theme**: the **theme switch** at the top right - sun = Light, moon = Dark, screen = follow the system (the default: light or dark with your computer, switching when it does). Hover it to see the current theme, click an icon to change it. The same choice is in Global Options → General, with an **accent color**.
- **Interface font and text size** (Global Options → General): the default is the **Mac look** - the Mac's own font on macOS, and on Windows and Linux *Mac look - Inter*, the open font closest to it (shipped with Termwright). *System font of this computer* (Segoe UI, Ubuntu …), *VS Code* (the font VS Code uses on each system), *Inter* or *IBM Plex Sans* are the other choices, with the text size from *Default (13)* to *Largest (17)*. The sample under the lists shows the choice before you press OK. It changes menus, dialogs and the Session Manager; the terminal font and size are under Terminal & Appearance (and View → Zoom). Terminals use **Termwright Mono** (DejaVu Sans Mono, which Menlo was made from, also shipped) where Menlo is missing, so they look like the Mac's on every system; a terminal font you chose yourself stays.

  ![Interface font and text size](screenshots/options-font.png)

  ![Theme switch in the dark and light theme](screenshots/theme-switch.png)
 The app icon on the start page and in About follows the theme.
- **Terminal color scheme**: Global Options → Terminal & Appearance (default for all sessions) or Session Options → Terminal (per session). Built in: Default, Classic, Green Screen, Light, Solarized Dark/Light, Dracula, Nord, Gruvbox Dark, Tomorrow Night, Catppuccin Mocha, Tango. Common scheme names found in imported sessions map to these automatically.
- **Custom schemes**: Global Options → Color Schemes → pick a scheme → *Copy as new…*, then edit any of the 20 colors with live preview. *Export…/Import…* share schemes as JSON.

![Color scheme editor](screenshots/schemes.png)

- **Font / cursor**: Global Options → Terminal & Appearance (font family, size, **line spacing**, cursor style, blink, scrollback; the default is 14 with normal line spacing); per-session font in Session Options → Terminal.
- **Keyword highlighting** (on by default with the *Network* set): Settings → Keyword Highlighting. The *Network* set colours **up / ok / enable(d) / active / forwarding** green, **down / error / failed / denied** red, **warning / disable(d) / blocked** amber and IPv4 addresses blue. In the EXOS port summary (`show ports`, `show port no-refresh`) the one-letter states are coloured the same way: Port State **E** (enabled) green and **D** (disabled) amber, Link State **A** (active - up) green and **R** (ready - down) red - only on rows that start with a port number. The text itself is coloured (*Style → Colour the background* brings the coloured background back). Create your own sets of words or regular expressions with colors; choose the default set (*None* turns highlighting off), or a set per session (Session Options → Terminal → Appearance). Only the rows on screen are highlighted, so long sessions stay fast; it is skipped in full-screen programs such as `vi` or `less`.

![Keyword Highlighting](screenshots/keywords.png)

## 13. Global Options reference

Global Options is called **Settings** (⚙ in the title bar). The General page starts with **Appearance** - theme previews, Icons (Tiles / Glyphs), **Folder colours** (*Soft* - the default, *Vivid* or *Steel blue*: the folders by level and the PROD / LAB tags), Start page buttons, interface font and text size - then **Behaviour** (each switch with a short explanation: confirm before closing, confirm multi-line paste, remember typed commands, **save Quick Connect sessions**, reopen the tabs that were open, show the Session Manager, Button Bar and Command Window) and **Folders and files**. Changes apply when you press *Save*.

### Interface language

**Start page footer**: the slogan, the version you run (green *up to date* or the newer release after the update check - click it for Software Update), the number of downloads from GitHub (not fetched when update checks are off), the licence and a link to the project.

**Settings → General → Style** (modern layout): **Glacier** (the default) turns the window into frosted glass - the Session Manager, the title bar, menus and dialogs are translucent panels (graphite glass in the dark theme, icy glass in the light one), and the start page shows raised cards, three per row and up to five rows. It works with the light, dark and system themes; terminals keep their own colours. The title bar keeps its size, so on macOS its buttons stay level with the window buttons. *Standard* is the plain look without glass - the lighter choice on old machines. **Folder and file icons** (just below): *As the style* gives glass folders and file pages in Glacier and flat ones in Standard; *Glass* or *Flat* picks one look for both styles.

| Glacier, dark | Glacier, light |
|---|---|
| ![Glacier style, dark](screenshots/glacier-dark.png) | ![Glacier style, light](screenshots/glacier-light.png) |

**Settings → General → Language** switches the interface language; the change shows as soon as you press *Save*, the main menu included (File, Edit, View, Transfer, Script, Options, Tab, Help - no restart needed). English is the default. **Русский (Russian)** translates the menus, the Session Manager, Quick Connect, Settings, Session Options and the dialogs; a text that has no translation yet stays in English. Terminal output, session and folder names, file names, and error messages from the program's engine are never translated. On macOS the application menu (About, Hide, Quit) is the system's and follows the language of macOS.

![Termwright in Russian](screenshots/language-ru.png)

A translation is a plain text list in the source (`frontend/src/app/i18n/ru.ts`, English text on the left, translation on the right), so improvements and further languages are welcome as pull requests: a text without an entry simply stays English. Numbers in a text use the language's own plural forms (for Russian: 1 минуту, 2 минуты, 5 минут).

![Settings - General](screenshots/options.png)

**Search options**: type in *Search options* at the top of the page list (e.g. `paste`, `font`, `sudo`); click a result to open its page with the option highlighted.

![Searching Global Options](screenshots/options-search.png)

| Page | Settings |
|---|---|
| **General** | Python interpreter for `.py` scripts (blank = automatic), sessions folder (it can point at a `Config/Sessions` folder shared with another program that uses the same files; back it up first), Logs folder, Downloads folder (ZMODEM), UI theme, accent color, confirm on close, **reopen last sessions on startup**, show Session Manager / Button Bar / Command Window / Connect Bar / menu bar inside the window. |
| **Terminal & Appearance** | Font, size, color scheme (*Auto* follows the UI theme; *Use this scheme for all sessions* overrides schemes set per session or imported), cursor, scrollback, copy on select, paste on right-click, paste line delay (ms between lines when pasting, for slow devices). |
| **Color Schemes** | Custom scheme editor, import/export. |
| **Keyword Highlighting** | Keyword sets and the default set. |
| **Key Map** | Key bindings. |
| **Button Bar** | Buttons: label, text, all tabs, action, script; reorder. |
| **Updates** | What to be told about (stable / stable and nightly / nothing), check at start-up; under *Advanced*, repository and token for your own fork. |

### Updates

![Software Update](screenshots/update.png)

**Help → Check for Updates…** (the same window opens from Options → Global Options → Updates → *Check for Updates…*) shows:

- the **installed** version (and the commit, for a nightly build);
- the **latest stable release** with its date and *What's new*; if it is newer, **Download** fetches the installer for this computer (macOS DMG, Windows setup.exe, Linux .deb) into the Downloads folder and **Open Installer** starts it. Termwright never replaces itself silently;
- a **nightly build**, only when one exists that is newer than what you run (Termwright asks GitHub whether the nightly commit is ahead of yours, so an older nightly is never offered). Nightly builds are untested: they may crash or change or lose settings and sessions. **Back Up Now** saves your settings and sessions to `Documents/Termwright Backups/termwright-backup-<date>.zip` (passwords stay in the OS keychain), and the nightly download is enabled only after a backup or after you tick *I have a backup and accept the risk*. To restore, use **Settings → General → Backup → Restore from Backup…**.

Options → Global Options → **Updates** decides when you are told about updates:

- **Notify me about**: *Stable releases* (default), *Stable releases and nightly builds*, or *Nothing*.
- **Check at start-up** (at most once a day; only a notice when something newer exists).
- **Advanced - only for your own fork**: repository and GitHub token. Leave them alone for the official releases.

## 14. Migrating from SecureCRT, PuTTY or MobaXterm

![Import Sessions](screenshots/import.png)

1. **File → Import Sessions…** and choose the source. The list is grouped into **SecureCRT**, **Other terminal apps** and **Inventories**:
   - **SecureCRT** (four ways to bring the same sessions):
     - *Configuration folder*: Windows `%APPDATA%\VanDyke\Config`, macOS `~/Library/Application Support/VanDyke/SecureCRT/Config`, Linux `~/.vandyke/SecureCRT/Config`
     - a *.zip* of that Sessions folder (e.g. a backup)
     - an *XML settings export* (SecureCRT: Tools → Export Settings…). On Windows, if the file picker shows no files, pick **All files (\*.\*)** in its file-type list; make sure the source is *XML settings export*, not *Configuration folder* (that one opens a folder picker).
     - a *single session .ini* file
   - your *OpenSSH* `~/.ssh/config`
   - **PuTTY / KiTTY**: a registry export (`reg export HKCU\Software\SimonTatham\PuTTY\Sessions putty.reg`) or the `~/.putty/sessions` folder. PuTTY Session Manager folders are kept; host, user, port, protocol, serial settings, port forwards, proxy, keepalive, remote command and logging are converted. PuTTY `.ppk` keys (formats 2 and 3, encrypted or not) are used directly - no conversion needed.
   - **SuperPuTTY**: choose its `Sessions.xml` (same *PuTTY / KiTTY / SuperPuTTY* entry). Folders (`SessionId`), host, port, protocol (SSH, Telnet, Raw, Serial with COM port and speed), username and the `-i` key file and `-l` / `-P` / `-A` / `-X` options from the extra arguments are converted. A password given with `-pw` is saved in the OS keychain, never in the session file - consider deleting `Sessions.xml` afterwards, since it holds those passwords in plain text. Cygterm and Mintty sessions (local Cygwin shells) become **Local Shell** sessions: their own command when they have one, else Cygwin's login bash on Windows when Cygwin is installed, else your normal shell. RLogin sessions are listed as skipped (RLogin sends passwords unencrypted and needs administrator rights - use SSH or Telnet).
   - **CSV** (from a spreadsheet, a script or an inventory): a header row and one session per row; only `host` is needed. The columns are `name, folder, protocol` (ssh, telnet, rdp, vnc, serial, raw, wol)`, port, username, device_type, environment, jump_host, identity_file, description, mac, account, files_panel, com_port, baud, network`. `jump_host` is the path of another session (`Folder/name`), so RDP and VNC through an SSH session import too, and follow the sessions when you import into a folder. Columns it does not know are ignored; the result lists what was left out or replaced. **Save a template CSV…** in the dialog writes an example with every column, and the **[Import format](IMPORT_FORMAT.md)** page describes it fully. **Tools Termwright has no importer for: convert their sessions to this CSV**, a few lines of script or a spreadsheet will do.

     ![Import from CSV](screenshots/import-csv.png)
   - **Ansible inventory** (INI or YAML): groups become folders; `ansible_host/user/port/connection`, `ansible_network_os` → device type.
   - **NetBox**: URL + API token (+ optional filter such as `site=dc1&role=leaf`); folders are Site/Role, the device type comes from the platform.
   - **AWS EC2**: uses the AWS CLI profile/region; running Linux instances with user `ec2-user` and `~/.ssh/<KeyName>.pem`.
   - **mRemoteNG**: the connections file (in mRemoteNG *File → Save Connection File As…*, by default `%APPDATA%\mRemoteNG\confCons.xml`). Containers become folders. SSH, Telnet, RAW, RDP and VNC connections are imported with the user name, domain, port and protocol they inherit from their container; their saved passwords go to the OS keychain (type the file's master password in the *Master password* box if you set one in mRemoteNG). HTTP, ICA, Rlogin and SSH1 connections are skipped and listed. If mRemoteNG encrypts the complete file, turn that off first (*Tools → Options → Security*).

     ![Import from mRemoteNG](screenshots/import-mremoteng.png)

   - **MobaXterm**: an exported `.mxtsessions` file (right-click *User sessions* → *Export*) or `MobaXterm.ini`. SSH, Telnet and Serial sessions are imported with their folders; RDP, VNC, FTP and others are skipped.
   - **Saved passwords**: SecureCRT session passwords (`Password V2` with the `02:` or `03:` format, the older `Password`, or plain text from an export without password encryption) are decrypted and moved into the OS keychain; the copied session files no longer contain them, and *Save password* is ticked. If you set a **configuration passphrase** in SecureCRT, type it in *Config passphrase* (shown for the SecureCRT sources). The result lists how many passwords were imported and which could not be decrypted - import those again with the passphrase and *If name exists: Overwrite*, or type them once when connecting.
2. Choose a target folder and what to do when names clash (keep both / overwrite / skip).
3. Files are copied unchanged; Termwright reads host, port, user, protocol, serial settings, emulation, logging, description, jump host and more. Settings it doesn't use are preserved, so exporting back loses nothing.
4. Passwords that cannot be decrypted (for example with a wrong or missing configuration passphrase) are left out; connect once and tick *Save password*.
5. **File → Export Sessions…** writes a folder (copy its `Sessions` into SecureCRT's Config), a zip, or XML (SecureCRT *Import Settings*).

Details: [SESSION_FILES.md](SESSION_FILES.md).

### Moving sessions to another Termwright

**File → Export Sessions…** → *Zip archive*, *Folder* or *XML* keeps **everything** in the sessions: device type, **account**, MAC address, environment, jump host, port forwards, logon actions, triggers, recording, smart card, timestamps and auto-scroll, the **Files panel** switch, *SFTP opens on*, the **Wake-on-LAN** options and **Wake-on-LAN sessions**. The **CSV** keeps the basics plus device type, environment, jump host, MAC address, account, Files panel and the network of Wake-on-LAN sessions; RDP, VNC and Wake-on-LAN sessions keep their protocol.

With **Include settings** (on by default) the zip and the folder also carry your settings, so nothing has to be copied by hand:

- **device types** (login, prompts, backup commands, fact rules), **accounts** (name, username and note) and **Folder Defaults** (including a folder's account);
- the **command library**, checklists, Run and Collect, backup jobs, alerts, buttons, keyboard shortcuts, workspaces, SFTP bookmarks, favourites, keyword sets and colour schemes;
- the **Global Options** (appearance, terminal and behaviour).

What belongs to one computer stays out: the folders on disk (sessions, logs, downloads, backups, recordings), the master password, open tabs, recent sessions and the window position. XML holds sessions only.

![Export with settings](screenshots/export-formats.png)

On the other computer choose **File → Import Sessions… → Termwright .zip** (or **Termwright folder**). When the export has settings, the dialog shows what is in it and lets you choose the parts: device types, accounts and Folder Defaults and the command library are ticked, the Global Options are not (they replace yours). Items with the same name are replaced and the others are kept, so importing twice changes nothing. When the sessions go into a folder (*Into folder*), the Folder Defaults, checklists, jobs and workspaces are moved with them, and a session renamed by *Keep both* is followed. The summary after the import lists what was added and updated.

![Import with settings](screenshots/import-settings.png)

Passwords are never in a session export. **Passwords & Accounts → Export / Import** (the password vault, encrypted with a passphrase) moves the saved passwords, the accounts' passwords and the device types' passwords - import it too, so sessions that log in with an account can log in without asking.

### Syncing your own computers

**Settings → General → Sync** keeps your sessions the same on your laptop, desktop and lab PC through a folder they share - OneDrive, iCloud Drive, Dropbox, Google Drive or a network share. Choose the folder on each computer (the same one); every half minute each Termwright writes its sessions and settings there (in *Termwright Sync*) and takes in what the others wrote.

- **What syncs**: sessions and folders, accounts (name and username), device types, Folder Defaults and the library (commands, checklists, Run and Collect, backup jobs, alerts, buttons, shortcuts, workspaces, bookmarks, favourites). Tick **Global Options too** for the appearance, terminal and behaviour settings.
- **What never syncs**: passwords and key passphrases (each computer's keychain - use *Passwords & Accounts → Export / Import* to move them once), team folders (they come from the team server), and what belongs to one computer (folders on disk, open tabs, recent sessions).
- **Changes and deletions**: a session changed on one computer is changed on the others; one deleted (or moved away) is deleted on the others - unless it was changed there later, then that version stays. Two computers changing the same session at the same time: the later change wins.
- **Sync Now** syncs at once; the line under it says when it last synced and with how many other computers. **Turn Off** stops syncing; nothing is deleted.

![Sync](screenshots/sync.png)

### Exporting to other programs

Your sessions are never locked in. **File → Export Sessions…** also writes them for other programs - all sessions or the selected folder or session:

![Export formats](screenshots/export-formats.png)

| Format | Use it in the other program |
|---|---|
| **PuTTY (.reg, Windows)** | Double-click the `.reg` file (or Registry Editor → File → Import); the sessions appear in PuTTY |
| **KiTTY (.reg, Windows)** | Double-click the `.reg` file; the sessions appear in KiTTY with their folders |
| **PuTTY sessions folder** (Linux / macOS) | Export into `~/.putty/sessions` |
| **MobaXterm (.mxtsessions)** | Right-click *User sessions* → *Import sessions from file* |
| **Tabby profiles (.yaml)** | Add the profiles to Tabby's `config.yaml` (Settings → Config file → Show config file) and restart Tabby |
| **OpenSSH config** | Add to `~/.ssh/config`, or save as `~/.ssh/termwright.conf` and put `Include termwright.conf` at its top - works with `ssh`, VS Code Remote-SSH and any SSH tool |
| **CSV** | Excel, Numbers, LibreOffice or an inventory; Termwright imports it again |

What is carried over: name, folder, protocol, address, port, username (also the device type's), key file, jump host (PuTTY 0.77+ SSH proxy, MobaXterm SSH gateway, Tabby jump host, OpenSSH `ProxyJump`), SOCKS / HTTP proxy, port forwards, keep-alive, agent and X11 forwarding, remote command, serial settings and logging (PuTTY). The CSV also has device type, environment, jump host and MAC address, and Termwright reads them back when it imports the CSV.

After the export a summary lists the sessions the format cannot hold (e.g. RDP, VNC and console sessions in PuTTY) and what stays in Termwright: device type login steps, logon actions, triggers, smart card choice, backups, checklists and recordings. **Passwords are never exported**; to move them between Termwright installations use the password vault export (Passwords & Accounts).

![Export summary](screenshots/export-report.png)

Tips: PuTTY needs `.ppk` keys (PuTTYgen converts OpenSSH keys) and OpenSSH cannot read `.ppk` keys; the summary says when this applies. Termwright's own folder, zip and XML keep everything.

**About** (Help → About Termwright) shows the version, how many times Termwright was downloaded from GitHub (all files of all releases; it is looked up when About opens, at most every six hours, and left out when GitHub cannot be reached), the platform, data folder, main features and links to the repository, this guide, issues and releases.

![About](screenshots/about.png)

## 15. Keyboard shortcuts

`Mod` = **Cmd** on macOS and **Ctrl+Shift** on Windows/Linux (Ctrl alone stays with the terminal). `Mod+Shift` = **Cmd+Shift** on macOS and **Alt+Shift** on Windows/Linux. Nearly every menu command has a shortcut; **Help → Keyboard Shortcuts** lists them all and has a search box.

![Keyboard Shortcuts](screenshots/shortcuts.png)

| Keys | Action | Keys | Action |
|---|---|---|---|
| Mod+O | Show Session Manager | Mod+Q | Quick Connect |
| Mod+L | Local shell | Mod+R | Reconnect |
| Mod+W | Close tab | Mod+P | Open SFTP tab |
| Mod+C / Ctrl+Insert | Copy | Mod+V / Shift+Insert | Paste |
| Mod+A | Select all | Mod+F | Find |
| Mod+S | Save scrollback | Mod+M | Toggle Session Manager |
| Mod+K | Toggle Command Window | Mod+B | Toggle Button Bar |
| Mod+E | Command Library | Mod+Y | Command History |
| Mod+J | Add marker to the recording | Mod+N | New session |
| Mod+D | Disconnect | Mod+Shift+K | Clear screen |
| Mod+Shift+A | Auto scroll on / off | Mod+Shift+T | Timestamps on / off |
| Mod+Shift+L | Log session | Mod+Shift+P | Save screen as image |
| Mod+Shift+6 | Save selection as device fact | Mod+G | Quick Open |
| Ctrl+Tab | Next tab | Cmd/Ctrl + / − / 0 | Zoom in / out / reset |
| F11 | Full screen | Enter (disconnected tab) | Reconnect |

Add your own in Global Options → Key Map. Help → Keyboard Shortcuts shows the list in the app.


**Dialogs and the keyboard**: **Escape** closes the menu or dialog on top (as its Close or Cancel button would - a dialog that is busy, such as a team server setup, asks you to cancel first), **Tab** and **Shift+Tab** stay inside the dialog, and a dialog that opens takes the focus. Dialogs are announced to screen readers (VoiceOver, Narrator, Orca) with their title, and buttons that show only an icon by their tooltip.

## 16. Files and folders Termwright uses

How the parts fit together - Termwright on your computer, your devices, and the optional team server and sync folder:

![Architecture](screenshots/architecture.png)


| What | Where |
|---|---|
| Data folder | macOS `~/Library/Application Support/Termwright`, Windows `%APPDATA%\Termwright`, Linux `~/.config/Termwright` (override with the `TERMWRIGHT_HOME` environment variable) |
| Backup and restore | Settings → General → Backup: **Back Up Now** writes `Documents/Termwright Backups/termwright-backup-<date>.zip` (the data folder without logs, and the sessions folder) - settings, sessions, accounts, library, device types, AI chats (`ai-chats.json`) and team links; passwords and keys stay in the OS keychain. **Restore from Backup…** backs up the current state first, closes Termwright and puts the backup back when it starts again. |
| Sessions | `<data>/Config/Sessions/**.ini` (or the folder set in Global Options) |
| Device types, command library, workspaces, folder defaults, alerts | `<data>/settings.json` |
| Window size and position | remembered in `<data>/settings.json` |
| Portable mode | create a folder named `termwright-data` next to the program (next to `Termwright.app` on macOS); everything is then kept there, e.g. on a USB stick |
| Global options | `<data>/settings.json` |
| Trusted host keys | `<data>/known_hosts` (plus read-only `~/.ssh/known_hosts`) |
| Logs | `<data>/Logs` unless set otherwise |
| Session recordings | `Documents/Termwright Recordings` unless set otherwise (asciinema `.cast`) |
| Saved passwords | OS keychain, service name `Termwright` |

Back up the data folder to back up everything except passwords.

## 17. Troubleshooting

| Problem | What to do |
|---|---|
| macOS says the app is damaged / can't be opened | `xattr -dr com.apple.quarantine /Applications/Termwright.app` (the build isn't notarized). |
| Windows SmartScreen warning | *More info → Run anyway* (the build isn't code-signed). |
| Linux: app doesn't start | Install `libwebkit2gtk-4.1-0` and `libgtk-3-0`. |
| "Host key changed" | The device was re-installed or re-keyed, or the connection is being intercepted. Verify the fingerprint before *Replace & Save*. |
| SSH auth fails with keys | Check the identity file path and that the key is not in an unsupported format; try *password* in SSH2 Authentication. |
| Serial port missing | Click *Scan* in the Serial page; on Linux add yourself to the `dialout` group; on macOS use `/dev/cu.*` devices. |
| Ctrl+C copies instead of interrupting | It doesn't: copy is Cmd+C on macOS and Ctrl+Shift+C on Windows/Linux. Check Global Options → Key Map for a conflicting binding. |
| Check for Updates fails | Read the message: *limit of 60 checks per hour* → wait an hour; *cannot reach GitHub* → check the network or proxy. Make sure the repository is `nperiannan/termwright`. A stale token is ignored, so you can also clear it. |
| Import: the .xml file is not listed (Windows) | Choose **All files (\*.\*)** in the file-type list of the picker, and check that the source is *XML settings export*. |
| Pasting into a switch drops characters | Set *Paste line delay* (e.g. 50 ms) in Global Options → Terminal. |
| ZMODEM does nothing | Install `lrzsz` on the remote host; check the Downloads folder in Global Options. |
| Highlighting is missing | Check Settings → Keyword Highlighting → Default set (it is *Network* unless you chose *None*) and the session's own keyword set (Session Options → Appearance); it is off in full-screen apps. |

---

*SecureCRT® is a registered trademark of VanDyke Software, Inc. Termwright is an independent open-source project, not affiliated with or endorsed by VanDyke Software; the name is used only to say which files Termwright can import and export.*
