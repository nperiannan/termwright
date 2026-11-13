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
# Test ports can take their address by DHCP (and IPv6 by SLAAC from router
# advertisements), announce themselves with gratuitous ARP and LLDP, and answer
# ARP, neighbour solicitations and ping.
# Each frame carries a signature: stream, run, sequence, send time and flow, so the
# receiver counts loss, out-of-order and duplicate frames, latency (a histogram for
# its percentiles) and jitter, per stream and per flow (the values of its changing
# fields), and the VLAN, priority and DSCP the frames arrived with. A round-trip
# stream's frames are sent back by the test port they reach: its latency is the
# round trip, measured on one clock (so also between test ports on two hosts).
# A pcap stream replays the frames of a capture (pcap/<id>.pcap, written by Termwright in
# classic little-endian pcap) at a rate or with the capture's own timing; the receiving
# test ports count its frames by their contents (from the EtherType on, any VLAN tags
# left out), so they are counted on switched paths, not routed ones.
import bisect, json, math, os, random, socket, struct, sys, time, select

d = sys.argv[1]
cfg_path, st_path = os.path.join(d, "traffic.json"), os.path.join(d, "traffic-stats.json")
MAGIC = b"TWTG"
SIG = "!HIIdH"  # stream, run, sequence, send time, flow
SIGN = struct.calcsize(SIG)
MAXF = 1024  # flows counted one by one (a stream with more is counted as a whole)
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


def ip_s(b):
    return socket.inet_ntop(socket.AF_INET6 if len(b) == 16 else socket.AF_INET, b)


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


def eui64(mac):
    return bytes([mac[0] ^ 2]) + mac[1:3] + b"\xff\xfe" + mac[3:6]


def prefix_len(mask):
    return bin(int.from_bytes(mask, "big")).count("1")


