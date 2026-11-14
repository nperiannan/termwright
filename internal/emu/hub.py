# Termwright hubs and L2 switches: lab nodes without a VM. Each leg is a switch port of the lab that has no
# cable (its QEMU sends to the UDP port a test port would listen on, and receives on another). A hub sends
# what comes in on one leg out of every other. An L2 switch learns source MACs per VLAN and sends a frame to
# the leg its destination was seen on (to every leg of the VLAN when unknown, broadcast or multicast; entries
# age out after 300 s); its ports are access (untagged in one VLAN) or trunk (tagged VLANs, untagged in the
# native one); an in-band management address answers ARP and ping; a command line listens on the host's
# 127.0.0.1 (telnet, and SSH when Python has paramiko). Reads hubs.json ({"hubs": [{"id", "name", "kind",
# "ports", "legs": [[listen, send, port, label], ...], "cfg", "console", "ssh"}]}), writes hubs-stats.json
# (frames per port, learned MACs, the configuration as it runs); ends when there are none.
import json, os, select, socket, struct, sys, threading, time

d = sys.argv[1]
path, st_path = os.path.join(d, "hubs.json"), os.path.join(d, "hubs-stats.json")
AGE = 300
lock = threading.RLock()
socks, where, m0, last = {}, {}, None, 0  # listen port -> socket; listen port -> (hub, leg index)
hubs, macs, count, cfgs = {}, {}, {}, {}  # hub id -> its settings; -> {(vlan, mac): (leg, seen)}; -> frames per port; -> cfg
servers = {}  # (hub id, kind) -> listening socket


def pcfg(h, port):  # a port's mode, VLAN and tagged VLANs
    c = cfgs.get(h["id"], {}).get("ports", {}).get(str(port)) or {}
    return c.get("mode", "access"), int(c.get("vlan", 1 if c.get("mode", "access") == "access" else 0) or 0), set(c.get("vlans") or [])


def in_vlan(h, port, vid):  # tagged (True), untagged (False) or not in the VLAN (None)
    mode, vlan, tagged = pcfg(h, port)
    if mode == "access":
        return False if vid == vlan else None
    if vid == vlan:
        return False
    return True if vid in tagged else None


def mymac(h):
    n = sum(h["id"].encode()) & 0xFFFF
    return bytes([0x02, 0x74, 0x68, 0x00, n >> 8, n & 0xFF])


