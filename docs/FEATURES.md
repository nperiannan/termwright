# Termwright features

Everything Termwright does, one line each. Details and screenshots are in the [User Guide](USER_GUIDE.md); scripting is in [SCRIPTING.md](SCRIPTING.md). This list is for version 0.21.

## Platforms and installation

- **macOS, Windows and Linux**: one app for all three (Apple Silicon Mac; Windows 10/11 x64; Linux x64 and ARM64).
- **Installers**: DMG for macOS, setup.exe for Windows, .deb for Debian/Ubuntu, plus zip / tar.gz archives that run without installing.
- **Portable mode**: a `termwright-data` folder next to the program keeps all settings there (e.g. on a USB stick).
- **Own data folder**: settings, known hosts and logs in one folder (`TERMWRIGHT_HOME` can move it); passwords stay in the OS keychain.
- **Updates**: Help → Check for Updates shows the installed version, the latest stable release with its changes and newer nightly builds; downloads and opens the installer.
- **Update channels**: be told about stable releases, stable and nightly, or nothing; optional check at start-up.
- **Release notes**: every release and nightly build lists its changes (CHANGELOG) and commits.
- **Keeps the computer awake**: while Termwright is open the computer does not go to sleep on its own (macOS, Windows, Linux; the display can still turn off) - a Global Option, on by default; also from the AI assistant, MCP, the local API and Python (`keep_awake`).
- **Free and open source**: Apache-2.0.

## Main window

- **Modern layout** (default): tabs, a Sessions button (every session with its icon, filter, connect), Quick Connect, SFTP, the theme switch, Settings and a ⋯ menu in one title bar.
- **Styles**: **Glacier** (the default) - frosted glass on the title bar, Session Manager and start page, raised start page cards three to a row, graphite in the dark theme and icy in the light one - or **Standard**; Settings → General → Style.
- **Glass or flat folder and file icons**: in the Session Manager, the Files panel, SFTP and the folder picker; *As the style*, *Glass* or *Flat* (Settings → General → Folder and file icons).
- **Start page**: a search-or-connect box, quick actions spread under it and cards for favourite and recent sessions (device icon, address, device facts, last used, live state).
- **Start page footer**: the slogan, the installed version and whether it is up to date, the GitHub download count, the licence and a link to the project.
- **Session side panel**: New Session and Properties open beside the terminal with the everyday fields, *Log in with* (Password, Key, Key then password - with key generation and copying the key at the first login), the Files panel, auto-reconnect and legacy algorithm switches and the device facts; All session options… opens the full dialog.
- **Connecting and error cards**: each connection step while it connects; on failure the reason, a hint and Try Again, Edit Session, Find by MAC Address, Close Tab.
- **Welcome flow**: theme, text size and layout; import found sessions; optional master password; tips (Help → Welcome…).
- **Menu bar**: File, Edit, View, Transfer, Script, Options, Tab, Help; on macOS also inside the window (can be turned off).
- **Toolbar**: connect, quick connect, local shell, reconnect, disconnect, copy, paste, find, log, SFTP, scripts, layouts, lock and more.
- **Connect Bar**: type `ssh admin@host`, `telnet host 2003`, `serial COM3 115200`, `raw host 4001`, `rdp host`, `vnc host`, `voss 10.1.1.1` or a session name and press Enter.
- **Tabs**: one per connection with a status dot (connected, connecting, error, disconnected); middle-click closes.
- **Drag tabs** to reorder them (in the title bar); the order is kept for *Reopen the tabs that were open*.
- **Double-click the empty title bar** to zoom (or minimise) the window, as the system's title-bar setting says.
- **Tab badges**: `LOG` (logging), `● REC` (session recording), `SCRIPT` (script recording), `SEL` (in the send group), `PROD` (production).
- **Layouts**: tabbed, tile side by side, tile stacked, tile grid.
- **Status bar**: state, protocol, user@host, server version, route (jump host), transfer progress, terminal size, emulation, connected tabs.
- **Restore tabs**: the open sessions come back at the next start (optional).
- **Window size and position** are remembered.
- **Interface font and text size**: the Mac look by default (Inter on Windows and Linux, shipped), System font of this computer, VS Code, Inter or IBM Plex Sans; text from 13 to 17 px. Terminals use Termwright Mono (DejaVu Sans Mono) where Menlo is missing.
- **Themes**: dark, light or follow the system (the default), switched with the sun / moon / screen icons at the top right, with an accent color.
- **Text kept as typed**: no auto-capitalisation, spelling or quote correction in fields (macOS).
- **About**: version and a Copy Link button to share Termwright.

## Connecting

- **Protocols**: SSH2, Telnet, Serial, Local Shell, Raw TCP, RDP, VNC and Console (a port of a console server).
- **Accounts**: a username and password (keychain or password manager) shared by sessions, folders and device types; each session chooses *Default* (folder → device type), an account, or its own login; one password change updates every session; Convert Sessions turns sessions with the same login into an account; *Update the Account Password* on a refused login.
- **Accounts with a private key**: an account logs in with a password or an SSH key (and its passphrase); Quick Connect can log in with an account.
- **Copy, Cut, Paste and Undo** in the Session Manager: copy or move sessions and whole folders (Cmd/Ctrl+C / X / V), confirmation before a move (on by default), and Undo of the last three moves, pastes or deletes, each saying first what it will do.
- **Saved sessions**: double-click, Connect Terminal, Connect SFTP Session, or Connect All in Folder.
- **Quick Connect**: one connect line (`voss admin@10.1.1.1`, `ssh user@host:2222`, `serial COM3 115200`) fills protocol, address, port, device type and user; or fill the fields; save it as a session (the switch is remembered; Settings → Save Quick Connect sessions makes it the default).
- **Quick Open** (`Mod+G`): find a session, folder, host, open tab, library command or menu action by typing.
- **Workspaces**: save the open tabs with their layout and reopen them with one click.
- **Local Shell**: zsh/bash on macOS and Linux, PowerShell on Windows; custom command and start folder.
- **Clone Session**: a second tab to the same device.
- **Reconnect**: Enter in a disconnected tab, the Reconnect button or `Mod+R`.
- **Auto reconnect**: retries after 2, 5, 10, 20, 30 s… (not after you disconnect or a login fails).
- **Errors that offer the fix**: a failed connection explains why and offers Reconnect, Edit Session, Edit Jump Host, Device Type Login, Forget Saved Password or Find by MAC Address.