class Tester:
    def __init__(s, t):
        s.id, s.listen = t["id"], t["listen"]
        s.xid = random.randrange(1 << 32)
        s.dhcp_state, s.dhcp_at, s.lease, s.server, s.offer = "", 0, None, None, None
        s.rs_at, s.lldp_at, s.garp_at = 0, 0, 0
        s.to = ("127.0.0.1", t["send"])
        s.sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        s.sock.setsockopt(socket.SOL_SOCKET, socket.SO_RCVBUF, 4 << 20)
        s.sock.bind(("127.0.0.1", t["listen"]))
        s.sock.setblocking(False)
        s.arp = {}  # ip bytes -> mac bytes
        s.last_arp, s.last_join = 0, 0
        s.set(t)

    def set(s, t):
        s.name = t.get("name", t["id"])
        s.mac, s.vlan = mac_b(t["mac"]), t.get("vlan", 0)
        s.dhcp, s.slaac, s.lldp = t.get("dhcp", False), t.get("slaac", False), t.get("lldp", False)
        if not s.dhcp or not s.lease:
            s.ip = ip_b(t.get("ip", ""))
            s.gw = ip_b(t.get("gw", "")) if t.get("gw") else None
        if not s.slaac or not getattr(s, "ip6", None):
            s.ip6 = ip_b(t["ip6"]) if t.get("ip6") else None
        s.ll = b"\xfe\x80" + b"\0" * 6 + eui64(s.mac)  # its IPv6 link-local address
        s.groups = [ip_b(g) for g in t.get("groups") or []]
        s.last_join, s.garp_at = 0, 0

    def send(s, frame):
        if len(frame) < 60:
            frame += b"\0" * (60 - len(frame))
        try:
            s.sock.sendto(frame, s.to)
        except OSError:
            pass

    def ask(s, ip):  # ARP request
        s.send(l2(BC, s.mac, s.vlan, 0, 0x0806) + arp(1, s.mac, s.ip, b"\0" * 6, ip))

    def garp(s):  # gratuitous ARP: the switch learns where it is
        if s.ip != b"\0\0\0\0":
            s.send(l2(BC, s.mac, s.vlan, 0, 0x0806) + arp(1, s.mac, s.ip, b"\0" * 6, s.ip))

    def dhcp_send(s, kind):  # DISCOVER (1) or REQUEST (3), broadcast
        opts = bytes([53, 1, kind, 61, 7, 1]) + s.mac + bytes([55, 3, 1, 3, 6, 12, len(s.name[:30])]) + s.name[:30].encode()
        if kind == 3 and s.offer:
            opts += bytes([50, 4]) + s.offer[0] + bytes([54, 4]) + s.offer[1]
        bootp = struct.pack("!BBBBIHH4s4s4s4s16s64s128s4s", 1, 1, 6, 0, s.xid, 0, 0x8000, b"\0" * 4, b"\0" * 4, b"\0" * 4, b"\0" * 4, s.mac + b"\0" * 10, b"\0" * 64, b"\0" * 128, b"\x63\x82\x53\x63") + opts + b"\xff"
        p = struct.pack("!HHHH", 68, 67, 8 + len(bootp), 0) + bootp
        src, dst = b"\0" * 4, b"\xff" * 4
        p = p[:6] + struct.pack("!H", l4sum(src, dst, 17, p) or 0xFFFF) + p[8:]
        s.send(l2(BC, s.mac, s.vlan, 0, 0x0800) + ipv4(src, dst, 17, p, 0, 64))

    def rs(s):  # a router solicitation, for SLAAC
        p = struct.pack("!BBHI", 133, 0, 0, 0) + b"\x01\x01" + s.mac
        dst = socket.inet_pton(socket.AF_INET6, "ff02::2")
        p = p[:2] + struct.pack("!H", l4sum(s.ll, dst, 58, p)) + p[4:]
        s.send(l2(b"\x33\x33\x00\x00\x00\x02", s.mac, s.vlan, 0, 0x86DD) + ipv6(s.ll, dst, 58, p, 0, 255))

    def lldp_send(s):  # LLDP: the switch sees "Termwright <name>" on its port
        def tlv(t, v):
            return struct.pack("!H", (t << 9) | len(v)) + v
        name = ("Termwright " + s.name).encode()[:200]
        f = tlv(1, b"\x04" + s.mac) + tlv(2, b"\x07" + s.name.encode()[:200]) + tlv(3, struct.pack("!H", 120)) + tlv(4, b"test port") + tlv(5, name)
        if s.ip != b"\0\0\0\0":
            f += tlv(8, bytes([5, 1]) + s.ip + b"\x02\x00\x00\x00\x00\x00")
        s.send(b"\x01\x80\xc2\x00\x00\x0e" + s.mac + b"\x88\xcc" + f + tlv(0, b""))

    def tick(s, now):  # its own protocols: DHCP, SLAAC, gratuitous ARP, LLDP
        if s.dhcp:
            if s.dhcp_state in ("", "discover") and now - s.dhcp_at > 4:
                s.dhcp_state, s.dhcp_at = "discover", now
                s.dhcp_send(1)
            elif s.dhcp_state == "request" and now - s.dhcp_at > 4:
                s.dhcp_state, s.dhcp_at = "discover", now
                s.dhcp_send(1)
            elif s.dhcp_state == "bound" and now > s.lease_end:
                s.dhcp_state, s.dhcp_at = "request", now
                s.dhcp_send(3)
        if s.slaac and not s.ip6 and now - s.rs_at > 8:
            s.rs_at = now
            s.rs()
        if s.lldp and now - s.lldp_at > 30:
            s.lldp_at = now
            s.lldp_send()
        if s.garp_at == 0 and s.ip != b"\0\0\0\0":
            s.garp_at = now
            s.garp()

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
        s.nflows = math.prod(max(1, m.get("count", 1)) for m in s.mods) if s.mods else 0
        s.flow, s.ftx, s.flab = 0, {}, {}
        s.pkts, s.offs, s.timing, s.dur = [], [], False, 0
        if x.get("kind") == "pcap":
            s.pkts, s.offs = read_pcap(os.path.join(d, "pcap", x.get("pcapId", "") + ".pcap"))
            n = len(s.pkts)
            if n == 0:
                err[x["id"]] = "the capture is not on this host, or has no frames"
            s.timing = bool(x.get("pcapTiming")) and n > 1
            s.dur = s.offs[-1] + s.offs[-1] / (n - 1) if n > 1 else 0
            if not x.get("count") and not (x.get("seconds") and not s.timing):
                s.count = n * max(1, x.get("loops", 1))
            if n == 0:
                s.count = 0

    def k(s, m):  # which of its values this frame's field takes (0 .. count-1)
        n = max(1, m.get("count", 1))
        return s.rnd.randrange(n) if m.get("mode") == "random" else s.seq % n

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
        if s.pkts:  # a capture's frame, as it was (or from this test port to the one it goes to)
            f = s.pkts[s.seq % len(s.pkts)]
            if x.get("rewriteMacs"):
                f = (s.dst or f[:6]) + tp.mac + f[12:]
            return f
        k = x.get("kind", "udp")
        src_ip = ip_b(x["srcIp"]) if x.get("srcIp") else tp.ip
        dst_ip = ip_b(x.get("dstIp") or "")
        if dst_ip == b"\0\0\0\0" and x.get("to") in testers:  # an address it took by DHCP
            dst_ip = testers[x["to"]].ip
        if len(dst_ip) == 16 and len(src_ip) != 16:
            src_ip = tp.ip6 or b"\0" * 16
        src_mac, dst = tp.mac, s.dst or BC
        if s.dst is None and is_mcast(dst_ip):
            dst = mcast_mac(dst_ip)
        vlan, prio, sport, dport = x.get("vlan", 0), x.get("prio", 0), 49152 + s.idx, x.get("dport", 5001 if k != "tcp" else 80)
        flow, mult, lab = 0, 1, []
        for m in s.mods:
            kk, f = s.k(m), m.get("field")
            v = kk * m.get("step", 1)
            flow, mult = flow + kk * mult, mult * max(1, m.get("count", 1))
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
        if 0 < s.nflows <= MAXF:
            s.ftx[flow] = s.ftx.get(flow, 0) + 1
            if flow not in s.flab:
                show = {"srcMac": lambda: src_mac.hex(":"), "dstMac": lambda: dst.hex(":"), "srcIp": lambda: ip_s(src_ip), "dstIp": lambda: ip_s(dst_ip),
                        "vlan": lambda: str(vlan), "sport": lambda: str(sport), "dport": lambda: str(dport)}
                s.flab[flow] = " ".join(m.get("field", "") + " " + show.get(m.get("field"), lambda: "?")() for m in s.mods)
        else:
            flow = 0xFFFF
        sig = MAGIC + struct.pack(SIG, s.idx, s.run, s.seq, now, flow)
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
hist, frx, qos = {}, {}, {}  # latency histogram, per-flow receive counters, what the frames arrived with
ftx, flab = {}, {}  # per flow: frames sent, its field values
last_st = 0


