# Frequently asked questions

Short answers to the questions people ask most often about Termwright. Each answer links to the guide that has the details.

- [Why use Termwright instead of SecureCRT?](#why-use-termwright-instead-of-securecrt)
- [Why not VS Code?](#why-not-vs-code)
- [Why not GNS3 or EVE-NG?](#why-not-gns3-or-eve-ng)
- [Why not Containerlab?](#why-not-containerlab)
- [Why not TRex?](#why-not-trex)
- [Why does terminal software need AI?](#why-does-terminal-software-need-ai)
- [Why Wails?](#why-wails)
- [How are credentials protected?](#how-are-credentials-protected)
- [What data reaches the AI service?](#what-data-reaches-the-ai-service)
- [Does the AI work locally?](#does-the-ai-work-locally)
- [What does the MCP server expose?](#what-does-the-mcp-server-expose)
- [Is QEMU bundled?](#is-qemu-bundled)
- [How does the lab work across several hosts?](#how-does-the-lab-work-across-several-hosts)
- [Was this written with AI?](#was-this-written-with-ai)

## Why use Termwright instead of SecureCRT?

SecureCRT is mature, polished and has very good scripting; if it does everything you need, keep using it. Termwright is different in these ways:

- It is open source (Apache-2.0) and free, and works the same on macOS, Windows and Linux.
- It imports SecureCRT sessions, folders and settings, and also PuTTY, MobaXterm and OpenSSH configurations. See [Migrating existing sessions](../README.md#migrating-existing-sessions).
- Beyond the terminal, it has a team inventory with device reservations, an emulated network lab with a traffic generator, and an optional AI assistant.

## Why not VS Code?

VS Code is an editor. Its remote features are made for servers that can run a VS Code server. Network engineers mostly work with switches, routers and console servers: serial ports, Telnet, XMODEM and YMODEM, jump hosts, device-specific logins, and logging and scripts per session. That is what Termwright is built for, and VS Code doesn't aim there. Some of Termwright's interface ideas, such as the chat box and the panel icons, come from VS Code on purpose.

## Why not GNS3 or EVE-NG?

Both are excellent, with larger communities and many more device templates; Termwright even imports GNS3 appliance files. The differences:

- Termwright works with plain Linux KVM hosts over SSH. Nothing is installed on them except QEMU from the distribution's own packages: no server, no special VM image.
- The lab is in the same app as your terminal sessions.
- It has a built-in traffic generator with pass/fail reports, and an AI agent that can build and test topologies.

If you need IOU, Dynamips or a large template library, GNS3 or EVE-NG is the better choice. See the [Emulated Lab Guide](EMULATOR_GUIDE.md).

## Why not Containerlab?

If your network operating system ships as a container and you want topologies as code in CI, Containerlab is the better tool. Termwright runs VM images (qcow2), because many network operating systems ship only that way, and has a visual workflow: canvas, consoles, traffic and reports. It doesn't run containers.

## Why not TRex?

Termwright supports TRex as an optional traffic engine: it drives a TRex server through TRex's own API, and that server's ports become test ports in the lab. The built-in engines are for functional tests (forwarding, ACLs, failover, TCP sessions) without extra hardware:

| Engine | 64-byte frames | 1518-byte frames |
|---|---|---|
| Built-in | about 100,000 frames/s per host | about 58,000 frames/s |
| Fast | about 420,000 frames/s per host | about 210,000 frames/s (2.5 Gbit/s) |

These were measured on a cloud VM with 2 vCPUs; the method and all results are in [PERFORMANCE.md](PERFORMANCE.md). For line-rate testing, use TRex. Termwright's TRex client has so far been tested only against a simulated TRex server.

## Why does terminal software need AI?

It doesn't, and the AI stays off until you set it up. When it's on, it helps you:

- explain output you select;
- find a problem across several devices' tabs;
- run a multi-step task in your tabs, with every change waiting for your approval;
- build and test lab topologies.

The MCP connector works the other way round: the AI tool you already use can work with your terminal tabs safely, instead of you copying and pasting between them.

## Why Wails?

[Wails](https://wails.io) pairs a Go backend with the operating system's own web view:

- **Go**: good SSH, Telnet, serial and SFTP libraries, a single static binary, and easy cross-compiling.
- **The system web view** (WebView2 on Windows, WKWebView on macOS, WebKitGTK on Linux) instead of a bundled browser engine, so the download and memory use stay smaller than an Electron app's.
- **xterm.js** draws the terminal.

The cost is small differences between the platforms' web views, especially WebKitGTK on Linux. That is why Termwright draws its own title bar and menus on Linux.

## How are credentials protected?

- Passwords are kept in the operating system's credential store: the macOS Keychain, Windows Credential Manager or Linux Secret Service. Session files never contain passwords.
- Passwords can come from 1Password, Bitwarden or HashiCorp Vault when you connect.
- Smart cards and other PKCS#11 tokens are supported for SSH keys.
- A password export is encrypted with AES-256-GCM, with a key derived from your passphrase with scrypt.
- SSH host keys are checked on every connection.
- Commands that look like they contain a secret are never saved in the command history.

See [Data and privacy](../README.md#data-and-privacy).

## What data reaches the AI service?

Only what you send:

- your question;
- the context you attach: a selection, a tab's recent output, the device type;
- in agent mode, the output of the commands it runs.

Before anything is sent, Termwright removes passwords, keys, SNMP communities, tokens and password hashes. An option also replaces IP addresses, MAC addresses and host names with consistent placeholders. Nothing is sent in the background, and you choose the AI service and use your own key or sign-in.

## Does the AI work locally?

Yes. It works with Ollama, or any server that offers an OpenAI-compatible API on your own computer or network, such as LM Studio, vLLM or the llama.cpp server. How well it works depends on the model, and agent mode needs a model that is good at calling tools.

## What does the MCP server expose?

The MCP server is off until you turn it on. Termwright then listens only on 127.0.0.1, on a random port, with a random key stored in a file only your user can read. `termwright mcp` connects an AI tool to it over standard input and output.

- **Sessions**: list sessions and tabs, read a tab (with secrets removed), open a session and send a command. Opening a session and sending a command ask for your approval in Termwright, and a production session asks for every command.
- **Status and help.**
- **The lab**: about 30 `lab_*` tools to add and cable switches, use their consoles, move them between hosts, run traffic and captures, and test. Lab changes don't ask each time, because a lab is disposable, but every change is recorded and can be reverted.

See [Use Termwright from other AI apps (MCP)](USER_GUIDE.md#use-termwright-from-claude-and-other-ai-apps-mcp) in the User Guide.

## Is QEMU bundled?

No. The lab's *Set Up* installs QEMU on the KVM host from the distribution's own packages, after showing you the commands. There are two exceptions:

- Espressif's QEMU, for emulated ESP32 boards, is downloaded from Espressif's releases and checked against its SHA-256.
- The fast traffic engine, a small Go program, is part of Termwright and is copied to the lab's folder on the host.

## How does the lab work across several hosts?

There is no agent or service on the hosts: Termwright connects to each host over SSH.

- **Switches**: each switch is a plain QEMU process.
- **Cables**: each cable is a point-to-point UDP link between two QEMU network ports, over 127.0.0.1 on one host or to the other host's address between hosts. A small relay on the host connects cables added while the switches run.
- **Moving a switch**: a running switch moves to another host with QEMU's live migration over TCP ports 29400–29999.
- **Management network**: a bridge or macvtap, a routed proxy-ARP mode for hosts that are themselves VMs, a NAT network, or port forwarding.
- **State**: the lab's topology and history are kept in Termwright's data folder. Switches keep running when Termwright closes, and Termwright picks them up again the next time it starts.

See [Use several hosts](EMULATOR_GUIDE.md#6-use-several-hosts) in the Emulated Lab Guide.

## Was this written with AI?

Yes, much of the code was written with an AI coding assistant, working from the author's design and specifications. The author decides what to build and how it should work and look, reviews mock-ups before any change to the interface, and tests each release on macOS and Linux. Every feature comes with its own tests: about 470 Go test functions and 20 Python tests. CI runs them on every change (the core packages with Go's race detector; the tests that need a real KVM host are run by hand) and builds it for macOS, Windows, Linux and Android. If you find something that looks wrong, please open an issue.