def csum(b):
    if len(b) % 2:
        b += b"\0"
    s = sum(struct.unpack("!%dH" % (len(b) // 2), b))
    s = (s >> 16) + (s & 0xFFFF)
    s += s >> 16
    return ~s & 0xFFFF


def send_leg(h, i, f):
    li, se = h["legs"][i][0], h["legs"][i][1]
    out = socks.get(li)
    if out is not None:
        try:
            out.sendto(f, ("127.0.0.1", se))
        except OSError:
            pass


def egress(h, i, f, vid, pcp):  # the frame (untagged) as port i sends it in VLAN vid, or None
    t = in_vlan(h, h["legs"][i][2], vid)
    if t is None:
        return None
    if t:
        return f[:12] + struct.pack("!HH", 0x8100, (pcp << 13) | vid) + f[12:]
    return f


def mgmt(h, i, f, vid):  # the switch's own address: ARP and ping
    c = cfgs.get(h["id"], {})
    ip = (c.get("mgmtIp") or "").split("/")[0]
    if not ip or vid != int(c.get("mgmtVlan") or 1):
        return False
    try:
        me = socket.inet_aton(ip)
    except OSError:
        return False
    mac, et, b = mymac(h), struct.unpack("!H", f[12:14])[0], f[14:]
    if et == 0x0806 and len(b) >= 28 and b[24:28] == me and struct.unpack("!H", b[6:8])[0] == 1:
        rep = f[6:12] + mac + b"\x08\x06" + struct.pack("!HHBBH6s4s6s4s", 1, 0x0800, 6, 4, 2, mac, me, b[8:14], b[14:18])
        out = egress(h, i, rep, vid, 0)
        if out:
            send_leg(h, i, out)
        return True
    if et == 0x0800 and f[0:6] == mac and len(b) >= 28 and b[9] == 1 and b[16:20] == me:
        ihl = (b[0] & 15) * 4
        icmp = b[ihl:]
        if icmp[0] == 8:
            icmp = b"\0\0\0\0" + icmp[4:]
            icmp = icmp[:2] + struct.pack("!H", csum(icmp)) + icmp[4:]
            iph = struct.pack("!BBHHHBBH4s4s", 0x45, 0, 20 + len(icmp), 0, 0, 64, 1, 0, me, b[12:16])
            iph = iph[:10] + struct.pack("!H", csum(iph)) + iph[12:]
            out = egress(h, i, f[6:12] + mac + b"\x08\x00" + iph + icmp, vid, 0)
            if out:
                send_leg(h, i, out)
        return True
    return f[0:6] == mac


def switch_frame(h, i, f, now):
    port = h["legs"][i][2]
    vid, pcp, et = 0, 0, struct.unpack("!H", f[12:14])[0]
    if et == 0x8100:
        tci = struct.unpack("!H", f[14:16])[0]
        vid, pcp = tci & 0xFFF, tci >> 13
        f = f[:12] + f[16:]
        if in_vlan(h, port, vid) is not True:
            return
    else:
        mode, vlan, _ = pcfg(h, port)
        if not vlan:
            return
        vid = vlan
    if f[0] & 1 or f[0:6] == mymac(h):
        if mgmt(h, i, f, vid) and not f[0] & 1:
            return
    tbl = macs[h["id"]]
    if not f[6] & 1:
        tbl[(vid, f[6:12])] = (i, now)
    to = tbl.get((vid, f[0:6]))
    outs = range(len(h["legs"]))
    if not f[0] & 1 and to and now - to[1] < AGE:
        outs = [to[0]] if to[0] != i else []
    for o in outs:
        if o != i:
            out = egress(h, o, f, vid, pcp)
            if out:
                send_leg(h, o, out)


# ---- the command line
HELP = """Commands (abbreviations work, ? for this list):
  show ports                          ports: what is there, mode, VLANs, frames
  show vlan                           VLANs and their ports
  show fdb                            learned MAC addresses
  show management                     the management address
  configure port <list> access vlan <n>
  configure port <list> trunk vlans <list> [native <n>]
  configure management ipaddress <a.b.c.d/len> [vlan <n>]
  unconfigure management ipaddress
  clear fdb
  exit
<list>: 1,3,5-8"""


def plist(s, top):
    out = []
    for part in s.split(","):
        if "-" in part:
            a, b = part.split("-", 1)
            out += list(range(int(a), int(b) + 1))
        elif part:
            out.append(int(part))
    if not out or any(p < 1 or p > top for p in out):
        raise ValueError("ports are 1-%d" % top)
    return out


def word(w, *names):
    return any(n.startswith(w) for n in names) and len(w) > 0


def run_cmd(hid, line):
    with lock:
        h = hubs.get(hid)
        if not h:
            return "This switch is gone."
        c = cfgs.setdefault(hid, {"rev": 0, "ports": {}, "mgmtVlan": 1})
        w = line.split()
        if not w:
            return ""
        top = h["ports"]
        try:
            if w[0] in ("?", "help"):
                return HELP
            if word(w[0], "show"):
                what = w[1] if len(w) > 1 else ""
                if word(what, "ports"):
                    at = {lg[2]: lg for lg in h["legs"]}
                    rows = ["Port  To                          Mode    VLANs            Frames in", "-" * 72]
                    for p in range(1, top + 1):
                        mode, vlan, tagged = pcfg(h, p)
                        v = str(vlan) if mode == "access" else ",".join(str(x) for x in sorted(tagged)) + (" native %d" % vlan if vlan else "")
                        lg = at.get(p)
                        rows.append("%-5d %-27s %-7s %-16s %d" % (p, (lg[3] if lg else "-")[:27], mode, v or "-", count.get(hid, [0] * top)[p - 1] if p - 1 < len(count.get(hid, [])) else 0))
                    return "\n".join(rows)
                if word(what, "vlan"):
                    vl = {}
                    for p in range(1, top + 1):
                        mode, vlan, tagged = pcfg(h, p)
                        if vlan:
                            vl.setdefault(vlan, []).append(str(p))
                        for t in tagged:
                            vl.setdefault(t, []).append(str(p) + "t")
                    return "\n".join(["VLAN  Ports (t = tagged)", "-" * 40] + ["%-5d %s" % (k, ",".join(v)) for k, v in sorted(vl.items())])
                if word(what, "fdb", "mac"):
                    now = time.time()
                    rows = ["VLAN  MAC                Port  Age", "-" * 40]
                    for (vid, m), (i, seen) in sorted(macs.get(hid, {}).items()):
                        if now - seen < AGE and i < len(h["legs"]):
                            rows.append("%-5d %s  %-5d %ds" % (vid, m.hex(":"), h["legs"][i][2], now - seen))
                    return "\n".join(rows)
                if word(what, "management"):
                    return "Management: %s, VLAN %d, MAC %s" % (c.get("mgmtIp") or "none", int(c.get("mgmtVlan") or 1), mymac(h).hex(":"))
                return "show ports | vlan | fdb | management"
            if word(w[0], "configure") and len(w) > 2 and word(w[1], "port"):
                ports = plist(w[2], top)
                if len(w) > 5 and word(w[3], "access") and word(w[4], "vlan"):
                    v = int(w[5])
                    if not 1 <= v <= 4094:
                        raise ValueError("VLAN 1-4094")
                    for p in ports:
                        c["ports"][str(p)] = {"mode": "access", "vlan": v}
                elif len(w) > 5 and word(w[3], "trunk") and word(w[4], "vlans"):
                    tagged = sorted(set(plist(w[5], 4094)))
                    native = int(w[7]) if len(w) > 7 and word(w[6], "native") else 0
                    for p in ports:
                        c["ports"][str(p)] = {"mode": "trunk", "vlan": native, "vlans": tagged}
                else:
                    return "configure port <list> access vlan <n> | trunk vlans <list> [native <n>]"
                c["rev"] = c.get("rev", 0) + 1
                return ""
            if word(w[0], "configure") and len(w) > 3 and word(w[1], "management") and word(w[2], "ipaddress"):
                ip = w[3] if "/" in w[3] else w[3] + "/24"
                socket.inet_aton(ip.split("/")[0])
                c["mgmtIp"] = ip
                if len(w) > 5 and word(w[4], "vlan"):
                    c["mgmtVlan"] = int(w[5])
                c["rev"] = c.get("rev", 0) + 1
                return ""
            if word(w[0], "unconfigure") and len(w) > 1 and word(w[1], "management"):
                c["mgmtIp"] = ""
                c["rev"] = c.get("rev", 0) + 1
                return ""
            if word(w[0], "clear") and len(w) > 1 and word(w[1], "fdb", "mac"):
                macs[hid] = {}
                return ""
        except (ValueError, OSError) as e:
            return "Error: %s" % e
        return "Unknown command: %s (? lists them)" % line.strip()


def cli(hid, rd, wr, close):  # one session: read lines, answer
    with lock:
        name = hubs.get(hid, {}).get("name", "l2sw")
    wr("\r\n%s - Termwright L2 switch. ? lists the commands.\r\n\r\n%s # " % (name, name))
    buf, prev = b"", 0
    while True:
        data = rd()
        if not data:
            break
        out = b""
        i = 0
        while i < len(data):
            ch = data[i]
            if ch == 255 and i + 1 < len(data):  # telnet IAC: skip its option
                i += 3 if data[i + 1] in (251, 252, 253, 254) else 2
                continue
            i += 1
            was, prev = prev, ch
            if ch in (13, 10):
                if ch == 10 and was == 13:
                    continue
                line = buf.decode("utf-8", "replace")
                buf = b""
                wr((out + b"\r\n").decode())
                out = b""
                if line.strip() in ("exit", "quit", "logout"):
                    close()
                    return
                ans = run_cmd(hid, line)
                if ans:
                    wr(ans.replace("\n", "\r\n") + "\r\n")
                wr("%s # " % name)
            elif ch in (8, 127):
                if buf:
                    buf = buf[:-1]
                    out += b"\b \b"
            elif ch == 63 and not buf.strip():  # ? at the start of a line
                wr("?\r\n" + HELP.replace("\n", "\r\n") + "\r\n%s # " % name)
            elif ch >= 32:
                buf += bytes([ch])
                out += bytes([ch])
        if out:
            wr(out.decode("utf-8", "replace"))
    close()


def telnet_client(hid, conn):
    try:
        conn.sendall(bytes([255, 251, 1, 255, 251, 3]))  # will echo, will suppress go-ahead
        cli(hid, lambda: conn.recv(1024), lambda s: conn.sendall(s.encode()), conn.close)
    except OSError:
        pass


def ssh_client(hid, conn, key):
    try:
        import paramiko

        class S(paramiko.ServerInterface):
            def check_auth_none(self, u):
                return paramiko.AUTH_SUCCESSFUL

            def check_auth_password(self, u, p):
                return paramiko.AUTH_SUCCESSFUL

            def get_allowed_auths(self, u):
                return "none,password"

            def check_channel_request(self, kind, cid):
                return paramiko.OPEN_SUCCEEDED if kind == "session" else paramiko.OPEN_FAILED_ADMINISTRATIVELY_PROHIBITED

            def check_channel_pty_request(self, *a):
                return True

            def check_channel_shell_request(self, ch):
                return True

        t = paramiko.Transport(conn)
        t.add_server_key(key)
        t.start_server(server=S())
        ch = t.accept(20)
        if ch is None:
            t.close()
            return
        cli(hid, lambda: ch.recv(1024), lambda s: ch.sendall(s.encode()), lambda: (ch.close(), t.close()))
    except Exception:
        try:
            conn.close()
        except OSError:
            pass


def ssh_key():
    try:
        import paramiko
    except ImportError:
        return None
    kp = os.path.join(d, "hub-ssh-key")
    try:
        return paramiko.RSAKey(filename=kp)
    except Exception:
        k = paramiko.RSAKey.generate(2048)
        k.write_private_key_file(kp)
        return k


def serve(hid, kind, s, key):
    while True:
        try:
            conn, _ = s.accept()
        except OSError:
            return
        f = telnet_client if kind == "console" else (lambda a, b: ssh_client(a, b, key))
        threading.Thread(target=f, args=(hid, conn), daemon=True).start()


KEY = None
ssh_ok = False
while True:
    try:
        m = os.stat(path).st_mtime
    except OSError:
        break
    if m != m0:
        m0 = m
        try:
            cfg = json.load(open(path)).get("hubs", [])
        except Exception:
            cfg = []
        with lock:
            hubs = {}
            for h in cfg:
                legs = [(int(x[0]), int(x[1]), int(x[2]) if len(x) > 2 and x[2] else j + 1, x[3] if len(x) > 3 else "") for j, x in enumerate(h.get("legs", []))]
                hubs[h["id"]] = {"id": h["id"], "name": h.get("name", h["id"]), "kind": h.get("kind", "hub"), "ports": int(h.get("ports") or 8), "legs": legs, "console": int(h.get("console") or 0), "ssh": int(h.get("ssh") or 0)}
                c = h.get("cfg") or {}
                if int(c.get("rev", 0)) > int(cfgs.get(h["id"], {}).get("rev", -1)):  # set in the screens: newer than what was typed
                    cfgs[h["id"]] = {"rev": int(c.get("rev", 0)), "ports": dict(c.get("ports") or {}), "mgmtIp": c.get("mgmtIp", ""), "mgmtVlan": int(c.get("mgmtVlan") or 1)}
                cfgs.setdefault(h["id"], {"rev": 0, "ports": {}, "mgmtIp": "", "mgmtVlan": 1})
                count[h["id"]] = (count.get(h["id"], []) + [0] * hubs[h["id"]]["ports"])[:hubs[h["id"]]["ports"]]
                macs.setdefault(h["id"], {})
            want = {}
            for hid, h in hubs.items():
                for i, lg in enumerate(h["legs"]):
                    want[lg[0]] = (hid, i)
            for lp in list(socks):
                if lp not in want:
                    socks.pop(lp).close()
            for lp in want:
                if lp not in socks:
                    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
                    s.setsockopt(socket.SOL_SOCKET, socket.SO_RCVBUF, 4 << 20)
                    try:
                        s.bind(("127.0.0.1", lp))
                    except OSError:
                        s.close()
                        continue
                    s.setblocking(False)
                    socks[lp] = s
            where = want
            for hid in list(macs):  # a removed hub forgets what it learned
                if hid not in hubs:
                    macs.pop(hid, None)
                    count.pop(hid, None)
                    cfgs.pop(hid, None)
            wanted = {}
            for hid, h in hubs.items():
                if h["kind"] == "switch":
                    if h["console"]:
                        wanted[(hid, "console")] = h["console"]
                    if h["ssh"]:
                        wanted[(hid, "ssh")] = h["ssh"]
            for k in list(servers):
                if k not in wanted or servers[k][1] != wanted[k]:
                    try:
                        servers.pop(k)[0].close()
                    except OSError:
                        pass
            for k, port in wanted.items():
                if k in servers:
                    continue
                if k[1] == "ssh":
                    KEY = KEY or ssh_key()
                    if KEY is None:
                        continue
                s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
                s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
                try:
                    s.bind(("127.0.0.1", port))
                    s.listen(4)
                except OSError:
                    s.close()
                    continue
                servers[k] = (s, port)
                threading.Thread(target=serve, args=(k[0], k[1], s, KEY), daemon=True).start()
            ssh_ok = KEY is not None
        if not hubs:
            break
    r, _, _ = select.select(list(socks.values()), [], [], 0.5) if socks else ([], [], time.sleep(0.5))
    now = time.time()
    with lock:
        for s in r:
            lp = s.getsockname()[1]
            hid, i = where.get(lp, (None, 0))
            h = hubs.get(hid)
            for _ in range(2000):
                try:
                    f = s.recv(65536)
                except OSError:
                    break
                if not h or len(f) < 14 or i >= len(h["legs"]):
                    continue
                p = h["legs"][i][2]
                if 1 <= p <= len(count[hid]):
                    count[hid][p - 1] += 1
                if h["kind"] == "switch":
                    switch_frame(h, i, f, now)
                    continue
                for o in range(len(h["legs"])):
                    if o != i:
                        send_leg(h, o, f)
        if now - last > 2:
            last = now
            st = {"at": now, "hubs": {hid: {"in": count.get(hid, []), "ssh": ssh_ok and hubs[hid]["kind"] == "switch",
                                            "macs": [[m.hex(":"), vid, hubs[hid]["legs"][v[0]][2] if v[0] < len(hubs[hid]["legs"]) else 0, int(now - v[1])] for (vid, m), v in macs.get(hid, {}).items() if now - v[1] < AGE][:500],
                                            "cfg": cfgs.get(hid) if hubs[hid]["kind"] == "switch" else None} for hid in hubs}}
            try:
                with open(st_path + ".tmp", "w") as fh:
                    json.dump(st, fh)
                os.replace(st_path + ".tmp", st_path)
            except OSError:
                pass