def reset():
    global tx, rx, other, lat, err, ooo, dup, jit, seen, hist, frx, qos
    tx, rx, other, lat, err, ooo, dup, jit, seen = {}, {}, {}, {}, {}, {}, {}, {}, {}
    hist, frx, qos = {}, {}, {}
    ftx.clear()
    flab.clear()


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
    phash.clear()
    for x in c.get("streams", []):
        if x.get("kind") == "pcap" and x.get("on") and x.get("pcapId"):
            for f in read_pcap(os.path.join(d, "pcap", x["pcapId"] + ".pcap"))[0]:
                phash[l3key(f)] = x
    if not c.get("run"):  # stopped: no more sending, the counts stay
        streams = []
    elif c["run"] != run:
        run = c["run"]
        reset()
        streams = [Stream(x, i, testers, run) for i, x in enumerate(c.get("streams", [])) if x.get("on") and x["from"] in testers]


def latency(sid, flow, ts, st):
    """A frame's latency (ms, from its send time): average, min, max, its histogram bucket (a quarter of an
    octave of microseconds, for the percentiles), jitter, and per flow."""
    dl = (time.time() - ts) * 1000
    l = lat.setdefault(sid, [0.0, 0.0, 0, 1e9])
    l[0] += dl
    l[1] = max(l[1], dl)
    l[2] += 1
    l[3] = min(l[3], dl)
    b = str(min(120, max(0, int(4 * math.log2(max(dl * 1000, 1))))))
    h = hist.setdefault(sid, {})
    h[b] = h.get(b, 0) + 1
    if flow != 0xFFFF:
        fr = frx.setdefault(sid, {}).setdefault(flow, [0, 0.0, 0.0, 0])
        fr[1] += dl
        fr[2] = max(fr[2], dl)
        fr[3] += 1
    if st[2] is not None:
        j = jit.setdefault(sid, [0.0, 0])
        j[0] += abs(dl - st[2])
        j[1] += 1
    st[2] = dl


