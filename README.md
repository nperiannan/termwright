<p align="center"><img src="docs/assets/termwright-icon-256.png" width="112" alt="Termwright" /></p>

<h1 align="center">Termwright</h1>

<p align="center">
  <b>AI-powered network access, automation, monitoring, emulation and reservation platform.</b><br/>
  SSH · Telnet · Serial · SFTP · RDP · VNC · WoL · SNMP · Syslog · REST API · Tests · Discover · Collaborate - on Windows, macOS and Linux.
</p>

<p align="center">
  <a href="https://github.com/nperiannan/termwright/actions/workflows/build.yml"><img alt="Build" src="https://github.com/nperiannan/termwright/actions/workflows/build.yml/badge.svg?branch=develop"></a>
  <a href="https://github.com/nperiannan/termwright/releases"><img alt="Release" src="https://img.shields.io/github/v/release/nperiannan/termwright?include_prereleases&sort=semver&label=release"></a>
  <a href="LICENSE"><img alt="License: Apache 2.0" src="https://img.shields.io/badge/license-Apache%202.0-blue"></a>
  <img alt="Platforms" src="https://img.shields.io/badge/platforms-Windows%20%7C%20macOS%20%7C%20Linux-555">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.24%2B-00ADD8">
  <img alt="MCP" src="https://img.shields.io/badge/MCP-connector-8A63D2">
</p>

<p align="center">
  <a href="docs/USER_GUIDE.md">User Guide</a> ·
  <a href="docs/FEATURES.md">Feature List</a> ·
  <a href="docs/EMULATOR_GUIDE.md">Emulated Lab Guide</a> ·
  <a href="docs/FAQ.md">FAQ</a> ·
  <a href="docs/TEAM_SERVER.md">Team Server</a> ·
  <a href="docs/API.md">Local API</a> ·
  <a href="docs/PYTHON.md">Python</a> ·
  <a href="docs/SCRIPTING.md">Scripting</a> ·
  <a href="CHANGELOG.md">Changelog</a>
</p>

<p align="center"><img src="docs/assets/termwright-demo.gif" alt="Termwright demo: discovery, Wake-on-LAN, SSH and the AI assistant, one command to many tabs, a script, SFTP, the emulated lab and traffic generator, the SNMP Browser, the syslog server, the REST API client and teams" width="900"></p>

<p align="center"><i>Discover Devices, Wake-on-LAN, SSH and the AI assistant, one command to every tab, a script, SFTP, the emulated lab and traffic generator, the SNMP Browser, the syslog server, the REST API client and teams - recorded with simulated devices.</i> <a href="docs/assets/termwright-demo.mp4">Watch the 2-minute demo (MP4)</a></p>

![Termwright - start page with 3D cards and the Session Manager in the Glacier style](docs/screenshots/glacier-start-dark.png)

<p align="center"><i>The start page and Session Manager: folders with their environments, and every kind of session - SSH, SFTP, Linux, network OS, IoT, RDP, VNC, Wake-on-LAN, Telnet, serial, local shell.</i></p>

## What Termwright is

Termwright began as a terminal and session manager for network engineers. Today it is one free, open-source desktop program for the whole loop of network work - reaching devices, building and testing networks, letting AI do the repetitive parts, and sharing a lab with a team:

| | |
|---|---|
| **Access** | Every way into a device - SSH, Telnet, serial, SFTP, RDP, VNC, Mosh, jump hosts and proxies - with hundreds of sessions organised in folders, logins per device type, device facts and passwords kept in the OS keychain. |
| **AI** | An assistant and agent beside the terminal that reads output and screenshots, connects to sessions and configures or troubleshoots devices with your approval - with Claude, Claude Code, OpenAI-compatible services or Ollama. Termwright is also an **MCP connector**, so Claude Desktop, Claude Code, Cursor, VS Code and other AI apps can use it. |
| **Emulate** | A network lab of real switch, router and server images on your own KVM hosts - one or several - with ESP32 microcontrollers, Linux PCs, lab servers, a traffic generator with RFC 2544 tests and packet capture, all on one canvas. |
| **Automate** | Logon actions and triggers, Python / VBScript / JavaScript scripts, a command window for many tabs, configuration backups with change detection, checklists, Run and Collect - and a local REST API, a command line and a Python client for the lab. |
| **Collaborate** | An optional team server of your own: who has which device open, reservations, a shared inventory the server reads from the devices, usage reports, discovery from inside the lab and single sign-on. |

## Network emulation lab

![The emulated lab: switches from real images on KVM hosts, cabled on the canvas](docs/screenshots/emu-canvas.png)

<p align="center"><i>The emulated lab: switches from real images on KVM hosts, cabled on the canvas; the pages above it - Canvas, Overview, Traffic, Appliances, Hosts - are also under the Lab menu.</i></p>

![The emulated lab workflow in nine steps: prerequisites, add a host, pick an appliance, get the disk image, build the topology, create traffic streams, analyse and save the report, capture and replay, and line rate with TRex; the AI assistant can do steps 2 to 9](docs/assets/emulation-workflow-dark.png)

<p align="center"><i>From an empty KVM host to a traffic report in nine steps. The <a href="docs/EMULATOR_GUIDE.md">Emulated Lab Guide</a> walks through each one.</i></p>

- **Real images on your hardware**: switch, router, firewall and server images run in QEMU/KVM on Linux hosts you add over SSH - qcow2 files, GNS3 appliances, a public catalogue, cloud images, or an image you install from the vendor's ISO with its console in the page.
- **Several hosts, one topology**: a lab spans KVM hosts (cables between hosts are tunnelled), a running switch moves live to another host, and the CPU tab shares or limits each host's cores.
- **Built for test cases**: draw the topology or let *New Lab…* build it (leaf-spine, mesh, star, LAGs); every device gets its console, an SSH session and its management address; fast start resumes switches in seconds; checkpoints, a change history with Revert, and an Events log of everything Termwright did.
- **Everything around the switches**: Linux PCs (with a desktop and browser when needed), L2 switches and hubs, HTTP / FTP / TFTP / DHCP servers, a RADIUS server for 802.1X, name servers, a route generator (OSPF, BGP), the host's network as a cloud, and **ESP32 microcontrollers** (S3, C3, C6, H2) with their MicroPython console.
- **Traffic generator**: test ports on switch ports send and count UDP, TCP, ICMP, ARP, raw or replayed pcap streams with VLAN, priority and DSCP, at rates in pps, Mbit/s or % of line, with latency, jitter, loss and per-flow statistics; **RFC 2544** throughput, latency, frame loss and back-to-back, **RFC 2889** address learning, convergence and iperf3 tests; 802.1X and DHCP clients, multicast, LACP, LLDP and spanning tree on test ports; packet capture with a built-in decoder; **real TCP sessions** (connected, refused, timed out, setup time) for ACLs, firewalls and NAT. It is Termwright's own - nothing to install on the hosts: the built-in engine sends about 100,000 frames a second per host, the **fast engine** (a compiled helper Termwright copies there) about 420,000 of 64 bytes or 2.5 Gbit/s of 1518 bytes on two vCPUs. **Real switches** are tested the same way: a KVM host's spare network card cabled to one is a test port, and for line rate, the ports of a **TRex** server you run are test ports too (see [PERFORMANCE.md](docs/PERFORMANCE.md) for the measured numbers).
- **Simulated lab**: lightweight simulated switches on this computer - no VMs - for training, trying scripts and the AI agent.
- **Scriptable and AI-ready**: the local REST API, `termwright lab …` on the command line, the Python client and the MCP connector all drive the same lab tools.

