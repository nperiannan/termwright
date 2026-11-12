# Termwright test ports: sends and counts frames on switch ports of the emulated lab.
# One per lab folder on a host. Reads traffic.json (written by Termwright), writes
# traffic-stats.json twice a second. A test port is a UDP socket pair with a switch
# port's QEMU socket network: frames the switch sends arrive on "listen", frames
# sent to "send" reach the switch.
import json, os, socket, struct, sys, time, select

d = sys.argv[1]
cfg_path, st_path = os.path.join(d, "traffic.json"), os.path.join(d, "traffic-stats.json")
MAGIC = b"TWTG"
BC = b"\xff" * 6


def mac_b(m):
    return bytes(int(x, 16) for x in m.split(":"))


def ip_b(a):
    return socket.inet_aton(a.split("/")[0]) if a else b"\0\0\0\0"


def csum(b):
    if len(b) % 2:
        b += b"\0"
    s = sum(struct.unpack("!%dH" % (len(b) // 2), b))
    while s >> 16:
        s = (s & 0xFFFF) + (s >> 16)
    return (~s) & 0xFFFF


def l2(dst, src, vlan, prio, etype):
    h = dst + src
    if vlan:
        h += struct.pack("!HH", 0x8100, (prio << 13) | vlan)
    return h + struct.pack("!H", etype)


def ipv4(src, dst, proto, payload):
    h = struct.pack("!BBHHHBBH4s4s", 0x45, 0, 20 + len(payload), 0, 0x4000, 64, proto, 0, src, dst)
    h = h[:10] + struct.pack("!H", csum(h)) + h[12:]
    return h + payload


def arp(op, smac, sip, tmac, tip):
    return struct.pack("!HHBBH6s4s6s4s", 1, 0x0800, 6, 4, op, smac, sip, tmac, tip)


class Tester:
    def __init__(s, t):
        s.id, s.mac, s.ip, s.vlan = t["id"], mac_b(t["mac"]), ip_b(t.get("ip", "")), t.get("vlan", 0)
        s.gw = ip_b(t.get("gw", "")) if t.get("gw") else None
        s.to, s.listen = ("127.0.0.1", t["send"]), t["listen"]
        s.sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        s.sock.setsockopt(socket.SOL_SOCKET, socket.SO_RCVBUF, 4 << 20)
        s.sock.bind(("127.0.0.1", t["listen"]))
        s.sock.setblocking(False)
        s.arp = {}  # ip bytes -> mac bytes
        s.last_arp = 0

    def send(s, frame):
        if len(frame) < 60:
            frame += b"\0" * (60 - len(frame))
        try:
            s.sock.sendto(frame, s.to)
        except OSError:
            pass

    def ask(s, ip):  # ARP request
        s.send(l2(BC, s.mac, s.vlan, 0, 0x0806) + arp(1, s.mac, s.ip, b"\0" * 6, ip))


class Stream:
    def __init__(s, x, idx, testers, run):
        s.x, s.idx, s.run = x, idx, run
        s.tp = testers.get(x["from"])
        s.pps, s.size = max(1, x.get("pps", 100)), max(60, min(9000, x.get("size", 128)))
        s.count = x.get("count") or int(s.pps * max(1, x.get("seconds", 10)))
        s.seq, s.start, s.gw_wait = 0, None, 0
        s.dst = mac_b(x["dstMac"]) if x.get("dstMac") else None

    def frame(s, now):
        x, tp = s.x, s.tp
        sig = MAGIC + struct.pack("!HIId", s.idx, s.run, s.seq, now)
        k = x.get("kind", "udp")
        src_ip = ip_b(x["srcIp"]) if x.get("srcIp") else tp.ip
        dst_ip = ip_b(x.get("dstIp") or "")
        dst = s.dst or BC
        if k == "arp":
            body = arp(1, tp.mac, src_ip, b"\0" * 6, dst_ip) + sig
            f = l2(BC if not s.dst else s.dst, tp.mac, x.get("vlan", 0), x.get("prio", 0), 0x0806) + body
        elif k == "raw":
            f = l2(dst, tp.mac, x.get("vlan", 0), x.get("prio", 0), 0x88B5) + sig
        else:
            hl = 18 if x.get("vlan") else 14
            pad = max(0, s.size - hl - 20 - (8 if k != "tcp" else 20) - len(sig))
            data = sig + b"\0" * pad
            if k == "icmp":
                p = struct.pack("!BBHHH", 8, 0, 0, 0x7477, s.seq & 0xFFFF) + data
                p = p[:2] + struct.pack("!H", csum(p)) + p[4:]
                f = l2(dst, tp.mac, x.get("vlan", 0), x.get("prio", 0), 0x0800) + ipv4(src_ip, dst_ip, 1, p)
            elif k == "tcp":
                p = struct.pack("!HHIIBBHHH", 49152 + s.idx, x.get("dport", 80), s.seq, 0, 5 << 4, 0x02, 65535, 0, 0) + data
                ph = src_ip + dst_ip + struct.pack("!BBH", 0, 6, len(p))
                p = p[:16] + struct.pack("!H", csum(ph + p)) + p[18:]
                f = l2(dst, tp.mac, x.get("vlan", 0), x.get("prio", 0), 0x0800) + ipv4(src_ip, dst_ip, 6, p)
            else:
                p = struct.pack("!HHHH", 49152 + s.idx, x.get("dport", 5001), 8 + len(data), 0) + data
                f = l2(dst, tp.mac, x.get("vlan", 0), x.get("prio", 0), 0x0800) + ipv4(src_ip, dst_ip, 17, p)
        if len(f) < s.size and k in ("raw", "arp"):
            f += b"\0" * (s.size - len(f))
        return f


cfg_m, cfg = 0, {}
testers, streams, run = {}, [], 0
tx, rx, other, lat, err = {}, {}, {}, {}, {}
last_st, idle_since = 0, time.time()


def load():
    global cfg, testers, streams, run, tx, rx, other, lat, err
    try:
        c = json.load(open(cfg_path))
    except Exception:
        return
    old = {t: testers[t] for t in testers}
    want = {t["id"]: t for t in c.get("testers", [])}
    for tid in list(old):
        if tid not in want or want[tid]["listen"] != old[tid].listen:
            old[tid].sock.close()
            del testers[tid]
    for tid, t in want.items():
        if tid not in testers:
            try:
                testers[tid] = Tester(t)
            except OSError as e:
                err["port " + tid] = str(e)
        else:
            tt = testers[tid]
            tt.ip, tt.vlan, tt.mac = ip_b(t.get("ip", "")), t.get("vlan", 0), mac_b(t["mac"])
            tt.gw = ip_b(t.get("gw", "")) if t.get("gw") else None
    cfg = c
    if not c.get("run"):  # stopped: no more sending, the counts stay
        streams = []
    elif c["run"] != run:
        run = c["run"]
        tx, rx, other, lat, err = {}, {}, {}, {}, {}
        streams = [Stream(x, i, testers, run) for i, x in enumerate(c.get("streams", [])) if x.get("on") and x["from"] in testers]


def handle(tp, f):
    o = 12
    vlan = 0
    if f[o:o + 2] == b"\x81\x00":
        vlan = struct.unpack("!H", f[o + 2:o + 4])[0] & 0xFFF
        o += 4
    et = struct.unpack("!H", f[o:o + 2])[0]
    b = f[o + 2:]
    i = f.find(MAGIC)
    if i >= 0 and len(f) >= i + 22:
        idx, r, seq, ts = struct.unpack("!HIId", f[i + 4:i + 22])
        if r == run and idx < len(cfg.get("streams", [])):
            x = cfg["streams"][idx]
            sid = x["id"]
            echo = x["from"] == tp.id and not x.get("to") and x.get("kind") == "icmp"  # the address answered
            if x.get("to") and x["to"] != tp.id:
                other[sid] = other.get(sid, 0) + 1
            elif x["from"] != tp.id or echo:
                rx[sid] = rx.get(sid, 0) + 1
                if x["from"] in testers:  # the same clock: latency
                    d = (time.time() - ts) * 1000
                    l = lat.setdefault(sid, [0.0, 0.0, 0])
                    l[0] += d
                    l[1] = max(l[1], d)
                    l[2] += 1
        return
    if et == 0x0806 and len(b) >= 28:
        op, smac, sip, tip = struct.unpack("!6xH6s4s6x4s", b[:28])
        if sip != b"\0\0\0\0":
            tp.arp[sip] = smac
        if op == 1 and tip == tp.ip and tp.ip != b"\0\0\0\0":
            tp.send(l2(smac, tp.mac, vlan, 0, 0x0806) + arp(2, tp.mac, tp.ip, smac, sip))
    elif et == 0x0800 and len(b) >= 28 and b[9] == 1 and b[16:20] == tp.ip and b[20] == 8:
        ihl = (b[0] & 15) * 4
        icmp = bytearray(b[ihl:struct.unpack("!H", b[2:4])[0]])
        icmp[0] = 0
        icmp[2:4] = b"\0\0"
        icmp[2:4] = struct.pack("!H", csum(bytes(icmp)))
        tp.send(l2(f[6:12], tp.mac, vlan, 0, 0x0800) + ipv4(tp.ip, b[12:16], 1, bytes(icmp)))


while True:
    try:
        m = os.stat(cfg_path).st_mtime
    except OSError:
        break
    if m != cfg_m:
        cfg_m = m
        load()
        idle_since = time.time()
    if not cfg.get("testers"):
        break
    now = time.time()
    socks = [t.sock for t in testers.values()]
    busy = any(s.tp is not None and s.seq < s.count for s in streams)
    r, _, _ = select.select(socks, [], [], 0.001 if busy else 0.05) if socks else ([], [], [])
    for t in testers.values():
        if t.sock in r:
            for _ in range(2000):
                try:
                    f = t.sock.recv(65536)
                except BlockingIOError:
                    break
                except OSError:
                    break
                if len(f) >= 14:
                    try:
                        handle(t, f)
                    except Exception:
                        pass
    for s in streams:
        if s.tp is None or s.seq >= s.count:
            continue
        x = s.x
        if s.dst is None and x.get("routed") and x.get("kind") != "arp":
            gw = s.tp.gw
            mac = s.tp.arp.get(gw) if gw else None
            if not mac:
                if now - s.tp.last_arp > 1 and gw:
                    s.tp.last_arp = now
                    s.tp.ask(gw)
                s.gw_wait = s.gw_wait or now
                if now - s.gw_wait > 6:
                    err[x["id"]] = "no ARP reply from the gateway " + x.get("gwIp", "")
                    s.seq = s.count
                continue
            s.dst = mac
        elif s.dst is None and x.get("toMac"):
            s.dst = mac_b(x["toMac"])
        if s.start is None:
            s.start = now
        due = min(s.count, int((now - s.start) * s.pps) + 1)
        n = 0
        while s.seq < due and n < 2000:
            s.tp.send(s.frame(time.time()))
            s.seq += 1
            n += 1
        tx[x["id"]] = s.seq
    if now - last_st > 0.5:
        last_st = now
        st = {"run": run, "at": now, "tx": tx, "rx": rx, "other": other, "lat": lat, "err": err,
              "busy": {s.x["id"]: s.seq < s.count for s in streams}, "testers": sorted(testers)}
        try:
            with open(st_path + ".tmp", "w") as fh:
                json.dump(st, fh)
            os.replace(st_path + ".tmp", st_path)
        except OSError:
            pass
