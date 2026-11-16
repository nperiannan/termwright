# Security policy

Termwright stores logins, connects to production devices, talks to AI services and can be driven by other AI apps, so security reports are very welcome.

## Reporting a vulnerability

Please **don't open a public issue** for a security problem. Report it privately on GitHub instead:

1. Open the repository's **Security** tab.
2. Choose **Report a vulnerability**.
3. Describe the problem: the version (Help → About), the operating system, what an attacker can do, and the steps to reproduce it.

You'll get an answer as soon as possible, usually within a week. Once a fix is released, the report is published as a GitHub security advisory, with credit to you unless you'd rather stay anonymous.

## Supported versions

Fixes go into the latest release and the nightly build. Older releases aren't patched; update to the latest one.

## Scope

In scope:

- the desktop app, including the local API, the MCP connector (`termwright mcp`) and the AI assistant;
- the team server (`termwright-team`) and its container image;
- the helpers Termwright copies to KVM hosts (the traffic engines and relays);
- the Android app;
- the release builds and the build workflow.

Out of scope:

- problems in third-party components (report them to their project; tell us too if Termwright needs an update);
- the security of the KVM hosts, TRex servers and devices you connect to;
- attacks that need an attacker already signed in to your computer as you.

## How Termwright protects your data

- Passwords are kept in the operating system's credential store or come from your password manager; session files never contain them.
- Before anything goes to an AI service, passwords, keys, SNMP communities, tokens and password hashes are removed.
- The local API and the MCP connector are off until you turn them on, listen only on 127.0.0.1 and need a random key.
- Termwright has no telemetry.

The details are in the [FAQ](docs/FAQ.md#how-are-credentials-protected) and in [Data and privacy](README.md#data-and-privacy).

## Verifying a download

Every release has a `SHA256SUMS` file and a signed build provenance attestation, which shows that the files were built by this repository's GitHub workflow from a given commit.

```bash
# the checksum (Linux; on macOS use: shasum -a 256 <file>)
sha256sum -c SHA256SUMS --ignore-missing

# the build provenance (needs the GitHub CLI)
gh attestation verify <file> --repo nperiannan/termwright
```

On Windows, compare `Get-FileHash <file> -Algorithm SHA256` with the line for that file in `SHA256SUMS`.
