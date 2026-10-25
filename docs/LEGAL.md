# Legal guardrails

*This is engineering guidance, not legal advice. Have counsel review it before a public release.*

Termwright is a **clean-room, independent implementation**. The following rules keep it that way.

## What we do
* **Write all code from scratch**, using only open-source libraries with permissive licenses (MIT, BSD, Apache-2.0). See NOTICE.
* **Interoperate with data formats.** We read and write the plain-text session `.ini` files and XML exports that users create
  with their own licensed copy. File-format compatibility for interoperability is widely accepted practice. Many tools import
  PuTTY, SecureCRT or MobaXterm sessions in the same way.
* **Use generic UI conventions**: a session tree, tabs, a toolbar, a command window and a button bar. These are common to most terminal
  emulators (PuTTY, MobaXterm, Termius, Windows Terminal, iTerm2).
* **Use other product names only descriptively**: "imports SecureCRT sessions".

## What we never do
* Never copy, decompile, disassemble or reverse-engineer VanDyke binaries, scripts or documentation text.
* Never use VanDyke icons, logos, screenshots, splash screens, color schemes, help text or the names *SecureCRT*/*SecureFX* in the
  product name, binary name, icons or marketing.
* Never decrypt SecureCRT-encrypted password fields (`Password V2`). They are preserved untouched on import and export.
* Never include VanDyke sample session files in the repo. Test fixtures are hand-written with documentation IP ranges (RFC 5737).
* Never implement license-key checks or anything that bypasses VanDyke licensing.
* Never accept contributions that contain VanDyke code or text. Add a DCO/CLA check in CI before accepting outside PRs.

## Naming
"Termwright" uses "CRT", a generic term for a cathode-ray-tube terminal. Before a 1.0 release, run a trademark search
in your target markets. If counsel prefers a fully distinct name, rename it: the name appears only in `wails.json`,
`main.go`, `README.md`, `NOTICE`, the UI brand text and the keychain service name.
