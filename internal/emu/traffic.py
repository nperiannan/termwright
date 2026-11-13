# Termwright test ports: sends and counts frames on switch ports of the emulated lab.
# One per lab folder on a host. Reads traffic.json (written by Termwright), writes
# traffic-stats.json twice a second. A test port is a UDP socket pair with a switch
# port's QEMU socket network: frames the switch sends arrive on "listen", frames
# sent to "send" reach the switch.
#
# Streams: udp, icmp, tcp (SYN), arp or raw frames over IPv4 or IPv6, with a VLAN
# tag, PCP and DSCP, fixed / stepped / IMIX sizes, bursts, and fields that change
# per frame (MACs, addresses, VLAN, ports: incrementing or random). Multicast
# streams reach the test ports that joined the group (they send IGMP/MLD reports).
# Each frame carries a signature: stream, run, sequence and send time, so the
# receiver counts loss, out-of-order and duplicate frames, latency and jitter.
import json, os, random, socket, struct, sys, time, select

d = sys.argv[1]
cfg_path, st_path = os.path.join(d, "traffic.json"), os.path.join(d, "traffic-stats.json")
MAGIC = b"TWTG"
BC = b"\xff" * 6
IMIX = [64] * 7 + [594] * 4 + [1518]


def mac_b(m):
    return bytes(int(x, 16) for x in m.split(":"))


def is6(a):
    return ":" in (a or "")


def ip_b(a):
    if not a:
        return b"\0\0\0\0"
    a = a.split("/")[0]
    return socket.inet_pton(socket.AF_INET6, a) if is6(a) else socket.inet_aton(a)


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