![The traffic generator: streams, live throughput, loss and latency, pass or fail per stream](docs/screenshots/emu-traffic.png)

## AI assistant and agent

- **Ask, Plan or Agent**: ask about the output or a screenshot; in **Agent** mode it finds sessions by folder, name or address, connects and runs commands - read-only by itself, changes after you allow them, or fully autonomous. **Production sessions always ask first**, and logs and recordings are kept as evidence with a status per device.
- **Your choice of AI**: Claude with an API key, **Claude Code** with your own Claude login (model and effort), OpenAI-compatible services (OpenAI, Azure OpenAI or a company gateway) or **Ollama** on your network.
- **It knows Termwright**: it looks up the guide of the version running and Termwright's live state - the version, each KVM host with its VMs, the labs, who has which team device - instead of guessing.
- **It runs the lab**: it builds topologies, starts and configures switches on their consoles, cables, tests with the traffic generator and reads the Events when something fails - each change approved by you.
- **Privacy**: nothing is sent until you ask; passwords, keys and SNMP communities are always removed, and addresses and host names can be hidden.
- **Termwright as an MCP connector**: *Help → Connect an AI App…* adds Termwright to Claude Desktop or Claude Code in one click, and copies a ready config for Cursor, Windsurf, Gemini CLI, VS Code or Codex. *Used by* shows which apps used it. Nothing to download - the connector is Termwright itself.

| | |
|---|---|
| ![AI agent](docs/screenshots/ai-agent.png) <br/>*The agent configures a switch, with a status per device* | ![Connect an AI app](docs/screenshots/ai-connector.png) <br/>*Help → Connect an AI App: Claude Desktop, Claude Code and other MCP apps* |

## Teams: who uses which device

- **A team server of your own**: a small Docker container on a lab host, set up and updated from Termwright over SSH. Members join with an invite; the certificate is pinned. The server never sees passwords, keys or terminal output.
- **Presence and reservations**: every member sees who has a device open; reserve a device for a time, and *Tell Me When It Is Free* when it is busy.
- **A shared inventory**: push sessions to the team (owners approve), every member gets the team folder, and the server reads each device's model, serial number and MAC address itself.
- **The admin page**: usage reports (hours, utilization, devices not used), discovery of the lab's networks from the server's host, members and roles, an activity log, backup and restore.
- **Single sign-on**: Microsoft Entra ID, Okta, Google or any OpenID Connect provider.

| | |
|---|---|
| ![Team inventory](docs/screenshots/team-inventory.png) <br/>*Team admin: the shared inventory - model and serial number read from the devices, who uses what now* | ![Team members](docs/screenshots/team-members.png) <br/>*Teams: a team's members, their roles and the invite* |

## Terminal and sessions

- SSH, Telnet (also over TLS), serial, raw TCP, Mosh and local shell, in tabs or tiles
- SFTP browser, and a Files panel beside the terminal; ZMODEM, YMODEM and XMODEM for consoles and boot loaders
- Jump hosts (chainable), proxies and port forwarding; device web interfaces opened through the SSH connection
- RDP and VNC, also through a jump host - and RDP or VNC set up on a Linux machine over SSH
- Device discovery with device type and network OS detection; device facts (model, version, serial number, MAC address, uptime); *Name After Devices*
- Automatic login per device type (EXOS, VOSS, Cisco IOS and NX-OS, Huawei VRP, Eltex MES, Ruckus ICX, Linux or your own) and shared accounts
- Which devices answer, for every session or one folder; devices that moved to a new address found by their MAC
- Environments (production, staging, lab or your own) with colours and safeguards
- Built-in TFTP, FTP and HTTP servers for images, licences and configurations; Network Check (reachability, traceroute, iPerf3)
- Session logs, recordings with playback, and the terminal saved as text or an HTML report
- Import from SecureCRT, PuTTY, MobaXterm, mRemoteNG and more - and export back
- Sync your own computers through a shared folder (never passwords)
- An Android companion app with an SSH and Telnet terminal, a device widget and Wake-on-LAN ([preview](mobile/README.md))

And much more - see the [feature list](docs/FEATURES.md).

## Tests: record, run, generate code

- **Record a test** while you work: commands in terminal tabs and REST requests become steps; right-click output → *Expect this* to add a check
- **Tests tab**: run one or all tests on a testbed, see which check failed and what came back, open the HTML report or JUnit XML
- **Setup and cleanup** that always runs: the recorder suggests each change's undo (`create vlan` → `delete vlan`, a POST → its DELETE, an SNMP set → the old value), and a test can put back the configuration or a lab snapshot afterwards
- **SNMP, CLI and REST** steps in one test; **roles and testbeds** run the same test on other devices, **params** run it once per set of values
- **Edit, select and recover**: edit steps and checks in the Tests tab, run only selected tests, and choose what a failed cleanup does - stop, restore the saved configuration, or factory default; device error messages fail a step, and steps without checks are flagged
- **Checks written for you**: a show command gets checks for what was configured before it (`create vlan v200 tag 200` → *contains v200*, *Tag 200*); command output as tables with TextFSM / ntc-templates
- **Suites that scale**: tags, suites and dependencies; loops, conditions, retry-until and waits; testbed locks and parallel runs on several testbeds; watch syslog and SNMP notifications, compare configurations before and after, lab traffic and software-upgrade steps
- **History and reporting**: pass rate, flaky tests and results per software version; send results to **qTest** and open **Jira** defects with the steps and output attached
- **Plain files**: tests are YAML in a folder that can be a Git repository (Pull, Commit, Push in the Tests tab), and each is also written as **Go**, **Python** and **TCL** test code (`go test ./cases/...`, `python lib/python/run_tests.py`, `tclsh lib/tcl/run_tests.tcl`) - no passwords in any of them, Termwright logs in with the saved logins
- **Scheduled runs** with a notification when one fails, and the AI assistant to **write a test** from a description or **explain why one failed**
- **Procedures** (shared steps with parameters), **CSV/Excel test data**, **NETCONF** steps, **breakpoints** and step-through, **checks over time** (average, max, rate) and a **table maker** that turns output into a table by clicking - no TextFSM to write
- **Spirent TestCenter and IxNetwork** traffic runs as test steps (REST API: load a configuration, run, check loss and latency per stream)
- **Web steps** with Playwright, **reruns** of flaky tests and **timeouts**, a **testbed calendar**, **Teams / e-mail notifications**, a **PDF report** and **trends** per run, test and software version
- **Export a standalone suite** for Linux, macOS or Windows (Go, Python or TCL, with the library, testbeds, a plan and a run script that writes JUnit XML and run logs), and **debug** a failing test: pause at the failed step, retry it, run one step or from a step
- **Import and teams**: Excel/CSV manual test cases, Postman collections and OpenAPI descriptions become tests; `roles.yaml` says who edits, runs and approves, and runs on production can wait for an approval
- **CI without the window**: `termwright tests run --junit junit.xml`, or `termwright tests serve` for the generated code; secrets from the credential store or `TERMWRIGHT_SECRET_…` variables
- Usable from the AI assistant, MCP apps, the local REST API and Python