def got(sid, tp, seq, ts, flow=0xFFFF, mark=""):
    """A frame of a stream arrived at a test port: counts, order, latency, jitter, per flow, how it was marked."""
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
    if mark:
        q = qos.setdefault(sid, {})
        q[mark] = q.get(mark, 0) + 1
    if flow != 0xFFFF:
        frx.setdefault(sid, {}).setdefault(flow, [0, 0.0, 0.0, 0])[0] += 1
    if ts > 0:
        latency(sid, flow, ts, st)


PCAPS = {}  # path -> (frames, offsets in seconds from the first)


def read_pcap(path):
    """The frames of a classic pcap file and when each was captured (seconds after the first)."""
    if path in PCAPS:
        return PCAPS[path]
    frames, offs = [], []
    try:
        b = open(path, "rb").read()
    except OSError:
        b = b""
    if len(b) >= 24:
        m = b[:4]
        bo = "<" if m in (b"\xd4\xc3\xb2\xa1", b"\x4d\x3c\xb2\xa1") else ">"
        div = 1e9 if m in (b"\x4d\x3c\xb2\xa1", b"\xa1\xb2\x3c\x4d") else 1e6
        i, t0 = 24, None
        while i + 16 <= len(b):
            sec, sub, incl, _ = struct.unpack(bo + "IIII", b[i:i + 16])
            data = b[i + 16:i + 16 + incl]
            i += 16 + incl
            if len(data) < 14:
                continue
            t = sec + sub / div
            t0 = t if t0 is None else t0
            frames.append(data + b"\0" * max(0, 60 - len(data)))
            offs.append(max(0.0, t - t0))
    PCAPS[path] = (frames, offs)
    return frames, offs


def l3key(f):
    """A frame's contents from its EtherType on (VLAN tags left out, padding too): how a replayed frame is known."""
    o = 12
    while f[o:o + 2] in (b"\x81\x00", b"\x88\xa8", b"\x91\x00"):
        o += 4
    return hash(f[o:].rstrip(b"\0"))


phash = {}  # pcap streams' frames: l3key -> stream


def reflect(tp, f, o, et):
    """A round-trip stream's frame goes back where it came from: MACs, addresses and ports swapped (the
    checksums stay right: their sums do not change)."""
    b = bytearray(f[6:12] + tp.mac + f[12:])
    i = o + 2
    if et == 0x0800 and len(b) >= i + 20:
        ihl = (b[i] & 15) * 4
        b[i + 12:i + 16], b[i + 16:i + 20] = b[i + 16:i + 20], b[i + 12:i + 16]
        l4, proto = i + ihl, b[i + 9]
    elif et == 0x86DD and len(b) >= i + 40:
        b[i + 8:i + 24], b[i + 24:i + 40] = b[i + 24:i + 40], b[i + 8:i + 24]
        l4, proto = i + 40, b[i + 6]
    else:
        l4, proto = 0, 0
    if proto in (6, 17) and len(b) >= l4 + 4:
        b[l4:l4 + 2], b[l4 + 2:l4 + 4] = b[l4 + 2:l4 + 4], b[l4:l4 + 2]
    tp.send(bytes(b))


