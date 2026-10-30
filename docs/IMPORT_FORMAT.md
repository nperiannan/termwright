# Import format

Termwright imports sessions from several tools (SecureCRT, PuTTY, SuperPuTTY, MobaXterm, mRemoteNG and OpenSSH
`config`). It will not grow a new importer for every tool, because each one has to follow a file format that changes
without notice. Instead, **this page says what Termwright accepts**: if your sessions are in any other tool, a
spreadsheet or an inventory, write them in this format (a script of a few lines, or a spreadsheet) and import the
result with **Import Sessions → Termwright template (CSV)**.

The easiest start is the template: **File → Import Sessions → *Save the Termwright import template…*** (the first thing in the dialog), or
[`examples/sessions-template.csv`](examples/sessions-template.csv). It has a session of every kind.

## CSV (the format to write)

A text file with a header row and one session per row. UTF-8 (a byte-order mark is fine). The separator is a comma, a
semicolon or a tab, found from the header. Column names are not case-sensitive and can be in any order; a column that
is missing is simply left empty.

| Column | What it holds | Notes |
|---|---|---|
| `host` | address or name of the machine | **The only column that must be there.** Also `hostname`, `ip`, `address`. A row without a host is skipped (a serial row needs `com_port`, a Wake-on-LAN row needs `mac`). |
| `name` | the session's name | Defaults to the host. A `/` in it is replaced by `-`. |
| `folder` | folder in the Session Manager | `Datacenter/Core` makes nested folders. Also `group`. |
| `protocol` | `ssh`, `telnet`, `rdp`, `vnc`, `serial`, `raw` or `wol` | Empty = ssh. An unknown value is reported and ssh is used. |
| `port` | TCP port | Empty = the protocol's default (22, 23, 3389, 5900). |
| `username` | login name | Also `user`. |
| `device_type` | a device type of Termwright | `VOSS`, `EXOS`, `Cisco IOS`, `Linux`, or one you made. Also `type`, `os`, `platform`. |
| `environment` | `production`, `staging`, `test` or `development` | Another value is reported and ignored. |
| `jump_host` | the **path of another session** used as jump host | `Folder/name`. See below. Also `jump`. |
| `identity_file` | private key file | Also `key`. |
| `description` | free text | |
| `mac` | MAC address | Used by Wake-on-LAN and *Find by MAC Address*. |
| `account` | name of a Termwright account (a login that several sessions share) | `-` = use the session's own login. |
| `files_panel` | `yes` to open the Files panel beside the terminal | SSH only. |
| `com_port`, `baud` | serial port (`COM3`, `/dev/ttyUSB0`) and speed | Serial rows. |
| `network` | network to search for the device, `192.0.2.0/24` | Wake-on-LAN rows. |

A column Termwright does not know is **not an error**: it is ignored and named in the import result, so you can keep
your own columns in the file.

**Passwords are not imported** from CSV. Put them in the OS keychain with *Passwords & Accounts*, or type them when
connecting and choose *Save Password*.

### Jump hosts, and RDP and VNC through an SSH session

`jump_host` names another session by its path (its `folder` and `name` joined by `/`). Rows that use a jump host are
connected through that session, which can be in the same file or already in your list.

The usual use is a remote desktop on a Linux machine that is reached through SSH. The RDP or VNC service listens on the
machine's own address only, so the session's host is `127.0.0.1`, and the machine is the jump host:

```csv
name,folder,protocol,host,port,username,jump_host
lab-pc,Lab,ssh,192.0.2.20,22,labuser,
lab-pc (RDP),Lab,rdp,127.0.0.1,3389,labuser-rdp,Lab/lab-pc
lab-pc (VNC),Lab,vnc,127.0.0.1,5902,labuser-vnc,Lab/lab-pc
office-pc,Lab,rdp,192.0.2.30,3389,jdoe,
```

(`127.0.0.1` here is the machine itself, as seen from the jump host. The last row reaches a Windows PC directly.)

Import **into a folder** (*Import into folder* in the dialog) and the jump hosts follow: every session is put under that
folder, and `jump_host` is changed to the imported copy of the jump session. A jump host that is in neither the file nor
your sessions is named in the result, so you can create it afterwards.

### What the import tells you

After the import the dialog lists, in *Left out or replaced*:

- columns it does not use,
- rows without a host (skipped),
- a protocol, port or environment it did not understand,
- jump hosts it could not find.

Nothing is dropped silently.

## Converting from another tool

You do not need Termwright's help for this. Any language that can write a CSV works. For example, from a JSON list of
hosts:

```python
import csv, json, sys

hosts = json.load(open("inventory.json"))   # [{"name": "sw1", "ip": "10.0.0.1", "site": "DC1", "os": "voss"}, ...]
with open("sessions.csv", "w", newline="", encoding="utf-8") as f:
    w = csv.writer(f)
    w.writerow(["name", "folder", "protocol", "host", "port", "username", "device_type"])
    for h in hosts:
        w.writerow([h["name"], h["site"], "ssh", h["ip"], 22, "admin", {"voss": "VOSS"}.get(h["os"], "Linux")])
```

From a spreadsheet: put these headers in the first row, fill the rows, and **Save As → CSV**.

## Termwright's own formats

- **Folder, zip and XML export** (*File → Export Sessions*) keep **everything** in a session: port forwards, logon
  actions, triggers, colours and more. They import back into Termwright with nothing lost, and the jump hosts follow
  as above. They use the layout described in [Session files](SESSION_FILES.md), the same layout SecureCRT® uses.
  To produce one from another tool, the practical way is to **export one session from Termwright and use it as the
  model**; for anything that is just a list of hosts, CSV is simpler.
- **CSV export** writes this same CSV (every session, RDP and VNC included), so *export, edit in a spreadsheet, import*
  works.
- PuTTY, MobaXterm, Tabby and OpenSSH `config` exports have no RDP or VNC, so those sessions are left out of them and
  listed in the export result.

## Other sources Termwright reads

Ansible inventories (INI and YAML), NetBox and AWS EC2 can be imported directly (see the [User Guide](USER_GUIDE.md#import-and-export)).
Everything else: convert to the CSV above.

---

*SecureCRT® is a registered trademark of VanDyke Software, Inc. Termwright is an independent open-source project, not affiliated with or endorsed by VanDyke Software.*