## Teams (optional)

Without a team Termwright works on its own and sends nothing anywhere.

- **A team server of your own**: Options → Teams… → *Set Up a Team Server…* starts a small Docker container on a lab host over SSH (free port from 7443, image downloaded or copied over SSH), restarting with the host; several teams can share a host. Standalone packages and a Compose file for manual installs.
- **Join with an invite**; Termwright trusts the server only by its certificate fingerprint. Roles: owner, co-owners, members.
- **Shared inventory**: the team's devices appear as a folder in everyone's Session Manager and follow changes within seconds; logins stay personal.
- **Push to Team…** from the Session Manager or Discover: devices the team has already (by address or MAC, any name) are recognised; members' pushes wait for approval.
- **Who uses which device** (network devices only - not computers, Linux machines, phones, printers or IoT boards): badges for open (● initials) and reserved (🔒), a card with the reservation, who has it open since when, last used and free for; **Reserve…** until a time with a note; a warning and a notice when connecting to someone else's reservation.
- **Admin page** (owners and co-owners, opened signed in from Termwright, no passwords on the server): overview with utilization, busy hours and insights (what to buy, what nobody uses), usage reports per device, member, reservation and session with CSV export, inventory with folders and CSV import/export, approvals, scans from the lab host, members and invites, activity log; light and dark theme.
- **Tell Me When It Is Free**: wait for a device someone else has open or reserved, a notice comes when it is free. **Hand over** the team to another member (the owner stays as co-owner). **Backup & restore** on the admin page (owner), and an **Activity** log of 5000 events with filter and CSV export.
- **Single sign-on** for teams: members join and stay with their company account (Microsoft Entra ID, Okta, Google - any OpenID Connect service), with allowed domains and groups and a sign-in again every few weeks; set up on the admin page.

## Network emulation lab

- **Emulated lab**: the vendors’ own VM images (qcow2, GNS3 appliance settings) booted with QEMU/KVM on a Linux host over SSH - the real OS; host check and one-click QEMU/KVM set-up, image profiles with day-0 commands typed on the console, copy-on-write disks, port forwarding or bridge management, SSH and console sessions per switch through the host, virtual cables that can be pulled, Restart and Back to Day 0.
- **Several KVM hosts in one topology**: cables between hosts are tunnelled, a running switch moves live to another host with the same QEMU, and the CPU tab shares or limits each host's cores.
- **Images**: qcow2 files, GNS3 appliances, a public catalogue, cloud images, and **Install from ISO** with the installer's console in the page (type its answers, *Take Over by Hand*); *Installed from ISO* lists the images made with their real size.
- **Built-in nodes**: Linux PCs (a desktop with a browser when needed), L2 switches, hubs, HTTP / FTP / TFTP / DHCP servers, a RADIUS server for 802.1X, name servers, a route generator (OSPF, BGP) and the host's network as a cloud.
- **Microcontrollers**: ESP32-S3, ESP32-C3, ESP32-C6 and ESP32-H2 in Espressif's QEMU, with their MicroPython console - on a KVM host or this computer.
- **Traffic generator**: test ports on switch ports; UDP, TCP, ICMP, ARP, raw and pcap streams with VLAN, 802.1p and DSCP, fixed, increasing, random or IMIX sizes and fields that change per frame; rates in pps, Mbit/s or % of line; latency, jitter, loss, order and per-flow counts; Statistics for every test port; a library of ready tests; **reports to share** as HTML, PDF or an Excel workbook with charts: the topology with the streams, graphs, frames sent and received (the first ones decoded, with what the switch changed), flows and test ports. **Quick streams and tests**: L2 and L3 streams, latency, jitter, loss, RFC 2544, bad packets, fuzz, VLAN isolation, MTU and 802.1X clients from a few questions, with everything filled in shown before it starts. **Custom frames** in hex, user-defined fields, 23 malformed-frame faults, fuzzing and streams that pass when dropped - no Scapy needed. **Headers**: QinQ, MPLS, GRE, VXLAN and IPv6 extension headers. **Protocols on test ports**: 802.1X (EAP-MD5, PEAP-MSCHAPv2), DHCP and DHCPv6 clients at scale, LACP, LLDP, STP/RSTP, VRRP and BFD with the switch's takeover and detection times measured. **TCP sessions**: real connections (handshake, data both ways, FIN) with connected, refused, timed out, setup time and sessions a second - for ACLs, firewalls, NAT and session limits. **Engines**: the built-in one (about 100,000 frames a second per host) or the fast one, a compiled helper copied to the hosts (about 420,000 frames a second of 64 bytes, 2.5 Gbit/s of 1518 bytes on two vCPUs). **Real switches**: a KVM host's spare network card cabled to a real device is a test port; **TRex** servers' ports are test ports too, for line rate on real hardware.
- **Tests**: RFC 2544 throughput, latency, frame loss and back-to-back; RFC 2889 address learning; convergence while a cable is pulled or a switch restarts; iperf3 between lab computers.
- **Test ports as hosts**: 802.1X and DHCP clients behind a test port (up to 4,000), multicast groups, LACP, LLDP and spanning tree.
- **Packet capture** on any switch port, decoded in Termwright.
- **Emulated Lab workflow picture**: a one-page picture of the nine steps from an empty KVM host to a traffic report, shown when the emulated lab opens (it can be turned off with *Do not show this when the Emulated Lab opens*) and always available from the **?** button in the lab and under Help → Emulated Lab Workflow….
- **The Lab menu**: *Emulated Lab*, *Simulated Lab*, *Overview*, *Traffic Generator*, *Appliances* and *KVM Hosts*.
- **Local API, command line and Python**: a REST API on this computer (OpenAPI), `termwright lab …` and a Python client drive the same lab tools - no approval asked, for your own automation.
- **Simulated lab**: switches simulated on this computer (an SSH server each, no VM) from a device pack (.vm) loaded with consent: models with their port layout, command line with abbreviations, `?` help and Tab, VLANs, ports, IP interfaces, save / reset, ping and LLDP over drawn cables; New Lab wizard with leaf-spine, chain or ring cabling; Lab window with list, topology and cables; sessions in a Simulated Lab folder with live state.