![Tests tab](docs/screenshots/tests.png)

| ![Trends](docs/screenshots/tests-guide/28-trends.png) <br/>*Trends: pass rate per run, the tests that fail most, per software version* | ![Testbed](docs/screenshots/tests-guide/30-testbed.png) <br/>*A testbed: its devices under their folders, with type and model* |
|---|---|
| ![Testbed calendar](docs/screenshots/tests-guide/29-calendar.png) <br/>*Testbed calendar: who has which testbed when* | ![Export](docs/screenshots/tests-guide/32-export.png) <br/>*Export a suite that runs without Termwright: Python, Go or twrun* |

Step by step with pictures: [Testing with Termwright](docs/TESTS_GUIDE.md).

## SNMP, syslog and REST API

- **SNMP Browser**: v1, v2c and v3 against saved sessions, a MIB tree with the standard MIBs built in and vendor MIBs loaded from files, Walk, Table, Compare, Poll with a live chart, saved dashboards with alarms, Excel export, walk files and a simulator that answers like the device, Set, Send Trap, and a trap receiver
- **Syslog server**: UDP, TCP and TLS, senders matched to saved sessions, search, severity filters, colour / notify / hide rules, files per sender, and a test sender
- **REST API client** in the spirit of Postman: API sessions with their login in the credential store, request tabs, collections, environments and globals (Postman import and export), per-request authorization, `pm.*` scripts and tests with `setNextRequest`, a collection runner, saved examples with a mock server, monitors, JSON Schema checks, OpenAPI and cURL import, code export
- All three are tools for the AI assistant, MCP apps, the local REST API and Python

| | |
|---|---|
| ![SNMP Browser](docs/screenshots/snmp-browser.png) <br/>*SNMP Browser: the MIB tree and a walk of the interface table* | ![SNMP polling](docs/screenshots/snmp-poll.png) <br/>*Polling counters with a live chart* |
| ![Syslog server](docs/screenshots/syslog.png) <br/>*Syslog server: live messages, severities, rules and senders* | ![REST API client](docs/screenshots/api-client.png) <br/>*REST API client: a collection, an environment and the variables panel* |

## Overview

Termwright is a native desktop program, not a web service. It is written in Go, with an Angular and xterm.js interface, and ships as a single binary for each platform. Sessions are plain-text files that you own. Passwords are kept in the operating system's keychain or your password manager, never in session files. Without the optional AI assistant, team server or sync folder, nothing is sent anywhere except to the hosts you connect to.

Existing sessions can be imported from SecureCRT, PuTTY, KiTTY, SuperPuTTY, MobaXterm and OpenSSH, or from CSV, Ansible, NetBox and AWS inventories. They can be exported back to those formats at any time. For any other tool, convert its sessions to the documented [import format](docs/IMPORT_FORMAT.md) (a CSV with a template) instead of waiting for an importer.

## Recent releases

- **0.43** - **Tests**: record, run and generate Go, Python and TCL code, with checks written from what you configured, suites, conditions and loops, parallel testbeds, history, qTest and Jira, Postman and OpenAPI import, team roles and approvals; SNMP **dashboards with alarms**, walk files and a **simulator**; API **mocks, monitors** and JSON Schema checks; the Network Map for **any vendor** with TextFSM templates.
- **0.42** - the **SNMP Browser** (v1, v2c, v3, MIBs, walk, tables, polling, traps), a **REST API client** like Postman (collections, environments, scripts, tests, Postman and OpenAPI import) and a **syslog server**, all usable by the AI assistant; device types for **Huawei VRP**, **Eltex MES**, **Ruckus ICX** and **Cisco NX-OS**, and a **Detect** pattern per device type so Discover Devices recognises any vendor; a new 2-minute demo.
- **0.41** - the **Network Map**: the real network drawn from your sessions (LLDP, CDP, EDP, STP, LAG, VLAN) in eight styles, live reading progress, cable and device details; the AI agent's fully autonomous mode asks only on production sessions.
- **0.40.1** - downloads you can **verify** (`SHA256SUMS` and build provenance), a security policy, contributor guide and issue forms, a [FAQ](docs/FAQ.md), a demo, and the Android build no longer fails when Maven Central rate-limits.
- **0.40** - **Linux** gets one title bar in Termwright's theme with the menus, window buttons and tabs on their own row; a status bar with the cursor, selection, size, CAP and NUM; the AI chat box as VS Code's; the **Emulated Lab guide**; the source moved into `app/`.
- **0.39** - the **traffic generator** grows up: quick streams and tests, custom frames, faults and fuzzing, QinQ, MPLS, GRE and VXLAN headers, PEAP, DHCPv6, VRRP and BFD on test ports, real TCP sessions, reports as HTML, PDF and Excel, the fast engine, test ports on real switches and TRex servers.
- **0.38** - the README and feature list as Termwright is now; a team's Members on their own page.
- **0.37** - *Connect an AI App* (MCP config for Cursor, VS Code, Windsurf, Gemini CLI, Codex; *Used by*), a **Lab** menu, icons on the lab's pages, traffic generator fixes and its measured speed.
- **0.36** - the **emulated lab** grows up: Install from ISO with the installer's console in the page, the AI agent with full control of the lab and knowledge of Termwright, packet captures, the local API, command line and Python client, the team server reading model, serial number and MAC from the devices.
- **0.34** - *Name After Devices*, Network Check, the file servers' firewall check, *who is free now* for teams.
- **0.33** - the **AI assistant and agent**, your choice of AI service, Termwright as a connector for Claude, built-in TFTP / FTP / HTTP servers, single sign-on for teams.

Every change is in the [changelog](CHANGELOG.md).

## Capabilities