def handle(tp, f):
    o = 12
    vlan = prio = 0
    if f[o:o + 2] == b"\x81\x00":
        tci = struct.unpack("!H", f[o + 2:o + 4])[0]
        vlan, prio = tci & 0xFFF, tci >> 13
        o += 4
    et = struct.unpack("!H", f[o:o + 2])[0]
    b = f[o + 2:]
    i = f.find(MAGIC)
    if i >= 0 and len(f) >= i + 4 + SIGN:
        idx, r, seq, ts, flow = struct.unpack(SIG, f[i + 4:i + 4 + SIGN])
        if r == run and idx < len(cfg.get("streams", [])):
            x = cfg["streams"][idx]
            sid = x["id"]
            echo = x["from"] == tp.id and not x.get("to") and x.get("kind") == "icmp"  # the address answered
            if x.get("rtt") and x["from"] == tp.id and x.get("to"):  # its own frame, sent back: the round trip
                k = (sid, "rtt")
                latency(sid, flow, ts, seen.setdefault(k, [-1, set(), None]))
            elif x.get("to") and x["to"] != tp.id:
                other[sid] = other.get(sid, 0) + 1
            elif x["from"] != tp.id or echo:
                dst = ip_b(x.get("dstIp") or "")
                if is_mcast(dst) and not x.get("mods") and dst not in tp.groups:
                    other[sid] = other.get(sid, 0) + 1  # flooded to a port that did not join
                else:
                    dscp = b[1] >> 2 if et == 0x0800 and len(b) > 1 else ((b[0] & 15) << 4 | b[1] >> 4) >> 2 if et == 0x86DD and len(b) > 1 else -1
                    rtt = x.get("rtt") and x.get("to") == tp.id
                    got(sid, tp, seq, ts if x["from"] in testers and not rtt else 0, flow, "%d/%d/%d" % (vlan, prio, dscp))
                    if rtt:
                        reflect(tp, f, o, et)
        return
    if phash:
        x = phash.get(l3key(f))
        if x is not None and x["from"] != tp.id:
            sid = x["id"]
            if x.get("to") and x["to"] != tp.id:
                other[sid] = other.get(sid, 0) + 1
            else:
                rx[sid] = rx.get(sid, 0) + 1
                dscp = b[1] >> 2 if et == 0x0800 and len(b) > 1 else ((b[0] & 15) << 4 | b[1] >> 4) >> 2 if et == 0x86DD and len(b) > 1 else -1
                q = qos.setdefault(sid, {})
                mk = "%d/%d/%d" % (vlan, prio, dscp)
                q[mk] = q.get(mk, 0) + 1
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
    elif et == 0x0800 and len(b) >= 28 + 240 and b[9] == 17 and tp.dhcp:  # a DHCP reply for it?
        ihl = (b[0] & 15) * 4
        u = b[ihl:]
        if struct.unpack("!H", u[2:4])[0] == 68 and len(u) >= 8 + 240:
            bp = u[8:]
            op, xid, yi = bp[0], struct.unpack("!I", bp[4:8])[0], bp[16:20]
            if op == 2 and xid == tp.xid and bp[236:240] == b"\x63\x82\x53\x63":
                o, i2 = {}, 240
                while i2 < len(bp) and bp[i2] != 255:
                    if bp[i2] == 0:
                        i2 += 1
                        continue
                    if i2 + 1 >= len(bp):
                        break
                    o[bp[i2]] = bp[i2 + 2:i2 + 2 + bp[i2 + 1]]
                    i2 += 2 + bp[i2 + 1]
                kind = o.get(53, b"\0")[0]
                if kind == 2 and tp.dhcp_state == "discover":  # OFFER: ask for it
                    tp.offer = (yi, o.get(54, b"\0" * 4))
                    tp.dhcp_state, tp.dhcp_at = "request", time.time()
                    tp.dhcp_send(3)
                elif kind == 5:  # ACK: it is ours
                    mask = o.get(1, b"\xff\xff\xff\0")
                    tp.ip, tp.dhcp_state = yi, "bound"
                    if o.get(3):
                        tp.gw = o[3][:4]
                    lt = struct.unpack("!I", o[51])[0] if len(o.get(51, b"")) == 4 else 3600
                    tp.lease_end = time.time() + max(30, lt // 2)
                    tp.lease = {"ip": socket.inet_ntoa(yi) + "/" + str(prefix_len(mask)), "gw": socket.inet_ntoa(tp.gw) if tp.gw else "", "server": socket.inet_ntoa(o.get(54, b"\0" * 4))}
                    tp.garp_at = 0
                elif kind == 6:  # NAK: start again
                    tp.dhcp_state, tp.dhcp_at, tp.offer = "", 0, None
    elif et == 0x0800 and len(b) >= 24 and b[9] == 2:  # an IGMP query: report the groups again
        ihl = (b[0] & 15) * 4
        if len(b) > ihl and b[ihl] == 0x11:
            tp.last_join = 0
    elif et == 0x86DD and len(b) >= 48 and b[6] == 58 and (tp.ip6 or tp.slaac):
        t, src, dst = b[40], b[8:24], b[24:40]
        if t == 134 and tp.slaac and not tp.ip6:  # a router advertisement: an address from its prefix
            i2 = 56
            while i2 + 8 <= len(b):
                ot, ol = b[i2], b[i2 + 1] * 8
                if ol == 0:
                    break
                if ot == 3 and ol == 32 and b[i2 + 3] & 0x40 and b[i2 + 2] == 64:  # prefix information, autonomous, /64
                    tp.ip6 = b[i2 + 16:i2 + 24] + eui64(tp.mac)
                    tp.lease6 = socket.inet_ntop(socket.AF_INET6, tp.ip6) + "/64"
                    tp.gw6 = socket.inet_ntop(socket.AF_INET6, src)
                i2 += ol
        elif t == 135 and len(b) >= 64 and (b[48:64] == tp.ip6 or b[48:64] == tp.ll):  # neighbour solicitation for its address: advertise
            me = b[48:64]
            p = struct.pack("!BBHI16s", 136, 0, 0, 0x60000000, me) + b"\x02\x01" + tp.mac
            p = p[:2] + struct.pack("!H", l4sum(me, src, 58, p)) + p[4:]
            tp.send(l2(f[6:12], tp.mac, vlan, 0, 0x86DD) + ipv6(me, src, 58, p, 0, 255))
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
        t.tick(now)
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
        if s.dst is None and x.get("routed") and x.get("kind") not in ("arp", "pcap"):
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
        if s.timing:  # the capture's own gaps, over and over
            el = now - s.start
            lp = int(el // s.dur)
            due = min(s.count, lp * len(s.pkts) + bisect.bisect_right(s.offs, el - lp * s.dur))
        else:
            due = min(s.count, int((now - s.start) * s.pps) + 1)
        if s.burst > 1:  # whole bursts, as often as the rate allows
            due = min(s.count, ((due + s.burst - 1) // s.burst) * s.burst)
        n = 0
        while s.seq < due and n < 4000:
            s.tp.send(s.frame(time.time()))
            s.seq += 1
            n += 1
        tx[x["id"]] = s.seq
        if s.ftx:
            ftx[x["id"]], flab[x["id"]] = s.ftx, s.flab
    if now - last_st > 0.5:
        last_st = now
        st = {"run": run, "at": now, "tx": tx, "rx": rx, "other": other, "lat": lat, "err": err, "ooo": ooo, "dup": dup, "jit": jit,
              "busy": {s.x["id"]: s.seq < s.count for s in streams}, "testers": sorted(testers),
              "hist": hist, "frx": frx, "qos": qos, "ftx": ftx, "flab": flab,
              "lease": {t.id: t.lease for t in testers.values() if t.lease}, "lease6": {t.id: {"ip": t.lease6, "gw": t.gw6} for t in testers.values() if getattr(t, "lease6", None)}}
        try:
            with open(st_path + ".tmp", "w") as fh:
                json.dump(st, fh)
            os.replace(st_path + ".tmp", st_path)
        except OSError:
            pass