## Tests (record and replay)

- **Test recorder**: Tests → Record a Test; commands typed in terminal tabs and requests sent in the API Client become steps; right-click selected output → *Expect this* (contains, field = value with the regular expression made from its label, matches, does not contain) or *Save as Test Variable*; **+ Check** for status, JSON path, time.
- **Tests tab**: tests by folder with their last result, testbeds, reports; a test as steps, YAML (edit and save) or generated Go; run one or all with a testbed; results with the first failure; HTML report and JUnit XML per run.
- **Test format**: YAML - `run` (session, command), `http` (session, method, url, headers, body) and `snmp` (session, get / walk / set) steps; checks `contains`, `notContains`, `matches`, `status`, `maxMs`, `field` / `json` / `oid` with `equals`, `notEquals`, `greater`, `less`, `exists`; `save` for `{{variables}}` (a value that changed: save, then `notEquals`); `params` to run a test once per set of values; `continueOnFail`, `timeout`.
- **Setup, cleanup, restore**: `setup` and `cleanup` sections (cleanup always runs, its failure reported as a dirty device); undo suggestions while recording from device type undo rules (built in for the standard types), a POST's DELETE or an SNMP set's old value, or the AI assistant; `restore: [config]` (device type save/restore commands) and `restore: [lab]` (emulated lab checkpoints).
- **SNMP in tests**: gets, walks and sets in the SNMP Browser are recorded; SNMP values checked by name or OID, readable (`up`) or raw (`1`).
- **Roles and testbeds**: recorded sessions become roles (`{{core_sw1}}`); a testbed written from a session folder (the emulated lab's, a site) points the same tests at other devices.
- **Git**: Pull (fast-forward only), Commit… and Push in the Tests tab, with ↑ / ↓ counts; reports stay out of Git.
- **Python and TCL tests**: each test is also written as a Python `unittest` test (pytest runs it too; `lib/python/run_tests.py` with JUnit) and a TCL script (Tcl 8.6, TAP output, `lib/tcl/run_tests.tcl` with JUnit); standard libraries only.
- **Scheduled runs**: daily, weekdays, weekly or every N hours, with a testbed; last result and next run in the Tests tab; a notification when a run fails; production only when allowed.
- **AI**: *Write a Test with AI* (the assistant writes, saves and runs a test from a description) and *Explain* on a failed test (device, network or test - with a corrected test); *Tests* is one of the assistant's tools.
- **Headless runs for CI**: `termwright tests run` (JUnit, exit code, `--production` to allow production sessions), `termwright tests serve` (only the local API, for the generated code), `termwright tests secrets`; `TERMWRIGHT_SECRET_<KEY>` environment variables where there is no credential store.
- **Go tests**: each test is also written as a Go test with a small standard-library helper; `go test ./cases/...` runs them through Termwright (saved logins, no passwords in code); `go run ./lib/junit` makes JUnit XML for CI.
- **Tests folder**: any folder, a Git repository too (Clone…; branch and changes shown); testbeds fill the variables; production sessions are asked first; tests with passwords are refused.
- **Honest results**: device error messages (`% Invalid input`, `Error:` … built in for seven device types, your own per device type) fail a step; steps without checks are marked *ran, nothing checked* with their output; the recorder suggests checks from the configuration you recorded (`contains v200`, in the cleanup `does not contain v200`) and asks before saving read steps without checks.
- **Failed cleanups**: a test whose cleanup fails is reported as *failed (cleanup)*; the run then stops (remaining tests skipped, with the reason), restores the configuration saved before the run, or resets the devices to factory default (never production), waits for them and goes on.
- **Checks from the configuration**: while recording, show commands get the checks for what was configured before them (field, word + value, or text; *does not contain* in the cleanup), only ones that hold for the recorded output.
- **Tables**: TextFSM templates (ntc-templates format, built-ins for EXOS, VOSS and Cisco) turn output into tables; table checks and saves by row and column, in runs and generated code.
- **Suites, tags, dependencies**: suites/*.yaml, tag filters, dependsOn with skipping and ordering; run by suite or tag in the GUI, CLI, schedules and API.
- **Logic**: wait, if, retry until the checks pass, loops over values, ranges or variables - in Termwright's runs and the generated Go, Python and TCL.
- **Testbeds at scale**: locks in the tests folder (shared, stale after 12 h, take-over and release) and the same tests on several testbeds at once.
- **Watching and comparing**: syslog and SNMP notifications that fail a test while it runs, configuration compared before and after, lab traffic test steps, software upgrade steps.
- **History**: pass rate, flips (flaky), durations and results per software version; versions recorded in each run and report.
- **Integrations**: qTest (send results with the report), Jira (defects with steps and output), Git host token - addresses in the settings, tokens in the OS keychain; automatic sending and defects.
- **Workspace**: Tests, Runs, Schedules, Testbeds and Library; tests as tables with expected vs got; a Run panel for every run setting.
- **Edit and select**: edit steps and checks in the Tests tab, rename, duplicate and delete tests (to `.removed/`), run only the ticked tests or a folder; the `lib/` helpers are listed apart from the tests.
- **Tests for AI and automation**: the `tests` tool for the AI assistant and MCP apps, `POST /api/v1/tools/tests`, and `lab.tests` in Python.
- **Procedures and data**: shared steps with parameters (`procs/`, call: {proc, with}), made from a test in one click; CSV or Excel test data, one run per row.
- **NETCONF**: get, get-config, edit-config (candidate + commit) and RPCs over SSH, XML replies checked by path, rpc-errors reported.
- **Breakpoints and stepping**: stop before chosen steps or every step; run, skip, continue, change variables.
- **Checks over time**: sample a step N times and check average, max, min, change or rate.
- **Table maker**: click a header line (or label: value lines) to turn output into a table with live preview; saved as TextFSM.
- **Standalone suites**: export tests, a plan or all as a zip for Linux, macOS or Windows - code (Go, Python, TCL), library, testbeds, plan and run script writing JUnit XML and run logs; the library adds run logs, configure, wait_for, log and testbed roles.
- **Debugging**: pause on a failed step (retry, continue, stop, open the device), run one step or from a step, variables per step.
- **Import from Postman and OpenAPI**: one test per request or operation, folders and tags as suites, status checks from the scripts, examples or 2xx answers; Excel and CSV manual test cases too.
- **Team roles and approvals**: `roles.yaml` (editors, runners, approvers) and approved runs on production sessions (or always), requested and decided in the Tests tab, valid 24 hours, in the audit log.
- **JSON Schema checks**: `schema` on API steps (inline or a file); SNMP gets and walks recorded with checks that hold (set values, configured names; no counters).

- **Run control**: rerun failed tests (flaky marked), per-test and per-run timeouts, in the Run panel, schedules, CLI and tools.
- **Notifications**: results of scheduled runs, API monitors and SNMP alarms to a Teams / chat webhook or e-mail.
- **PDF report and trends**: `report.pdf` per run; Trends with pass rate per run, most failing and flaky tests, pass rate per software version.
- **Testbed calendar**: book testbeds for a time (shared `bookings.yaml`); other people's runs and schedules respect it.
- **Standalone export**: Python or Go code, or the twrun program with the YAML tests - runs without Termwright - SSH/Telnet/API/SNMP/NETCONF/web itself, inventory from your sessions, passwords from the environment or keyring; procedures as reusable functions.
- **Spirent TestCenter / IxNetwork**: traffic runs as test steps through their REST APIs - configuration, chassis ports, protocols, per-stream loss and latency.
- **Web steps (Playwright)**: open pages, click, type, check text, screenshot - set up from the step editor.
## AI assistant and connector

- **AI assistant** (optional, off by default): a chat panel docked right or left or floating, with chat history, pasted pictures and *Add context* (selection, clipboard, whole terminal, other tabs, files) and a token warning; **Agent mode** runs commands in your tabs to debug or fix - read-only commands by themselves, changes after you allow them, production always asks; *Explain with AI* for output and errors and *Ask AI for a Command* for the tab's device type, with Claude, an OpenAI-compatible service, a local Ollama or **Claude Code** (your own Claude login, no API key); secrets removed before sending, commands pasted without Enter, *Save* to the Command Library.
- **The agent and the lab**: it builds topologies, starts and configures switches on their consoles, cables them, runs traffic tests and reads the Events - each change approved by you; it looks up Termwright's own guide and live state (version, KVM hosts and their VMs, teams).
- **Connector for AI apps (MCP)**: Claude Desktop, Claude Code and other MCP apps (Cursor, VS Code, Windsurf, Gemini CLI, Codex - a ready config to copy; Help → Connect an AI App…; *Used by* shows which apps used it) can list your tabs and sessions, read a tab (secrets removed) and - after you allow it in Termwright, each time or for 15 minutes per tab - open a session and run a command; local only, off by default.

## SSH2

- **Mosh** for SSH sessions: roaming over Wi-Fi changes, sleep and new addresses with the mosh client of your computer (session's address, port, user and key).
- **Password and keyboard-interactive** logins, with the password saved in the OS keychain if you want.
- **Public keys**: identity file, encrypted keys, default `~/.ssh/id_*` keys and the SSH agent.
- **PuTTY `.ppk` keys** used directly (formats 2 and 3), no conversion.
- **SSH certificates**: `key-cert.pub` next to the key or any certificate file.
- **FIDO2 / U2F security keys** (`sk-ed25519`, `sk-ecdsa`) through ssh-agent.
- **Smart cards** (PIV, CAC, YubiKey, other PKCS#11 tokens): any key on the card or a chosen one; PIN asked once and kept in memory only.
- **Kerberos / GSSAPI**: uses your `kinit` ticket, or logs in to Kerberos itself.
- **Host key checking**: known_hosts (Termwright's and `~/.ssh/known_hosts`), prompts for new and changed keys.
- **Old devices**: *Allow legacy algorithms* (`diffie-hellman-group1-sha1`, `aes128-cbc`, `hmac-sha1` and more) per session - in the New Session / Properties panel, Session Options and Quick Connect - or for a whole folder at once (Edit All Sessions in Folder → *Old SSH algorithms*); or key exchange, cipher, MAC and host key lists set by hand in the OpenSSH syntax.
- **Keep-alive** at a chosen interval.
- **Remote command**: run a command instead of a shell, e.g. `ssh -t` from a jump station to a cloud host.
- **Agent forwarding** (including smart card keys) and **X11 forwarding**.
- **Send Break**: SSH break request (also serial break and Telnet BREAK).

## Key login setup

- **Generate keys**: Ed25519, RSA 4096 or ECDSA, or reuse an existing key.
- **Install the key** on Linux, macOS, BSD or Windows OpenSSH servers (incl. `administrators_authorized_keys`).
- **Test and switch**: tries a key-only login, then switches the session to the key.
- **Key passphrases** remembered in the keychain, listed, changed or forgotten in Passwords & Accounts.

## Telnet, Serial and Raw

- **Telnet** with option negotiation (window size, terminal type, echo, suppress go-ahead), and **Telnet over TLS** (telnets, port 992) with the system's certificate check or *Accept any certificate* for self-signed appliances.
- **Local echo** for Telnet, raw and serial devices that do not echo what you type.
- **Serial**: COM ports / tty devices listed in order with the USB adapter's name, updated by itself when a cable is plugged in; baud rate, data bits, parity, stop bits.
- **Detect Speed**: finds the baud rate of a console (Enter at each common speed, the readable answer wins); optionally on every connect.
- **Send bar** on serial tabs: port and speed of the connection, message box with CR / LF / CR+LF / no line ending, ↑/↓ recall, Timestamps, Auto scroll, Clear and Save; available on any tab from the right-click menu.
- **Hex on serial**: send bytes typed in hex (`48 65 0D 0A`, `0x02,0x03`, `4865`) and view what the device sends as hex bytes.
- **Serial flow control**: none, RTS/CTS or XON/XOFF; DTR/RTS on open; break.
- **Raw TCP** for terminal servers and devices with a plain TCP console.
- **Send initial carriage return** to wake up consoles.

## Remote desktops

- **RDP** opens in the computer's Remote Desktop client (Remote Desktop Connection on Windows, Windows App on macOS, FreeRDP or Remmina on Linux).
- **RDP options**: Windows domain, full screen or window size; saved password handed over (Windows) or Copy Password (macOS).
- **VNC** in a built-in viewer tab: VNC servers, Raspberry Pi, Linux desktops, macOS Screen Sharing.
- **VNC options**: view only, fit to tab, Ctrl+Alt+Del, clipboard both ways.
- **Through jump hosts and proxies**: Termwright opens a local tunnel for RDP and VNC.
- **Set Up Remote Desktop…** (right-click an SSH session): installs XFCE with xrdp and/or TigerVNC on Ubuntu 22.04 / 24.04 and Debian 12, running a script in the background (nothing typed in the terminal; one line of progress). Each service gets its own new user (`name-rdp`, `name-vnc`); passwords default to the SSH login's (saved or typed) and travel on the SSH connection's input, never shown; the created sessions store the user name and password of the new user; services already running are left alone; creates the RDP and VNC sessions through the SSH session when it succeeds; *Check First* only reports. A service that does not come up is removed again (its new user, files, service and firewall rule) without touching the other one; *Copy log* copies the output.
- **One desktop per user**: opening RDP while VNC to the same machine as the same user is open (or the reverse) asks to close the first; when someone else is connected to the machine, Termwright warns and never closes their session.
- **Diagnose Remote Desktop** (right-click an RDP or VNC session): one line says what is wrong (red) or that nothing is, with the checks behind it - from this computer, through the jump host and on the machine over SSH; *Restart VNC / RDP service* fixes a blocked service.

## Console servers

- **Console sessions**: open a device through a console server's port, using the console server's session for address, jump host and SSH login.
- **Access methods**: SSH as `user:portNN` (Opengear-style), SSH as `user:ttySN` (Avocent-style), SSH to 3000 + N, Telnet to 2000 + N or 7000 + N, or custom.
- **Device login over the console**: the device type's login steps run after the console server login.
- **Console Server Ports…**: create sessions for a range or list of ports (`1-16`, `5 rack4-dist1`) with a preview.

## Device types (login profiles)

- **Built-in profiles**: EXOS, VOSS, Cisco IOS, Cisco NX-OS, Huawei VRP, Eltex MES, Ruckus (Brocade) ICX and Linux, with login, paging off, configuration backup and device facts; add your own, with a **Detect** pattern that Discover Devices and *Identify Device Type* use to recognise it.
- **Own username and password** per device type (keychain, password manager, or *No password*).
- **Login steps** for Telnet / Serial / Raw and **after-login steps** for every protocol (`enable`, paging off).
- **Remember for device type**: a password typed at a login prompt can be saved for the whole device type.
- **Inherit or override**: sessions use the device type's login unless they set their own.
- **Prompt pattern** per device type, used by background runs and paced sends.
- **Backup commands** per device type (EXOS `show configuration`, VOSS / Cisco `show running-config`).
- **Password state shown** (saved, asked, no password) in Session Options and Quick Connect.

## Session Manager

- **Device icons**: switch, router, firewall, access point, server, computer, storage, phone, IoT, printer - as coloured tiles or plain glyphs (View → Tile / Glyph Icons).
- **Folders coloured by level**: the top level and five levels of subfolders each have their own colour; a folder in an environment takes the environment's colour, and so does everything in it.
- **Wake-on-LAN sessions** (protocol *Wake-on-LAN*, own icon): MAC address, network, optional IP address and UDP port; double-click sends the magic packet to the network's broadcast address only, then pings the device (with its NetBIOS name) or searches the network for the MAC address (4 times, 30 s apart, configurable) and saves the address found; pinged every 2 minutes (configurable) - green dot up, orange down.
- **Wake-on-LAN**: wake a session's device by its MAC address - from this computer or, for devices behind a jump host or jump station, from that server; broadcast address and port per session; *Wake Up and Connect*; folders and selections; waits until the device answers.
- **Name After Devices**: sessions named by an address get the device's own hostname (or hostname with model), many at once from a folder, offered after connecting, or renamed automatically with Undo; team devices are renamed by the owner on the admin page.
- **Device facts**: hostname, model, version, serial, MAC and uptime read after connecting in a background login, shown on the tab, status bar, cards and side panel; teach a fact by selecting the value (Edit → Save Selection as Device Fact, or Session Options → Device Facts → Run); edit, remove or add by hand.
- **Clear Recent / Clear Favourites**, **Create SFTP Session / Create SSH Session** copies.
- **Folder tree** with filter: an exact match on address or name shows only that device, the rest on request.
- **Favourites and Recent** sections, with when each session was last connected.
- **Hover card**: target, jump host, device type, MAC address, password state, last connected, description.
- **Multi-select** (Cmd/Ctrl-click, Shift-click): connect, edit, back up, delete, add to favourites or drag together.
- **Edit many sessions at once**: folder, device type, username, password, protocol, port, jump host, environment, color scheme, keyword set, auto reconnect.
- **Drag and drop** sessions and folders; new, edit, duplicate, rename, delete.
- **Expand / Collapse All** folders; the **Delete** key deletes the selection; a confirmation before deleting (on by default, Settings → General).
- **Jump hosts stay linked**: moving, renaming or deleting a session that other sessions (SSH, RDP, VNC) use as their jump host lists them and asks whether to update - or delete - them, even when confirmations are turned off.
- **Folder defaults**: username, device type, jump host, identity file, keyword set, color scheme, environment, smart card and recording for a whole folder.
- **Environments**: Production, Staging, Test / Lab and Development to start with - rename, recolour or remove them, or add your own (Pre-production, Customer A…) in ten colours that are never folder colours; right-click a folder or session → *Environment…*; subfolders and sessions inherit it; coloured tags and tab lines; protected environments (production) ask before multi-line pastes and multi-tab sends.
- **Which devices answer**: optional dot per session from a TCP check every minute - for every session, or only the sessions of the selected folder.
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
- **This computer's networks** (wired, Wi-Fi, VPN) listed when the scan opens, to start a scan from one of them or type a network or range.
- **Copy** a result's address, MAC address, vendor or system text.
- **Wake-on-LAN sessions** for devices that show no login port.
- **Connect ▾** per result with every way it answered (SSH, SFTP, Telnet, RDP, VNC, web), the recommended first.
- **Device types for every device** (phones, IoT, TVs too) and **+ New device type…** with its icon, right from the results.
- **Push to Team…** the selected results to a team's inventory.

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
- **Keyword highlighting** (off by default): sets of words or regular expressions with colors, per session or default (built-in network set: up/down, errors…); only the rows on screen are marked, so long sessions stay fast.
- **Clickable links** and OSC 8 hyperlinks.
- **Timestamps**: local time with milliseconds in front of each output line (display only), per tab or on by default for a session.
- **Zoom** (`Mod +`, `Mod -`, `Mod 0`), font size 14 and normal line spacing by default, **line spacing** setting, per-session font and size, cursor style and blink.
- **Backspace** sends DEL or BS per session.
- **Lock Session / Lock All Sessions** with a password.
- **Send Text File** line by line.
- **Compare With…**: diff this tab with another tab or a file; line numbers, ignore whitespace / timestamps / prompts, only differences, and an *identical* / *differ* verdict at the top.

## Command Window, Button Bar, Command Library and History

- **Command Window**: type once, send to the active tab, selected tabs or all connected tabs; multi-line; character mode.
- **Send group**: pick tabs with Choose… or Add to Send Group.
- **Suggestions** while typing, from history and the Command Library, the tab's device type first.
- **Button Bar**: buttons that send text (with escapes), run an action and/or a script; to one or all tabs.
- **Command Library**: saved commands in folders, multi-line, `{{variables}}` with defaults, for chosen device types.
- **Commands ▾**: library commands for the active tab's device type, put in the box to check before sending.
- **Command History** (`Mod+Y`): every typed command with session, device type and time; search, type, run or reuse; remove a single command, or clear the history of chosen device types.
- **Secrets never remembered**: commands with passwords, secrets, communities or keys, and anything typed at a password prompt.
- **Key Map**: any key combination sends text, runs an action and/or a script.

## Logging and recording

- **Session logs**: plain text or raw, append, start on connect, file name tokens (`%H %S %Y%M%D…`), per-line timestamp prefix.
- **Save Scrollback / Save Selection** to a file.
- **Save Scrollback As HTML**: a report page with the Termwright logo, session, address, model, OS version, serial number, MAC address, device type, protocol, user, date and time, and the output with colors and keyword highlighting.
- **Copy Scrollback** to the clipboard as text or as the HTML report (tab menu), to paste into a ticket or an e-mail.
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

- **Files panel beside the terminal** (opposite the Session Manager, per session or per tab): follows the terminal's folder, cd's the terminal to a folder opened in it, opens a double-clicked file on this computer and offers to upload saved changes; upload, download, rename, delete.
- **SFTP browser (modern)**: a top bar with Run as, the terminal folder and SFTP / SCP; Copy, Sync and Diff between the panes; clickable folder paths, a bookmark bar, `..` row, file-type badges, Today / date times.
- **Several files**: check boxes (and a select-all box), Cmd/Ctrl-click, Shift-click, Cmd/Ctrl+A.
- **Keyboard**: ↑ / ↓ (Shift selects a range), Home / End, Page Up / Down, Enter opens, Backspace goes up, Delete deletes.
- **Diff**: any two selected files - one on each side or both on one side - open in Compare, which says whether they are identical.
- **New File / New Folder / Delete on both sides**: deleting on this computer moves to the Trash / Recycle Bin.
- **Transfer bar**: progress, speed, time left, Cancel, the other transfers; ✕ closes it while copies go on.
- **SFTP browser**: dual pane (this computer / server), over the terminal's login or as its own SFTP session.
- **Transfers**: files and whole folders, queue with progress, cancel, **resume** of failed copies.
- **Sync** new or changed files in either direction (nothing is deleted).
- **Bookmarks** for local and server folders.
- **Server-to-server copy** between two SFTP tabs.
- **Copy via SCP** from the SFTP browser for devices with a weak SFTP server.
- **Run as root or another user** (sudo, su, any command) in the SFTP tab.
- **Through a jump station**: SFTP / SCP on the target of an `ssh … host` remote command.
- **Working directory detection**: SFTP opens in the terminal's folder, follows every `cd`, and can `cd` the terminal; also follows `sudo su -`, and knows whether the terminal is on the jump station or the target (*SFTP opens on*: where the terminal is, or always one of them).
- **Colored files**: folders and files colored by type, links and hidden files marked.
- **SCP** upload and download of files and folders.
- **Network Check**: who can reach what - this computer and the open device tabs (their own ping and traceroute) against gateway, DNS, internet and any host or port, as a matrix; a cell opens the traceroute path with the hop where it stops; **iPerf3** server and client for throughput inside the network.
- **Firewall check for the file servers**: whether the computer's firewall lets devices in, one-click allow, and a test fetch from the device.
- **File servers (TFTP, FTP, HTTP)** on this computer for devices to fetch images and licences and send configurations: one folder, one address, uploads only when allowed, a transfer log, the device command written for you (EXOS, Cisco IOS / NX-OS, Arista, Junos, Linux, or your own), a status-bar indicator, and they stop on their own.
- **ZMODEM** (`sz` / `rz` detected automatically), **YMODEM** batch and **XMODEM** for serial consoles and boot loaders.

## Tunnels and routing

- **Web pages through a session**: *Open Web Page Through This Session* and *Open Web Interface* open Chrome, Edge, Chromium, Brave or Firefox in a window with a temporary profile whose traffic and name lookups go through the session's SSH server or the device's jump host; closing the window ends the tunnel and deletes the profile.
- **Port forwarding**: local, remote and dynamic (SOCKS5).
- **Jump hosts**: through another saved SSH2 session, chainable, for SSH2, Telnet, Raw, SFTP, RDP and VNC.
- **Proxies**: SOCKS5 and HTTP CONNECT with optional login.

## Automation and scripting

- **Logon actions**: Expect/Send table with hide, trailing CR, named credentials and escapes (`\s` user, `\w` password, `\v` clipboard, `\p` pause…).
- **Triggers**: answer output patterns automatically (e.g. `--More--` → space), loop-protected.
- **Alerts**: a pattern in any tab gives a notice, a tab badge, a sound and a desktop notification.
- **Python scripts** with the `crt` script object, run with your installed Python 3.
- **VBScript scripts** with the `crt` object (Windows).
- **JavaScript scripts** (built in): `term.waitFor`, `readUntil`, `openSession` and more.
- **Logon scripts** with arguments; Run Script; scripts on buttons and keys.
- **Reading the screen** as rendered (`crt.Screen.Get`, `CurrentRow`).
- **Script recorder**: type commands, get a script that waits for each prompt.

## Passwords and security

- **Unlock window**: with a master password, Termwright starts locked until it is typed (Not now asks for passwords when connecting); Lock Now locks again.
- **Master password strength**: any 4 or more characters, with a strength meter - no forced capitals, numbers or symbols; optional step in the welcome flow.
- **OS keychain** for passwords and passphrases; never in session files.
- **Password managers**: 1Password (`op://`), Bitwarden (`bw://`), HashiCorp Vault (`vault://`) or any command (`cmd://`); read when connecting, never stored.
- **Named credentials** shared by many sessions.
- **Master password** asked at start-up before any saved secret is used.
- **Password vault export / import**: one file encrypted with AES-256-GCM (scrypt).
- **Forget Saved Password** per session.

## Import and export

- **Native storage**: plain-text `.ini` files (`Config/Sessions/*.ini`); unknown keys are kept.
- **Import from SecureCRT**: Config folder, zip, XML export or single `.ini`, with saved passwords moved to the keychain.
- **Import from other programs**: PuTTY / KiTTY (`.reg`, `~/.putty/sessions`), SuperPuTTY (`Sessions.xml`, `-pw` passwords to the keychain, Cygterm / Mintty as Local Shell), MobaXterm (`.mxtsessions`, `MobaXterm.ini`), mRemoteNG (`confCons.xml`: folders, inherited user / port / protocol, passwords to the keychain), OpenSSH `~/.ssh/config`.
- **Import from inventories**: CSV, Ansible (INI / YAML), NetBox, AWS EC2.
- **Termwright template (CSV)**: a template to save and a [format guide](IMPORT_FORMAT.md), to bring sessions from any other tool.
- **Export**: Termwright folder, zip or XML (everything kept; SecureCRT can read them).
- **Sync your own computers** through a shared folder (OneDrive, iCloud Drive, Dropbox, a network share): sessions, accounts, device types, folder defaults and the library (optionally Global Options) every half minute, deletions included; never passwords, never team folders.
- **Settings travel with the export**: zip and folder exports include device types, accounts (no passwords), Folder Defaults, the command library, checklists, jobs and Global Options; Import Sessions shows what is in the file and imports the parts you choose, following the target folder and renamed sessions.
- **Import Termwright exports**: Termwright .zip or export folder as their own import sources.
- **Export to other programs**: PuTTY and KiTTY (.reg), PuTTY sessions folder, MobaXterm (.mxtsessions), Tabby profiles, OpenSSH config and CSV.
- **Export summary**: which sessions a format cannot hold and which settings stay in Termwright; passwords are never exported.
- **Color schemes**: import and export as JSON.

## Appearance

- **Keyword highlighting on by default** (Network set): up / enable green, down / error red, warning / disable amber, addresses blue, and the EXOS port summary's E / D / A / R states; the text is coloured (or the background, as an option).
- **Save Screen As Image**: the visible terminal as a PNG with its colours and highlighting.
- **12 built-in color schemes** (Solarized, Dracula, Nord, Gruvbox, Catppuccin…), *Auto* follows the UI theme.
- **Custom scheme editor** with live preview.
- **One scheme for all sessions** or per session.
- **Global Options search**: find any option by name.

## Keyboard shortcuts

- **Dialogs**: Escape closes the menu or dialog on top, Tab stays inside the dialog, and dialogs and icon buttons are announced to screen readers.
- **A shortcut for nearly every menu command** (e.g. Disconnect, Clear Screen, Auto Scroll, Timestamps, Log Session, Open SFTP Tab, Save Screen As Image, Save Selection as Device Fact); Help → Keyboard Shortcuts lists them with a search box.
- `Mod` = Cmd on macOS, Ctrl+Shift on Windows/Linux: `O` sessions, `Q` quick connect, `L` local shell, `R` reconnect, `W` close tab, `P` SFTP tab, `F` find, `S` save scrollback, `M` session manager, `K` command window, `B` button bar, `E` command library, `Y` command history, `J` recording marker, `G` Quick Open.

- **Syslog server** (Syslog menu): UDP, TCP (newline or octet-counted) and TLS (own or self-signed certificate); RFC 3164 and 5424; live list with saved session of each sender, severity counts and filters, words or /regex/ search, pause / follow, details, Connect, Export CSV / text; rules that colour, notify or hide; daily files (per sender); start with Termwright.
- **Syslog client**: test messages (RFC 5424 or 3164, any facility and severity, UDP / TCP / TLS, many at once).
- **Syslog for automation**: the in-app AI agent, MCP apps, the local REST API (`/api/v1/syslog…`) and the Python client (`wait_for` a message in tests).
- **API scripts by the AI**: Write with AI in the Scripts tab; the agent writes and runs `pm.*` tests. **Monochrome body** button for plain black / white answers.
- **AI instructions and skills**: rules the AI follows in every chat (global, per folder, per device type) and skills - SKILL.md procedures in the open Agent Skills format that the agent loads when a request matches; add, edit, import, export, Write with AI, examples; for MCP apps, the local API and Python too.
- **API client** (API menu; double-click an API session): requests with any method, parameters, headers and bodies (JSON, text, XML, forms, multipart with files, GraphQL); answers as coloured JSON, headers, cookies, tests and a timing timeline; history; code as cURL, Python, JavaScript or PowerShell.
- **API sessions** (protocol REST API): base URL, API key, bearer token, login request (token found in the answer, renewed by itself), basic, OAuth 2.0; `{{variables}}` with Production values; secret variables; any-certificate and own-CA TLS. Secrets in the OS credential store, masked everywhere.
- **Collections and runner**: plain JSON files with folders; Postman-style `pm.*` tests and pre-request scripts (`postman.setNextRequest`, `pm.sendRequest`, `pm.execution.skipRequest`); collection authorization, scripts and variables; Run Collection with an environment, iterations, CSV / JSON data, delay, stop on failure.
- **Like Postman**: request tabs; Docs, Params with path variables and bulk edit, Authorization (inherit, none, bearer, basic, API key) per request and per collection, Headers with hidden auto headers and presets, Body (form-data with files, URL-encoded, raw JSON / Text / JavaScript / HTML / XML, binary, GraphQL), Scripts before and after, request Settings (HTTP version, certificates, redirects, URL encoding, cookies, timeout); environments and globals with secret values in the credential store; coloured `{{variables}}` and a variables panel; console; cookie manager; Send and Download; paste cURL into the URL.
- **Import and export**: Postman v2.0 / v2.1 collections and environments, OpenAPI 3 and Swagger 2 (YAML or JSON), cURL; export for Postman v2.1.
- **API client for automation**: the in-app AI agent, MCP apps, the local REST API (`/api/v1/http…`) and the Python client; changes on production sessions always ask.
- **API mocks, monitors and schemas**: saved examples per request, a mock server per collection (path variables, `x-mock-example`), monitors that run a collection every few minutes and notify on failure and recovery, and JSON Schema checks of answers (`pm.response.to.have.jsonSchema`).
- **SNMP Browser** (SNMP menu, or right-click a session → Open SNMP Browser): Get, Get Next, Get Bulk, Walk, Table and Set; SNMP v1, v2c, v3 (MD5, SHA-1, SHA-2; DES, AES-128/192/256); MIB tree with search and object details; 24 IETF/IANA MIBs built in, vendor MIBs from files; readable values (enum names, speeds, MAC addresses); Table view, History, Favourites, Export CSV.
- **SNMP Browser extras**: several devices at once, Compare two walks, Poll with a live chart (counter rates), Send Trap (v1/v2c/v3), MIBs window (modules, problems, open folder, remove).
- **SNMP dashboards and alarms**: saved pages of values from several devices with sparklines, rates per second and alarms (above, below, not a value) as notifications, also in the background.
- **SNMP files and simulator**: export to Excel, save and open walk files (`snmpwalk -On` format), and a v1/v2c agent that answers from a walk like the device.
- **SNMP sessions** and a separate write community; Open SNMP Browser from a terminal tab.
- **SNMP credentials in the session**: session properties → SNMP (version, address, port, community or v3 user and protocols, context, Allow Set, Test); the community and v3 passwords are in the OS credential store.
- **Trap receiver**: v1, v2c and v3 traps and informs on any UDP port, named from the MIBs, matched to sessions.
- **SNMP for automation**: the in-app AI agent, MCP apps, the local REST API (`/api/v1/snmp…`, `/api/v1/tools`) and the Python client; a Set needs Allow Set, and production sessions always ask.
- **Network Map**: live topology from your sessions (LLDP, CDP, EDP, STP, LAG; EXOS, VOSS, Cisco, Arista, Junos, Huawei VRP, Ruckus ICX, Eltex MES and any device type with TextFSM templates in its Network map; CLI or SNMP), auto-arranged, zoom, export to JPEG, PDF or HTML. The labs have the same Arrange styles.