| Area | Summary |
|---|---|
| Protocols | SSH2; Telnet (NAWS, TTYPE, ECHO, SGA) and Telnet over TLS; serial (flow control, DTR/RTS, break, speed detection); raw TCP; Mosh; local shell (PTY on macOS and Linux, ConPTY on Windows); RDP through the system's client; VNC in a built-in viewer; Wake-on-LAN sessions. |
| SSH authentication | Password and keyboard-interactive. Public keys: OpenSSH, encrypted, and PuTTY `.ppk`. SSH certificates, FIDO2 security keys, PIV / PKCS#11 smart cards, Kerberos (GSSAPI), the SSH agent. `known_hosts` verification with prompts for new and changed keys. |
| Key management | Generate Ed25519, RSA 4096 or ECDSA keys. Install them on Linux, macOS, BSD and Windows OpenSSH servers, test the key-only login, then switch the session to it. |
| Device types | Per-platform login profiles: login steps for Telnet and serial, after-login steps for every protocol (for example `enable`, paging off), and credentials inherited or overridden per session. |
| Device facts | Model, version, serial number, MAC address, uptime and custom values, read in a background login. Built-in rules for VOSS, EXOS, Cisco IOS and NX-OS, Huawei VRP, Eltex MES, Ruckus ICX and Linux. |
| Session management | Wake-on-LAN (also through a jump host); folders with defaults; favourites and recents; environments, with confirmation before multi-tab sends to production; reachability checks; bulk edit; workspaces; Quick Open; drag and drop. |
| Syslog | A syslog server (UDP, TCP, TLS; RFC 3164 and 5424) with live view, senders matched to saved sessions, search, severity filters, colour / notify / hide rules and daily files, plus a test message sender; usable from the AI assistant, MCP apps, the local API and Python. |
| Tests | Record tests while you work (terminal commands and REST requests, *Expect this* checks on selected output), run them on testbeds from the Tests tab with HTML and JUnit reports, edit them as YAML, and run them as generated Go tests (`go test`) in CI; usable from the AI assistant, MCP apps, the local API and Python. |
| API client | A free REST client in the spirit of Postman: API sessions with API key, bearer, login-request, basic and OAuth 2.0 authentication (secrets in the credential store), `{{variables}}` with Postman-style environments and globals (import Postman environments), request tabs, per-request and per-collection authorization, request settings, collections as plain JSON files, `pm.*` tests and pre-request scripts with `setNextRequest` and `sendRequest`, a collection runner with data files, a console, import of Postman collections, OpenAPI / Swagger (YAML or JSON) and cURL, code export; usable from the AI assistant, MCP apps, the local API and Python. |
| SNMP | SNMP Browser for v1, v2c and v3 (Get, Walk, Table, Set) with a MIB tree, the standard MIBs built in and vendor MIBs loaded from files, a trap receiver, credentials kept in the computer's credential store; usable from the AI assistant, MCP apps, the local API and Python. |
| Discovery | Scans a network or range (up to /22): open ports, SSH and Telnet banners, web servers and certificates, SIP, SNMP v2c, DNS, MAC vendor. Each result shows its evidence, a guessed kind, a device type and *Connect ▾*; selected devices are added as sessions. Discovery never logs in. |
| Emulated lab | QEMU/KVM on one or several Linux hosts: real images (qcow2, GNS3 appliances, a public catalogue, cloud images, Install from ISO), device packs with port layouts and personas, consoles and SSH sessions per device, cables within and between hosts, live moves, fast start, checkpoints, history with Revert, CPU sharing and limits; Linux PCs, L2 switches, hubs, lab servers (HTTP, FTP, TFTP, DHCP, RADIUS, DNS), a route generator (OSPF, BGP) and ESP32 microcontrollers. |
| Traffic generator | Test ports on switch ports, on a host's network card cabled to a real switch, or on a TRex server; UDP, TCP, ICMP, ARP, raw and pcap streams and real TCP sessions with VLAN, 802.1p and DSCP, fixed, increasing, random or IMIX sizes and changing fields; latency, jitter, loss, order and per-flow counts; RFC 2544 (throughput, latency, frame loss, back-to-back), RFC 2889 learning, convergence and iperf3 tests; 802.1X and DHCP clients, multicast, LACP, LLDP and spanning tree; packet capture and decode; a test library and HTML, PDF and Excel reports; a built-in and a fast engine. |
| AI | Ask, Plan and Agent modes beside the terminal; Claude, Claude Code, OpenAI-compatible services and Ollama; screenshots; the agent connects to sessions and runs the lab with your approval; secrets removed before sending. MCP connector for Claude Desktop, Claude Code and other MCP apps. |
| Teams | Optional self-hosted team server (Docker): presence, reservations and *Tell Me When It Is Free*, a shared inventory with approvals and device facts read by the server, usage reports, discovery from the lab, roles, single sign-on (Entra ID, Okta, Google, OpenID Connect), activity log, backup and restore, updates from Termwright. |
| Import and export | Sessions are plain-text `.ini` files, and unknown keys are preserved byte for byte. Import from SecureCRT (Config folder, zip, XML), PuTTY / KiTTY, SuperPuTTY, MobaXterm, mRemoteNG, OpenSSH config, CSV, Ansible, NetBox and AWS EC2. Export to the same formats plus Tabby. |
| Terminal | Tabs and tiled layouts; xterm-256color and true colour; Unicode; scrollback search; keyword highlighting; timestamps; auto-scroll control; clickable links; session lock; zoom; per-session fonts and colour schemes. |
| File transfer | SFTP browser with folder transfers, sync, resume, bookmarks, compare, server-to-server copy, SCP, sudo / su and jump-station targets. Files panel beside the terminal. ZMODEM (automatic `sz` / `rz`), YMODEM batch, XMODEM. Built-in TFTP, FTP and HTTP servers. |
| Automation | Expect/send logon actions with named credentials; output triggers; logon scripts; Python, VBScript (Windows) and JavaScript scripting through a `crt`-style object; script recorder; key map; button bar; output alerts. For the lab: a local REST API (OpenAPI), `termwright lab …` and a Python client. |
| Network operations | Command Window that sends to the active, selected or all tabs; command library with `{{variables}}`; command history; scheduled configuration backups kept only when changed; change checklists; Run and Collect to CSV; Compare; Network Check (reachability, traceroute, iPerf3). |
| Logging and recording | Raw or plain-text session logs with per-line timestamps and filename tokens; scrollback to text or HTML; session recordings with markers and playback. |
| Security | OS keychain for passwords and passphrases; 1Password, Bitwarden, HashiCorp Vault and command-based password managers; master password with unlock at start-up; encrypted password export (AES-256-GCM, scrypt). |
| Networking | Web pages and device web interfaces through a session (browser window with a temporary profile); local, remote and dynamic (SOCKS5) port forwarding; chainable jump hosts; SOCKS5 and HTTP CONNECT proxies; X11 and agent forwarding; automatic reconnect. |
| Updates | Stable and nightly channels from GitHub Releases, with the installer downloaded and opened from Help → Check for Updates; the team server is updated from Termwright. |

The complete list is in [docs/FEATURES.md](docs/FEATURES.md).

## Screenshots