def ipv4(src, dst, proto, payload, tos=0, ttl=64, opts=b""):
    hl = 20 + len(opts)
    h = struct.pack("!BBHHHBBH4s4s", 0x40 | (hl // 4), tos, hl + len(payload), 0, 0x4000, ttl, proto, 0, src, dst) + opts
    h = h[:10] + struct.pack("!H", csum(h)) + h[12:]
    return h + payload


def ipv6(src, dst, nh, payload, tc=0, hop=64):
    return struct.pack("!IHBB16s16s", (6 << 28) | (tc << 20), len(payload), nh, hop, src, dst) + payload


def l4sum(src, dst, proto, p):
    if len(src) == 16:
        ph = src + dst + struct.pack("!IxxxB", len(p), proto)
    else:
        ph = src + dst + struct.pack("!BBH", 0, proto, len(p))
    return csum(ph + p)


def mcast_mac(ip):
    if len(ip) == 16:
        return b"\x33\x33" + ip[12:]
    return b"\x01\x00\x5e" + bytes([ip[1] & 0x7F]) + ip[2:]


def is_mcast(ip):
    return (len(ip) == 4 and 224 <= ip[0] <= 239) or (len(ip) == 16 and ip[0] == 0xFF)


def arp(op, smac, sip, tmac, tip):
    return struct.pack("!HHBBH6s4s6s4s", 1, 0x0800, 6, 4, op, smac, sip, tmac, tip)


class Tester:
    def __init__(s, t):
        s.id, s.listen = t["id"], t["listen"]
        s.to = ("127.0.0.1", t["send"])
        s.sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        s.sock.setsockopt(socket.SOL_SOCKET, socket.SO_RCVBUF, 4 << 20)
        s.sock.bind(("127.0.0.1", t["listen"]))
        s.sock.setblocking(False)
        s.arp = {}  # ip bytes -> mac bytes
        s.last_arp, s.last_join = 0, 0
        s.set(t)

    def set(s, t):
        s.mac, s.ip, s.vlan = mac_b(t["mac"]), ip_b(t.get("ip", "")), t.get("vlan", 0)
        s.ip6 = ip_b(t["ip6"]) if t.get("ip6") else None
        s.gw = ip_b(t.get("gw", "")) if t.get("gw") else None
        s.groups = [ip_b(g) for g in t.get("groups") or []]
        s.last_join = 0

    def send(s, frame):
        if len(frame) < 60:
            frame += b"\0" * (60 - len(frame))
        try:
            s.sock.sendto(frame, s.to)
        except OSError:
            pass

    def ask(s, ip):  # ARP request
        s.send(l2(BC, s.mac, s.vlan, 0, 0x0806) + arp(1, s.mac, s.ip, b"\0" * 6, ip))

    def join(s):  # IGMPv2 / MLDv1 reports for the groups it is in
        for g in s.groups:
            if len(g) == 4:
                p = struct.pack("!BBH4s", 0x16, 0, 0, g)
                p = p[:2] + struct.pack("!H", csum(p)) + p[4:]
                s.send(l2(mcast_mac(g), s.mac, s.vlan, 0, 0x0800) + ipv4(s.ip, g, 2, p, 0xC0, 1, b"\x94\x04\x00\x00"))
            elif s.ip6:
                p = struct.pack("!BBHHH16s", 131, 0, 0, 0, 0, g)
                p = p[:2] + struct.pack("!H", l4sum(s.ip6, g, 58, p)) + p[4:]
                hbh = struct.pack("!BB", 58, 0) + b"\x05\x02\x00\x00\x01\x00"  # router alert, PadN
                s.send(l2(mcast_mac(g), s.mac, s.vlan, 0, 0x86DD) + ipv6(s.ip6, g, 0, hbh + p, 0, 1))


def bump(b, n):  # an address or MAC plus n
    v = (int.from_bytes(b, "big") + n) % (1 << (8 * len(b)))
    return v.to_bytes(len(b), "big")


class Stream:
    def __init__(s, x, idx, testers, run):
        s.x, s.idx, s.run = x, idx, run
        s.tp = testers.get(x["from"])
        s.pps = max(1, x.get("pps", 100))
        s.size = max(60, min(9000, x.get("size", 128)))
        s.size_max = max(s.size, min(9000, x.get("sizeMax", 0) or s.size))
        s.count = x.get("count") or int(s.pps * max(1, x.get("seconds", 10)))
        s.burst = max(1, x.get("burst", 1))
        s.seq, s.start, s.gw_wait = 0, None, 0
        s.dst = mac_b(x["dstMac"]) if x.get("dstMac") else None
        s.mods = x.get("mods") or []
        s.rnd = random.Random(idx * 7919 + run)

    def val(s, m):  # how far this frame's field moves (0 .. count-1, times step)
        n = max(1, m.get("count", 1))
        k = s.rnd.randrange(n) if m.get("mode") == "random" else s.seq % n
        return k * m.get("step", 1)

    def frame_size(s):
        m = s.x.get("sizeMode", "")
        if m == "imix":
            return IMIX[s.seq % len(IMIX)]
        if m == "inc":
            return s.size + s.seq % (s.size_max - s.size + 1)
        if m == "random":
            return s.rnd.randint(s.size, s.size_max)
        return s.size

    def frame(s, now):
        x, tp = s.x, s.tp
        sig = MAGIC + struct.pack("!HIId", s.idx, s.run, s.seq, now)
        k = x.get("kind", "udp")
        src_ip = ip_b(x["srcIp"]) if x.get("srcIp") else tp.ip
        dst_ip = ip_b(x.get("dstIp") or "")
        if len(dst_ip) == 16 and len(src_ip) != 16:
            src_ip = tp.ip6 or b"\0" * 16
        src_mac, dst = tp.mac, s.dst or BC
        if s.dst is None and is_mcast(dst_ip):
            dst = mcast_mac(dst_ip)
        vlan, prio, sport, dport = x.get("vlan", 0), x.get("prio", 0), 49152 + s.idx, x.get("dport", 5001 if k != "tcp" else 80)
        for m in s.mods:
            v, f = s.val(m), m.get("field")
            if f == "srcMac":
                src_mac = bump(src_mac, v)
            elif f == "dstMac":
                dst = bump(dst, v)
            elif f == "srcIp":
                src_ip = bump(src_ip, v)
            elif f == "dstIp":
                dst_ip = bump(dst_ip, v)
                if is_mcast(dst_ip) and s.dst is None:
                    dst = mcast_mac(dst_ip)
            elif f == "vlan" and vlan:
                vlan = (vlan - 1 + v) % 4094 + 1
            elif f == "sport":
                sport = (sport + v) & 0xFFFF
            elif f == "dport":
                dport = (dport + v) & 0xFFFF
        size, tos = s.frame_size(), x.get("dscp", 0) << 2
        if k == "arp":
            f = l2(BC if not s.dst else s.dst, src_mac, vlan, prio, 0x0806) + arp(1, src_mac, src_ip, b"\0" * 6, dst_ip) + sig
        elif k == "raw":
            f = l2(dst, src_mac, vlan, prio, 0x88B5) + sig
        else:
            v6 = len(dst_ip) == 16
            hl = (18 if vlan else 14) + (40 if v6 else 20) + (20 if k == "tcp" else 8)
            data = sig + b"\0" * max(0, size - hl - len(sig))
            if k == "icmp":
                p = struct.pack("!BBHHH", 128 if v6 else 8, 0, 0, 0x7477, s.seq & 0xFFFF) + data
                p = p[:2] + struct.pack("!H", l4sum(src_ip, dst_ip, 58, p) if v6 else csum(p)) + p[4:]
                proto = 58 if v6 else 1
            elif k == "tcp":
                p = struct.pack("!HHIIBBHHH", sport, dport, s.seq, 0, 5 << 4, 0x02, 65535, 0, 0) + data
                p = p[:16] + struct.pack("!H", l4sum(src_ip, dst_ip, 6, p)) + p[18:]
                proto = 6
            else:
                p = struct.pack("!HHHH", sport, dport, 8 + len(data), 0) + data
                p = p[:6] + struct.pack("!H", l4sum(src_ip, dst_ip, 17, p) or 0xFFFF) + p[8:]
                proto = 17
            if v6:
                f = l2(dst, src_mac, vlan, prio, 0x86DD) + ipv6(src_ip, dst_ip, proto, p, tos)
            else:
                f = l2(dst, src_mac, vlan, prio, 0x0800) + ipv4(src_ip, dst_ip, proto, p, tos)
        if len(f) < size and k in ("raw", "arp"):
            f += b"\0" * (size - len(f))
        return f


cfg_m, cfg = 0, {}
testers, streams, run = {}, [], 0
tx, rx, other, lat, err, ooo, dup, jit, seen = {}, {}, {}, {}, {}, {}, {}, {}, {}
last_st = 0


def reset():
    global tx, rx, other, lat, err, ooo, dup, jit, seen
    tx, rx, other, lat, err, ooo, dup, jit, seen = {}, {}, {}, {}, {}, {}, {}, {}, {}


def load():
    global cfg, testers, streams, run
    try:
        c = json.load(open(cfg_path))
    except Exception:
        return
    want = {t["id"]: t for t in c.get("testers", [])}
    for tid in list(testers):
        if tid not in want or want[tid]["listen"] != testers[tid].listen:
            testers[tid].sock.close()
            del testers[tid]
    for tid, t in want.items():
        if tid not in testers:
            try:
                testers[tid] = Tester(t)
            except OSError as e:
                err["port " + tid] = str(e)
        else:
            testers[tid].set(t)
    cfg = c
    if not c.get("run"):  # stopped: no more sending, the counts stay
        streams = []
    elif c["run"] != run:
        run = c["run"]
        reset()
        streams = [Stream(x, i, testers, run) for i, x in enumerate(c.get("streams", [])) if x.get("on") and x["from"] in testers]


def got(sid, tp, seq, ts):
    """A frame of a stream arrived at a test port: counts, order, latency, jitter."""
    rx[sid] = rx.get(sid, 0) + 1
    k = (sid, tp.id)
    st = seen.get(k)
    if st is None:
        st = seen[k] = [-1, set(), None]  # highest sequence, recent ones, last latency
    hi, recent = st[0], st[1]
    if seq in recent:
        dup[sid] = dup.get(sid, 0) + 1
        rx[sid] -= 1
        return
    recent.add(seq)
    if len(recent) > 8192:
        st[1] = recent = {q for q in recent if q > hi - 4096}
    if seq < hi:
        ooo[sid] = ooo.get(sid, 0) + 1
    else:
        st[0] = seq
    if ts > 0:
        dl = (time.time() - ts) * 1000
        l = lat.setdefault(sid, [0.0, 0.0, 0, 1e9])
        l[0] += dl
        l[1] = max(l[1], dl)
        l[2] += 1
        l[3] = min(l[3], dl)
        if st[2] is not None:
            j = jit.setdefault(sid, [0.0, 0])
            j[0] += abs(dl - st[2])
            j[1] += 1
        st[2] = dl


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
                dst = ip_b(x.get("dstIp") or "")
                if is_mcast(dst) and not x.get("mods") and dst not in tp.groups:
                    other[sid] = other.get(sid, 0) + 1  # flooded to a port that did not join
                else:
                    got(sid, tp, seq, ts if x["from"] in testers else 0)
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
    elif et == 0x0800 and len(b) >= 24 and b[9] == 2:  # an IGMP query: report the groups again
        ihl = (b[0] & 15) * 4
        if len(b) > ihl and b[ihl] == 0x11:
            tp.last_join = 0
    elif et == 0x86DD and len(b) >= 48 and b[6] == 58 and tp.ip6:
        t, src, dst = b[40], b[8:24], b[24:40]
        if t == 135 and len(b) >= 64 and b[48:64] == tp.ip6:  # neighbour solicitation for its address: advertise
            p = struct.pack("!BBHI16s", 136, 0, 0, 0x60000000, tp.ip6) + b"\x02\x01" + tp.mac
            p = p[:2] + struct.pack("!H", l4sum(tp.ip6, src, 58, p)) + p[4:]
            tp.send(l2(f[6:12], tp.mac, vlan, 0, 0x86DD) + ipv6(tp.ip6, src, 58, p, 0, 255))
        elif t == 128 and dst == tp.ip6:  # echo request: reply
            p = bytearray(b[40:])
            p[0], p[2:4] = 129, b"\0\0"
            p[2:4] = struct.pack("!H", l4sum(tp.ip6, src, 58, bytes(p)))
            tp.send(l2(f[6:12], tp.mac, vlan, 0, 0x86DD) + ipv6(tp.ip6, src, 58, bytes(p)))
        elif t == 130:  # MLD query
            tp.last_join = 0


while True:
    try:
        m = os.stat(cfg_path).st_mtime
    except OSError:
        break
    if m != cfg_m:
        cfg_m = m
        load()
    if not cfg.get("testers"):
        break
    now = time.time()
    for t in testers.values():
        if t.groups and now - t.last_join > 10:
            t.last_join = now
            t.join()
    socks = [t.sock for t in testers.values()]
    busy = any(s.tp is not None and s.seq < s.count for s in streams)
    r, _, _ = select.select(socks, [], [], 0.001 if busy else 0.05) if socks else ([], [], [])
    for t in testers.values():
        if t.sock in r:
            for _ in range(4000):
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
        if s.burst > 1:  # whole bursts, as often as the rate allows
            due = min(s.count, ((due + s.burst - 1) // s.burst) * s.burst)
        n = 0
        while s.seq < due and n < 4000:
            s.tp.send(s.frame(time.time()))
            s.seq += 1
            n += 1
        tx[x["id"]] = s.seq
    if now - last_st > 0.5:
        last_st = now
        st = {"run": run, "at": now, "tx": tx, "rx": rx, "other": other, "lat": lat, "err": err, "ooo": ooo, "dup": dup, "jit": jit,
              "busy": {s.x["id"]: s.seq < s.count for s in streams}, "testers": sorted(testers)}
        try:
            with open(st_path + ".tmp", "w") as fh:
                json.dump(st, fh)
            os.replace(st_path + ".tmp", st_path)
        except OSError:
            pass
