# Changelog

All notable changes to Termwright. Each release on GitHub shows its section of this file and the list of commits.

## Unreleased

* Release notes: every release and nightly build now lists its changes and commits ([CHANGELOG.md](CHANGELOG.md)).
* Build: more reliable nightly publishing and faster Linux package installation.

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
