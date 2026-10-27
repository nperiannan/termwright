# Session files

Termwright stores every session as a plain-text `.ini` file under `Config/Sessions/<folder>/<name>.ini`. Folder order lives
in `__FolderData__.ini` (`Z:"Folder List"`, `Z:"Session List"`), and the root `Default.ini` is the template for new
sessions. When a file is edited, only the keys below are changed. Every other line, including unknown types, is written
back unchanged. Files keep their UTF-8 BOM and CRLF style.

## Line types

| Prefix | Meaning | Example |
|---|---|---|
| `S:` | string | `S:"Hostname"=192.0.2.10` |
| `D:` | 32-bit value, 8 hex digits | `D:"[SSH2] Port"=00000016` |
| `B:` | binary: byte count, then hex bytes on indented lines | `B:"X"=00000002` / ` 0a 0b` |
| `Z:` | string list: count, then one indented line per item | `Z:"Description"=00000001` / ` text` |

## Mapped keys

| Termwright field | Key | Notes |
|---|---|---|
| Protocol | `S:"Protocol Name"` | `SSH2`, `Telnet`, `Serial`, `Local Shell`, `Raw`; `RDP`, `VNC` and `Console` (console server port) are Termwright-only (other programs do not open them) |
| Hostname / Username | `S:"Hostname"`, `S:"Username"` | |
| SSH port | `D:"[SSH2] Port"` | |
| Telnet / Raw port | `D:"Port"` | |
| Identity file | `S:"Identity Filename V2"` | `:raw`/`:pubkey` suffixes ignored |
| Auth methods | `S:"SSH2 Authentications V2"` | comma list |
| Emulation | `S:"Emulation"` | Xterm, VT100, VT102, VT220, ANSI, Linux |
| Description | `Z:"Description"` | |
| Rows / Cols / Scrollback | `D:"Rows"`, `D:"Cols"`, `D:"Scrollback"` | |
| Keep-alive | `D:"Idle NO-OP Check"`, `D:"Idle NO-OP Timeout"` | |
| Serial | `S:"[Serial] Com Port"`, `D:"[Serial] Baud Rate"`, `D:"[Serial] Data Bits"`, `D:"[Serial] Stop Bits"`, `D:"[Serial] Parity"` | stop bits 0/1/2 = 1/1.5/2; parity 0-4 = N/O/E/M/S |
| Log | `S:"Log Filename V2"`, `D:"Start Log Upon Connect"`, `D:"Append to Log File"`, `S:"Custom Log Message Each Line"` + `D:"Log Timestamp Each Line"` | the prefix is only used when the timestamp flag is on (verified 9.x keys) |
| Local directory | `S:"Initial Local Directory V2"` | |
| Color scheme | `S:"Color Scheme"` | name; common scheme names of imported sessions are mapped to built-in schemes |
| Jump host | `S:"Firewall Name"` | `Session:<path>` = jump through that saved session, `None` = direct |
| Auto reconnect | `D:"Auto Reconnect"` | |
| X11 / agent forwarding | `D:"Forward X11"`, `D:"Enable Agent Forwarding"` | |
| Keyword set | `S:"Keyword Set"` | name of a Termwright keyword set |
| Automate logon | `D:"Use Login Script"` | verified 9.x key |
| Send initial CR | `D:"Send Initial Carriage Return"` | verified 9.x key |
| Logon script | `D:"Use Script File"`, `S:"Script Filename V2"`, `S:"Script Arguments"` | verified 9.x keys; only `.js` scripts run in Termwright |
| SSH2 remote command | `D:"Use Shell Command"`, `S:"Shell Command"` | verified 9.x keys |
| Connect as SFTP | `S:"Termwright Open As"` | `sftp` = open as an SFTP-only session |
| Serial flow control | `S:"Termwright Serial Flow"` | `rtscts` / `xonxoff` |

Termwright-only keys use a `Termwright ` prefix (`Termwright Logon Actions`, `Termwright Triggers`, `Termwright Port Forwards`,
`Termwright Proxy Type/Host/Port/User`, `Termwright Font`, `Termwright Font Size`, `Termwright Backspace`, `Termwright Save Password`,
`Termwright Raw Log`, `Termwright Local Command`, `Termwright Terminal Type`, `Termwright Console Server/Port/Access/User/Base Port/Protocol`, `Termwright Record Session`, `Termwright Smart Card`, `Termwright MAC Address`, `Termwright Serial Detect Speed`, `Termwright Show Timestamps`, `Termwright No Auto Scroll`). The original application ignores them.

Keys marked *verified* were checked against real session files created by users.

## XML

Imports and exports XML settings exports: a `<key name="Sessions">` tree with `<string>`, `<dword>` (decimal),
`<binary>` (hex) and `<array>` values. A key that holds values is a session, and a key that only holds keys is a folder.

## Saved passwords
* Saved passwords in imported session files (`S:"Password V2"`, the older `S:"Password"`, or plain text) are moved into the OS
  keychain when the user imports their own sessions, and removed from the copied file (`Termwright Save Password` is set). A
  configuration passphrase, if one was set, has to be typed. Values that cannot be read are left in the file and listed in the
  import result. Export does not write passwords back.

## Not converted
* `Z:"Login Script V4"` (an encrypted expect/send table) is kept unchanged;
  Termwright keeps its own table in `Z:"Termwright Logon Actions"` (`expect<TAB>send<TAB>flags<TAB>credential`), so
  re-create the steps once in Termwright. `S:"Credential Title"` is preserved.
* Port forwards (verified with 9.x files): `Z:"Port Forward Table V3"` / `Z:"Reverse Forward Table V3"` lines are
  `enabled|name|bindIP,port|destDifferent|destHost|destPort|application|arguments`, e.g. `1|local|10.0.0.5,8080|0||80||`
  (empty host = the SSH server itself) and `1|dynamic|,1080|0|socks,|||` (SOCKS). They are read into Termwright's forwards
  when a session has none of its own (disabled entries skipped; reverse-table entries become remote forwards). The
  original tables are kept unchanged.
* Keyword sets, fonts (binary LOGFONT), key maps and scripts are preserved but not interpreted.

## Import and export

The same layout is used by SecureCRT®, so a SecureCRT `Config/Sessions` folder, a zip of it or an XML settings export can be imported, and Termwright's export writes files that SecureCRT can read (File → Export Sessions → folder, zip or XML). Keys that Termwright does not know are kept as they are.

---

*SecureCRT® is a registered trademark of VanDyke Software, Inc. Termwright is an independent open-source project, not affiliated with or endorsed by VanDyke Software; the name is used only to say which files Termwright can import and export.*