| | |
|---|---|
| ![Start page](docs/screenshots/home.png) <br/>*Start page: search or connect, favourites and recent sessions* | ![Session side panel](docs/screenshots/session-panel.png) <br/>*Session properties beside the terminal, with device facts* |
| ![Files panel](docs/screenshots/files-panel.png) <br/>*Files panel following the terminal's folder* | ![SFTP](docs/screenshots/sftp.png) <br/>*SFTP browser: bookmarks, multi-select, Copy / Sync / Diff* |
| ![Compare](docs/screenshots/sftp-compare.png) <br/>*Comparing two files, with the verdict at the top* | ![Device facts](docs/screenshots/device-facts.png) <br/>*Device facts: read, edit, teach* |
| ![Tiled sessions](docs/screenshots/grid.png) <br/>*Tiled sessions with keyword highlighting* | ![Session Manager](docs/screenshots/session-manager.png) <br/>*Session Manager with the hover card* |
| ![Quick Connect](docs/screenshots/quickconnect.png) <br/>*Quick Connect with a device type* | ![Settings](docs/screenshots/options.png) <br/>*Settings* |
| ![Discover Devices](docs/screenshots/discover.png) <br/>*Network discovery: evidence, device type and Connect per device* | ![Configuration backups](docs/screenshots/backups.png) <br/>*Configuration backups* |
| ![Change checklist](docs/screenshots/checklist.png) <br/>*Change checklist: same or changed per device* | ![Run and Collect](docs/screenshots/collect.png) <br/>*Run and Collect into one table* |
| ![Open Web Interface](docs/screenshots/web-open.png) <br/>*A device's web interface through its jump station* | ![Command History](docs/screenshots/history.png) <br/>*Command History* |
| ![Tell Me When It Is Free](docs/screenshots/team-watch.png) <br/>*Teams: wait for a busy device* | ![Files panel on the left](docs/screenshots/files-panel-right.png) <br/>*Files panel on the left when the Session Manager is on the right* |

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

