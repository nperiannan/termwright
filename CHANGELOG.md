# Changelog

All notable changes to Termwright. Each release on GitHub shows its section of this file and the list of commits.

## Unreleased

* **Importing keeps the jump host of RDP, VNC and SSH sessions working.** The jump host of a session is a reference to another session by its path. When sessions were imported into a folder (*Import into folder*) or an imported copy was renamed because the name existed, the reference still pointed at the old path and the session could not connect. The import now points each session at the imported copy of its jump host, wherever it landed; a jump host that is not part of the import keeps its path and uses the session of that name in your list. Exports to Termwright's own formats (folder, .zip, .xml) and CSV keep RDP and VNC sessions with their port, user and jump host; PuTTY, MobaXterm, Tabby and ssh config have no RDP or VNC, so those sessions are left out and listed in the export report.

* **A VNC password typed at the login can be saved**: after a VNC login with a typed password works, a toast offers *Save Password*, which keeps it with the session (OS keychain) so it is not asked for again.
* **Set Up Remote Desktop says when a session has no saved password.** Sessions get the password only when the run set it (a new user, or a VNC service it configured). When the desktop user or the VNC service already existed, the password is not known to Termwright; the dialog now says so instead of leaving a surprise prompt at the first connect, and tells to run the setup again with *Reconfigure services that already run* to set a new one.

* **The Session Manager refreshes by itself.** Adding, changing, moving, renaming or deleting a session or folder updates the list at once (the backend announces every change, so no path can leave it stale), and changes made to the session files from outside - a sync tool, a git pull, another copy of Termwright - show up within a few seconds. The *Refresh* button is no longer needed; an unchanged list is left as it is, so nothing flickers.

