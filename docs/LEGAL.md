# Legal guardrails

*This is engineering guidance, not legal advice. Have a lawyer review it before a 1.0 release or any commercial use.*

Termwright is a **clean-room, independent implementation**. The following rules keep it that way.

## What we do
* **Write all code from scratch**, using only open-source libraries with permissive licenses (MIT, BSD, Apache-2.0). See NOTICE.
* **Interoperate with data formats.** We read and write the plain-text session `.ini` files and XML exports that users create
  with their own licensed copy. File-format compatibility for interoperability is widely accepted practice. Many tools import
  PuTTY, SecureCRT or MobaXterm sessions in the same way.
* **Use generic UI conventions**: a session tree, tabs, a toolbar, a command window and a button bar. These are common to most terminal
  emulators (PuTTY, MobaXterm, Termius, Windows Terminal, iTerm2).
* **Use other product names only descriptively**: "imports SecureCRT sessions". Termwright is not affiliated with or endorsed by
  VanDyke Software; SecureCRT and SecureFX are their trademarks.

## Saved passwords on import
When a user imports **their own** SecureCRT sessions, Termwright can move the passwords saved in those files into the operating
system's keychain, so the user does not have to type them again:

* It runs only on the user's computer, only on files the user selects, and only when the user starts the import.
* The encrypted fields (`Password V2` and the older `Password`) are decoded using the file format that independent security
  researchers have publicly documented, including its published default keys. The code is written from that description, not from
  VanDyke code, and it does not touch SecureCRT itself, its binaries or its licensing.
* If the user protected the configuration with a passphrase, the user has to type it; nothing is guessed or brute-forced.
* After the import the passwords are removed from the copied session files and kept only in the keychain. Export never writes them back.

This is the part of Termwright closest to another vendor's product. It is worth a specific look in any legal review.

## What we never do
* Never copy, decompile, disassemble or reverse-engineer VanDyke binaries, scripts or documentation text.
* Never use VanDyke icons, logos, screenshots, splash screens, color schemes, help text or the names *SecureCRT*/*SecureFX* in the
  product name, binary name, icons or marketing.
* Never include VanDyke sample session files in the repo. Test fixtures are hand-written with documentation IP ranges (RFC 5737).
* Never implement license-key checks or anything that bypasses VanDyke licensing.
* Never accept contributions that contain VanDyke code or text. Add a DCO/CLA check in CI before accepting outside PRs.

## Naming
"Termwright" is an original name: "term" (terminal) + "wright" (a maker, as in playwright or shipwright). It does not contain or
imitate any other product's name. Before a 1.0 release, run a trademark search in the markets where it will be offered. If the
name ever has to change, it appears only in `wails.json`, `main.go`, `README.md`, `NOTICE`, the UI brand text and the keychain
service name.