Pre-built packages are published on the [Releases](https://github.com/nperiannan/termwright/releases) page. Tagged versions are stable; the *nightly* pre-release is rebuilt from every change to `develop`; releases are tagged on `main`.

| Platform | Package | Notes |
|---|---|---|
| macOS (Apple Silicon) | `.dmg`, `.zip` | Current community builds are ad-hoc signed and not yet Apple-notarized. After installing, run `xattr -dr com.apple.quarantine /Applications/Termwright.app` once (or right-click → *Open*). |
| Windows 10 / 11 (x64) | installer `.exe`, `.zip` | Requires the WebView2 runtime, which is included in current Windows versions. SmartScreen may warn about an unsigned file. |
| Debian / Ubuntu (amd64, arm64) | `.deb` | `sudo apt install ./termwright_*.deb` |
| Other Linux (amd64, arm64) | `.tar.gz` | Requires GTK 3 and WebKitGTK 4.1. |
| Android (preview) | `.apk` | Current builds are signed with a temporary key: uninstall the earlier version before installing a new one. |

Every release has a `SHA256SUMS` file and a build provenance attestation, to check that a download is the one GitHub built from this repository: see [Verifying a download](SECURITY.md#verifying-a-download).

Installation, first start and every feature are described in the [User Guide](docs/USER_GUIDE.md); the [Emulated Lab Guide](docs/EMULATOR_GUIDE.md) goes step by step from a KVM host to the first traffic test. The [FAQ](docs/FAQ.md) answers common questions: how Termwright compares with other tools, what reaches the AI service, and how credentials are protected.

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
make test             # go vet, the Go tests (including an end-to-end SSH test) and the Python client's tests
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
| Emulated lab | Images, disks and the lab's files on the KVM hosts you add; the topology and its history in the data folder |

Termwright has no telemetry. Apart from the hosts you connect to (and the reachability checks and background logins you enable for them), it contacts only GitHub - for the update check and the download count shown in About - and, when you turn them on, the AI service you chose (only what you ask about, secrets removed), your team server and your KVM hosts. The MCP connector and the local API listen on this computer only, with a random key.

## Architecture

```mermaid
flowchart TB
  subgraph PC["Your computer - Termwright"]
    direction LR
    UI["Angular UI<br/>terminals (xterm.js), Session Manager,<br/>dialogs, VNC viewer"] <-- "bound methods, events" --> Go["Go backend (Wails)<br/>SSH2 · Telnet and Telnet over TLS · Serial · Raw<br/>Local shell · Mosh · SFTP/SCP · ZMODEM/X/YMODEM<br/>tunnels · scripts · backups · discovery · Wake-on-LAN<br/>TFTP · FTP · HTTP file servers"]
    Go --- Data[("Data folder<br/>sessions .ini, settings,<br/>logs, recordings, backups")]
    Go --- Keys[("OS keychain /<br/>password managers")]
  end
  Dev["Your devices<br/>switches, routers, firewalls, servers"]
  subgraph Lab["Lab host - Docker (optional, per team)"]
    TS["Team server (termwright-team)<br/>members, inventory, who uses what,<br/>reservations, usage history, admin page"] --- TD[("/data<br/>team.json, usage.json, cert, key")]
  end
  subgraph KVM["KVM hosts (optional, the emulated lab)"]
    VM["QEMU/KVM switches, routers, servers,<br/>PCs, ESP32 · captures<br/>traffic engines: built-in and fast"]
  end
  TRex["TRex server (optional)<br/>line-rate test ports"]
  AI["AI service (optional)<br/>Claude · Claude Code · OpenAI-compatible · Ollama"]
  Apps["AI apps (MCP)<br/>Claude Desktop · Claude Code · Cursor · VS Code"]
  Sync[("Sync folder (optional)<br/>OneDrive / iCloud / Dropbox / share")]
  GH["GitHub<br/>releases, ghcr.io image"]
  Browser["Browser<br/>admin page"]
  Go -- "SSH, Telnet, serial, RDP, VNC, mosh<br/>directly, via jump hosts or proxies" --> Dev
  Dev -- "TFTP, FTP, HTTP: images, licences, configs<br/>(file servers, only while started)" --> Go
  Go -- "HTTPS, pinned certificate<br/>only after joining a team" --> TS
  Go -- "sessions and settings,<br/>never passwords" --> Sync
  Go -- "update check, downloads" --> GH
  Browser -- "HTTPS, one-time link from Termwright" --> TS
  TS -- "scans (Discover), device facts" --> Dev
  Go -- "SSH: images, QEMU, consoles, test ports" --> VM
  VM -- "test ports on host network ports,<br/>cabled to your devices" --> Dev
  Go -- "TRex API (JSON-RPC over ZeroMQ)" --> TRex
  TRex -- "test ports, cabled" --> Dev
  Go -- "what you ask, secrets removed" --> AI
  Apps -- "MCP (stdio), this computer only" --> Go
  classDef tw fill:#1f3a5f,stroke:#58a6ff,stroke-width:2px,color:#eaf3ff
  classDef ai fill:#4a1d3f,stroke:#db61a2,stroke-width:2px,color:#ffeaf6
  classDef store fill:#4d3b0b,stroke:#e3b341,stroke-width:2px,color:#fff6dc
  classDef engine fill:#173f2a,stroke:#3fb950,stroke-width:2px,color:#e6ffed
  classDef team fill:#0f3d44,stroke:#39c5cf,stroke-width:2px,color:#e0fbff
  classDef trex fill:#5a3110,stroke:#f0883e,stroke-width:2px,color:#fff0e3
  classDef dut fill:#5c1d22,stroke:#f85149,stroke-width:2px,color:#ffecec
  classDef ext fill:#2d333b,stroke:#8b949e,stroke-width:2px,color:#f0f6fc
  class UI,Go tw
  class Data,Keys,TD,Sync store
  class AI,Apps ai
  class VM engine
  class TS team
  class TRex trex
  class Dev dut
  class GH,Browser ext
  style PC fill:#58a6ff14,stroke:#58a6ff,stroke-width:2px,stroke-dasharray:6 4
  style Lab fill:#39c5cf14,stroke:#39c5cf,stroke-width:2px,stroke-dasharray:6 4
  style KVM fill:#3fb95014,stroke:#3fb950,stroke-width:2px,stroke-dasharray:6 4
  linkStyle 0,1,2,3,7,8 stroke:#8b949e,stroke-width:2px
  linkStyle 4,5 stroke:#f85149,stroke-width:2px
  linkStyle 6,9,10 stroke:#39c5cf,stroke-width:2px
  linkStyle 11,12 stroke:#3fb950,stroke-width:2px
  linkStyle 13,14 stroke:#f0883e,stroke-width:2px
  linkStyle 15,16 stroke:#db61a2,stroke-width:2px
```

- **Termwright** is one program: an Angular interface over a Go backend (Wails). Connections, file transfers, scripts, backups and discovery run in the Go backend; passwords stay in the operating system's keychain or a password manager.
- **Teams are optional.** Without a team nothing is sent anywhere. A team runs its own **team server** - a small container on a lab host, set up and updated from Termwright over SSH - which every member's Termwright talks to over HTTPS with the certificate pinned by the invite. The server never sees passwords, keys or terminal output.
- **Sync is optional** too: with a sync folder, each Termwright writes its sessions and settings (never passwords) to a folder your computers share.
- **The emulated lab runs on your KVM hosts**: Termwright logs in over SSH, copies the images, starts QEMU and its own traffic engines there, and connects the consoles and test ports back. **AI is optional**: the assistant talks to the service you choose; AI apps reach Termwright through its MCP connector on this computer.

### Traffic generation

```mermaid
flowchart TB
  subgraph PC["Your computer - Termwright"]
    TUI["Traffic panel<br/>test ports, streams, TCP sessions,<br/>pass/fail limits, live graphs"]
    MCP["AI apps (MCP)<br/>lab_traffic, lab_test"]
    TGo["Go backend<br/>writes each host's traffic.json,<br/>picks the engine, adds up the counters"]
    Rep[("Report<br/>HTML / PDF, Excel")]
  end
  subgraph H1["KVM host"]
    Py["tw-traffic.py - built-in engine<br/>streams · ARP, DHCP, LLDP, LACP, STP,<br/>VRRP, BFD, IGMP/MLD, 802.1X clients,<br/>TCP/HTTP sessions, pcap replay"]
    Fast["tw-fast - fast engine (optional, Go)<br/>sends and counts the streams in batches"]
    SW["QEMU switch<br/>port with a test port"]
    NIC["Host network port<br/>(AF_PACKET)"]
  end
  subgraph H2["Another KVM host"]
    E2["tw-traffic.py / tw-fast"]
    SW2["QEMU switch"]
  end
  TRex["TRex server (optional)<br/>stateless mode, DPDK ports"]
  Real["Real device under test"]
  TUI <--> TGo
  MCP --> TGo
  TGo --> Rep
  TGo -- "SSH: traffic.json, start;<br/>counters twice a second" --> Py
  TGo -- "SSH" --> E2
  TGo -- "TRex JSON-RPC over ZeroMQ, TCP 4501:<br/>ports, streams, start, counters" --> TRex
  Py <-- "local UDP:<br/>every frame but the streams" --> Fast
  Fast <-- "QEMU UDP socket" --> SW
  Fast <-- "raw frames" --> NIC
  Py -. "without tw-fast:<br/>the same sockets" .- SW
  E2 <-- "QEMU UDP socket" --> SW2
  SW <-- "cable: UDP between hosts" --> SW2
  NIC <-- "Ethernet" --> Real
  TRex <-- "Ethernet / fibre" --> Real
  classDef tw fill:#1f3a5f,stroke:#58a6ff,stroke-width:2px,color:#eaf3ff
  classDef ai fill:#4a1d3f,stroke:#db61a2,stroke-width:2px,color:#ffeaf6
  classDef store fill:#4d3b0b,stroke:#e3b341,stroke-width:2px,color:#fff6dc
  classDef engine fill:#173f2a,stroke:#3fb950,stroke-width:2px,color:#e6ffed
  classDef qemu fill:#35255a,stroke:#a371f7,stroke-width:2px,color:#f1eaff
  classDef nic fill:#0f3d44,stroke:#39c5cf,stroke-width:2px,color:#e0fbff
  classDef trex fill:#5a3110,stroke:#f0883e,stroke-width:2px,color:#fff0e3
  classDef dut fill:#5c1d22,stroke:#f85149,stroke-width:2px,color:#ffecec
  class TUI,TGo tw
  class MCP ai
  class Rep store
  class Py,Fast,E2 engine
  class SW,SW2 qemu
  class NIC nic
  class TRex trex
  class Real dut
  style PC fill:#58a6ff14,stroke:#58a6ff,stroke-width:2px,stroke-dasharray:6 4
  style H1 fill:#3fb95014,stroke:#3fb950,stroke-width:2px,stroke-dasharray:6 4
  style H2 fill:#3fb95014,stroke:#3fb950,stroke-width:2px,stroke-dasharray:6 4
  linkStyle 0,1,2 stroke:#8b949e,stroke-width:2px
  linkStyle 3,4 stroke:#58a6ff,stroke-width:2px
  linkStyle 5,13 stroke:#f0883e,stroke-width:2px
  linkStyle 6 stroke:#3fb950,stroke-width:2px
  linkStyle 7,9,10,11 stroke:#a371f7,stroke-width:2px
  linkStyle 8,12 stroke:#39c5cf,stroke-width:2px
```

- **Test ports** sit on an emulated switch's port (the same QEMU UDP socket a cable uses, so no VM is needed), on a KVM host's network port cabled to a real device, or on a TRex server's port.
- **Two engines on each host.** `tw-traffic.py` (Python) speaks the protocols and sends the streams. With the fast engine on, `tw-fast` - a small Go program Termwright copies to the host - sends and counts the streams in batches and hands every other frame to `tw-traffic.py`. That is about 100,000 or 420,000 frames per second per host ([benchmarks](docs/PERFORMANCE.md)).
- **Across hosts** the streams travel over the lab's cables. Each frame carries its stream, sequence number and send time, so loss, order and latency are counted where it arrives, and Termwright adds up every host's counters.
- **TRex** (optional) is for line rate: Termwright drives it through its JSON-RPC API, and its results appear with the rest in the same report.

### Tests

```mermaid
flowchart TB
  AIx["AI assistant / MCP apps<br/>write, run, explain tests"]
  subgraph PC["Your computer - Termwright"]
    Rec["Test recorder<br/>commands, API requests and SNMP<br/>become steps; checks verified"]
    Ed["Tests tab<br/>steps · YAML · code · procedures<br/>testbeds · calendar · breakpoints"]
    Run["Test runner<br/>setup · steps · cleanup · loops · data<br/>reruns · timeouts · restore"]
    Drv["Step drivers<br/>SSH / Telnet · REST · SNMP · NETCONF<br/>traffic generators"]
    Web["Playwright runner<br/>Node.js + Chromium"]
    Out[("Reports<br/>HTML · PDF · JUnit · run logs · trends")]
    Rec --> Ed --> Run
    Run --> Drv
    Run --> Web
    Run --> Out
  end
  Folder[("Tests folder (Git)<br/>cases · procs · testbeds<br/>templates · bookings · roles")]
  subgraph DUT["Under test"]
    Dev["Devices<br/>switches, routers, servers"]
    Api["REST APIs"]
    Pages["Web pages<br/>device GUIs, portals"]
    TG["Spirent TestCenter / IxNetwork<br/>chassis via REST API"]
  end
  subgraph Std["Exported suite - no Termwright needed"]
    Py["Python suite<br/>unittest · twlocal · twprocs"]
    GoS["Go suite<br/>go test · lib/standalone · twprocs"]
    TwR["twrun<br/>the YAML tests as they are"]
  end
  CI["CI<br/>Jenkins, GitHub Actions…"]
  Int["qTest · Jira<br/>Teams / e-mail"]
  AIx -- "tests tool" --> Run
  Ed <--> Folder
  Drv -- "CLI · SNMP · NETCONF" --> Dev
  Drv -- "HTTPS" --> Api
  Web -- "browser" --> Pages
  Out -- "results, defects" --> Int
  Folder -- "Export Automation Suite<br/>inventory.yaml, no passwords" --> Std
  Std --> DUT
  CI -- "run.sh · twrun · go test" --> Std
  Drv -- "REST API" --> TG
  classDef tw fill:#1f3a5f,stroke:#58a6ff,stroke-width:2px,color:#eaf3ff
  classDef ai fill:#4a1d3f,stroke:#db61a2,stroke-width:2px,color:#ffeaf6
  classDef store fill:#4d3b0b,stroke:#e3b341,stroke-width:2px,color:#fff6dc
  classDef engine fill:#173f2a,stroke:#3fb950,stroke-width:2px,color:#e6ffed
  classDef qemu fill:#35255a,stroke:#a371f7,stroke-width:2px,color:#f1eaff
  classDef dut fill:#5c1d22,stroke:#f85149,stroke-width:2px,color:#ffecec
  classDef ext fill:#2d333b,stroke:#8b949e,stroke-width:2px,color:#f0f6fc
  class Rec,Ed,Run,Drv tw
  class Web qemu
  class Out,Folder store
  class Py,GoS,TwR engine
  class Dev,Api,Pages,TG dut
  class Int,CI ext
  class AIx ai
  style PC fill:#58a6ff14,stroke:#58a6ff,stroke-width:2px,stroke-dasharray:6 4
  style Std fill:#3fb95014,stroke:#3fb950,stroke-width:2px,stroke-dasharray:6 4
  style DUT fill:#f8514914,stroke:#f85149,stroke-width:2px,stroke-dasharray:6 4
  linkStyle 0,1,2,4,6 stroke:#58a6ff,stroke-width:2px
  linkStyle 5 stroke:#db61a2,stroke-width:2px
  linkStyle 7,8 stroke:#f85149,stroke-width:2px
  linkStyle 3,9 stroke:#a371f7,stroke-width:2px
  linkStyle 10,13 stroke:#8b949e,stroke-width:2px
  linkStyle 11,12 stroke:#3fb950,stroke-width:2px
  linkStyle 14 stroke:#f0883e,stroke-width:2px
```

- **Record, then run.** Commands typed in terminal tabs, API Client requests and SNMP queries become steps; each check is tried on what the device showed before the test is saved. The **test runner** sends the steps through the same logins as the terminals, drives web pages with **Playwright** (Node.js and Chromium on this computer) and writes HTML, PDF and JUnit reports with each step's expected and found values.
- **The tests folder is plain files** - YAML tests, procedures, testbeds, templates and bookings - so a team shares it with Git. qTest, Jira and Teams / e-mail get the results when you set them up.
- **Exported suites run without Termwright**: Python or Go code with their own device library, or the YAML with **twrun**. They read the devices from `inventory.yaml` and the passwords from the environment, the keychain or a prompt - never from the suite - so CI runs them anywhere.

### API Client

```mermaid
flowchart LR
  subgraph PC["Your computer - Termwright"]
    direction TB
    AUI["API Client<br/>requests, collections, environments,<br/>auth, scripts, code snippets, history"]
    AGo["Go backend<br/>variables, login / OAuth2 tokens,<br/>pre-request and test scripts (JavaScript)"]
    Mock["Mock servers<br/>a collection's saved examples"]
    Mon["Monitors<br/>collections on a schedule"]
    Col[("Collections and environments<br/>Postman / OpenAPI import")]
    Sec[("OS keychain<br/>API keys, tokens, secrets")]
  end
  Api["REST APIs<br/>devices, controllers, cloud"]
  App["Front ends and tests<br/>under development"]
  Tst["Test recorder<br/>requests become test steps"]
  Note["Teams / e-mail<br/>monitor fails and recovers"]
  AUI <--> AGo
  AGo --- Col
  AGo --- Sec
  AGo -- "HTTPS (any certificate: per session)" --> Api
  Mon --> AGo
  App -- "HTTP, this computer" --> Mock
  AGo -- "recorded requests" --> Tst
  Mon -- "notifications" --> Note
  classDef tw fill:#1f3a5f,stroke:#58a6ff,stroke-width:2px,color:#eaf3ff
  classDef store fill:#4d3b0b,stroke:#e3b341,stroke-width:2px,color:#fff6dc
  classDef engine fill:#173f2a,stroke:#3fb950,stroke-width:2px,color:#e6ffed
  classDef dut fill:#5c1d22,stroke:#f85149,stroke-width:2px,color:#ffecec
  classDef ext fill:#2d333b,stroke:#8b949e,stroke-width:2px,color:#f0f6fc
  class AUI,AGo tw
  class Mock,Mon engine
  class Col,Sec store
  class Api dut
  class App,Tst,Note ext
  style PC fill:#58a6ff14,stroke:#58a6ff,stroke-width:2px,stroke-dasharray:6 4
  linkStyle 0,1,2,4 stroke:#58a6ff,stroke-width:2px
  linkStyle 3 stroke:#f85149,stroke-width:2px
  linkStyle 5,6,7 stroke:#8b949e,stroke-width:2px
```

- Requests go from the Go backend, with the session's variables, login (API key, bearer, login request, OAuth2) and its pre-request and test scripts. Secrets stay in the keychain. **Mock servers** answer with a collection's saved examples on this computer; **monitors** run collections on a schedule and say when they start failing and when they pass again.

### SNMP Browser

```mermaid
flowchart LR
  subgraph PC["Your computer - Termwright"]
    direction TB
    SUI["SNMP Browser<br/>MIB tree, get / walk / set, table view,<br/>compare, poll, history"]
    SGo["Go SNMP engine<br/>v1 · v2c · v3 (USM: SHA-2, AES-256)<br/>MIB parser with the standard MIBs"]
    Dash["Dashboards and alarms<br/>graphs, thresholds"]
    Trap["Trap receiver<br/>v1 / v2c / v3 traps and informs"]
    Sim["SNMP simulator<br/>an agent from a saved walk"]
    MIBs[("MIBs<br/>built in + yours")]
    Sec[("OS keychain<br/>communities, v3 passwords")]
  end
  Dev["Devices<br/>SNMP agents, UDP 161"]
  NMS["Monitoring tools and tests<br/>that need an agent"]
  Note["Teams / e-mail<br/>alarm starts"]
  SUI <--> SGo
  SGo --- MIBs
  SGo --- Sec
  SGo -- "get, getnext, bulk, walk, set (UDP 161)" --> Dev
  Dash --> SGo
  Dev -- "traps and informs (UDP 162)" --> Trap
  NMS -- "UDP, this computer" --> Sim
  Dash -- "notifications" --> Note
  classDef tw fill:#1f3a5f,stroke:#58a6ff,stroke-width:2px,color:#eaf3ff
  classDef store fill:#4d3b0b,stroke:#e3b341,stroke-width:2px,color:#fff6dc
  classDef engine fill:#173f2a,stroke:#3fb950,stroke-width:2px,color:#e6ffed
  classDef dut fill:#5c1d22,stroke:#f85149,stroke-width:2px,color:#ffecec
  classDef ext fill:#2d333b,stroke:#8b949e,stroke-width:2px,color:#f0f6fc
  class SUI,SGo tw
  class Dash,Trap,Sim engine
  class MIBs,Sec store
  class Dev dut
  class NMS,Note ext
  style PC fill:#58a6ff14,stroke:#58a6ff,stroke-width:2px,stroke-dasharray:6 4
  linkStyle 0,1,2,4 stroke:#58a6ff,stroke-width:2px
  linkStyle 3,5 stroke:#f85149,stroke-width:2px
  linkStyle 6,7 stroke:#8b949e,stroke-width:2px
```

- One SNMP engine for the browser, dashboards, the Network Map, tests and the AI tools: v1, v2c and v3 with SHA-2 and AES, and a MIB parser with the standard MIBs and yours. The **trap receiver** takes traps and informs; the **simulator** answers as an agent from a saved walk, for tools and tests without the device.

### Syslog

```mermaid
flowchart LR
  subgraph PC["Your computer - Termwright"]
    direction TB
    LUI["Syslog view<br/>cards by severity, search, filters"]
    LGo["Syslog server (Go)<br/>RFC 3164 / 5424 parser"]
    Al["Rules<br/>alerts on patterns"]
    Files[("Saved files and exports")]
  end
  Dev["Devices<br/>send syslog"]
  Tst["Tests<br/>watch for messages during a step"]
  AIx["AI assistant / MCP apps<br/>syslog tool"]
  Dev -- "UDP, TCP or TLS (RFC 5425)" --> LGo
  LGo --> LUI
  LGo --> Al
  LGo --- Files
  Tst -- "watch: expect / forbid messages" --> LGo
  AIx -- "read and send messages" --> LGo
  classDef tw fill:#1f3a5f,stroke:#58a6ff,stroke-width:2px,color:#eaf3ff
  classDef ai fill:#4a1d3f,stroke:#db61a2,stroke-width:2px,color:#ffeaf6
  classDef store fill:#4d3b0b,stroke:#e3b341,stroke-width:2px,color:#fff6dc
  classDef engine fill:#173f2a,stroke:#3fb950,stroke-width:2px,color:#e6ffed
  classDef dut fill:#5c1d22,stroke:#f85149,stroke-width:2px,color:#ffecec
  classDef ext fill:#2d333b,stroke:#8b949e,stroke-width:2px,color:#f0f6fc
  class LUI,LGo tw
  class Al engine
  class Files store
  class Dev dut
  class Tst ext
  class AIx ai
  style PC fill:#58a6ff14,stroke:#58a6ff,stroke-width:2px,stroke-dasharray:6 4
  linkStyle 0 stroke:#f85149,stroke-width:2px
  linkStyle 1,2,3 stroke:#58a6ff,stroke-width:2px
  linkStyle 4 stroke:#8b949e,stroke-width:2px
  linkStyle 5 stroke:#db61a2,stroke-width:2px
```

- The syslog server receives over UDP, TCP or TLS and reads RFC 3164 and RFC 5424 messages; the view shows them as cards by severity, rules raise alerts, and tests can wait for a message (or fail on one) while a step runs.

## Project structure

```
main.go                     the entry point: embeds the window, the guides and scripts, and starts app/
app/run.go, app/*_app.go    the desktop app: window and menus, methods bound to the UI
app/teams_*.go, sync_app.go Teams (join, set up and update a team server, inventory sync, status) and folder sync
internal/session            session model, folder store, import and export
internal/sessionfile        round-trip .ini / XML session file reader and writer
internal/term               SSH2, Telnet (and over TLS), serial, local PTY, Mosh, SFTP/SCP, file transfer protocols, scripting
internal/fileserve          TFTP, FTP and HTTP servers devices fetch files from and send files to
internal/team               the team server and its client: members, inventory, presence, reservations, usage, admin page
internal/discover           network discovery (also used by the team server's scans)
internal/emu, app/emu_*.go  the emulated lab: KVM hosts, QEMU, images, cables, the traffic generator (traffic.py), captures, tests
internal/sim                the simulated lab's switches
app/ai_*.go, app/mcp_app.go the AI assistant and agent, the MCP connector
app/api_app.go, scripts/py… the local REST API and the Python client
internal/clirun             logs in to a device and runs show commands (the team server's device facts)
internal/backup, checklist  configuration backups, change checklists
internal/config             data directory and global settings
internal/secrets, vault     OS keychain integration, the encrypted password export
internal/secretref          password manager integration
internal/history            command history
internal/facts, wol, update device facts, Wake-on-LAN, software update
cmd/termwright-team         the team server program
deploy/team                 Dockerfile, Compose file and systemd unit of the team server
frontend/                   Angular application (standalone components, signals, zoneless) with xterm.js
docs/                       user guide, feature list, team server guide, API, Python, scripting and session file references
```

## Contributing

Bug reports and feature requests are welcome in [GitHub Issues](https://github.com/nperiannan/termwright/issues/new/choose); the forms ask for the version, the operating system and the steps to reproduce. For pull requests, see [CONTRIBUTING.md](CONTRIBUTING.md). Report security problems privately, as described in the [security policy](SECURITY.md). Everyone taking part follows the [code of conduct](CODE_OF_CONDUCT.md).

## License

Termwright is licensed under the [Apache License 2.0](LICENSE). Third-party notices are listed in [NOTICE](NOTICE).

---

<sub>SecureCRT® is a registered trademark of VanDyke Software, Inc. PuTTY, KiTTY, SuperPuTTY, MobaXterm, mRemoteNG and other product names are the property of their respective owners. Termwright is an independent open-source project, not affiliated with or endorsed by any of them; these names are used only to identify the file formats Termwright can import and export.</sub>