* **Diagnose Remote Desktop** (was *Collect Logs*): for an RDP or VNC session that does not connect, right-click it → *Diagnose (why it does not connect)…* (or the machine's SSH session → *Diagnose Remote Desktop…*). One click runs a connection test from your computer through the jump host (the first bytes of the RDP / VNC handshake) and reads the state and logs of that service on the machine over the SSH session, and the dialog **names the problem in a line or two** in red with what to do (a blocked VNC address after wrong passwords, a stopped or failed service, nothing listening on the port, a refused port forward, a missing user or home folder, failed logins in the log), or says *No problem found* in green. *Show details* lists everything that was checked in green and red and holds the full report to copy or save. Some checks need sudo; the result says so and *Diagnose Again* takes the sudo password (sent on standard input, never shown). Nothing is changed and no password is in the report.
* **Restart a service from the diagnosis**: *Restart VNC service* / *Restart RDP service* (an SSH session offers both) restarts `tigervnc@<display>` or `xrdp` and `xrdp-sesman` on the machine over the SSH session, after asking, then diagnoses again to show the result. It is highlighted when a restart should fix the problem, such as TigerVNC blocking the address after several wrong passwords (answer `RFB 003.003`, *Too many security failures*), which a restart clears.
* **Set Up Remote Desktop: who can connect is chosen per service.** RDP and VNC each have *Through this SSH session (recommended)* or *Directly from the network*, and the sessions are saved to match: `127.0.0.1` with the SSH session as jump host, or the machine's own address without a jump host. Opening VNC directly warns that it is not encrypted and its password has at most 8 characters. The firewall network applies to the services opened directly.
* **Session Manager shows what 127.0.0.1 means**: a session that reaches `127.0.0.1` through a jump host is listed as `127.0.0.1 (on <jump host>)`, in the list, the hover card and the address bar.


* **Interface language** ([#2](https://github.com/nperiannan/termwright/issues/2)): Settings → General → **Language**. The first language is **Русский** (partial): the menus, Session Manager, Quick Connect, Settings, Session Options (page names, Connection and Authentication) and Import Sessions are translated; whatever is not translated yet stays English. Terminal output, session, folder and file names and the program's own error messages are not translated. A language is one plain list of texts (`frontend/src/app/i18n/ru.ts`), with plural forms for counts, so more screens and more languages can be added without touching the screens themselves.

## 0.23.0 (2026-10-30)

Remote desktop setup, old SSH devices and mRemoteNG import.

* **Old SSH servers** ([#4](https://github.com/nperiannan/termwright/issues/4)): a device that only offers old algorithms (for example `diffie-hellman-group1-sha1`, `aes128-cbc`, `hmac-sha1`) failed with *no common algorithm*. Session Options → Authentication → **Algorithms** now has **Allow legacy algorithms** (the old ones are added at the end of each list, so modern ones are still preferred whenever the server offers them) and lists for **Key exchange**, **Ciphers**, **MACs** and **Host key** in the OpenSSH syntax (`a,b` exactly these, `+a` add, `-a*` remove, `^a` put first). An unknown name is reported by name. When the handshake fails with *no common algorithm*, the error now says to tick *Allow legacy algorithms*. Tested against a server that offers only those old algorithms.
* **mRemoteNG import** ([#3](https://github.com/nperiannan/termwright/issues/3)): Import Sessions → *mRemoteNG connections (confCons.xml)*. Containers become folders; SSH, Telnet, RAW, RDP and VNC connections come with the user name, domain, port and protocol they inherit from their container, and the description. Saved passwords are decrypted (the default master password, or the one typed in the *Master password* box) and moved into the OS keychain, never written into the session files. HTTP, ICA, Rlogin and SSH1 connections are skipped and listed. A file mRemoteNG encrypts as a whole is refused with what to change in mRemoteNG. The password decryption follows mRemoteNG's documented format and is tested with data built the same way, not yet against a file from a real mRemoteNG.
* **Set Up Remote Desktop…**: right-click an SSH session (or its tab, or Options) to install the XFCE desktop with xrdp (RDP) and/or TigerVNC (VNC) on an Ubuntu 22.04, Ubuntu 24.04 or Debian 12 machine. The script (`scripts/remote-desktop/`) runs **in the background** over the tab's SSH connection: nothing is typed in the terminal and the dialog shows one line of progress with the details behind a link. Each service gets a **user of its own** (`name-rdp`, `name-vnc`, created without administrator rights; an existing user is left as it is). The passwords for the new users, sudo and VNC default to the SSH login's password (saved, or typed when connecting and kept in memory for that tab) and travel on the script's standard input, never on a command line, and are masked in all output. A service that is already running is left untouched; by default both listen on the machine's own address only. When it ends without errors, Termwright creates `name (RDP)` and `name (VNC)` next to the SSH session (127.0.0.1, the SSH session as jump host) with Open buttons; they store the user name and, for a user the setup created, its password. *Check First* reports without changing anything. When a service does not come up, what the setup created for it is removed again (only for that service: VNC stays when only RDP fails); users that existed and installed packages are left alone. The dialog has a **Copy log** button. *Reconfigure services that already run* repairs an earlier setup. The dialog warns when a chosen user is logged in at the machine's own screen.
* **RDP and VNC to the same machine as the same user**: opening one while the other is open asks to close the first (Cancel opens nothing). When someone else is connected to the machine's RDP or VNC, Termwright warns first and never closes their session.
* **Sessions button in the title bar**: the wide *Search or connect…* box is now a small button that drops down a list of every session with its small device icon and address, grouped by folder with favourites first; type to filter, ↵ connects, Alt+↵ opens SFTP, and an address that is no session connects to it. Quick Open (sessions, tabs, commands) stays on ⌘G / Ctrl+Shift+G and the start page.
* **View → Connect Bar in the modern layout** opens Quick Open (which does the same) and says so, instead of switching an option that only shows in the classic toolbar.
* **Compact rows keep the modern folders**: in the modern layout, compact Session Manager rows now show the coloured folders (by level, production in red) at a smaller size and the session counts, instead of the classic blue folders.
* **Files panel follows the terminal's user**: after `sudo su -` in the terminal the panel browses as root (red *root* badge) instead of showing *permission denied* for /root, also on a host behind a jump station; after `exit` it browses as the login user again. ⋯ → *Browse as root (sudo)* switches by hand. A sudo password asked for the panel no longer switches the active tab away.
* **Session Manager**: the button that shows which devices answer (pulse) is now beside the filter box in the modern layout on every platform - before, it was only in the ⋯ menu unless rows were compact. With compact rows the header has a **two-line rows** button and the ⋯ menu, so compact rows can be turned off where they were turned on.
* **macOS title bar**: the tabs and buttons of the modern title bar now sit on the same line as the window buttons (close, minimise, zoom). Termwright reads where macOS draws them, so this holds on every macOS version.

## 0.22.0 (2026-10-29)

Accounts, and sessions and settings that move together between computers.

* **Accounts** ([#1](https://github.com/nperiannan/termwright/issues/1)): Passwords & Credentials is now **Passwords & Accounts**. An account (username and password, from the keychain or a password manager) can be chosen in Session Options and the side panel (**Account**: *Default* - the folder's account, else the device type's - an account, or *Own username and password*), in Folder Defaults, in device types and in Edit Sessions. A card says who will log in. Changing an account's password updates every session that uses it; a refused login offers **Update the Account Password**; *Convert Sessions…* turns sessions that share a username and saved password into one account. Renaming an account renames it everywhere. Exports to other programs take the account's username (never its password).
* **Import / export**: Termwright's own folder, zip and XML exports keep accounts, the Files panel, *SFTP opens on*, the Wake-on-LAN options and Wake-on-LAN sessions (tested); the CSV gains *account*, *network* and *files_panel* columns and reads back RDP, VNC and Wake-on-LAN sessions with their protocol (they came back as SSH before); exports to PuTTY, KiTTY, MobaXterm, Tabby and OpenSSH leave Wake-on-LAN sessions out and list them in the summary. The password vault carries the accounts with their notes.
* **Settings in exports**: Export Sessions → Zip or Folder now includes the settings (*Include settings*, on by default): device types, accounts (without passwords), Folder Defaults, the command library, checklists, Run and Collect, backup jobs, alerts, buttons, shortcuts, workspaces, bookmarks and the Global Options. Folders on disk, the master password, open tabs and the window position are left out. Import Sessions has **Termwright .zip** and **Termwright folder** sources; when the file has settings, it shows what is in it and imports the parts you tick (Global Options only when chosen). Same-named items are replaced, the others kept; Folder Defaults, checklists, jobs and workspaces follow the *Into folder* choice and renamed sessions. Copying `settings.json` by hand is no longer needed.
* **Changed**: a device type with an account chosen in Global Options → Device Types now logs in with that account (the choice had no effect before). Sessions with their own username and password are not affected.

## 0.21.0 (2026-10-29)

A cleaner, more modern look, now the default. The **Classic** layout (toolbar, tab strip and menu bar) stays: switch at any time with View → Modern / Classic Layout, the ⋯ menu, Quick Open or Global Options → General → Layout.

* **Title bar**: the tabs (with a status dot and badges), a **Search or connect** box (Quick Open), Quick Connect, SFTP, the layout menu, the theme switch, Global Options and a **⋯** menu with File, Edit, View, Transfer, Tools and Help. On macOS the tabs share the title bar with the window buttons (after a restart when the layout is changed).
* **Quick Open connects**: type an address or a connect line (`ssh admin@10.1.1.1`, `voss 10.1.1.1`, `telnet host 2001`, `serial COM3 115200`) and Enter connects.
* **Start page**: a large search-or-connect box, quick actions (Quick Connect, New Session, Local Shell, Import, Discover) and cards for Favourites and Recent sessions with device icons, address and when they were last used.
* **Session Manager**: roomier two-line rows with a device icon (switch, router, firewall, server… from the device type) and the address, solid folders coloured by level (top-level amber, subfolders purple, deeper blue; production folders light red - right-click a folder → Mark as Production) with session counts and New / Import / Scan buttons at the bottom.
* **Session options in a side panel**: New Session and Session Options open beside the terminal with the everyday fields (name, folder, protocol, open as terminal or SFTP only, address and port or serial port and speed, device type, user name, password, environment); the Advanced rows and **More options…** open the full dialog at that page.
* **Running scripts** show as a pill in the title bar with a Stop button.
* **Many tabs**: tabs keep their names and scroll sideways (mouse wheel or trackpad); the active tab is always scrolled into view.
* **Compact Session Manager** (Global Options → General, or ⋯ → Compact Session Manager): one line per session, as in the classic layout, for long lists.
* **Terminal** (modern layout): more room around the text, the cursor in the accent colour, a lighter terminal bar and a tidier status bar (state, protocol · user@host:port, device type, size).
* **Keyword highlighting colours the text** (up in green, down in red, addresses in blue) instead of the background; Global Options → Keyword Highlighting → Style brings the coloured background back.
* **Connecting card**: while a session connects for the first time, a card shows each step (opening the connection or the jump host, checking the host key, logging in, starting the shell).
* **Error card**: when a session cannot connect, a card shows the reason, a hint and Try Again, Edit Session, Find by MAC Address and the other fixes.
* **Welcome flow** once after installing or updating to 0.21 (Help → Welcome… shows it again): pick the theme, text size and layout; import sessions - SecureCRT, PuTTY, SuperPuTTY, MobaXterm and OpenSSH sessions found in their usual places are listed with an Import button, export files (SecureCRT .xml/.zip or Config folder, PuTTY/KiTTY .reg, SuperPuTTY Sessions.xml, MobaXterm, CSV) are one click away; then a few tips to get started.
* **New device and session icons**: a vivid rounded tile with the device drawn in white - a switch with its ports, a router with antennas, a firewall shield, a ceiling access point, a rack server, and computer, storage, phone, mobile, IoT and printer. View → Tile / Glyph Icons (or Global Options → General → Icons) shows just the device in its colour, without the tile.
* **Keyboard shortcuts for the menus**: nearly every menu command has one, e.g. Disconnect ⌘D, Clear Screen ⌘⇧K, Auto Scroll ⌘⇧A, Timestamps ⌘⇧T, Log Session ⌘⇧L, Open SFTP Tab ⌘P, Save Screen As Image ⌘⇧P, layouts ⌘1-⌘4 (on Windows and Linux Ctrl+Shift+key and Alt+Shift+key). Help → Keyboard Shortcuts lists them all.
* **Device facts**: model, version, serial, MAC address and uptime (and any value you add) are read after connecting, in a background login - nothing is typed in your tab. Version and uptime are read at every connect, model, serial and MAC once. They show on the tab (hover), the status bar ("VOSS 8.10.0.0 · up 41 days"), the start page cards, the Session Manager hover card, the session side panel and the HTML scrollback report; a MAC address found is saved with the session, so Find by MAC works. VOSS, EXOS, Cisco IOS and Linux come with the rules; right-click a session → Refresh Device Facts reads them now.
* **Teach a device fact in two clicks**: run the command in a tab (e.g. show sys-info), select the value, right-click → Save Selection as Device Fact. Termwright works out the command and the pattern (regex) from the output, shows that it finds the value, and saves it with the device type. Global Options → Device Types lists and edits the facts.
* **Device Facts in Session Options**: every fact in one list - value, command, every connect or once - with Edit (name, value, command, pattern; the pattern is tested against the last output) and Remove (stops reading it for the device type and clears the value), Add by Hand for a value without a command (e.g. Location), Read Now, and teach a new fact without the terminal: type the command (e.g. show sys-info), Run - it runs in a background login - select the value in the output and Save Selection as Fact. The session side panel's Device facts box has Edit, which opens this page. Save Selection as Device Fact is also in the Edit menu (⌘⇧6 / Alt+Shift+6), for when a right-click in the terminal pastes.
* **Keyword highlighting is on**: the Network set (up, enable(d), forwarding in green; down, error, failed in red; warning, disable(d) in amber; IP addresses in blue) is used by every session that does not choose its own; Settings → Keyword Highlighting → Default set → None turns it off.
* **Clear Recent / Clear Favourites**: right-click the Recent or Favourites heading, the Session Manager's ⋯ menu, or Clear on the start page; right-click a recent session → Remove from Recent.
* **Create SFTP Session / Create SSH Session**: right-click an SSH2 session (Session Manager, start page card or tab) to save a copy that opens the SFTP file browser - or, for an SFTP session, a terminal - with the same address, login and saved password.
* **Save Screen As Image** (File menu, tab menu): the visible terminal screen as a PNG, with its colours and highlighting.
* **Folder picker**: Discover Devices (Add to folder) and the session side panel choose any existing folder or subfolder from a list, or take a new name.
* **Discover Devices** in the modern layout: a cleaner scan bar, progress and results table.
* **Quick Connect** (modern layout): one box takes a connect line (`voss admin@10.10.0.7`, `ssh user@host:2222`, `serial COM3 115200`) and fills the fields below it - protocol, address and port, device type, user name - or fill the fields yourself; a switch saves it as a session, More options… opens the full Session Options.
* **Session Options** (modern layout): the device icon, name, folder, protocol and address at the top; navigation with icons (Connection, Authentication, Jump Host / Proxy, Port Forwarding; Terminal: Appearance, Logon Actions, Logging); Name | Folder (folder picker), Protocol, Address | Port and Device type | Environment side by side, and a card with Use the device type's login and Reconnect automatically.
* **Settings** (Global Options in the modern layout): Appearance first with theme previews and Layout / Icons switches, then Behaviour (switches with a short explanation under each) and Folders and files; the navigation has icons and Automation and System groups.
* **SFTP** (modern layout): a bar with the session, Run as and the copy protocol (SFTP / SCP); flat panes with a folder path you can click (each folder opens with one click, a click beside it types a path), a .. row, amber folders, coloured file-type badges (CFG, LOG, IMG, TXT, KEY…), Name / Size / Modified (Today 09:41, 12 Oct - the full date and the permissions on hover), and one transfer bar with progress, speed, time left, Cancel and the other transfers; ✕ closes it (running transfers go on in the background, the bar comes back for the next one). The classic transfer list has Close too. A bookmark bar under each path (click to open, right-click to remove, + Bookmark), and a Delete button on both sides: on the server it deletes, on this computer it moves to the Trash / Recycle Bin.
* **Command Window**: Active tab / All connected / Selected as one switch and Send as the main button; the find bar has icon buttons.
* **Import / Export** (modern layout): Import shows the sessions found on this computer first (SecureCRT, PuTTY, MobaXterm, OpenSSH - Use fills the source and path), then the sources as cards, a folder picker and Keep both / Replace / Skip. Export: All or Selected, Termwright formats as cards, the other programs as chips, and where to save.
* **Passwords & Credentials, Command Library, Command History, Workspaces, Folder Defaults** (modern layout): icon headers; credentials in a table with password-manager and remove buttons; commands listed with their first line, Name | Folder, device-type chips, Send to as a switch and To Command Window; history rows with a device badge, age and count and the keys in the footer; workspaces as cards with a layout preview and their sessions (green when open); folder defaults in pairs with the folder's colour.
* **Prompts** (New Folder, Rename, confirmations, passwords and host keys) have an icon and a button that says what it does (Create, Rename, Save).
* The **+** after the tabs opens New Session (Quick Connect is the lightning button).
* **Backups, Change Checklists, Run and Collect, Recordings, Key Login Setup, Software Update, About, Keyboard Shortcuts, Console Server Ports, Bulk Edit, Devices With a New Address, Compare** (modern layout): icon headers, tables with coloured status pills (no change / changed / failed), checklist steps Before → After → Compare, choice cards and a key-type switch for key login, the nightly build folded away in Software Update, features as tiles in About, a searchable Keyboard Shortcuts list with keycaps, a switch per field in Bulk Edit, and old → new address for moved devices.
* **Start page**: the search box, the quick buttons (spread across its width), the card headings and Clear line up on one column; bigger session cards (lift on hover), and a card with Connect when there are no sessions yet.
* **Master password**: an **unlock window** at start-up when a master password is set (Unlock, or Not now to be asked for passwords when connecting); **Lock Now** in Passwords & Credentials; any 4 or more characters with a strength meter (no forced capitals, numbers or symbols); the welcome flow has an optional master password step.
* **SFTP**: check boxes to select several files (and a select-all box), **New File** on both sides (never overwrites), Delete on both sides (this computer: to the Trash / Recycle Bin), a bookmark bar.
* **Quick Connect** remembers *Save as a session*, and Settings → General → *Save Quick Connect sessions* saves every Quick Connect.
* **Terminal font**: 14 with normal line spacing (the font's own); Settings → Terminal → **Line spacing** (1 - 1.35) next to Font size.
* **SFTP lists scroll** in the modern layout (a long folder pushed the panes and the Copy / Sync / Diff buttons out of the window), and the keyboard works in them: ↑ / ↓ (Shift selects a range), Home / End, Page Up / Down and Enter to open.
* **Timestamps** start on the first line after they are turned on (devices that end lines with \n\r lost the stamp of the first line).
* **SFTP through a jump station**: after `sudo su -`, `sudo -i`, `su` or a new shell in the terminal an SFTP tab follows, the folder and user are read again from the new shell, so an SFTP tab opened before becoming root follows root.
* **SFTP**: the copy arrows, **Sync** (→ / ←) and **Diff** sit between the panes; select one file on each side and Diff opens Compare with both (line numbers, ignore whitespace and timestamps). The transfer bar always shows the speed, and a finished transfer its average speed.
* **Session Manager**: device types line up in one column (room for any IPv4 address), and a new hover card (device icon, name, address, then Via, device type, facts, MAC and login in two columns) that opens faster.
* **SFTP Diff**: compares any two selected files - one on each side or both on the same side (disabled with more than two); **Compare** says at the top in green that the files (or outputs) are identical - exactly, or once whitespace / timestamps are ignored - or in amber how many lines differ.
* **SFTP and the terminal through a jump station**: the terminal's host is read from the shell's folder report, so an SFTP tab on the jump station follows the terminal once you `exit` from the target (folder detection is turned on in the shell you return to), and the target's SFTP tab stops following it. When the terminal is on the other host, the tab shows **>_ On <host> ▾**: Switch This Tab to <host> reopens it there. An SFTP session with its own login follows an open terminal tab of the same session too. **Open SFTP Tab** (title bar, ⌘P, tab menu) on such a session opens on the host the terminal is on - the target, or the jump station after `exit` - so the tab can follow its folder (the jump station if SFTP on the target does not start). Session Options → Logon Actions → **SFTP opens on** has *Where the terminal is* (the default), *Always <jump station>* and *Always <target>*; a host chosen there wins (sessions set to the jump station before keep *Where the terminal is* - choose *Always* again if you want it).
* **Session Manager: Copy, Cut, Paste and Undo**: copy or move sessions, selections and whole folders (right-click or Cmd/Ctrl+C / X / V); dragging or pasting into another folder asks first (Settings → General → *Confirm before moving sessions*, on by default); **Undo** (⋯ menu, Cmd/Ctrl+Z, or the notice) takes back the last three moves, pastes or deletes and says first what it will do (*session X will be moved from Y back to the top level*, *folder A will be re-created*, *session D will be deleted*).
* The session side panel has **More Options…** next to Cancel.
* **Wake-on-LAN sessions**: a new protocol (New Session → More… → Wake-on-LAN) with the MAC address, the network or the IP address (one of them is required; `192.168.1.0`, `192.168.1` or an address mean its /24) and the UDP port (9); *Wake Up and Connect to …* opens the device's other session (same MAC or IP) once it answers. Double-click sends the magic packet to that network's broadcast address only; with an IP address the device is pinged until it answers (and its NetBIOS name read), with only a network the network is searched for the MAC address 4 times, 30 s apart (both settings) and the address found is saved - or the device is marked not present / down. Sessions with an address are pinged every 2 minutes (setting): a green dot when up, orange when down. Own power icon in the Session Manager.
* **Wake-on-LAN**: right-click a session with a MAC address → **Wake Up** or **Wake Up and Connect** (also the tab menu, Transfer menu, folders and selections, and the error card when a connection times out). The magic packet goes to the broadcast address of the device's network only, from this computer or - for a device behind a jump host or jump station - from that server (python3 or wakeonlan); Session Options → Connection has the broadcast address, port and *from*. Termwright then watches the device's port for up to three minutes and says when it answers.
* **Web pages through a session**: right-click a session or tab → **Open Web Page Through This Session…** (any address, out through that SSH server - internal names like `nas.lan` work) or **Open Web Interface…** (the device's own web UI through its jump host or jump station). Chrome, Edge, Chromium, Brave or Firefox opens in a window of its own with a temporary profile whose traffic and name lookups go through a private SOCKS5 tunnel; the web server only sees the SSH server. Closing the window ends the tunnel and deletes the profile.
* **Files panel** beside an SSH terminal - on the side opposite the Session Manager (Session Options → *Files panel beside the terminal*, or View → Files Panel / the title-bar button / ⌘⇧1 for a tab): a small file browser that **follows the terminal's folder** (through a jump station too), **cd's the terminal** to a folder you open in it (when the shell is at a prompt), and opens a **double-clicked file on this computer** with its usual program; when you save it, the panel asks to **upload** it back (Upload Now / Not Now / Stop Watching). Upload, Download…, New Folder, Rename, Delete, Copy Path, hidden files on/off, drag the edge to resize. Programs and shortcuts (.exe, .app, .command, .bat …) are never opened from a server.
* The **session side panel** has the *Files panel beside the terminal* and *Reconnect automatically* switches for SSH sessions.
* **Command History**: wider, a tidy footer, a trash button on each command (or Shift+Delete) to forget a mistyped or no longer supported command, and **Clear History…** (a red button) asks which device types to forget.
* Passwords & Credentials: Export / Import no longer overlap the fields above them.
* **Docs**: a rewritten README (overview, highlights, capabilities, installation, migration, data and privacy), the User Guide and FEATURES updated for every 0.21 feature, and new screenshots of every page.
* Corners are less rounded (dialogs, cards, buttons); on macOS the title bar is as tall as the window buttons need, so the sidebar button and the tabs line up with them.
* Dialogs, menus, notices, the Command Window and the Button Bar use the same design (surfaces, rounded corners, spacing) in the modern layout.
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
